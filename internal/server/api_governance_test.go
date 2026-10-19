package server

import (
	"net/http"
	"strings"
	"testing"
)

func TestGovernanceThroughTheAPI(t *testing.T) {
	w := newWorld(t)
	it := w.bert.expect("POST", "/api/items", `{"space":"PLAT","type":"task","title":"VPN","properties":{"priority":"low"}}`, http.StatusCreated)
	id := field(it, "item", "id").(string)

	// Saving a password restricts the item to its author at once.
	after := w.bert.expect("PUT", "/api/items/"+id+"/body", `{"blocks":[{"kind":"paragraph","text":"password: Sommer2026!"}]}`, http.StatusOK)
	if r := field(after, "item", "restriction", "read").([]any); len(r) != 1 || r[0] != w.ids["bert"] {
		t.Fatalf("restriction %v", field(after, "item", "restriction"))
	}
	w.vera.expect("GET", "/api/items/"+id, "", http.StatusNotFound)

	// The administrator sees the finding, masked, and lifts the restriction.
	rep := w.admin.expect("GET", "/api/spaces/PLAT/governance", "", http.StatusOK)
	sens := rep["sensitive"].([]any)
	if len(sens) != 1 {
		t.Fatalf("sensitive %v", rep)
	}
	f := sens[0].(map[string]any)
	if f["kind"] != "password" || f["where"] != "block 1" || f["responsible"] != w.ids["bert"] || f["restricted"] != true ||
		strings.Contains(f["masked"].(string), "Sommer2026") {
		t.Fatalf("finding %v", f)
	}
	w.admin.expect("PUT", "/api/items/"+id+"/restriction", `{"read":[],"edit":[]}`, http.StatusOK)
	w.vera.expect("GET", "/api/items/"+id, "", http.StatusOK)

	// staleDays bounds what counts as stale; nothing here is a day old.
	if rep := w.vera.expect("GET", "/api/spaces/PLAT/governance?staleDays=1", "", http.StatusOK); len(rep["stale"].([]any)) != 0 {
		t.Fatalf("stale %v", rep["stale"])
	}
	w.vera.expect("GET", "/api/spaces/PLAT/governance?staleDays=0", "", http.StatusBadRequest)
	w.carla.expect("GET", "/api/spaces/PLAT/governance", "", http.StatusNotFound)
}
