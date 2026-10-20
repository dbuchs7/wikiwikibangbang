package sso

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/dbuchs7/wikiwikibangbang/internal/budget"
	"github.com/dbuchs7/wikiwikibangbang/internal/sso/ssotest"
)

func newProvider(t *testing.T) (*Provider, *ssotest.IdP) {
	t.Helper()
	idp := ssotest.New(t, "wiki")
	p, err := New(Config{Issuer: idp.URL, ClientID: "wiki", ClientSecret: "s", RedirectURL: "http://localhost:8080/api/oidc/callback"}, idp.Client())
	if err != nil {
		t.Fatal(err)
	}
	return p, idp
}

func TestLogin(t *testing.T) {
	p, idp := newProvider(t)
	ctx := context.Background()
	authURL, state, err := p.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	cb := idp.Authorize(t, authURL, ssotest.Claims{Subject: "s-1", PreferredUsername: "anna", Email: "anna@example.org"})
	if cb.Get("state") != state {
		t.Fatal("state not passed through")
	}
	id, err := p.Finish(ctx, state, cb.Get("code"))
	if err != nil {
		t.Fatal(err)
	}
	if id.Subject != "s-1" || id.Issuer != idp.URL || id.Username != "anna" || !id.EmailVerified {
		t.Fatalf("identity %+v", id)
	}
	if _, err := p.Finish(ctx, state, cb.Get("code")); !errors.Is(err, ErrUnknownState) {
		t.Fatalf("a state was usable twice: %v", err)
	}
	if p.Name() != "single sign-on" || p.Issuer() != idp.URL {
		t.Fatal("defaults")
	}
}

