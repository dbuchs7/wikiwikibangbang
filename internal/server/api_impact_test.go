package server

import (
	"net/http"
	"slices"
	"testing"

	"github.com/dbuchs7/orbithek/internal/budget"
)

// The impact of an item (ADR-0159): what depends on it and what it
// depends on, by key or ID, to a depth or all, read with a read token too,
// cut at the budget; a bad direction or depth is refused, an unknown or
// unreadable item not found.
func TestImpactThroughTheAPI(t *testing.T) {
	w := newWorld(t)
	b := w.bert
	create := func(title, parent string) string {
		it := b.expect("POST", "/api/items", `{"space":"PLAT","type":"task","title":"`+title+`","parent":"`+parent+`","properties":{"priority":"low"}}`, http.StatusCreated)
		return field(it, "item", "id").(string)
	}
	base := create("Base", "")
	mid := create("Middle", "")
	top := create("Top", "")
	b.expect("POST", "/api/relations", `{"type":"depends-on","from":"`+mid+`","to":"`+base+`"}`, http.StatusCreated)
	b.expect("POST", "/api/relations", `{"type":"depends-on","from":"`+top+`","to":"`+mid+`"}`, http.StatusCreated)

	ids := func(m map[string]any) (out []string) {
		for _, n := range m["nodes"].([]any) {
			n := n.(map[string]any)
			out = append(out, n["id"].(string)+"@"+string(rune('0'+int(n["distance"].(float64)))))
		}
		return out
	}
	m := readToken(w.vera).expect("GET", "/api/items/PLAT-1/impact", "", http.StatusOK)
	if got := ids(m); !slices.Equal(got, []string{base + "@0", mid + "@1"}) || m["direction"] != "affected" || m["depth"] != float64(1) ||
		m["complete"] != false || m["truncated"] != false || m["start"] != base || m["space"] != "PLAT" {
		t.Fatalf("depth 1 %v", m)
	}
	via := m["nodes"].([]any)[1].(map[string]any)["via"].([]any)[0].(map[string]any)
	if via["kind"] != "relation" || via["type"] != "depends-on" || via["from"] != mid || via["to"] != base {
		t.Errorf("via %v", via)
	}
	m = w.vera.expect("GET", "/api/items/"+base+"/impact?direction=affected&depth=all", "", http.StatusOK)
	if got := ids(m); !slices.Equal(got, []string{base + "@0", mid + "@1", top + "@2"}) || m["depth"] != "all" || m["complete"] != true || len(m["edges"].([]any)) != 2 {
		t.Errorf("all %v", m)
	}
	m = w.vera.expect("GET", "/api/items/PLAT-3/impact?direction=dependson&depth=5", "", http.StatusOK)
	if got := ids(m); !slices.Equal(got, []string{top + "@0", mid + "@1", base + "@2"}) || m["complete"] != true {
		t.Errorf("depends on %v", m)
	}

	small := budget.Default()
	small.ImpactNodes = 2
	budget.Set(small)
	defer budget.Set(budget.Default())
	m = w.vera.expect("GET", "/api/items/PLAT-1/impact?depth=all", "", http.StatusOK)
	if len(m["nodes"].([]any)) != 2 || m["truncated"] != true || m["complete"] != false {
		t.Errorf("truncated %v", m)
	}

	w.vera.expect("GET", "/api/items/PLAT-1/impact?direction=sideways", "", http.StatusBadRequest)
	for _, d := range []string{"0", "6", "x"} {
		w.vera.expect("GET", "/api/items/PLAT-1/impact?depth="+d, "", http.StatusBadRequest)
	}
	w.vera.expect("GET", "/api/items/PLAT-99/impact", "", http.StatusNotFound)
	w.vera.expect("GET", "/api/items/PLAT-1/impact?picture=nope", "", http.StatusNotFound)
	w.carla.expect("GET", "/api/items/PLAT-1/impact", "", http.StatusNotFound)
}
