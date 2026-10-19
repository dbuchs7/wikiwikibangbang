package server

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestCommentsThroughTheAPI(t *testing.T) {
	w := newWorld(t)
	it := w.bert.expect("POST", "/api/items", `{"space":"PLAT","type":"task","title":"Patch","properties":{"priority":"low"}}`, http.StatusCreated)
	key := field(it, "item", "key").(string)
	path := "/api/items/" + key + "/comments"

	c := w.bert.expect("POST", path, `{"text":"<script>alert(1)</script> ok"}`, http.StatusCreated)
	id := field(c, "comment", "id").(string)
	if field(c, "comment", "author") != w.ids["bert"] || field(c, "comment", "text") != "<script>alert(1)</script> ok" {
		t.Fatalf("created %v", c)
	}
	w.bert.expect("PUT", path+"/"+id, `{"text":"edited"}`, http.StatusOK)

	// A viewer reads and may not write; someone without a role sees nothing.
	list := w.vera.expect("GET", path, "", http.StatusOK)
	got := list["comments"].([]any)
	if len(got) != 1 || got[0].(map[string]any)["text"] != "edited" || got[0].(map[string]any)["edited"] == nil {
		t.Fatalf("viewer lists %v", list)
	}
	w.vera.expect("POST", path, `{"text":"x"}`, http.StatusForbidden)
	w.carla.expect("GET", path, "", http.StatusNotFound)
	w.carla.expect("POST", path, `{"text":"x"}`, http.StatusNotFound)

	// Only the author edits and deletes; empty text is refused.
	w.admin.expect("PUT", path+"/"+id, `{"text":"x"}`, http.StatusForbidden)
	w.bert.expect("POST", path, `{"text":"  "}`, http.StatusBadRequest)
	w.bert.expect("PUT", path+"/nope", `{"text":"x"}`, http.StatusNotFound)
	w.bert.expect("DELETE", path+"/"+id, "", http.StatusNoContent)
	deleted := w.bert.expect("GET", path, "", http.StatusOK)["comments"].([]any)[0].(map[string]any)
	if deleted["deleted"] != true || deleted["text"] != nil {
		t.Fatalf("a deleted comment shows %v", deleted)
	}

	// The history names the comment events without their text.
	hist := w.bert.expect("GET", "/api/items/"+key+"/history", "", http.StatusOK)
	s := fmt.Sprint(hist)
	if !strings.Contains(s, "CommentAdded") || !strings.Contains(s, "CommentDeleted") || strings.Contains(s, "edited") {
		t.Fatalf("history %s", s)
	}
}

func TestCommentListingsArePaged(t *testing.T) {
	w := newWorld(t)
	it := w.bert.expect("POST", "/api/items", `{"space":"PLAT","type":"task","title":"Patch","properties":{"priority":"low"}}`, http.StatusCreated)
	path := "/api/items/" + field(it, "item", "id").(string) + "/comments"
	for i := range 3 {
		w.bert.expect("POST", path, fmt.Sprintf(`{"text":"comment %d"}`, i), http.StatusCreated)
	}
	first := w.bert.expect("GET", path+"?limit=2", "", http.StatusOK)
	if len(first["comments"].([]any)) != 2 || first["next"] == nil || field(first, "total", "value") != 3.0 {
		t.Fatalf("first page %v", first)
	}
	rest := w.bert.expect("GET", path+"?limit=2&after="+first["next"].(string), "", http.StatusOK)
	if c := rest["comments"].([]any); len(c) != 1 || c[0].(map[string]any)["text"] != "comment 2" || rest["next"] != nil {
		t.Fatalf("second page %v", rest)
	}
	w.bert.expect("GET", path+"?limit=0", "", http.StatusBadRequest)
}
