package server

import (
	"net/http"
	"testing"

	"github.com/dbuchs7/orbithek/internal/engine"
	"github.com/dbuchs7/orbithek/internal/engineauth/engineauthtest"
	"github.com/dbuchs7/orbithek/internal/item"
)

// TestRemovingAnEngineHandsItsTasksBackToTheWiki covers ADR-0152's gap in
// ADR-0151: an open linked item may be finished only at its engine, so
// removing the engine — or giving it another address — detaches its items:
// they keep their place and state, say which engine they came from, and
// may then be finished as any item.
func TestRemovingAnEngineHandsItsTasksBackToTheWiki(t *testing.T) {
	fake := engineauthtest.New(t)
	fake.ListToken = "lister"
	fake.Tasks = []map[string]any{
		{"key": 4711, "processInstanceKey": 1, "elementInstanceKey": 2, "processId": "urlaub", "elementId": "genehmigen", "name": "Urlaub genehmigen"},
		{"key": 4712, "processInstanceKey": 3, "elementInstanceKey": 4, "processId": "urlaub", "elementId": "genehmigen", "name": "Urlaub von Ben"},
	}
	env := map[string]string{engine.TaskListVar("atlas"): "lister", engine.TaskListVar("other"): "lister"}
	w := newWorld(t, func(d *Deps) { d.Engines = engine.NewReader(func(k string) string { return env[k] }, fake.Client()) })
	w.admin.expect("POST", "/api/types", `{"name":"engine-task","body":true,"workflow":{"initial":"open","states":["open","done","cancelled"],
		"transitions":[{"name":"finish","from":["open"],"to":"done"},{"name":"cancel","from":["open"],"to":"cancelled"}]}}`, http.StatusCreated)
	links := `{"enabled":true,"rules":[{"id":"holidays","space":"PLAT","type":"engine-task","completed":"done","cancelled":"cancelled"}]}`
	for _, id := range []string{"atlas", "other"} {
		w.admin.expect("PUT", "/api/engines/"+id, `{"name":"Atlas","kind":"atlas","baseURL":"`+fake.URL+`"}`, http.StatusCreated)
		w.admin.expect("PUT", "/api/engines/"+id+"/task-links", links, http.StatusOK)
		w.admin.expect("POST", "/api/engines/"+id+"/task-links/run", "", http.StatusOK)
	}
	var ids []string
	w.db.Read(func(s *item.Store) {
		for _, it := range s.LinkedTasks("atlas") {
			ids = append(ids, string(it.ID))
		}
	})
	if len(ids) != 2 {
		t.Fatalf("linked items of atlas = %v", ids)
	}
	path := "/api/items/" + ids[0]
	w.admin.expect("POST", path+"/transitions", `{"name":"finish"}`, http.StatusForbidden)

	w.admin.expect("DELETE", "/api/engines/atlas", "", http.StatusNoContent)
	got, _ := w.admin.expect("GET", path, "", http.StatusOK)["item"].(map[string]any)
	if got["linked"] != nil || got["detached"] != "atlas" || got["state"] != "open" {
		t.Fatalf("after removing its engine the item is %v", got)
	}
	w.admin.expect("POST", path+"/transitions", `{"name":"finish"}`, http.StatusOK)
	var st string
	w.db.Read(func(s *item.Store) { it, _ := s.Get(item.ID(ids[0])); st = it.State })
	if st != "done" {
		t.Errorf("a detached item finished is %q", st)
	}
	var otherLinked int
	w.db.Read(func(s *item.Store) { otherLinked = len(s.LinkedTasks("other")) })
	if otherLinked != 2 {
		t.Errorf("another engine's tasks were detached: %d left linked", otherLinked)
	}

	// Another address detaches too: the grants and tasks belong to the
	// engine at its address.
	w.admin.expect("PUT", "/api/engines/other", `{"name":"Atlas","kind":"atlas","baseURL":"https://atlas.example.org"}`, http.StatusOK)
	w.db.Read(func(s *item.Store) { otherLinked = len(s.LinkedTasks("other")) })
	if otherLinked != 0 {
		t.Errorf("after the address changed, %d items are still linked", otherLinked)
	}
}
