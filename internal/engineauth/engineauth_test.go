package engineauth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/dbuchs7/wikiwikibangbang/internal/engine"
	"github.com/dbuchs7/wikiwikibangbang/internal/engineauth/engineauthtest"
	"github.com/dbuchs7/wikiwikibangbang/internal/engineconn"
	"github.com/dbuchs7/wikiwikibangbang/internal/site"
)

// plainSealer keeps tokens as they are; internal/storage seals them.
type plainSealer struct{ shredded []string }

func (plainSealer) Seal(_ string, pt, _ []byte) ([]byte, error) { return pt, nil }
func (plainSealer) Open(_ string, ct, _ []byte) ([]byte, error) { return ct, nil }
func (s *plainSealer) Shred(keys ...string) error {
	s.shredded = append(s.shredded, keys...)
	return nil
}

type setup struct {
	fake   *engineauthtest.Engine
	e      site.Engine
	b      *Broker
	conns  *engineconn.Connections
	env    map[string]string
	ended  []string
	client *http.Client
}

func newSetup(t *testing.T) *setup {
	t.Helper()
	fake := engineauthtest.New(t)
	env := map[string]string{engine.ClientIDVar("atlas"): engineauthtest.ClientID, engine.ClientSecretVar("atlas"): engineauthtest.ClientSecret}
	n := 0
	conns, err := engineconn.New(nil, func([]byte) error { return nil }, &plainSealer{}, func() string { n++; return fmt.Sprint(n) })
	if err != nil {
		t.Fatal(err)
	}
	getenv := func(k string) string { return env[k] }
	client := fake.Client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	s := &setup{fake: fake, e: site.Engine{ID: "atlas", Name: "Atlas", Kind: site.EngineAtlas, BaseURL: fake.URL}, conns: conns, env: env, client: client}
	s.b = NewBroker(conns, engine.NewReader(getenv, client), client, getenv, "https://wiki.example.org/")
	s.b.OnEnded(func(user, engineID string) { s.ended = append(s.ended, user+"@"+engineID) })
	return s
}

