package server

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/dbuchs7/wikiwikibangbang/internal/audit"
	"github.com/dbuchs7/wikiwikibangbang/internal/engine"
	"github.com/dbuchs7/wikiwikibangbang/internal/engineauth"
	"github.com/dbuchs7/wikiwikibangbang/internal/engineauth/engineauthtest"
	"github.com/dbuchs7/wikiwikibangbang/internal/item"
)

// connectWorld is a world whose engine atlas is the fake engine, with this
// server registered there.
func connectWorld(t *testing.T) (world, *engineauthtest.Engine) {
	t.Helper()
	fake := engineauthtest.New(t)
	env := map[string]string{engine.ClientIDVar("atlas"): engineauthtest.ClientID, engine.ClientSecretVar("atlas"): engineauthtest.ClientSecret}
	getenv := func(k string) string { return env[k] }
	client := fake.Client()
	w := newWorld(t, func(d *Deps) {
		d.Engines = engine.NewReader(getenv, client)
		d.Connect = engineauth.NewBroker(d.DB.Connections, d.Engines, client, getenv, "http://wiki.test")
		d.Connect.OnEnded(RecordEnded(d.DB))
	})
	w.admin.expect("PUT", "/api/engines/atlas", `{"name":"Atlas","kind":"atlas","baseURL":"`+fake.URL+`"}`, http.StatusCreated)
	return w, fake
}

