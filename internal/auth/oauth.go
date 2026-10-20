package auth

import (
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/dbuchs7/wikiwikibangbang/internal/budget"
)

// OAuth for AI assistants (ADR-0068): wikiwikibangbang is the authorization
// server for its own MCP endpoint. An application (a client) is registered
// by an administrator, or registers itself when that is open; a person
// approves it once; it then holds short-lived access tokens that act as
// that person, for the MCP endpoint only, and a refresh token that turns
// over with every use. Clients and approvals are journaled; codes and
// access tokens live in memory.

// OAuth lifetimes.
const (
	OAuthCodeLifetime    = time.Minute
	OAuthAccessLifetime  = time.Hour
	OAuthRefreshLifetime = 30 * 24 * time.Hour
)

// Prefixes of the OAuth secrets, so secret scanners know them.
const (
	OAuthAccessPrefix  = "wwbba_"
	OAuthRefreshPrefix = "wwbbr_"
	OAuthSecretPrefix  = "wwbbc_"
)

// Client authentication at the token endpoint.
const (
	AuthClientSecret = "client_secret_post" // or client_secret_basic, the same secret
	AuthNone         = "none"               // a public client: PKCE alone
)

// OAuthClient is a registered application.
type OAuthClient struct {
	ID        string
	Name      string
	Redirects []string
	// Self is true for a client that registered itself; the consent screen
	// says so, for nobody vetted it.
	Self bool
	// Method is how it authenticates at the token endpoint.
	Method  string
	Created time.Time
}

// Grant is a person's approval of a client.
type Grant struct {
	User     UserID
	Client   string
	Created  time.Time
	LastUsed time.Time
}

// Errors of the OAuth flow, named as RFC 6749 names them.
var (
	ErrInvalidClient = errors.New("invalid_client")
	ErrInvalidGrant  = errors.New("invalid_grant")
)

type oauthRecord struct {
	Client    string   `json:"client"`
	Name      string   `json:"name,omitempty"`
	Redirects []string `json:"redirects,omitempty"`
	Self      bool     `json:"self,omitempty"`
	Method    string   `json:"method,omitempty"`
	// Resource is what a grant's tokens are for.
	Resource string `json:"resource,omitempty"`
}

const (
	oauthClientCreated = "OAuthClientCreated"
	oauthClientRemoved = "OAuthClientRemoved"
	oauthGranted       = "OAuthGranted"
	oauthRefreshed     = "OAuthRefreshed"
	oauthRevoked       = "OAuthRevoked"
)

type oauthClient struct {
	OAuthClient
	secretHash string
	approved   bool // some person approved it once
}

type oauthGrant struct {
	Grant
	refreshHash string
	// usedHash is the refresh token turned over last; presented again, it
	// shows a copy.
	usedHash string
	target   string
	expires  time.Time
}

type oauthCode struct {
	user              UserID
	client, redirect  string
	challenge, target string
	expires           time.Time
}

type oauthToken struct {
	user    UserID
	client  string
	target  string
	expires time.Time
}

// oauthState is the OAuth part of Accounts; callers hold a.mu.
type oauthState struct {
	clients map[string]*oauthClient
	grants  map[string]*oauthGrant // user + "\x00" + client
	codes   map[string]*oauthCode  // by hash
	access  map[string]*oauthToken // by hash
}

func grantKey(u UserID, client string) string { return string(u) + "\x00" + client }

func (a *Accounts) oauth() *oauthState {
	if a.oa == nil {
		a.oa = &oauthState{clients: map[string]*oauthClient{}, grants: map[string]*oauthGrant{},
			codes: map[string]*oauthCode{}, access: map[string]*oauthToken{}}
	}
	return a.oa
}

