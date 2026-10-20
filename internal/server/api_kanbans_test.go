package server

import (
	"net/http"
	"testing"
)

// TestKanbansThroughTheAPI: editors make, rename and remove kanbans;
// readers list them; a card joins a kanban through its label (ADR-0060).
func TestKanbansThroughTheAPI(t *testing.T) {
	w := newWorld(t)
	w.vera.expect("POST", "/api/spaces/PLAT/kanbans", `{"name":"Web","type":"task"}`, http.StatusForbidden)
	w.bert.expect("POST", "/api/spaces/PLAT/kanbans", `{"name":"Web"}`, http.StatusBadRequest)
	k := w.bert.expect("POST", "/api/spaces/PLAT/kanbans", `{"name":"Website Relaunch","type":"task"}`, http.StatusCreated)
	id := field(k, "kanban", "id").(string)
	if field(k, "kanban", "label") != "website-relaunch" {
		t.Fatalf("kanban %v", k)
	}
	w.bert.expect("PUT", "/api/spaces/PLAT/kanbans/"+id, `{"name":"Website"}`, http.StatusOK)
	list := field(w.vera.expect("GET", "/api/kanbans", "", http.StatusOK), "kanbans").([]any)
	if len(list) != 1 || list[0].(map[string]any)["name"] != "Website" {
		t.Fatalf("kanbans %v", list)
	}
	if got := field(w.carla.expect("GET", "/api/kanbans", "", http.StatusOK), "kanbans").([]any); len(got) != 0 {
		t.Errorf("an outsider lists %v", got)
	}
	it := w.bert.expect("POST", "/api/items", `{"space":"PLAT","type":"task","title":"Hero image","properties":{"priority":"low"}}`, http.StatusCreated)
	key := field(it, "item", "key").(string)
	w.bert.expect("PUT", "/api/items/"+key+"/labels", `{"labels":["website-relaunch","ops"]}`, http.StatusOK)
	cards := field(w.vera.expect("GET", "/api/items?space=PLAT&type=task&label=website-relaunch", "", http.StatusOK), "items").([]any)
	if len(cards) != 1 {
		t.Errorf("cards %v", cards)
	}
	w.vera.expect("DELETE", "/api/spaces/PLAT/kanbans/"+id, "", http.StatusForbidden)
	w.bert.expect("DELETE", "/api/spaces/PLAT/kanbans/"+id, "", http.StatusNoContent)
}
