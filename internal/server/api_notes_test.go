package server

import (
	"net/http"
	"strings"
	"testing"
)

// TestPersonalNotesThroughTheAPI: a note is its maker's alone; no one else
// lists, changes or deletes it, and it shows in no shared view of the
// item; taking away access hides it (ADR-0107).
func TestPersonalNotesThroughTheAPI(t *testing.T) {
	w := newWorld(t)
	it := w.bert.expect("POST", "/api/items", `{"space":"PLAT","type":"task","title":"Budget","properties":{"priority":"low"}}`, http.StatusCreated)
	key, id := field(it, "item", "key").(string), field(it, "item", "id").(string)
	w.bert.expect("PUT", "/api/items/"+key+"/body", `{"blocks":[{"kind":"paragraph","text":"The budget for 2027 is fixed."}]}`, http.StatusOK)

	const private = "Ask Paul about the reserve"
	n := w.vera.expect("POST", "/api/items/"+key+"/notes", `{"anchor":{"quote":"budget","prefix":"The "},"text":"`+private+`"}`, http.StatusCreated)
	note := field(n, "note", "id").(string)
	w.vera.expect("POST", "/api/items/"+key+"/notes", `{"anchor":{"quote":"budget"},"text":" "}`, http.StatusBadRequest)
	w.carla.expect("POST", "/api/items/"+key+"/notes", `{"anchor":{"quote":"budget"},"text":"x"}`, http.StatusNotFound)

	if got := field(w.vera.expect("GET", "/api/items/"+key+"/notes", "", http.StatusOK), "notes").([]any); len(got) != 1 || got[0].(map[string]any)["text"] != private {
		t.Fatalf("vera's notes: %v", got)
	}
	if got := field(w.bert.expect("GET", "/api/items/"+key+"/notes", "", http.StatusOK), "notes").([]any); len(got) != 0 {
		t.Fatalf("bert sees vera's note: %v", got)
	}
	w.bert.expect("PUT", "/api/items/"+key+"/notes/"+note, `{"text":"mine now"}`, http.StatusNotFound)
	w.bert.expect("DELETE", "/api/items/"+key+"/notes/"+note, "", http.StatusNotFound)
	w.admin.expect("DELETE", "/api/items/"+key+"/notes/"+note, "", http.StatusNotFound)
	for _, path := range []string{"/api/items/" + key, "/api/items/" + key + "/history", "/api/items/" + key + "/comments", "/api/search?q=reserve", "/api/notifications"} {
		for _, c := range []*client{w.bert, w.admin, w.vera} {
			if body := jsonOf(c.expect("GET", path, "", http.StatusOK)); strings.Contains(body, private) {
				t.Errorf("%s shows the note: %s", path, body)
			}
		}
	}

	if e := w.vera.expect("PUT", "/api/items/"+key+"/notes/"+note, `{"text":"Ask Paul"}`, http.StatusOK); field(e, "note", "text") != "Ask Paul" {
		t.Errorf("edited %v", e)
	}
	mine := field(w.vera.expect("GET", "/api/notes", "", http.StatusOK), "notes").([]any)
	if len(mine) != 1 || mine[0].(map[string]any)["key"] != key {
		t.Fatalf("all of vera's notes: %v", mine)
	}

	// Access taken away hides one's notes on the item.
	w.bert.expect("PUT", "/api/items/"+id+"/restriction", `{"read":["`+w.ids["bert"]+`"],"edit":[]}`, http.StatusOK)
	if got := field(w.vera.expect("GET", "/api/notes", "", http.StatusOK), "notes").([]any); len(got) != 0 {
		t.Errorf("notes on an item one may no longer read: %v", got)
	}
	w.vera.expect("GET", "/api/items/"+key+"/notes", "", http.StatusNotFound)
	w.bert.expect("PUT", "/api/items/"+id+"/restriction", `{"read":[],"edit":[]}`, http.StatusOK)
	w.vera.expect("DELETE", "/api/items/"+key+"/notes/"+note, "", http.StatusNoContent)
	if got := field(w.vera.expect("GET", "/api/notes", "", http.StatusOK), "notes").([]any); len(got) != 0 {
		t.Errorf("after deleting: %v", got)
	}
}