// connect runs the browser's part: to the engine's consent, which
// approves at once, and back with the code.
func (s *setup) connect(t *testing.T, user string) (engineconn.Connection, error) {
	t.Helper()
	authURL, state, err := s.b.Begin(context.Background(), s.e, user)
	if err != nil {
		return engineconn.Connection{}, err
	}
	res, err := s.client.Get(authURL)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	back, err := url.Parse(res.Header.Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	if back.Query().Get("state") != state {
		t.Fatalf("the engine sent back state %q, want %q", back.Query().Get("state"), state)
	}
	if e := back.Query().Get("error"); e != "" {
		s.b.Cancel(state)
		return engineconn.Connection{}, errors.New(e)
	}
	return s.b.Finish(context.Background(), s.e, state, back.Query().Get("code"))
}

// TestAPersonConnectsWithTheirOwnSignIn covers ADR-0151's connection: the
// engine's consent is asked for its tasks resource, by the code flow with
// PKCE, and sends the person back to this server's address for the
// engine; the code is exchanged with the client's credentials in the form
// and the verifier; who the grant belongs to is read with it.
func TestAPersonConnectsWithTheirOwnSignIn(t *testing.T) {
	s := newSetup(t)
	if got := s.b.Callback("atlas"); got != "https://wiki.example.org/api/engines/atlas/connect/callback" {
		t.Errorf("Callback = %q", got)
	}
	c, err := s.connect(t, "bert")
	if err != nil {
		t.Fatal(err)
	}
	q := s.fake.LastAuthorize
	if q.Get("resource") != s.fake.Resource() || q.Get("code_challenge_method") != "S256" || q.Get("redirect_uri") != s.b.Callback("atlas") {
		t.Errorf("the consent was asked with %v", q)
	}
	if c.Subject != "u-17" || c.Username != "anna" || c.Name != "Anna Muster" || c.Email != "anna@example.org" || strings.Join(c.Groups, ",") != "hr,Team Lead" {
		t.Fatalf("connection %+v", c)
	}
	if got, ok := s.conns.Get("bert", "atlas"); !ok || got.Subject != "u-17" {
		t.Fatalf("kept %+v", got)
	}
	tok, err := s.b.Token(context.Background(), s.e, "bert")
	if err != nil || !strings.HasPrefix(tok, "atlasoa_") || s.fake.Refreshes != 0 {
		t.Fatalf("Token = %q, %v after %d renewals; the exchanged access token is held", tok, err, s.fake.Refreshes)
	}
	// Connecting again replaces the connection, and the grant it replaces
	// is revoked at the engine.
	if _, err := s.connect(t, "bert"); err != nil {
		t.Fatal(err)
	}
	if s.conns.Count() != 1 || s.fake.Revoked != 1 {
		t.Errorf("after connecting again: %d connections, %d grants revoked", s.conns.Count(), s.fake.Revoked)
	}
}

// TestABegunConnectionIsUsedOnce: a state works once, for its engine and
// its address, and a refused consent is forgotten.
func TestABegunConnectionIsUsedOnce(t *testing.T) {
	s := newSetup(t)
	_, state, err := s.b.Begin(context.Background(), s.e, "bert")
	if err != nil {
		t.Fatal(err)
	}
	if user, id, ok := s.b.Pending(state); !ok || user != "bert" || id != "atlas" {
		t.Fatalf("Pending = %q %q %v", user, id, ok)
	}
	moved := s.e
	moved.BaseURL = "https://elsewhere.example.org"
	if _, err := s.b.Finish(context.Background(), moved, state, "x"); !errors.Is(err, ErrUnknownState) {
		t.Errorf("a state finished for the engine at another address = %v", err)
	}
	if _, err := s.b.Finish(context.Background(), s.e, state, "x"); !errors.Is(err, ErrUnknownState) {
		t.Errorf("a state finished twice = %v", err)
	}
	s.fake.Lock()
	s.fake.Deny = true
	s.fake.Unlock()
	if _, err := s.connect(t, "bert"); err == nil || err.Error() != "access_denied" {
		t.Errorf("a refused consent = %v", err)
	}
	if _, ok := s.conns.Get("bert", "atlas"); ok {
		t.Error("a refused consent made a connection")
	}

	delete(s.env, engine.ClientSecretVar("atlas"))
	if _, _, err := s.b.Begin(context.Background(), s.e, "bert"); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("Begin without the client's secret = %v", err)
	}
	if NewBroker(s.conns, nil, s.client, func(string) string { return "x" }, "").Configured("atlas") {
		t.Error("connectable without a public address to come back to")
	}
}

