package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dbuchs7/wikiwikibangbang/internal/audit"
	"github.com/dbuchs7/wikiwikibangbang/internal/engine"
	"github.com/dbuchs7/wikiwikibangbang/internal/item"
)

// fakeTaskEngine lists one open user task for the task list credential
// "lister".
func fakeTaskEngine(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer lister" || r.URL.Path != "/api/v1/tasks" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		_, _ = w.Write([]byte(`{"items":[{"key":4711,"processInstanceKey":1,"elementInstanceKey":2,"processId":"urlaub",
			"elementId":"genehmigen","name":"Urlaub genehmigen","candidateGroups":"hr","dueDate":1793000000000}],"truncated":false}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestTaskLinksThroughTheAPI covers ADR-0151's administration: only
// administrators read and set an engine's task links; a rule whose items
// the synchronisation could not make or end is refused; a dry round
// reports, a round links the task — hidden from the space's readers — and
// both are answered with their report, the real one audited.
func TestTaskLinksThroughTheAPI(t *testing.T) {
	srv := fakeTaskEngine(t)
	env := map[string]string{"WWBB_ENGINE_ATLAS_TASKLIST_TOKEN": "lister"}
	w := newWorld(t, func(d *Deps) { d.Engines = engine.NewReader(func(k string) string { return env[k] }, srv.Client()) })
	w.admin.expect("PUT", "/api/engines/atlas", `{"name":"Atlas","kind":"atlas","baseURL":"`+srv.URL+`"}`, http.StatusCreated)

	w.bert.expect("GET", "/api/engines/atlas/task-links", "", http.StatusForbidden)
	w.bert.expect("PUT", "/api/engines/atlas/task-links", `{}`, http.StatusForbidden)
	w.bert.expect("POST", "/api/engines/atlas/task-links/run", "", http.StatusForbidden)
	w.admin.expect("GET", "/api/engines/nope/task-links", "", http.StatusNotFound)

	got := w.admin.expect("GET", "/api/engines/atlas/task-links", "", http.StatusOK)
	if got["enabled"] != false || got["credentialVar"] != "WWBB_ENGINE_ATLAS_TASKLIST_TOKEN" || got["credential"] != true || got["last"] != nil {
		t.Fatalf("links before any = %v", got)
	}

	rule := func(typ, due, cancelled string) string {
		return `{"enabled":true,"rules":[{"id":"holidays","processes":["urlaub"],"space":"PLAT","type":"` + typ +
			`","labels":["atlas","{process}"],"dueField":"` + due + `","completed":"done","cancelled":"` + cancelled + `"}]}`
	}
	// The world's type task requires a priority, which the engine does not set.
	w.admin.expect("PUT", "/api/engines/atlas/task-links", rule("task", "due", "done"), http.StatusBadRequest)
	w.admin.expect("PUT", "/api/engines/atlas/task-links", `{"rules":[{"space":"PLAT","type":"engine-task","completed":"done","cancelled":"done"}]}`, http.StatusBadRequest)
	w.admin.expect("POST", "/api/types", `{"name":"engine-task","body":true,"fields":[{"name":"due","kind":"date"},{"name":"estimate","kind":"number"}],
		"workflow":{"initial":"open","states":["open","done","cancelled"],"transitions":[
			{"name":"finish","from":["open"],"to":"done"},{"name":"cancel","from":["open"],"to":"cancelled"}]}}`, http.StatusCreated)
	w.admin.expect("PUT", "/api/engines/atlas/task-links", rule("engine-task", "due", "gone"), http.StatusBadRequest)
	w.admin.expect("PUT", "/api/engines/atlas/task-links", rule("engine-task", "estimate", "cancelled"), http.StatusBadRequest)
	w.admin.expect("PUT", "/api/engines/atlas/task-links", rule("engine-task", "colour", "cancelled"), http.StatusBadRequest)
	set := w.admin.expect("PUT", "/api/engines/atlas/task-links", rule("engine-task", "due", "cancelled"), http.StatusOK)
	if set["enabled"] != true || len(set["rules"].([]any)) != 1 {
		t.Fatalf("set = %v", set)
	}

	dry := w.admin.expect("POST", "/api/engines/atlas/task-links/run?dry=true", "", http.StatusOK)
	if dry["dry"] != true || dry["linked"] != float64(1) || dry["open"] != float64(1) {
		t.Fatalf("dry round = %v", dry)
	}
	var linked int
	w.db.Read(func(s *item.Store) { linked = len(s.LinkedTasks("atlas")) })
	if linked != 0 {
		t.Fatal("a dry round linked a task")
	}
	rep := w.admin.expect("POST", "/api/engines/atlas/task-links/run", "", http.StatusOK)
	if rep["linked"] != float64(1) || rep["error"] != nil {
		t.Fatalf("round = %v", rep)
	}
	var it item.Item
	w.db.Read(func(s *item.Store) { it = s.LinkedTasks("atlas")[0] })
	got = w.admin.expect("GET", "/api/items/"+string(it.ID), "", http.StatusOK)
	admin := got["item"].(map[string]any)
	if got["may"].(map[string]any)["trash"] == true {
		t.Error("an open linked task is offered for the trash")
	}
	if admin["linked"] != "atlas" || admin["title"] != "Urlaub genehmigen" || !strings.Contains(strings.Join(toStrings(admin["linkedFields"]), ","), "due") {
		t.Fatalf("the linked item = %v", admin)
	}
	w.bert.expect("GET", "/api/items/"+string(it.ID), "", http.StatusNotFound)
	if last := w.admin.expect("GET", "/api/engines/atlas/task-links", "", http.StatusOK)["last"]; last == nil || last.(map[string]any)["linked"] != float64(1) {
		t.Errorf("last = %v", last)
	}

	events := map[string]int{}
	for _, e := range w.db.Audit.List(0, 1000) {
		if e.Subject == "atlas" {
			events[e.Event]++
		}
	}
	if events[audit.TaskLinksChanged] != 1 || events[audit.TaskLinksRun] != 1 {
		t.Errorf("audited %v, want one change and one round (the dry one is not)", events)
	}

	// A round that cannot read the engine says why, in its report.
	delete(env, "WWBB_ENGINE_ATLAS_TASKLIST_TOKEN")
	if failed := w.admin.expect("POST", "/api/engines/atlas/task-links/run", "", http.StatusOK); !strings.Contains(failed["error"].(string), "WWBB_ENGINE_ATLAS_TASKLIST_TOKEN") {
		t.Errorf("a round without the credential = %v", failed)
	}
}