// applyOAuth applies an OAuth event; it runs live and on load.
func (a *Accounts) applyOAuth(e event) error {
	o, s := e.OAuth, a.oauth()
	if o == nil {
		return fmt.Errorf("%s without its client", e.Kind)
	}
	switch e.Kind {
	case oauthClientCreated:
		s.clients[o.Client] = &oauthClient{OAuthClient: OAuthClient{ID: o.Client, Name: o.Name, Redirects: o.Redirects, Self: o.Self, Method: o.Method, Created: e.At}, secretHash: e.Hash}
	case oauthClientRemoved:
		delete(s.clients, o.Client)
		for k, g := range s.grants {
			if g.Client == o.Client {
				delete(s.grants, k)
			}
		}
		for k, t := range s.access {
			if t.client == o.Client {
				delete(s.access, k)
			}
		}
	case oauthGranted, oauthRefreshed:
		if c, ok := s.clients[o.Client]; ok {
			c.approved = true
		}
		g, ok := s.grants[grantKey(e.User, o.Client)]
		if !ok {
			g = &oauthGrant{Grant: Grant{User: e.User, Client: o.Client, Created: e.At}}
			s.grants[grantKey(e.User, o.Client)] = g
		}
		if e.Kind == oauthRefreshed {
			g.usedHash = g.refreshHash
		}
		g.refreshHash, g.LastUsed, g.expires, g.target = e.Hash, e.At, e.At.Add(OAuthRefreshLifetime), o.Resource
	case oauthRevoked:
		delete(s.grants, grantKey(e.User, o.Client))
		for k, t := range s.access {
			if t.client == o.Client && t.user == e.User {
				delete(s.access, k)
			}
		}
	}
	return nil
}

// ValidRedirect accepts an https address, or http on the local machine
// for an application running there; no fragment.
func ValidRedirect(s string) bool {
	u, err := url.Parse(s)
	if err != nil || u.Fragment != "" || u.Host == "" || len(s) > 2048 {
		return false
	}
	host := u.Hostname()
	return u.Scheme == "https" || (u.Scheme == "http" && (host == "localhost" || host == "127.0.0.1" || host == "::1"))
}

func validClient(name string, redirects []string) error {
	if strings.TrimSpace(name) == "" || utf8.RuneCountInString(name) > 80 || strings.ContainsAny(name, "\r\n<>") {
		return fmt.Errorf("%w: an application has a name of 1 to 80 characters", ErrInvalid)
	}
	if len(redirects) == 0 || len(redirects) > 10 {
		return fmt.Errorf("%w: an application has 1 to 10 redirect addresses", ErrInvalid)
	}
	for _, r := range redirects {
		if !ValidRedirect(r) {
			return fmt.Errorf("%w: %q is no https redirect address", ErrInvalid, r)
		}
	}
	return nil
}

