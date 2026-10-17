package sso

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

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
	old := maxPending
	maxPending = 50
	t.Cleanup(func() { maxPending = old })
	p, _ := newProvider(t)
	for range maxPending {
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
