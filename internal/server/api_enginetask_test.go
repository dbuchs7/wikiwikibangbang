package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/dbuchs7/wikiwikibangbang/internal/engine"
	"github.com/dbuchs7/wikiwikibangbang/internal/engineauth"
	"github.com/dbuchs7/wikiwikibangbang/internal/engineauth/engineauthtest"
	"github.com/dbuchs7/wikiwikibangbang/internal/item"
)

// TestAPersonWorksALinkedTaskAtItsEngine covers ADR-0151's working of a
// task: one reads the task, its form and its values as the engine shows
// them to one; takes it on; the engine's refusals come back in its words;
// completing it sends the form's values with one's own grant, and only
// once the engine accepts does the item move to the completed state, as
// one's own transition. Who cannot read the item finds no task (D10); who
// is not connected is asked to connect.
func TestAPersonWorksALinkedTaskAtItsEngine(t *testing.T) {
	fake := engineauthtest.New(t)
	fake.ListToken = "lister"
	fake.Tasks = []map[string]any{
		{"key": 4711, "processInstanceKey": 1, "elementInstanceKey": 2, "processId": "urlaub", "elementId": "genehmigen", "name": "Urlaub genehmigen", "formId": "approval"},
		{"key": 4712, "processInstanceKey": 3, "elementInstanceKey": 4, "processId": "urlaub", "elementId": "genehmigen", "name": "Urlaub von Ben", "assignee": "someone"},
	}
	fake.Sees = map[string][]uint64{"u-17": {4711, 4712}}
	fake.Forms = map[string]map[string]any{"approval": {"type": "default", "components": []any{
		map[string]any{"type": "radio", "key": "approved", "label": "Approved", "values": []any{map[string]any{"label": "Yes", "value": "yes"}}},
		map[string]any{"type": "textarea", "key": "comment", "label": "Comment"}}}}
	fake.Values = map[string]any{"applicant": "Ben"}
	env := map[string]string{engine.ClientIDVar("atlas"): engineauthtest.ClientID, engine.ClientSecretVar("atlas"): engineauthtest.ClientSecret,
		engine.TaskListVar("atlas"): "lister"}
	getenv := func(k string) string { return env[k] }
	hc := fake.Client()
	w := newWorld(t, func(d *Deps) {
		d.Engines = engine.NewReader(getenv, hc)
		d.Connect = engineauth.NewBroker(d.DB.Connections, d.Engines, hc, getenv, "http://wiki.test")
	})
	w.admin.expect("PUT", "/api/engines/atlas", `{"name":"Atlas","kind":"atlas","baseURL":"`+fake.URL+`"}`, http.StatusCreated)
	w.admin.expect("POST", "/api/types", `{"name":"engine-task","body":true,"workflow":{"initial":"open","states":["open","done","cancelled"],
		"transitions":[{"name":"finish","from":["open"],"to":"done"},{"name":"cancel","from":["open"],"to":"cancelled"}]}}`, http.StatusCreated)
	w.admin.expect("PUT", "/api/engines/atlas/task-links", `{"enabled":true,"rules":[{"id":"holidays","space":"PLAT","type":"engine-task",
		"completed":"done","cancelled":"cancelled"}]}`, http.StatusOK)
	w.admin.expect("POST", "/api/engines/atlas/task-links/run", "", http.StatusOK)
	ids := map[string]string{}
	w.db.Read(func(s *item.Store) {
		for _, it := range s.LinkedTasks("atlas") {
			ids[it.Origin.ExternalKey] = string(it.ID)
		}
	})
	path := "/api/items/" + ids["4711"] + "/engine-task"

	// Not connected: the task is offered to connect to, not worked.
	got := w.admin.expect("GET", path, "", http.StatusOK)
	if got["connected"] != false || got["completed"] != "done" || got["task"] != nil || !strings.HasSuffix(got["taskApp"].(string), "/#/tasks/t/4711") {
		t.Fatalf("unconnected = %v", got)
	}
	if rec := w.admin.do("POST", path+"/claim", ""); rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), `"connect":true`) {
		t.Errorf("claim unconnected: %d %s", rec.Code, rec.Body)
	}

	back, state := begin(t, w.bert, fake)
	callback(t, w.admin.h, back, state)
	w.admin.expect("POST", "/api/engines/atlas/task-links/run", "", http.StatusOK)
	w.vera.expect("GET", path, "", http.StatusNotFound) // not a reader of the item

	got = w.bert.expect("GET", path, "", http.StatusOK)
	task, _ := got["task"].(map[string]any)
	if got["connected"] != true || got["open"] != true || got["mine"] != false || task["name"] != "Urlaub genehmigen" || task["form"] != "approval" {
		t.Fatalf("the task as bert sees it = %v", got)
	}
	if f := got["form"].(map[string]any); f["form"] != "approval" || !strings.Contains(toJSON(t, f["schema"]), `"key":"approved"`) {
		t.Errorf("form = %v", f)
	}
	if v := got["values"].(map[string]any); v["applicant"] != "Ben" {
		t.Errorf("values = %v", v)
	}

	if a := w.bert.expect("POST", path+"/claim", "", http.StatusOK); a["assignee"] != "anna" {
		t.Errorf("claim = %v", a)
	}
	if got = w.bert.expect("GET", path, "", http.StatusOK); got["mine"] != true {
		t.Errorf("after claiming: mine = %v", got["mine"])
	}
	other := "/api/items/" + ids["4712"] + "/engine-task"
	if rec := w.bert.do("POST", other+"/claim", ""); rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "assigned to somebody else") {
		t.Errorf("claiming another's task: %d %s", rec.Code, rec.Body)
	}

	if rec := w.bert.do("POST", path+"/complete", `{"variables":{"approved":"maybe"}}`); rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "not a value of the list") {
		t.Errorf("a refused value: %d %s", rec.Code, rec.Body)
	}
	w.bert.expect("POST", path+"/complete", `{"variables":[1]}`, http.StatusBadRequest)
	var st string
	w.db.Read(func(s *item.Store) { it, _ := s.Get(item.ID(ids["4711"])); st = it.State })
	if st != "open" || len(fake.Completed) != 0 {
		t.Fatalf("a refused completion moved the item to %q or completed %v", st, fake.Completed)
	}

	done := w.bert.expect("POST", path+"/complete", `{"variables":{"approved":"yes","comment":"enjoy"}}`, http.StatusOK)
	if done["completed"] != true || done["item"].(map[string]any)["state"] != "done" {
		t.Fatalf("complete = %v", done)
	}
	if len(fake.Completed) != 1 || fake.Completed[0].Person != "u-17" || fake.Completed[0].Variables["comment"] != "enjoy" {
		t.Errorf("the engine recorded %+v", fake.Completed)
	}
	var actor string
	w.db.Read(func(s *item.Store) {
		h := s.History(item.ID(ids["4711"]))
		actor = h[len(h)-1].Actor
	})
	if actor != w.ids["bert"] {
		t.Errorf("the transition is recorded as %q, want bert", actor)
	}
	// The task is gone at the engine now.
	if rec := w.bert.do("POST", path+"/complete", `{}`); rec.Code != http.StatusConflict {
		t.Errorf("completing an ended task: %d %s", rec.Code, rec.Body)
	}
	if got = w.bert.expect("GET", path, "", http.StatusOK); got["open"] != false {
		t.Errorf("an ended task is open: %v", got)
	}
}

func toJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
