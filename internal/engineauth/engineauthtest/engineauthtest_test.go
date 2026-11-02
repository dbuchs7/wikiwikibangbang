package engineauthtest_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/dbuchs7/orbithek/internal/engine"
	"github.com/dbuchs7/orbithek/internal/engineauth"
	"github.com/dbuchs7/orbithek/internal/engineauth/engineauthtest"
	"github.com/dbuchs7/orbithek/internal/engineconn"
	"github.com/dbuchs7/orbithek/internal/site"
)

type plain struct{}

func (plain) Seal(_ string, pt, _ []byte) ([]byte, error) { return pt, nil }
func (plain) Open(_ string, ct, _ []byte) ([]byte, error) { return ct, nil }
func (plain) Shred(...string) error                       { return nil }

// world is the fake engine with a broker and a reader of this server, which
// names itself by its client metadata document.
type world struct {
	fake   *engineauthtest.Engine
	b      *engineauth.Broker
	r      *engine.Reader
	e      site.Engine
	client *http.Client
}

func newWorld(t *testing.T) *world {
	t.Helper()
	fake := engineauthtest.New(t)
	n := 0
	conns, err := engineconn.New(nil, func([]byte) error { return nil }, plain{}, func() string { n++; return fmt.Sprint(n) })
	if err != nil {
		t.Fatal(err)
	}
	env := map[string]string{}
	getenv := func(k string) string { return env[k] }
	client := fake.Client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	w := &world{fake: fake, client: client, e: site.Engine{ID: "atlas", Name: "Atlas", Kind: site.EngineAtlas, BaseURL: fake.URL}}
	w.r = engine.NewReader(getenv, client)
	w.b = engineauth.NewBroker(conns, w.r, client, getenv, "https://wiki.example.org")
	w.r.WithSystemGrants(w.b)
	fake.Documents = func(id string) (map[string]any, error) {
		if id != w.b.MetadataClientID() {
			return nil, errors.New("unknown")
		}
		return w.b.ClientMetadata("Example wiki", []site.Engine{w.e})
	}
	return w
}

// connect runs the browser's part of a connection and finishes it; err is
// the engine's error code when it sent one back.
func (w *world) connect(t *testing.T, user string, o engineauth.Flow) (engineconn.Connection, error) {
	t.Helper()
	authURL, state, err := w.b.Begin(context.Background(), w.e, user, o)
	if err != nil {
		return engineconn.Connection{}, err
	}
	res, err := w.client.Get(authURL)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	back, _ := url.Parse(res.Header.Get("Location"))
	if code := back.Query().Get("error"); code != "" {
		w.b.Cancel(state)
		return engineconn.Connection{}, errors.New(code)
	}
	return w.b.Finish(context.Background(), w.e, state, back.Query().Get("code"))
}

// TestTheFakeEngineConnectsASystemAsTheContractSays drives engineauthtest
// with the broker and the reader as ADR-0152 uses them: a system grant
// with the capabilities kept, a person connected silently under
// pre-consent and refused without it or without a session, renewals by
// signed assertions, a task answered open, ended or out of reach, and the
// grant given back.
func TestTheFakeEngineConnectsASystemAsTheContractSays(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	w.fake.Tasks = []map[string]any{{"key": 1, "processInstanceKey": 1, "elementInstanceKey": 2, "processId": "p", "elementId": "e", "name": "One"}}
	w.fake.Ended = map[uint64]string{2: "cancelled"}

	// Without a system grant a silent connection is not offered.
	if _, err := w.connect(t, "bert", engineauth.Flow{Silent: true}); !errors.Is(err, engineauth.ErrNotConfigured) {
		t.Fatalf("a person before the engine is connected: %v", err)
	}
	w.fake.Keep = []string{"diagrams", "tasklist"}
	w.fake.Preconsent = true
	sys, err := w.connect(t, "admin", engineauth.Flow{System: true})
	if err != nil || !sys.System.Holds("tasklist") || sys.System.Holds("events") || !sys.System.Preconsent || sys.Username != "patrick" {
		t.Fatalf("system connection = %+v %v", sys, err)
	}
	if w.fake.System == nil || w.fake.Assertions == 0 {
		t.Fatalf("the engine holds %+v after %d assertions", w.fake.System, w.fake.Assertions)
	}

	// The reader lists, and asks about single tasks, with the grant.
	tasks, _, err := w.r.OpenTasks(ctx, w.e)
	if err != nil || len(tasks) != 1 {
		t.Fatalf("open tasks with the grant = %v %v", tasks, err)
	}
	if err := w.r.TaskEnd(ctx, w.e, 1); err != nil {
		t.Errorf("an open task = %v", err)
	}
	var ended engine.TaskEnded
	if err := w.r.TaskEnd(ctx, w.e, 2); !errors.As(err, &ended) || ended.Ended != "cancelled" {
		t.Errorf("an ended task = %v", err)
	}
	if err := w.r.TaskEnd(ctx, w.e, 3); !errors.Is(err, engine.ErrNotFound) {
		t.Errorf("a task out of reach = %v", err)
	}
	if _, _, _, err := w.r.TaskFacts(ctx, w.e, ""); err == nil {
		t.Error("the feed was read though the grant does not reach it")
	}

	// People: silently with a session, refused without one.
	w.fake.NoSession = true
	if _, err := w.connect(t, "vera", engineauth.Flow{Silent: true}); err == nil || err.Error() != "login_required" {
		t.Errorf("a silent connection without a session = %v", err)
	}
	w.fake.NoSession = false
	c, err := w.connect(t, "bert", engineauth.Flow{Silent: true})
	if err != nil || c.Client != w.b.MetadataClientID() {
		t.Fatalf("a silent connection = %+v %v", c, err)
	}
	w.fake.Expire()
	w.b.Refused("bert", "atlas")
	before := w.fake.Assertions
	if _, err := w.b.Token(ctx, w.e, "bert"); err != nil || w.fake.Assertions != before+1 {
		t.Errorf("renewing with an assertion = %v (%d assertions)", err, w.fake.Assertions-before)
	}

	// Without pre-consent, prompt=none needs a consent.
	w.fake.Lock()
	w.fake.System.Preconsent = false
	w.fake.Unlock()
	if _, err := w.connect(t, "vera", engineauth.Flow{Silent: true}); err == nil || err.Error() != "consent_required" {
		t.Errorf("a silent connection without pre-consent = %v", err)
	}

	// An assertion of another key is refused.
	if err := w.b.ReplaceKey(); err != nil {
		t.Fatal(err)
	}
	doc := w.fake.Documents
	w.fake.Documents = func(string) (map[string]any, error) {
		return map[string]any{"jwks": map[string]any{"keys": []map[string]string{}}}, nil
	}
	w.b.Refused("bert", "atlas")
	if _, err := w.b.Token(ctx, w.e, "bert"); err == nil || !strings.Contains(err.Error(), "invalid_client") {
		t.Errorf("an assertion the engine cannot check = %v", err)
	}
	w.fake.Documents = doc

	// Giving the grant back.
	if err := w.b.Disconnect(ctx, w.e, engineconn.SystemUser); err != nil {
		t.Fatal(err)
	}
	if w.fake.System != nil {
		t.Errorf("the engine still holds %+v", w.fake.System)
	}

	// An engine without system grants says so.
	w.fake.NoSystem = true
	w.b.EndEngine("atlas", "test") // forget its metadata
	if _, err := w.connect(t, "admin", engineauth.Flow{System: true}); !errors.Is(err, engineauth.ErrNoSystem) {
		t.Errorf("an engine without system grants = %v", err)
	}
}