// TestHowCurrentLinkedTasksAreIsTold covers ADR-0151's freshness: anyone
// signed in learns when an engine's tasks were last read and whether that
// is long ago; the administrators' view names the events variable; the
// metrics count each engine's rounds and connected people.
func TestHowCurrentLinkedTasksAreIsTold(t *testing.T) {
	srv := fakeTaskEngine(t)
	env := map[string]string{"WWBB_ENGINE_ATLAS_TASKLIST_TOKEN": "lister"}
	w := newWorld(t, func(d *Deps) { d.Engines = engine.NewReader(func(k string) string { return env[k] }, srv.Client()) })
	w.admin.expect("PUT", "/api/engines/atlas", `{"name":"Atlas","kind":"atlas","baseURL":"`+srv.URL+`"}`, http.StatusCreated)
	w.bert.expect("GET", "/api/engines/nope/tasks-read", "", http.StatusNotFound)
	got := w.bert.expect("GET", "/api/engines/atlas/tasks-read", "", http.StatusOK)
	if got["fresh"].(map[string]any)["readAt"] != nil || got["staleAfter"] != float64(300) {
		t.Fatalf("before a round = %v", got)
	}
	links := w.admin.expect("GET", "/api/engines/atlas/task-links", "", http.StatusOK)
	if links["eventsVar"] != "WWBB_ENGINE_ATLAS_EVENTS_TOKEN" || links["events"] != false {
		t.Errorf("events = %v %v", links["eventsVar"], links["events"])
	}
	w.admin.expect("POST", "/api/engines/atlas/task-links/run", "", http.StatusOK)
	fresh := w.bert.expect("GET", "/api/engines/atlas/tasks-read", "", http.StatusOK)["fresh"].(map[string]any)
	if fresh["readAt"] == nil || fresh["stale"] != false {
		t.Errorf("after a round = %v", fresh)
	}
	rec := w.admin.do("GET", "/metrics", "")
	for _, want := range []string{`wwbb_tasklinks_last_read_timestamp_seconds{engine="atlas"}`, `wwbb_tasklinks_failed_rounds{engine="atlas"} 0`} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Errorf("the metrics lack %s", want)
		}
	}
	delete(env, "WWBB_ENGINE_ATLAS_TASKLIST_TOKEN")
	w.admin.expect("POST", "/api/engines/atlas/task-links/run", "", http.StatusOK)
	if rec := w.admin.do("GET", "/metrics", ""); !strings.Contains(rec.Body.String(), `wwbb_tasklinks_failed_rounds{engine="atlas"} 1`) {
		t.Error("a failed round is not counted")
	}
}
