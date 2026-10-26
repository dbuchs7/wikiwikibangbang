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

// TestKanbanLimitsColoursAndCardSigns: a column's limit and a kanban's
// colouring go through the API and come back on the board; a card in the
// listing tells when it reached its state and how many comments and
// files it has (ADR-0134).
func TestKanbanLimitsColoursAndCardSigns(t *testing.T) {
	w := newWorld(t)
	k := w.bert.expect("POST", "/api/spaces/PLAT/kanbans", `{"name":"Web","type":"task"}`, http.StatusCreated)
	path := "/api/spaces/PLAT/kanbans/" + field(k, "kanban", "id").(string)
	got := w.bert.expect("PUT", path, `{"name":"Web","columns":[{"name":"To do","state":"open","limit":3},{"name":"Done","state":"done"}],"colorField":"priority","colors":{"high":"red"}}`, http.StatusOK)
	if s := jsonOf(got); !strings.Contains(s, `"limit":3`) || !strings.Contains(s, `"colorField":"priority"`) || !strings.Contains(s, `"colors":{"high":"red"}`) {
		t.Fatalf("kanban %s", s)
	}
	if s := jsonOf(w.vera.expect("GET", path+"/board", "", http.StatusOK)); !strings.Contains(s, `"limit":3`) || !strings.Contains(s, `"colorField":"priority"`) {
		t.Fatalf("board %s", s)
	}
	// A rename keeps the colouring; an empty field takes it off.
	if s := jsonOf(w.bert.expect("PUT", path, `{"name":"Website"}`, http.StatusOK)); !strings.Contains(s, `"colorField":"priority"`) {
		t.Fatalf("rename %s", s)
	}
	if s := jsonOf(w.bert.expect("PUT", path, `{"name":"Website","colorField":""}`, http.StatusOK)); strings.Contains(s, `"colorField"`) || strings.Contains(s, `"colors"`) {
		t.Fatalf("off %s", s)
	}
	w.bert.expect("PUT", path, `{"name":"Website","colorField":"priority","colors":{"high":"pink"}}`, http.StatusBadRequest)
	w.bert.expect("PUT", path, `{"name":"Website","columns":[{"name":"To do","state":"open","limit":-1}]}`, http.StatusBadRequest)

	it := w.bert.expect("POST", "/api/items", `{"space":"PLAT","type":"task","title":"Card","properties":{"priority":"low"}}`, http.StatusCreated)
	key := field(it, "item", "key").(string)
	if field(it, "item", "stateSince") == nil || strings.Contains(jsonOf(it), "commentCount") || strings.Contains(jsonOf(it), "fileCount") {
		t.Fatalf("a new card has reached its state and has no comments or files: %s", jsonOf(it))
	}
	w.bert.expect("POST", "/api/items/"+key+"/comments", `{"text":"one"}`, http.StatusCreated)
	c := w.bert.expect("POST", "/api/items/"+key+"/comments", `{"text":"two"}`, http.StatusCreated)
	w.bert.expect("DELETE", "/api/items/"+key+"/comments/"+field(c, "comment", "id").(string), "", http.StatusNoContent)
	if rec := w.bert.upload("/api/items/"+key+"/attachments?name=a.txt", "application/octet-stream", []byte("text")); rec.Code != http.StatusCreated {
		t.Fatalf("upload %d %s", rec.Code, rec.Body)
	}
	listed := field(w.vera.expect("GET", "/api/items?space=PLAT&type=task", "", http.StatusOK), "items").([]any)[0].(map[string]any)
	if listed["commentCount"] != float64(1) || listed["fileCount"] != float64(1) || listed["stateSince"] == nil {
		t.Fatalf("a listed card %s", jsonOf(listed))
	}
}