// begin starts c's connection and answers the address the engine sends
// the browser back to, and the browser's state cookie.
func begin(t *testing.T, c *client, fake *engineauthtest.Engine) (string, *http.Cookie) {
	t.Helper()
	rec := c.do("GET", "/api/engines/atlas/connect", "")
	if rec.Code != http.StatusFound {
		t.Fatalf("connect: %d %s", rec.Code, rec.Body)
	}
	var state *http.Cookie
	for _, ck := range rec.Result().Cookies() {
		if ck.Name == connectCookie {
			state = ck
		}
	}
	if state == nil || !state.HttpOnly || state.SameSite != http.SameSiteLaxMode {
		t.Fatalf("state cookie %+v", state)
	}
	nc := fake.Client()
	nc.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	res, err := nc.Get(rec.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	return res.Header.Get("Location"), state
}

// callback comes back from the engine as the browser does: without the
// session cookie, which is SameSite=Strict, with the state cookie.
func callback(t *testing.T, h http.Handler, back string, state *http.Cookie) string {
	t.Helper()
	u, err := url.Parse(back)
	if err != nil || u.Host != "wiki.test" {
		t.Fatalf("the engine sends the browser to %q", back)
	}
	req := httptest.NewRequest("GET", u.RequestURI(), nil)
	req.RemoteAddr = "192.0.2.1:4000"
	if state != nil {
		req.AddCookie(state)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("callback: %d %s", rec.Code, rec.Body)
	}
	return rec.Header().Get("Location")
}

// TestAPersonConnectsTheirAccountToAnEngine covers ADR-0151's connection
// through the API: one begins from the web UI, signed in; the engine's
// redirect back counts only in the browser that began it, and only once;
// the connection names who one is at the engine and whether its mail
// address differs from one's own; administrators see whether people can
// connect and how many did; the engine moving or being removed ends the
// connections; each step is audited.
func TestAPersonConnectsTheirAccountToAnEngine(t *testing.T) {
	w, fake := connectWorld(t)
	h := w.admin.h
	w.bert.expect("PUT", "/api/me/contacts", `{"email":"bert@example.org"}`, http.StatusOK)
	got := w.bert.expect("GET", "/api/me/engines", "", http.StatusOK)["engines"].([]any)
	if len(got) != 1 || got[0].(map[string]any)["connectable"] != true || got[0].(map[string]any)["connection"] != nil {
		t.Fatalf("engines before connecting = %v", got)
	}

	// Not with an API token: the grant is the person's, approved in the
	// browser.
	tok := w.bert.expect("POST", "/api/tokens", `{"name":"ci","scope":"full","days":1}`, http.StatusCreated)
	(&client{t: t, h: h, bearer: tok["secret"].(string)}).expect("GET", "/api/engines/atlas/connect", "", http.StatusForbidden)

	back, state := begin(t, w.bert, fake)
	// Another browser cannot finish it, nor one without the cookie.
	if loc := callback(t, h, back, &http.Cookie{Name: connectCookie, Value: "other"}); loc != "/?engine_error=state#/account" {
		t.Errorf("a foreign state cookie went to %q", loc)
	}
	if loc := callback(t, h, back, nil); loc != "/?engine_error=state#/account" {
		t.Errorf("no state cookie went to %q", loc)
	}
	if loc := callback(t, h, back, state); loc != "/?engine_connected=atlas#/account" {
		t.Fatalf("the callback went to %q", loc)
	}
	if loc := callback(t, h, back, state); loc != "/?engine_error=state#/account" {
		t.Errorf("a callback used twice went to %q", loc)
	}
	got = w.bert.expect("GET", "/api/me/engines", "", http.StatusOK)["engines"].([]any)
	conn, _ := got[0].(map[string]any)["connection"].(map[string]any)
	if conn == nil || conn["username"] != "anna" || conn["subject"] != "u-17" || conn["emailDiffers"] != true || conn["engine"] != "atlas" {
		t.Fatalf("the connection = %v", got[0])
	}
	if mine := w.vera.expect("GET", "/api/me/engines", "", http.StatusOK)["engines"].([]any); mine[0].(map[string]any)["connection"] != nil {
		t.Error("vera sees bert's connection")
	}

	links := w.admin.expect("GET", "/api/engines/atlas/task-links", "", http.StatusOK)["connect"].(map[string]any)
	if links["configured"] != true || links["connected"] != float64(1) || links["callback"] != "http://wiki.test/api/engines/atlas/connect/callback" ||
		links["clientIdVar"] != "WWBB_ENGINE_ATLAS_CLIENT_ID" || links["clientSecretVar"] != "WWBB_ENGINE_ATLAS_CLIENT_SECRET" {
		t.Errorf("the administrators' view = %v", links)
	}

	// A refused consent comes back as such.
	fake.Lock()
	fake.Deny = true
	fake.Unlock()
	back, state = begin(t, w.vera, fake)
	if loc := callback(t, h, back, state); loc != "/?engine_error=denied#/account" {
		t.Errorf("a refused consent went to %q", loc)
	}
	fake.Lock()
	fake.Deny = false
	fake.Unlock()

	w.vera.expect("DELETE", "/api/me/engines/atlas", "", http.StatusNotFound)
	w.bert.expect("DELETE", "/api/me/engines/atlas", "", http.StatusNoContent)
	if fake.Revoked != 1 {
		t.Errorf("revoked %d grants at the engine, want 1", fake.Revoked)
	}

	// The engine moves: the grants belong to it at its old address.
	back, state = begin(t, w.bert, fake)
	callback(t, h, back, state)
	w.admin.expect("PUT", "/api/engines/atlas", `{"name":"Atlas","kind":"atlas","baseURL":"https://atlas.example.org"}`, http.StatusOK)
	if n := w.db.Connections.Count(); n != 0 {
		t.Errorf("%d connections outlive the engine's move", n)
	}

	events := map[string]int{}
	for _, e := range w.db.Audit.List(0, 1000) {
		if e.Subject == "atlas" {
			events[e.Event]++
		}
	}
	if events[audit.EngineConnected] != 2 || events[audit.EngineDisconnected] != 1 || events[audit.EngineConnectionEnded] != 1 {
		t.Errorf("audited %v", events)
	}
}

// TestALinkedTaskIsReadByTheConnectedPeopleTheEngineShowsItTo covers
// ADR-0151's visibility through the API: after a person connects, a round
// reads their own task list, and the linked task is read by the connected
// people whose list holds it and who hold a role in its space — the
// protection narrows, it never grants (D9); nobody else finds it (D10);
// after disconnecting, the next round hides it again.
func TestALinkedTaskIsReadByTheConnectedPeopleTheEngineShowsItTo(t *testing.T) {
	fake := engineauthtest.New(t)
	fake.ListToken = "lister"
	fake.Tasks = []map[string]any{{"key": 4711, "processInstanceKey": 1, "elementInstanceKey": 2, "processId": "urlaub",
		"elementId": "genehmigen", "name": "Urlaub genehmigen", "candidateGroups": "hr"}}
	fake.Sees = map[string][]uint64{"u-bert": {4711}, "u-carla": {4711}}
	env := map[string]string{engine.ClientIDVar("atlas"): engineauthtest.ClientID, engine.ClientSecretVar("atlas"): engineauthtest.ClientSecret,
		engine.TaskListVar("atlas"): "lister"}
	getenv := func(k string) string { return env[k] }
	hc := fake.Client()
	w := newWorld(t, func(d *Deps) {
		d.Engines = engine.NewReader(getenv, hc)
		d.Connect = engineauth.NewBroker(d.DB.Connections, d.Engines, hc, getenv, "http://wiki.test")
	})
	h := w.admin.h
	w.admin.expect("PUT", "/api/engines/atlas", `{"name":"Atlas","kind":"atlas","baseURL":"`+fake.URL+`"}`, http.StatusCreated)
	w.admin.expect("POST", "/api/types", `{"name":"engine-task","body":true,"workflow":{"initial":"open","states":["open","done"],
		"transitions":[{"name":"finish","from":["open"],"to":"done"}]}}`, http.StatusCreated)
	w.admin.expect("PUT", "/api/engines/atlas/task-links", `{"enabled":true,"rules":[{"id":"holidays","space":"PLAT","type":"engine-task",
		"completed":"done","cancelled":"done"}]}`, http.StatusOK)
	rep := w.admin.expect("POST", "/api/engines/atlas/task-links/run", "", http.StatusOK)
	if rep["linked"] != float64(1) || rep["visible"] != true || rep["people"] != float64(0) {
		t.Fatalf("the first round = %v", rep)
	}
	var id string
	w.db.Read(func(s *item.Store) { id = string(s.LinkedTasks("atlas")[0].ID) })
	for _, c := range []*client{w.bert, w.vera, w.carla} {
		c.expect("GET", "/api/items/"+id, "", http.StatusNotFound)
	}

	// Bert (an editor of PLAT) and carla (no role there) connect; vera (a
	// viewer) does not.
	for _, who := range []struct {
		c       *client
		subject string
	}{{w.bert, "u-bert"}, {w.carla, "u-carla"}} {
		fake.Lock()
		fake.Person.ID = who.subject
		fake.Unlock()
		back, state := begin(t, who.c, fake)
		callback(t, h, back, state)
	}
	rep = w.admin.expect("POST", "/api/engines/atlas/task-links/run", "", http.StatusOK)
	if rep["visible"] != true || rep["people"] != float64(2) || rep["readers"] != float64(1) {
		t.Fatalf("the round after connecting = %v", rep)
	}
	w.bert.expect("GET", "/api/items/"+id, "", http.StatusOK)
	w.vera.expect("GET", "/api/items/"+id, "", http.StatusNotFound)
	w.carla.expect("GET", "/api/items/"+id, "", http.StatusNotFound)

	w.bert.expect("DELETE", "/api/me/engines/atlas", "", http.StatusNoContent)
	if rep = w.admin.expect("POST", "/api/engines/atlas/task-links/run", "", http.StatusOK); rep["readers"] != float64(1) {
		t.Fatalf("the round after disconnecting = %v", rep)
	}
	w.bert.expect("GET", "/api/items/"+id, "", http.StatusNotFound)
}
