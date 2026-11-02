package server

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/dbuchs7/orbithek/internal/audit"
	"github.com/dbuchs7/orbithek/internal/engine"
	"github.com/dbuchs7/orbithek/internal/engineauth"
	"github.com/dbuchs7/orbithek/internal/engineauth/engineauthtest"
	"github.com/dbuchs7/orbithek/internal/engineconn"
	"github.com/dbuchs7/orbithek/internal/item"
)

// beginAt starts a connection at path for c and answers where the engine
// sends the browser back to, and the state cookie.
func beginAt(t *testing.T, c *client, fake *engineauthtest.Engine, path string) (string, *http.Cookie) {
	t.Helper()
	rec := c.do("GET", path, "")
	if rec.Code != http.StatusFound {
		t.Fatalf("%s: %d %s", path, rec.Code, rec.Body)
	}
	var state *http.Cookie
	for _, ck := range rec.Result().Cookies() {
		if ck.Name == connectCookie {
			state = ck
		}
	}
	if state == nil {
		t.Fatalf("%s set no state cookie; it went to %s", path, rec.Header().Get("Location"))
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

// TestAnAdministratorConnectsTheEngineWithOneApproval covers ADR-0152 through
// the API: the server names itself by its client metadata document; an
// administrator connects the engine, whose administrator approves a system
// grant for the capabilities they keep, and no client, token or secret is
// copied; the grant takes the place of the task list credential, and a
// task that left the list is ended only on the engine's word — detached
// when out of reach; people connect through the document, silently where
// the engine approved them beforehand; a person who disconnected is not
// connected again without asking; a credential of the environment wins;
// disconnecting gives the grant back; each step is audited.
func TestAnAdministratorConnectsTheEngineWithOneApproval(t *testing.T) {
	fake := engineauthtest.New(t)
	env := map[string]string{}
	getenv := func(k string) string { return env[k] }
	hc := fake.Client()
	var broker *engineauth.Broker
	w := newWorld(t, func(d *Deps) {
		d.Engines = engine.NewReader(getenv, hc)
		d.Connect = engineauth.NewBroker(d.DB.Connections, d.Engines, hc, getenv, "http://wiki.test")
		d.Connect.OnEnded(RecordEnded(d.DB))
		broker = d.Connect
	})
	fake.Documents = func(id string) (map[string]any, error) {
		if id != broker.MetadataClientID() {
			return nil, http.ErrNoLocation
		}
		return broker.ClientMetadata("Example wiki", w.db.Site.Engines())
	}
	w.admin.expect("PUT", "/api/engines/atlas", `{"name":"Atlas","kind":"atlas","baseURL":"`+fake.URL+`"}`, http.StatusCreated)

	// The document, for anyone.
	anon := &client{t: t, h: w.admin.h}
	doc := anon.expect("GET", "/api/engines/client-metadata", "", http.StatusOK)
	if doc["client_id"] != "http://wiki.test/api/engines/client-metadata" || doc["token_endpoint_auth_method"] != "private_key_jwt" ||
		!strings.Contains(toJSON(t, doc["redirect_uris"]), "http://wiki.test/api/engines/atlas/connect/callback") ||
		!strings.Contains(toJSON(t, doc["jwks"]), `"crv":"Ed25519"`) || strings.Contains(toJSON(t, doc), "secret") {
		t.Fatalf("document = %v", doc)
	}

	// Before the engine is connected people cannot connect: no client is
	// registered there.
	if got := w.bert.expect("GET", "/api/me/engines", "", http.StatusOK)["engines"].([]any); len(got) != 0 {
		t.Errorf("before connecting, bert is offered %v", got)
	}
	w.bert.expect("GET", "/api/engines/atlas/system/connect", "", http.StatusForbidden)

	fake.Keep = []string{"diagrams", "tasklist"}
	fake.Preconsent = true
	back, state := beginAt(t, w.admin, fake, "/api/engines/atlas/system/connect")
	if q := fake.LastAuthorize; q.Get("resource") != fake.URL+"/system" || q.Get("scope") != "diagrams tasklist events" || q.Get("client_id") != broker.MetadataClientID() {
		t.Fatalf("the engine was asked %v", q)
	}
	if to := callback(t, w.admin.h, back, state); to != "/?engine_system=atlas#/admin" {
		t.Fatalf("after connecting, the administrator is sent to %q", to)
	}
	if fake.Assertions == 0 {
		t.Error("the code was exchanged without a signed assertion")
	}
	sys := w.admin.expect("GET", "/api/engines", "", http.StatusOK)["engines"].([]any)[0].(map[string]any)["system"].(map[string]any)
	if sys["connected"] != true || sys["approvedBy"] != "patrick" || sys["preconsent"] != true ||
		toJSON(t, sys["fromGrant"]) != `{"diagrams":true,"events":false,"tasklist":true}` {
		t.Fatalf("the connection as administrators see it = %v", sys)
	}

	// The task links read the open tasks with the grant.
	fake.Tasks = []map[string]any{
		{"key": 4711, "processInstanceKey": 1, "elementInstanceKey": 2, "processId": "urlaub", "elementId": "genehmigen", "name": "Urlaub genehmigen"},
		{"key": 4712, "processInstanceKey": 3, "elementInstanceKey": 4, "processId": "urlaub", "elementId": "genehmigen", "name": "Urlaub von Ben"},
		{"key": 4713, "processInstanceKey": 5, "elementInstanceKey": 6, "processId": "urlaub", "elementId": "genehmigen", "name": "Urlaub von Vera"},
	}
	w.admin.expect("POST", "/api/types", `{"name":"engine-task","body":true,"workflow":{"initial":"open","states":["open","done","cancelled"],
		"transitions":[{"name":"finish","from":["open"],"to":"done"},{"name":"cancel","from":["open"],"to":"cancelled"}]}}`, http.StatusCreated)
	links := w.admin.expect("PUT", "/api/engines/atlas/task-links", `{"enabled":true,"rules":[{"id":"holidays","space":"PLAT","type":"engine-task",
		"completed":"done","cancelled":"cancelled"}]}`, http.StatusOK)
	if links["credential"] != true || links["listFromGrant"] != true || links["events"] != false {
		t.Errorf("task links = %v", links)
	}
	if rep := w.admin.expect("POST", "/api/engines/atlas/task-links/run", "", http.StatusOK); rep["linked"] != float64(3) {
		t.Fatalf("a round with the grant = %v", rep)
	}
	// One ended, cancelled; one left the reach.
	fake.Lock()
	fake.Tasks = fake.Tasks[2:]
	fake.Ended = map[uint64]string{4711: "cancelled"}
	fake.Unlock()
	if rep := w.admin.expect("POST", "/api/engines/atlas/task-links/run", "", http.StatusOK); rep["ended"] != float64(1) || rep["detached"] != float64(1) {
		t.Fatalf("a round after two tasks left = %v", rep)
	}
	states := map[string]string{}
	w.db.Read(func(s *item.Store) {
		for _, key := range []string{"4711", "4712"} {
			it, _ := s.FindByOrigin(item.LinkedSystem, "atlas", "task/"+key)
			states[key] = it.State + "/" + it.DetachedFrom()
		}
	})
	if states["4711"] != "cancelled/" || states["4712"] != "open/atlas" {
		t.Errorf("after the round: %v", states)
	}

	// People connect through the document, silently.
	mine := w.bert.expect("GET", "/api/me/engines", "", http.StatusOK)["engines"].([]any)
	if len(mine) != 1 || mine[0].(map[string]any)["connectable"] != true || mine[0].(map[string]any)["silent"] != true {
		t.Fatalf("bert is offered %v", mine)
	}
	back, state = beginAt(t, w.bert, fake, "/api/engines/atlas/connect?silent=1&back="+url.QueryEscape("#/tasks"))
	if fake.LastAuthorize.Get("prompt") != "none" {
		t.Errorf("a silent connection asked %v", fake.LastAuthorize)
	}
	if to := callback(t, w.admin.h, back, state); to != "/#/tasks" {
		t.Fatalf("after a silent connection bert is sent to %q", to)
	}
	conn, ok := w.db.Connections.Get(w.ids["bert"], "atlas")
	if !ok || conn.Client != broker.MetadataClientID() {
		t.Fatalf("bert's connection = %+v %v", conn, ok)
	}
	fake.Expire()
	broker.Refused(w.ids["bert"], "atlas") // as the engine's refusal of the expired token tells it
	before := fake.Assertions
	e, _ := w.db.Site.Engine("atlas")
	if _, err := broker.Token(context.Background(), e, w.ids["bert"]); err != nil || fake.Assertions != before+1 {
		t.Errorf("renewing bert's grant = %v, %d assertions", err, fake.Assertions-before)
	}
	// A back that is not a page of this server is not followed.
	rec := w.vera.do("GET", "/api/engines/atlas/connect?silent=1&back="+url.QueryEscape("https://evil.example/"), "")
	if loc := rec.Header().Get("Location"); rec.Code != http.StatusFound || !strings.HasPrefix(loc, fake.URL) {
		t.Errorf("a foreign back: %d %s", rec.Code, loc)
	}
	// Without a session at the engine, back to the page, not connected.
	fake.NoSession = true
	back, state = beginAt(t, w.vera, fake, "/api/engines/atlas/connect?silent=1&back="+url.QueryEscape("#/space/PLAT"))
	if to := callback(t, w.admin.h, back, state); to != "/#/space/PLAT" {
		t.Errorf("a silent connection without a session goes to %q", to)
	}
	if _, ok := w.db.Connections.Get(w.ids["vera"], "atlas"); ok {
		t.Error("vera was connected without a session at the engine")
	}
	fake.NoSession = false

	// Who disconnects is not connected again without asking.
	w.bert.expect("DELETE", "/api/me/engines/atlas", "", http.StatusNoContent)
	if got := w.bert.expect("GET", "/api/me/engines", "", http.StatusOK)["engines"].([]any)[0].(map[string]any); got["silent"] != false {
		t.Errorf("after disconnecting, bert is still connected silently: %v", got)
	}

	// The environment wins.
	env[engine.TaskListVar("atlas")] = "lister"
	if got := w.admin.expect("GET", "/api/engines/atlas/task-links", "", http.StatusOK); got["listFromGrant"] != false {
		t.Errorf("with the variable set, the list still comes from the grant: %v", got)
	}

	// A new key is published; disconnecting gives the grant back.
	kid := toJSON(t, doc["jwks"])
	w.bert.expect("POST", "/api/engines/client-key", "", http.StatusForbidden)
	w.admin.expect("POST", "/api/engines/client-key", "", http.StatusNoContent)
	if again := anon.expect("GET", "/api/engines/client-metadata", "", http.StatusOK); toJSON(t, again["jwks"]) == kid {
		t.Error("replacing the key did not change the document")
	}
	w.admin.expect("DELETE", "/api/engines/atlas/system", "", http.StatusNoContent)
	if fake.System != nil {
		t.Errorf("the engine still holds the system grant %+v", fake.System)
	}
	if _, ok := w.db.Connections.Get(engineconn.SystemUser, "atlas"); ok {
		t.Error("the system grant is still kept")
	}
	w.admin.expect("DELETE", "/api/engines/atlas/system", "", http.StatusNotFound)

	events := map[string]int{}
	for _, e := range w.db.Audit.List(0, 1000) {
		events[e.Event]++
	}
	for _, ev := range []string{audit.EngineSystemConnected, audit.EngineSystemDisconnected, audit.ClientKeyReplaced, audit.EngineConnected, audit.EngineDisconnected} {
		if events[ev] != 1 {
			t.Errorf("audit %s recorded %d times", ev, events[ev])
		}
	}
}
