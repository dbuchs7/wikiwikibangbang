package server

import (
	"net/http"
	"strings"
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

// A kanban's own columns, its cards' fields, and cards put in columns
// (ADR-0076).
func TestKanbanColumnsThroughTheAPI(t *testing.T) {
	w := newWorld(t)
	k := w.bert.expect("POST", "/api/spaces/PLAT/kanbans", `{"name":"Web","type":"task"}`, http.StatusCreated)
	id := field(k, "kanban", "id").(string)
	path := "/api/spaces/PLAT/kanbans/" + id
	card := w.bert.expect("POST", "/api/items", `{"space":"PLAT","type":"task","title":"Card","properties":{"priority":"low"}}`, http.StatusCreated)
	key := field(card, "item", "key").(string)
	w.bert.expect("PUT", "/api/items/"+key+"/labels", `{"labels":["web"]}`, http.StatusOK)

	board := w.bert.expect("GET", path+"/board", "", http.StatusOK)
	cols := board["columns"].([]any)
	if len(cols) != 2 || field(cols[0].(map[string]any), "id") != "state:open" || len(cols[0].(map[string]any)["cards"].([]any)) != 1 {
		t.Fatalf("board %s", jsonOf(board))
	}
	got := w.bert.expect("PUT", path, `{"name":"Web","columns":[{"name":"Backlog"},{"name":"To do","state":"open"},{"name":"Done","state":"done"}],"cardFields":["priority","labels"]}`, http.StatusOK)
	backlog := field(got, "kanban", "columns").([]any)[0].(map[string]any)["id"].(string)
	if !strings.Contains(jsonOf(got), `"cardFields":["priority","labels"]`) || backlog == "" {
		t.Fatalf("kanban %s", jsonOf(got))
	}
	w.bert.expect("PUT", path, `{"name":"Web","cardFields":["colour"]}`, http.StatusBadRequest)
	if got := w.bert.expect("PUT", path, `{"name":"Website"}`, http.StatusOK); len(field(got, "kanban", "columns").([]any)) != 3 {
		t.Fatalf("renaming lost the columns %s", jsonOf(got))
	}

	w.vera.expect("POST", path+"/place", `{"card":"`+key+`","column":"`+backlog+`"}`, http.StatusForbidden)
	w.bert.expect("POST", path+"/place", `{"card":"`+key+`","column":"`+backlog+`"}`, http.StatusNoContent)
	w.bert.expect("POST", path+"/place", `{"card":"`+key+`","column":"`+backlog+`","before":"PLAT-999"}`, http.StatusNotFound)
	w.bert.expect("POST", path+"/place", `{"card":"PLAT-999","column":"`+backlog+`"}`, http.StatusNotFound)
	board = w.bert.expect("GET", path+"/board", "", http.StatusOK)
	if first := board["columns"].([]any)[0].(map[string]any); first["name"] != "Backlog" || len(first["cards"].([]any)) != 1 {
		t.Fatalf("placed %s", jsonOf(board))
	}
	w.bert.expect("POST", path+"/place", `{"card":"`+key+`","column":"state:done"}`, http.StatusNoContent)
	w.bert.expect("POST", path+"/place", `{"card":"`+key+`","column":"state:open"}`, http.StatusBadRequest)

	// An administrator adds the missing move; then it works.
	w.bert.expect("POST", "/api/types/task/transitions", `{"from":"done","to":"open"}`, http.StatusForbidden)
	tr := w.admin.expect("POST", "/api/types/task/transitions", `{"from":"done","to":"open"}`, http.StatusOK)
	if !strings.Contains(jsonOf(tr), `"name":"to open"`) {
		t.Fatalf("type %s", jsonOf(tr))
	}
	w.bert.expect("POST", path+"/place", `{"card":"`+key+`","column":"state:open"}`, http.StatusNoContent)
	w.carla.expect("GET", path+"/board", "", http.StatusNotFound)
}
