package server

import (
	"bytes"
	"net/http"
	"testing"
)

// A kanban carries a picture or logo, set by editors, shown to everyone
// with a role in the space, and gone with the kanban (ADR-0087).
func TestKanbanIcons(t *testing.T) {
	w := newWorld(t)
	png := append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{3}, 40)...)
	k := w.bert.expect("POST", "/api/spaces/PLAT/kanbans", `{"name":"Website","type":"task"}`, http.StatusCreated)
	id := field(k, "kanban", "id").(string)
	path := "/api/spaces/PLAT/kanbans/" + id + "/icon"

	// Vera views PLAT, Bert edits it, Carla has no role.
	if r := w.vera.put(path, "application/octet-stream", png); r.Code != http.StatusForbidden {
		t.Fatalf("a viewer sets the picture: %d", r.Code)
	}
	if r := w.carla.put(path, "application/octet-stream", png); r.Code != http.StatusNotFound {
		t.Fatalf("an outsider sets the picture: %d", r.Code)
	}
	if r := w.bert.put("/api/spaces/PLAT/kanbans/nope/icon", "application/octet-stream", png); r.Code != http.StatusNotFound {
		t.Fatalf("a picture for no kanban: %d", r.Code)
	}
	if r := w.bert.put(path, "application/octet-stream", []byte("plain text")); r.Code != http.StatusBadRequest {
		t.Fatalf("text as picture: %d", r.Code)
	}
	w.vera.expect("GET", path, "", http.StatusNotFound)
	if r := w.bert.put(path, "application/octet-stream", png); r.Code != http.StatusOK {
		t.Fatalf("set: %d %s", r.Code, r.Body)
	}
	board := w.vera.expect("GET", "/api/spaces/PLAT/kanbans/"+id+"/board", "", http.StatusOK)
	if icon, _ := field(board, "kanban", "icon").(string); icon == "" {
		t.Fatalf("the board names the picture: %v", board)
	}
	list := field(w.vera.expect("GET", "/api/kanbans", "", http.StatusOK), "kanbans").([]any)
	if icon, _ := list[0].(map[string]any)["icon"].(string); icon == "" {
		t.Fatalf("the listing names the picture: %v", list)
	}
	if got := w.vera.do("GET", path, ""); got.Code != http.StatusOK || !bytes.Equal(got.Body.Bytes(), png) || got.Header().Get("Content-Security-Policy") == "" {
		t.Fatalf("get: %d", got.Code)
	}
	w.carla.expect("GET", path, "", http.StatusNotFound)

	w.vera.expect("DELETE", path, "", http.StatusForbidden)
	w.bert.expect("DELETE", path, "", http.StatusNoContent)
	w.vera.expect("GET", path, "", http.StatusNotFound)

	// Removing the kanban removes its picture.
	if r := w.bert.put(path, "application/octet-stream", png); r.Code != http.StatusOK {
		t.Fatalf("set again: %d", r.Code)
	}
	w.bert.expect("DELETE", "/api/spaces/PLAT/kanbans/"+id, "", http.StatusNoContent)
	if _, ok := w.db.Site.Asset("kanban-PLAT-" + id); ok {
		t.Fatal("the picture outlived its kanban")
	}
}
