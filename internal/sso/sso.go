// Package sso is the OpenID Connect relying party (ADR-0013, ADR-0030): the
// authorization code flow with PKCE, state and nonce, against one
// configured provider. It answers who the provider says the user is; what
// that user may do is decided in the product, never by provider claims.
package sso

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

// Config names the provider and this application's registration there.
type Config struct {
	Issuer       string
	ClientID     string
	ClientSecret string
	// RedirectURL is this server's callback as registered at the provider,
	// e.g. https://wiki.example.org/api/oidc/callback.
	RedirectURL string
	// Name is shown on the login button, e.g. "Microsoft Entra ID".
	Name string
}

// Identity is what the provider asserts about the user.
type Identity struct {
	Issuer        string
	Subject       string
	Username      string // preferred_username, else the e-mail address
	Email         string
	EmailVerified bool
}

// FlowLifetime bounds how long a started login may take.
const FlowLifetime = 10 * time.Minute

// maxPending bounds the memory unfinished logins can use; a variable only so
// the test of the bound need not start ten thousand logins.
var maxPending = 10_000

var (
	ErrUnknownState = errors.New("sso: unknown or expired login")
	ErrNotReady     = errors.New("sso: the identity provider cannot be reached")
)

// Provider is safe for concurrent use.
type Provider struct {
	cfg    Config
	client *http.Client
	now    func() time.Time

	mu       sync.Mutex
	verifier *oidc.IDTokenVerifier
	oauth    *oauth2.Config
	pending  map[string]flow
}

type flow struct {
	nonce, verifier string
	expires         time.Time
}

// New checks the configuration. The provider's discovery document is
// fetched on the first login, so the server starts while the provider is
// down. client may be nil.
func New(cfg Config, client *http.Client) (*Provider, error) {
	for name, v := range map[string]string{"issuer": cfg.Issuer, "client ID": cfg.ClientID, "redirect URL": cfg.RedirectURL} {
		if v == "" {
			return nil, fmt.Errorf("sso: the %s is required", name)
		}
	}
	for name, v := range map[string]string{"issuer": cfg.Issuer, "redirect URL": cfg.RedirectURL} {
		u, err := url.Parse(v)
		if err != nil || u.Host == "" || (u.Scheme != "https" && !isLoopback(u.Hostname())) {
			return nil, fmt.Errorf("sso: the %s must be an https URL (http only for localhost)", name)
		}
	}
	if cfg.Name == "" {
		cfg.Name = "single sign-on"
	}
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	return &Provider{cfg: cfg, client: client, now: time.Now, pending: map[string]flow{}}, nil
}

func isLoopback(host string) bool { return host == "localhost" || host == "127.0.0.1" || host == "::1" }

// Name is the provider's display name.
func (p *Provider) Name() string { return p.cfg.Name }

// Issuer is the configured issuer.
func (p *Provider) Issuer() string { return p.cfg.Issuer }

func (p *Provider) ctx(ctx context.Context) context.Context {
	return oidc.ClientContext(ctx, p.client)
}

// ready discovers the provider once; callers hold p.mu.
func (p *Provider) ready(ctx context.Context) error {
	if p.oauth != nil {
		return nil
	}
	dctx, cancel := context.WithTimeout(p.ctx(ctx), 10*time.Second)
	defer cancel()
	prov, err := oidc.NewProvider(dctx, p.cfg.Issuer)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrNotReady, err)
	}
	p.verifier = prov.Verifier(&oidc.Config{ClientID: p.cfg.ClientID})
	p.oauth = &oauth2.Config{
		ClientID: p.cfg.ClientID, ClientSecret: p.cfg.ClientSecret, RedirectURL: p.cfg.RedirectURL,
		Endpoint: prov.Endpoint(), Scopes: []string{oidc.ScopeOpenID, "profile", "email"},
	}
	return nil
}

func random() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err) // crypto/rand does not fail on supported platforms
	}
	return hex.EncodeToString(b)
}

// Begin starts a login and returns the provider URL to send the browser to
// and the state, which the caller binds to the browser (a cookie).
func (p *Provider) Begin(ctx context.Context) (authURL, state string, err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.ready(ctx); err != nil {
		return "", "", err
	}
	now := p.now()
	for k, f := range p.pending {
		if !now.Before(f.expires) {
			delete(p.pending, k)
		}
	}
	if len(p.pending) >= maxPending {
		return "", "", fmt.Errorf("sso: too many logins in progress")
	}
	state, nonce, verifier := random(), random(), oauth2.GenerateVerifier()
	p.pending[state] = flow{nonce: nonce, verifier: verifier, expires: now.Add(FlowLifetime)}
	return p.oauth.AuthCodeURL(state, oidc.Nonce(nonce), oauth2.S256ChallengeOption(verifier)), state, nil
}

// Finish completes a login: the state must be one Begin issued and not yet
// used, the code is exchanged with the PKCE verifier, and the ID token's
// signature, issuer, audience, expiry and nonce are verified.
func (p *Provider) Finish(ctx context.Context, state, code string) (Identity, error) {
	p.mu.Lock()
	f, ok := p.pending[state]
	delete(p.pending, state) // one use only
	oauth, verifier := p.oauth, p.verifier
	p.mu.Unlock()
	if !ok || !p.now().Before(f.expires) || oauth == nil {
		return Identity{}, ErrUnknownState
	}
	tok, err := oauth.Exchange(p.ctx(ctx), code, oauth2.VerifierOption(f.verifier))
	if err != nil {
		return Identity{}, fmt.Errorf("sso: exchanging the code: %w", err)
	}
	raw, ok := tok.Extra("id_token").(string)
	if !ok || raw == "" {
		return Identity{}, errors.New("sso: the provider returned no ID token")
	}
	idt, err := verifier.Verify(p.ctx(ctx), raw)
	if err != nil {
		return Identity{}, fmt.Errorf("sso: ID token: %w", err)
	}
	if idt.Nonce != f.nonce {
		return Identity{}, errors.New("sso: ID token: nonce does not match")
	}
	var claims struct {
		PreferredUsername string `json:"preferred_username"`
		Email             string `json:"email"`
		EmailVerified     bool   `json:"email_verified"`
	}
	if err := idt.Claims(&claims); err != nil {
		return Identity{}, fmt.Errorf("sso: ID token claims: %w", err)
	}
	id := Identity{Issuer: idt.Issuer, Subject: idt.Subject, Username: claims.PreferredUsername, Email: claims.Email, EmailVerified: claims.EmailVerified}
	if id.Username == "" {
		id.Username = id.Email
	}
	return id, nil
}
