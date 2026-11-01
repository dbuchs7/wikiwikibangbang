package server

import (
	"net/http"
	"slices"
	"testing"

	"github.com/dbuchs7/orbithek/internal/budget"
)

// The starmap of a space (ADR-0157): its items, a relation and a shared
// label as nodes and edges, read with a read token too; cut at the budget
// and said so; an unknown space or one without a role is not found.
func TestStarmapThroughTheAPI(t *testing.T) {
	w := newWorld(t)
	b := w.bert
	create := func(title, parent string) string {
		it := b.expect("POST", "/api/items", `{"space":"PLAT","type":"task","title":"`+title+`","parent":"`+parent+`","properties":{"priority":"low"}}`, http.StatusCreated)
		return field(it, "item", "id").(string)
	}
	home := create("Home", "")
	child := create("Child", home)
	b.expect("POST", "/api/relations", `{"type":"blocks","from":"`+child+`","to":"`+home+`"}`, http.StatusCreated)
	for _, id := range []string{home, child} {
		b.expect("PUT", "/api/items/"+id+"/labels", `{"labels":["ops"]}`, http.StatusOK)
	}

	m := readToken(w.vera).expect("GET", "/api/spaces/PLAT/starmap", "", http.StatusOK)
	var ids []string
	for _, n := range m["nodes"].([]any) {
		ids = append(ids, n.(map[string]any)["id"].(string))
	}
	if !slices.Equal(ids, []string{home, child, "label:ops"}) || m["truncated"] != false {
		t.Fatalf("starmap %v", m)
	}
	first := m["nodes"].([]any)[0].(map[string]any)
	if first["kind"] != "item" || first["key"] != "PLAT-1" || first["title"] != "Home" || first["type"] != "task" ||
		first["space"] != "PLAT" || first["state"] != "open" || first["outside"] != nil {
		t.Errorf("node %v", first)
	}
	kinds := map[string]bool{}
	for _, e := range m["edges"].([]any) {
		e := e.(map[string]any)
		kinds[e["kind"].(string)] = true
		if e["kind"] == "relation" && (e["type"] != "blocks" || e["from"] != child || e["to"] != home) {
			t.Errorf("relation %v", e)
		}
	}
	if !kinds["parent"] || !kinds["relation"] || !kinds["label"] {
		t.Errorf("edges %v", m["edges"])
	}

	small := budget.Default()
	small.StarmapNodes = 1
	budget.Set(small)
	defer budget.Set(budget.Default())
	m = w.vera.expect("GET", "/api/spaces/PLAT/starmap", "", http.StatusOK)
	if len(m["nodes"].([]any)) != 1 || len(m["edges"].([]any)) != 0 || m["truncated"] != true {
		t.Errorf("truncated starmap %v", m)
	}

	w.vera.expect("GET", "/api/spaces/NOPE/starmap", "", http.StatusNotFound)
	w.carla.expect("GET", "/api/spaces/PLAT/starmap", "", http.StatusNotFound)
}