func TestForgedOrStaleLoginsAreRefused(t *testing.T) {
	cases := map[string]ssotest.Claims{
		"other audience": {Subject: "s", Audience: "someone-else"},
		"other issuer":   {Subject: "s", Issuer: "https://evil.example"},
		"expired":        {Subject: "s", Expires: time.Now().Add(-time.Hour)},
		"wrong nonce":    {Subject: "s", Nonce: "replayed"},
	}
	for name, c := range cases {
		p, idp := newProvider(t)
		authURL, state, err := p.Begin(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		cb := idp.Authorize(t, authURL, c)
		if _, err := p.Finish(context.Background(), state, cb.Get("code")); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}

	p, idp := newProvider(t)
	authURL, state, _ := p.Begin(context.Background())
	cb := idp.Authorize(t, authURL, ssotest.Claims{Subject: "s"})
	if _, err := p.Finish(context.Background(), "forged", cb.Get("code")); !errors.Is(err, ErrUnknownState) {
		t.Errorf("forged state: %v", err)
	}
	if _, err := p.Finish(context.Background(), state, "wrong-code"); err == nil {
		t.Error("wrong code accepted")
	}

	p, idp = newProvider(t)
	authURL, state, _ = p.Begin(context.Background())
	cb = idp.Authorize(t, authURL, ssotest.Claims{Subject: "s"})
	p.now = func() time.Time { return time.Now().Add(FlowLifetime) }
	if _, err := p.Finish(context.Background(), state, cb.Get("code")); !errors.Is(err, ErrUnknownState) {
		t.Errorf("expired flow: %v", err)
	}
}

func TestUsernameFallsBackToEmail(t *testing.T) {
	p, idp := newProvider(t)
	authURL, state, _ := p.Begin(context.Background())
	cb := idp.Authorize(t, authURL, ssotest.Claims{Subject: "s", Email: "x@example.org"})
	id, err := p.Finish(context.Background(), state, cb.Get("code"))
	if err != nil || id.Username != "x@example.org" {
		t.Fatalf("%+v %v", id, err)
	}
}

func TestConfiguration(t *testing.T) {
	bad := []Config{
		{ClientID: "c", RedirectURL: "https://w/cb"},
		{Issuer: "https://i", RedirectURL: "https://w/cb"},
		{Issuer: "https://i", ClientID: "c"},
		{Issuer: "http://idp.example", ClientID: "c", RedirectURL: "https://w/cb"},
		{Issuer: "https://i", ClientID: "c", RedirectURL: "http://wiki.example/cb"},
		{Issuer: "::", ClientID: "c", RedirectURL: "https://w/cb"},
	}
	for _, c := range bad {
		if _, err := New(c, nil); err == nil {
			t.Errorf("%+v accepted", c)
		}
	}
	p, err := New(Config{Issuer: "http://127.0.0.1:1", ClientID: "c", RedirectURL: "http://localhost/cb", Name: "Corp"}, nil)
	if err != nil || p.Name() != "Corp" {
		t.Fatal(err)
	}
	if _, _, err := p.Begin(context.Background()); !errors.Is(err, ErrNotReady) || !strings.Contains(err.Error(), "cannot be reached") {
		t.Fatalf("unreachable provider: %v", err)
	}
}

func TestPendingLoginsAreBounded(t *testing.T) {
	small := budget.Default()
	small.SSOPending = 50
	budget.Set(small)
	t.Cleanup(func() { budget.Set(budget.Default()) })
	p, _ := newProvider(t)
	for range small.SSOPending {
		if _, _, err := p.Begin(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := p.Begin(context.Background()); err == nil {
		t.Fatal("no bound on unfinished logins")
	}
	p.now = func() time.Time { return time.Now().Add(FlowLifetime) }
	if _, _, err := p.Begin(context.Background()); err != nil {
		t.Fatalf("expired logins were not cleared: %v", err)
	}
}

// TestDomains: with domains set, only an identity whose domain the
// provider asserts signs in (ADR-0066).
func TestDomains(t *testing.T) {
	idp := ssotest.New(t, "wiki")
	login := func(cfg Config, c ssotest.Claims) error {
		t.Helper()
		cfg.Issuer, cfg.ClientID, cfg.RedirectURL = idp.URL, "wiki", "http://localhost/api/oidc/callback"
		p, err := New(cfg, idp.Client())
		if err != nil {
			t.Fatal(err)
		}
		authURL, state, err := p.Begin(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		cb := idp.Authorize(t, authURL, c)
		_, err = p.Finish(context.Background(), state, cb.Get("code"))
		return err
	}
	google := Config{ID: "google", Kind: "google", Domains: []string{" Example.ORG "}}
	for name, tc := range map[string]struct {
		cfg  Config
		c    ssotest.Claims
		want error
	}{
		"hosted domain":           {google, ssotest.Claims{Subject: "1", Email: "a@example.org", HostedDomain: "example.org"}, nil},
		"verified address":        {google, ssotest.Claims{Subject: "2", Email: "a@EXAMPLE.org"}, nil},
		"other domain":            {google, ssotest.Claims{Subject: "3", Email: "a@gmail.com"}, ErrDomain},
		"unverified address":      {google, ssotest.Claims{Subject: "4", Email: "a@example.org", EmailUnverified: true}, ErrDomain},
		"username not trusted":    {google, ssotest.Claims{Subject: "5", PreferredUsername: "a@example.org"}, ErrDomain},
		"entra principal name":    {Config{ID: "entra", Domains: []string{"example.org"}, UsernameIsVerified: true}, ssotest.Claims{Subject: "6", PreferredUsername: "a@example.org"}, nil},
		"no domains: any account": {Config{}, ssotest.Claims{Subject: "7", Email: "a@gmail.com"}, nil},
	} {
		if err := login(tc.cfg, tc.c); !errors.Is(err, tc.want) {
			t.Errorf("%s: %v, want %v", name, err, tc.want)
		}
	}
	p, _ := New(Config{Issuer: idp.URL, ClientID: "wiki", RedirectURL: "http://localhost/cb", ID: "google", Kind: "google", Domains: []string{"example.org"}}, nil)
	if p.ID() != "google" || p.Kind() != "google" || p.Domains()[0] != "example.org" {
		t.Fatal("accessors")
	}
	if d, _ := New(Config{Issuer: idp.URL, ClientID: "wiki", RedirectURL: "http://localhost/cb"}, nil); d.ID() != "sso" {
		t.Fatal("the default ID")
	}
}
