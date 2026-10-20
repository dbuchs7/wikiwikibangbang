package auth

import (
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strings"
	"testing"

	"github.com/dbuchs7/wikiwikibangbang/internal/budget"
)

const (
	verifier = "a-pkce-verifier-of-at-least-forty-three-characters-xyz"
	resource = "https://wiki.example.org/mcp"
	callback = "https://claude.ai/api/mcp/auth_callback"
)

func challengeOf(v string) string {
	sum := sha256.Sum256([]byte(v))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func TestOAuthFlow(t *testing.T) {
	a, j, c := newAccounts(t)
	u, err := a.CreateUser("Olga", pw)
	noErr(t, err)
	cl, secret, err := a.CreateOAuthClient("Claude", []string{callback}, "", false)
	noErr(t, err)
	if !strings.HasPrefix(secret, OAuthSecretPrefix) || cl.Self || cl.Method != AuthClientSecret {
		t.Fatalf("client %+v %q", cl, secret)
	}
	if _, err := a.CheckAuthorization(cl.ID, "https://evil.example/cb"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("an unregistered redirect: %v", err)
	}
	if _, err := a.IssueCode(u.ID, cl.ID, callback, "short", resource); !errors.Is(err, ErrInvalid) {
		t.Fatalf("no challenge: %v", err)
	}
	code, err := a.IssueCode(u.ID, cl.ID, callback, challengeOf(verifier), resource)
	noErr(t, err)
	for name, try := range map[string]func() error{
		"wrong secret": func() error { _, err := a.ExchangeCode(cl.ID, "wrong", code, callback, verifier, resource); return err },
		"wrong verifier": func() error {
			_, err := a.ExchangeCode(cl.ID, secret, code, callback, "other-verifier", resource)
			return err
		},
	} {
		if err := try(); err == nil {
			t.Fatalf("%s: accepted", name)
		}
	}
	// The wrong verifier used the code up.
	code, err = a.IssueCode(u.ID, cl.ID, callback, challengeOf(verifier), resource)
	noErr(t, err)
	if _, err := a.ExchangeCode(cl.ID, secret, code, callback, verifier, "https://other.example/mcp"); !errors.Is(err, ErrInvalidGrant) {
		t.Fatalf("another resource: %v", err)
	}
	code, err = a.IssueCode(u.ID, cl.ID, callback, challengeOf(verifier), resource)
	noErr(t, err)
	tok, err := a.ExchangeCode(cl.ID, secret, code, callback, verifier, resource)
	noErr(t, err)
	if _, err := a.ExchangeCode(cl.ID, secret, code, callback, verifier, resource); !errors.Is(err, ErrInvalidGrant) {
		t.Fatalf("a code twice: %v", err)
	}
	p, target, ok := a.OAuthPrincipal(tok.Access)
	if !ok || p.User != u.ID || p.Via != "oauth" || target != resource {
		t.Fatalf("principal %+v %q %v", p, target, ok)
	}
	if _, ok := a.TokenPrincipal(tok.Access); ok {
		t.Fatal("an OAuth token is no API token")
	}
	if g := a.Grants(u.ID); len(g) != 1 || g[0].Client != cl.ID {
		t.Fatalf("grants %+v", g)
	}

	// Access tokens expire; the refresh token turns over.
	c.t = c.t.Add(OAuthAccessLifetime)
	if _, _, ok := a.OAuthPrincipal(tok.Access); ok {
		t.Fatal("an expired access token")
	}
	next, err := a.Refresh(cl.ID, secret, tok.Refresh)
	noErr(t, err)
	if _, target, ok := a.OAuthPrincipal(next.Access); !ok || target != resource {
		t.Fatal("the refreshed token")
	}

	// The approval and the newest refresh token survive a restart.
	b, err := New(j.records, j.append, WithClock(c.now), WithIterations(1000))
	noErr(t, err)
	if _, err := b.Refresh(cl.ID, secret, next.Refresh); err != nil {
		t.Fatalf("refresh after a restart: %v", err)
	}

	// Presenting a turned-over refresh token again ends the approval.
	again, err := a.Refresh(cl.ID, secret, next.Refresh)
	noErr(t, err)
	if _, err := a.Refresh(cl.ID, secret, next.Refresh); !errors.Is(err, ErrInvalidGrant) {
		t.Fatalf("reuse: %v", err)
	}
	if len(a.Grants(u.ID)) != 0 {
		t.Fatal("reuse ends the approval")
	}
	if _, _, ok := a.OAuthPrincipal(again.Access); ok {
		t.Fatal("and its tokens")
	}
}

func TestOAuthRevokeAndRemove(t *testing.T) {
	a, _, _ := newAccounts(t)
	u, err := a.CreateUser("Paul", pw)
	noErr(t, err)
	cl, secret, err := a.CreateOAuthClient("Claude", []string{callback}, "", false)
	noErr(t, err)
	login := func() OAuthTokens {
		code, err := a.IssueCode(u.ID, cl.ID, callback, challengeOf(verifier), resource)
		noErr(t, err)
		tok, err := a.ExchangeCode(cl.ID, secret, code, callback, verifier, resource)
		noErr(t, err)
		return tok
	}
	tok := login()
	noErr(t, a.RevokeGrant(u.ID, cl.ID))
	if _, _, ok := a.OAuthPrincipal(tok.Access); ok {
		t.Fatal("a withdrawn approval ends its tokens")
	}
	if _, err := a.Refresh(cl.ID, secret, tok.Refresh); !errors.Is(err, ErrInvalidGrant) {
		t.Fatal("and its refresh token")
	}
	if err := a.RevokeGrant(u.ID, cl.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("twice")
	}
	tok = login()
	noErr(t, a.RemoveOAuthClient(cl.ID))
	if _, _, ok := a.OAuthPrincipal(tok.Access); ok || len(a.Grants(u.ID)) != 0 || len(a.OAuthClients()) != 0 {
		t.Fatal("a removed application keeps nothing")
	}
}

func TestOAuthClientsAreChecked(t *testing.T) {
	a, _, _ := newAccounts(t)
	for name, redirects := range map[string][]string{
		"none":     nil,
		"http":     {"http://example.org/cb"},
		"fragment": {"https://example.org/cb#x"},
	} {
		if _, _, err := a.CreateOAuthClient("App", redirects, "", false); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, _, err := a.CreateOAuthClient("App", []string{"http://localhost:3000/cb"}, "", false); err != nil {
		t.Errorf("a local application: %v", err)
	}
	if _, _, err := a.CreateOAuthClient("App", []string{callback}, "private_key_jwt", false); !errors.Is(err, ErrInvalid) {
		t.Error("an unsupported method")
	}
	pub, secret, err := a.CreateOAuthClient("Public", []string{callback}, AuthNone, true)
	if err != nil || secret != "" || !pub.Self {
		t.Fatalf("a public client: %+v %v", pub, err)
	}
}

func TestSelfRegistrationIsBounded(t *testing.T) {
	a, _, c := newAccounts(t)
	u, err := a.CreateUser("Quinn", pw)
	noErr(t, err)
	b := budget.Default()
	b.OAuthClients = 2
	budget.Set(b)
	defer budget.Set(budget.Default())
	first, s1, err := a.CreateOAuthClient("One", []string{callback}, "", true)
	noErr(t, err)
	code, err := a.IssueCode(u.ID, first.ID, callback, challengeOf(verifier), resource)
	noErr(t, err)
	_, err = a.ExchangeCode(first.ID, s1, code, callback, verifier, resource)
	noErr(t, err) // approved: never evicted
	c.t = c.t.Add(1)
	second, _, err := a.CreateOAuthClient("Two", []string{callback}, "", true)
	noErr(t, err)
	c.t = c.t.Add(1)
	if _, _, err := a.CreateOAuthClient("Three", []string{callback}, "", true); err != nil {
		t.Fatalf("eviction: %v", err)
	}
	if _, ok := a.OAuthClientByID(second.ID); ok {
		t.Fatal("the oldest unapproved one goes")
	}
	if _, ok := a.OAuthClientByID(first.ID); !ok {
		t.Fatal("an approved one stays")
	}
	// Administrators' registrations do not count.
	if _, _, err := a.CreateOAuthClient("Admin", []string{callback}, "", false); err != nil {
		t.Fatal(err)
	}
}

func TestOAuthEdges(t *testing.T) {
	a, _, c := newAccounts(t)
	u, err := a.CreateUser("Rita", pw)
	noErr(t, err)
	cl, secret, err := a.CreateOAuthClient("Claude", []string{callback}, "", false)
	noErr(t, err)
	pub, _, err := a.CreateOAuthClient("Public", []string{callback}, AuthNone, false)
	noErr(t, err)
	if got := a.OAuthClients(); len(got) != 2 || got[0].ID != cl.ID {
		t.Fatalf("clients, oldest first: %+v", got)
	}
	if _, err := a.IssueCode("nobody", cl.ID, callback, challengeOf(verifier), resource); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown user: %v", err)
	}
	if _, err := a.IssueCode(u.ID, "wwbb-none", callback, challengeOf(verifier), resource); !errors.Is(err, ErrInvalidClient) {
		t.Fatalf("unknown client: %v", err)
	}
	// A public client needs no secret; PKCE alone binds the code.
	code, err := a.IssueCode(u.ID, pub.ID, callback, challengeOf(verifier), resource)
	noErr(t, err)
	tok, err := a.ExchangeCode(pub.ID, "", code, callback, verifier, "")
	noErr(t, err)
	if _, err := a.Refresh(cl.ID, secret, tok.Refresh); !errors.Is(err, ErrInvalidGrant) {
		t.Fatalf("another client's refresh token: %v", err)
	}
	if _, err := a.Refresh(cl.ID, "", tok.Refresh); !errors.Is(err, ErrInvalidClient) {
		t.Fatalf("no secret: %v", err)
	}
	// An expired code and an expired refresh token are refused.
	code, err = a.IssueCode(u.ID, cl.ID, callback, challengeOf(verifier), resource)
	noErr(t, err)
	c.t = c.t.Add(OAuthCodeLifetime)
	if _, err := a.ExchangeCode(cl.ID, secret, code, callback, verifier, resource); !errors.Is(err, ErrInvalidGrant) {
		t.Fatalf("expired code: %v", err)
	}
	c.t = c.t.Add(OAuthRefreshLifetime)
	if _, err := a.Refresh(pub.ID, "", tok.Refresh); !errors.Is(err, ErrInvalidGrant) {
		t.Fatalf("expired refresh token: %v", err)
	}
	if _, _, ok := a.OAuthPrincipal(tok.Access); ok {
		t.Fatal("an expired access token still counts")
	}
	if err := a.RevokeGrant(u.ID, "wwbb-none"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("revoke unknown: %v", err)
	}
	if err := a.RemoveOAuthClient("wwbb-none"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("remove unknown: %v", err)
	}
}

func TestOAuthTokenRequestsAreThrottled(t *testing.T) {
	a, _, _ := newAccounts(t)
	fail := func() (OAuthTokens, error) { return OAuthTokens{}, ErrInvalidClient }
	for range MaxFailures {
		if _, err := a.ThrottleOAuth("203.0.113.9", fail); !errors.Is(err, ErrInvalidClient) {
			t.Fatalf("before the throttle: %v", err)
		}
	}
	if _, err := a.ThrottleOAuth("203.0.113.9", fail); !errors.Is(err, ErrThrottled) {
		t.Fatalf("after %d failures: %v", MaxFailures, err)
	}
	if _, err := a.ThrottleOAuth("203.0.113.10", func() (OAuthTokens, error) { return OAuthTokens{Access: "x"}, nil }); err != nil {
		t.Fatalf("another address: %v", err)
	}
}
