package server

import (
	"fmt"
	"net/http"
	"testing"
)

func TestWatchingThroughTheAPI(t *testing.T) {
	w := newWorld(t)
	it := w.bert.expect("POST", "/api/items", `{"space":"PLAT","type":"task","title":"Patch","properties":{"priority":"low"}}`, http.StatusCreated)
	key := field(it, "item", "key").(string)

	w.vera.expect("PUT", "/api/items/"+key+"/watch", "", http.StatusOK)
	w.carla.expect("PUT", "/api/items/"+key+"/watch", "", http.StatusNotFound)
	w.carla.expect("PUT", "/api/spaces/PLAT/watch", "", http.StatusNotFound)
	watches := w.vera.expect("GET", "/api/me/watches", "", http.StatusOK)["watches"].([]any)
	if len(watches) != 1 || watches[0].(map[string]any)["key"] != key {
		t.Fatalf("watches %v", watches)
	}

	w.bert.expect("PUT", "/api/items/"+key+"/title", `{"title":"Patch the kernel"}`, http.StatusOK)
	w.bert.expect("POST", "/api/items/"+key+"/comments", `{"text":"done"}`, http.StatusCreated)
	got := w.vera.expect("GET", "/api/notifications", "", http.StatusOK)
	list := got["notifications"].([]any)
	if len(list) != 2 || got["unread"] != 2.0 {
		t.Fatalf("notifications %v", got)
	}
	newest := list[0].(map[string]any)
	if newest["kind"] != "CommentAdded" || newest["title"] != "Patch the kernel" || newest["actor"] != w.ids["bert"] || newest["unread"] != true {
		t.Fatalf("newest %v", newest)
	}
	seq := int(newest["seq"].(float64))
	w.vera.expect("POST", "/api/notifications/seen", fmt.Sprintf(`{"seq":%d}`, seq), http.StatusNoContent)
	if got := w.vera.expect("GET", "/api/notifications", "", http.StatusOK); got["unread"] != 0.0 {
		t.Fatalf("after seen %v", got)
	}
	page := w.vera.expect("GET", "/api/notifications?limit=1", "", http.StatusOK)
	if len(page["notifications"].([]any)) != 1 || page["next"] == nil {
		t.Fatalf("page %v", page)
	}
	w.vera.expect("GET", "/api/notifications?before=x", "", http.StatusBadRequest)

	// Nothing for someone who watches nothing, and stopping works.
	if got := w.carla.expect("GET", "/api/notifications", "", http.StatusOK)["notifications"].([]any); len(got) != 0 {
		t.Fatalf("outsider %v", got)
	}
	w.vera.expect("DELETE", "/api/items/"+key+"/watch", "", http.StatusOK)
	w.vera.expect("PUT", "/api/spaces/PLAT/watch", "", http.StatusOK)
	if got := w.vera.expect("GET", "/api/me/watches", "", http.StatusOK)["watches"].([]any); len(got) != 1 || got[0].(map[string]any)["space"] != "PLAT" {
		t.Fatalf("watches after the switch %v", got)
	}
}