// TestRenewalsRunOneAfterAnotherAndOnlyARefusalEnds: the engine replaces
// the refresh token on every use, so concurrent callers renew once; a
// busy engine leaves the connection; the engine refusing the grant ends
// it and says so.
func TestRenewalsRunOneAfterAnotherAndOnlyARefusalEnds(t *testing.T) {
	s := newSetup(t)
	if _, err := s.connect(t, "bert"); err != nil {
		t.Fatal(err)
	}
	s.fake.Lock()
	s.fake.ExpiresIn = 30 // shorter than the minute held back, so every call renews
	s.fake.Unlock()
	s.b.forget("bert", "atlas")

	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for range 8 {
		wg.Go(func() {
			_, err := s.b.Token(context.Background(), s.e, "bert")
			errs <- err
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("a concurrent renewal failed: %v", err)
		}
	}
	if _, ok := s.conns.Get("bert", "atlas"); !ok {
		t.Fatal("concurrent renewals ended the connection")
	}
	if s.fake.Refreshes != 8 {
		t.Errorf("%d renewals, want 8 one after another", s.fake.Refreshes)
	}

	s.fake.Lock()
	s.fake.Busy = true
	s.fake.Unlock()
	if _, err := s.b.Token(context.Background(), s.e, "bert"); !errors.Is(err, ErrUnavailable) || !strings.Contains(err.Error(), "temporarily_unavailable") {
		t.Errorf("a busy engine = %v", err)
	}
	if _, ok := s.conns.Get("bert", "atlas"); !ok || len(s.ended) != 0 {
		t.Fatal("a busy engine ended the connection")
	}

	s.fake.Lock()
	s.fake.Busy = false
	s.fake.Unlock()
	s.fake.Withdraw()
	if _, err := s.b.Token(context.Background(), s.e, "bert"); !errors.Is(err, ErrEnded) {
		t.Errorf("a withdrawn grant = %v", err)
	}
	if _, ok := s.conns.Get("bert", "atlas"); ok || strings.Join(s.ended, ",") != "bert@atlas" {
		t.Errorf("after the engine refused the grant: ended %v", s.ended)
	}
	if _, err := s.b.Token(context.Background(), s.e, "bert"); !errors.Is(err, engineconn.ErrNotConnected) {
		t.Errorf("Token without a connection = %v", err)
	}
}

// TestDisconnectingRevokesWhereTheEngineOffersIt, and ends the connection
// where it does not.
func TestDisconnectingRevokesWhereTheEngineOffersIt(t *testing.T) {
	s := newSetup(t)
	if _, err := s.connect(t, "bert"); err != nil {
		t.Fatal(err)
	}
	if err := s.b.Disconnect(context.Background(), s.e, "bert"); err != nil {
		t.Fatal(err)
	}
	if s.fake.Revoked != 1 {
		t.Errorf("revoked %d grants at the engine, want 1", s.fake.Revoked)
	}
	if err := s.b.Disconnect(context.Background(), s.e, "bert"); !errors.Is(err, engineconn.ErrNotConnected) {
		t.Errorf("disconnecting twice = %v", err)
	}

	s.fake.Lock()
	s.fake.NoRevocation = true
	s.fake.Unlock()
	s.b.mu.Lock()
	s.b.meta = map[string]metadata{}
	s.b.mu.Unlock()
	if _, err := s.connect(t, "vera"); err != nil {
		t.Fatal(err)
	}
	if err := s.b.Disconnect(context.Background(), s.e, "vera"); err != nil || s.fake.Revoked != 1 {
		t.Errorf("without revocation: %v, revoked %d", err, s.fake.Revoked)
	}
	if _, ok := s.conns.Get("vera", "atlas"); ok {
		t.Error("the connection did not end")
	}

	if _, err := s.connect(t, "carla"); err != nil {
		t.Fatal(err)
	}
	if users, err := s.b.EndEngine("atlas", "the engine was removed"); err != nil || strings.Join(users, ",") != "carla" {
		t.Errorf("EndEngine = %v, %v", users, err)
	}
}

// TestTheEngineMayNotSendTokensElsewhere: the endpoints its metadata
// names must be on its authorization server, and the code flow must offer
// PKCE.
func TestTheEngineMayNotSendTokensElsewhere(t *testing.T) {
	for _, tc := range []struct{ name, metadata string }{
		{"another server", `{"authorization_endpoint":"https://evil.example/authorize","token_endpoint":"%s/oauth/token","code_challenge_methods_supported":["S256"]}`},
		{"no PKCE", `{"authorization_endpoint":"%s/oauth/authorize","token_endpoint":"%s/oauth/token","code_challenge_methods_supported":["plain"]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var base string
			fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/.well-known/oauth-protected-resource/tasks":
					fmt.Fprintf(w, `{"resource":"%s/tasks","authorization_servers":["%s"]}`, base, base)
				case "/.well-known/oauth-authorization-server":
					fmt.Fprint(w, strings.ReplaceAll(tc.metadata, "%s", base))
				default:
					http.NotFound(w, r)
				}
			}))
			t.Cleanup(fake.Close)
			base = fake.URL
			b := NewBroker(nil, nil, fake.Client(), func(string) string { return "x" }, "https://wiki.example.org")
			_, _, err := b.Begin(context.Background(), site.Engine{ID: "atlas", Kind: site.EngineAtlas, BaseURL: base}, "bert")
			if !errors.Is(err, ErrUnavailable) {
				t.Errorf("Begin = %v", err)
			}
		})
	}
	b := NewBroker(nil, nil, http.DefaultClient, func(string) string { return "x" }, "https://wiki.example.org")
	if _, _, err := b.Begin(context.Background(), site.Engine{ID: "x", Kind: "other", BaseURL: "https://x.example"}, "bert"); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("an engine of another kind = %v", err)
	}
}