// CreateOAuthClient registers an application, as an administrator does or,
// with self set, as the application does itself. The secret is returned
// once; a public client (AuthNone) has none. Self-registered clients are
// bounded: at the budget, the oldest one nobody approved goes, and when
// all are approved the registration is refused.
func (a *Accounts) CreateOAuthClient(name string, redirects []string, method string, self bool) (OAuthClient, string, error) {
	name = strings.TrimSpace(name)
	if err := validClient(name, redirects); err != nil {
		return OAuthClient{}, "", err
	}
	if method == "" {
		method = AuthClientSecret
	}
	if method != AuthClientSecret && method != AuthNone {
		return OAuthClient{}, "", fmt.Errorf("%w: token_endpoint_auth_method is client_secret_post, client_secret_basic or none", ErrInvalid)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	s := a.oauth()
	if self {
		n, oldest := 0, ""
		var at time.Time
		for id, c := range s.clients {
			if !c.Self {
				continue
			}
			n++
			if !c.approved && (oldest == "" || c.Created.Before(at)) {
				oldest, at = id, c.Created
			}
		}
		if n >= budget.Get().OAuthClients {
			if oldest == "" {
				return OAuthClient{}, "", ErrFull
			}
			if err := a.record(event{Kind: oauthClientRemoved, OAuth: &oauthRecord{Client: oldest}}); err != nil {
				return OAuthClient{}, "", err
			}
		}
	}
	id, err := a.newID(12)
	if err != nil {
		return OAuthClient{}, "", err
	}
	id = "wwbb-" + id
	secret, hash := "", ""
	if method == AuthClientSecret {
		raw, err := a.newID(32)
		if err != nil {
			return OAuthClient{}, "", err
		}
		secret = OAuthSecretPrefix + raw
		hash = hashSecret(secret)
	}
	err = a.record(event{Kind: oauthClientCreated, Hash: hash, OAuth: &oauthRecord{Client: id, Name: name, Redirects: slices.Clone(redirects), Self: self, Method: method}})
	if err != nil {
		return OAuthClient{}, "", err
	}
	return s.clients[id].OAuthClient, secret, nil
}

// OAuthClients lists the applications, oldest first.
func (a *Accounts) OAuthClients() []OAuthClient {
	a.mu.Lock()
	defer a.mu.Unlock()
	var out []OAuthClient
	for _, c := range a.oauth().clients {
		out = append(out, c.OAuthClient)
	}
	slices.SortFunc(out, func(x, y OAuthClient) int { return x.Created.Compare(y.Created) })
	return out
}

// OAuthClientByID returns an application.
func (a *Accounts) OAuthClientByID(id string) (OAuthClient, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	c, ok := a.oauth().clients[id]
	if !ok {
		return OAuthClient{}, false
	}
	return c.OAuthClient, true
}

// RemoveOAuthClient removes an application with every approval of it and
// every token it holds.
func (a *Accounts) RemoveOAuthClient(id string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, ok := a.oauth().clients[id]; !ok {
		return ErrNotFound
	}
	return a.record(event{Kind: oauthClientRemoved, OAuth: &oauthRecord{Client: id}})
}

// authenticate checks a client's credential; callers hold a.mu.
func (a *Accounts) authenticate(id, secret string) (*oauthClient, error) {
	c, ok := a.oauth().clients[id]
	if !ok {
		return nil, ErrInvalidClient
	}
	if c.Method == AuthNone {
		return c, nil
	}
	if secret == "" || !equalStrings(c.secretHash, hashSecret(secret)) {
		return nil, ErrInvalidClient
	}
	return c, nil
}

// CheckAuthorization checks an authorization request's client and redirect
// address, before anything is shown to the person.
func (a *Accounts) CheckAuthorization(clientID, redirect string) (OAuthClient, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	c, ok := a.oauth().clients[clientID]
	if !ok {
		return OAuthClient{}, ErrInvalidClient
	}
	if !slices.Contains(c.Redirects, redirect) {
		return OAuthClient{}, fmt.Errorf("%w: the redirect address is not registered for this application", ErrInvalid)
	}
	return c.OAuthClient, nil
}

// IssueCode records a person's approval in the form of a one-time code
// bound to the client, the redirect address, the PKCE challenge (S256)
// and the resource the token is for.
func (a *Accounts) IssueCode(user UserID, clientID, redirect, challenge, resource string) (string, error) {
	if len(challenge) < 43 || len(challenge) > 128 {
		return "", fmt.Errorf("%w: a code_challenge (S256) is required", ErrInvalid)
	}
	if _, err := a.CheckAuthorization(clientID, redirect); err != nil {
		return "", err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, ok := a.users[user]; !ok {
		return "", ErrNotFound
	}
	s, now := a.oauth(), a.now()
	for k, c := range s.codes {
		if !now.Before(c.expires) {
			delete(s.codes, k)
		}
	}
	if len(s.codes) >= budget.Get().SSOPending {
		return "", ErrFull
	}
	raw, err := a.newID(32)
	if err != nil {
		return "", err
	}
	s.codes[hashSecret(raw)] = &oauthCode{user: user, client: clientID, redirect: redirect, challenge: challenge, target: resource, expires: now.Add(OAuthCodeLifetime)}
	return raw, nil
}

// OAuthTokens is what the token endpoint answers.
type OAuthTokens struct {
	Access, Refresh string
	ExpiresIn       time.Duration
}

// ExchangeCode turns a code into tokens: the client authenticates, the
// redirect address and resource are the code's, and the verifier matches
// the challenge. A code is used once.
func (a *Accounts) ExchangeCode(clientID, secret, code, redirect, verifier, resource string) (OAuthTokens, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	c, err := a.authenticate(clientID, secret)
	if err != nil {
		return OAuthTokens{}, err
	}
	s := a.oauth()
	h := hashSecret(code)
	k, ok := s.codes[h]
	delete(s.codes, h)
	if !ok || !a.now().Before(k.expires) || k.client != c.ID || k.redirect != redirect || (resource != "" && resource != k.target) {
		return OAuthTokens{}, ErrInvalidGrant
	}
	sum := sha256.Sum256([]byte(verifier))
	if verifier == "" || base64.RawURLEncoding.EncodeToString(sum[:]) != k.challenge {
		return OAuthTokens{}, ErrInvalidGrant
	}
	return a.issueTokens(k.user, c.ID, k.target, oauthGranted)
}

// Refresh turns a refresh token over: the old one stops, a new pair
// comes. A refresh token presented again after it was turned over means it
// was copied, and ends the approval (RFC 9700).
func (a *Accounts) Refresh(clientID, secret, refresh string) (OAuthTokens, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	c, err := a.authenticate(clientID, secret)
	if err != nil {
		return OAuthTokens{}, err
	}
	s := a.oauth()
	h := hashSecret(refresh)
	for _, g := range s.grants {
		if g.Client != c.ID {
			continue
		}
		if equalStrings(g.refreshHash, h) {
			if !a.now().Before(g.expires) {
				return OAuthTokens{}, ErrInvalidGrant
			}
			return a.issueTokens(g.User, c.ID, g.target, oauthRefreshed)
		}
		if g.usedHash != "" && equalStrings(g.usedHash, h) {
			if err := a.record(event{Kind: oauthRevoked, User: g.User, OAuth: &oauthRecord{Client: c.ID}}); err != nil {
				return OAuthTokens{}, err
			}
			return OAuthTokens{}, ErrInvalidGrant
		}
	}
	return OAuthTokens{}, ErrInvalidGrant
}

// ThrottleOAuth runs a token request from addr: refused while the address
// is throttled, and a failed client or grant counts against it, as a failed
// login does (ADR-0013).
func (a *Accounts) ThrottleOAuth(addr string, run func() (OAuthTokens, error)) (OAuthTokens, error) {
	now, key := a.now(), "oauth:"+addr
	a.mu.Lock()
	wait := a.throttle.blocked(now, key)
	a.mu.Unlock()
	if wait > 0 {
		return OAuthTokens{}, &ThrottledError{Wait: wait}
	}
	t, err := run()
	if errors.Is(err, ErrInvalidClient) || errors.Is(err, ErrInvalidGrant) {
		a.mu.Lock()
		a.throttle.fail(now, key)
		a.mu.Unlock()
	}
	return t, err
}

// issueTokens records the grant with a new refresh token and makes an
// access token; callers hold a.mu.
func (a *Accounts) issueTokens(user UserID, client, target, kind string) (OAuthTokens, error) {
	rawA, err := a.newID(32)
	if err != nil {
		return OAuthTokens{}, err
	}
	rawR, err := a.newID(32)
	if err != nil {
		return OAuthTokens{}, err
	}
	access, refresh := OAuthAccessPrefix+rawA, OAuthRefreshPrefix+rawR
	if err := a.record(event{Kind: kind, User: user, Hash: hashSecret(refresh), OAuth: &oauthRecord{Client: client, Resource: target}}); err != nil {
		return OAuthTokens{}, err
	}
	a.oauth().access[hashSecret(access)] = &oauthToken{user: user, client: client, target: target, expires: a.now().Add(OAuthAccessLifetime)}
	return OAuthTokens{Access: access, Refresh: refresh, ExpiresIn: OAuthAccessLifetime}, nil
}

// OAuthPrincipal resolves an access token to the person it acts for, with
// the resource it was issued for.
func (a *Accounts) OAuthPrincipal(token string) (Principal, string, bool) {
	if !strings.HasPrefix(token, OAuthAccessPrefix) {
		return Principal{}, "", false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	h := hashSecret(token)
	t, ok := a.oauth().access[h]
	if !ok {
		return Principal{}, "", false
	}
	if !a.now().Before(t.expires) {
		delete(a.oauth().access, h)
		return Principal{}, "", false
	}
	if _, ok := a.users[t.user]; !ok {
		return Principal{}, "", false
	}
	return a.principal(t.user, ScopeFull, "oauth", ""), t.target, true
}

// Grants lists a person's approvals, newest first.
func (a *Accounts) Grants(user UserID) []Grant {
	a.mu.Lock()
	defer a.mu.Unlock()
	var out []Grant
	for _, g := range a.oauth().grants {
		if g.User == user {
			out = append(out, g.Grant)
		}
	}
	slices.SortFunc(out, func(x, y Grant) int { return y.Created.Compare(x.Created) })
	return out
}

// RevokeGrant withdraws a person's approval of a client and ends its
// tokens.
func (a *Accounts) RevokeGrant(user UserID, client string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, ok := a.oauth().grants[grantKey(user, client)]; !ok {
		return ErrNotFound
	}
	return a.record(event{Kind: oauthRevoked, User: user, OAuth: &oauthRecord{Client: client}})
}
