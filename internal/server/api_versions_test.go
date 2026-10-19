package server

import (
	"fmt"
	"net/http"
	"testing"
)

func TestBodyVersionsThroughTheAPI(t *testing.T) {
	w := newWorld(t)
	it := w.bert.expect("POST", "/api/items", `{"space":"PLAT","type":"task","title":"Patch","properties":{"priority":"low"}}`, http.StatusCreated)
	key := field(it, "item", "key").(string)
	w.bert.expect("PUT", "/api/items/"+key+"/body", `{"blocks":[{"kind":"paragraph","text":"first"}]}`, http.StatusOK)
	w.bert.expect("PUT", "/api/items/"+key+"/body", `{"blocks":[{"kind":"task","text":"second","checked":true}]}`, http.StatusOK)
	var seqs []int
	for _, e := range w.bert.expect("GET", "/api/items/"+key+"/history", "", http.StatusOK)["events"].([]any) {
		if ev := e.(map[string]any); ev["kind"] == "BodyCommitted" {
			seqs = append(seqs, int(ev["seq"].(float64)))
		}
	}
	if len(seqs) != 2 {
		t.Fatalf("body commits %v", seqs)
	}
	v := w.vera.expect("GET", fmt.Sprintf("/api/items/%s/versions/%d", key, seqs[0]), "", http.StatusOK)
	body := v["body"].([]any)
	if len(body) != 1 || body[0].(map[string]any)["text"] != "first" || v["actor"] != w.ids["bert"] {
		t.Fatalf("version %v", v)
	}
	w.vera.expect("GET", "/api/items/"+key+"/versions/x", "", http.StatusBadRequest)
	w.vera.expect("GET", fmt.Sprintf("/api/items/%s/versions/%d", key, seqs[0]-1), "", http.StatusNotFound)
	w.carla.expect("GET", fmt.Sprintf("/api/items/%s/versions/%d", key, seqs[0]), "", http.StatusNotFound)

	w.vera.expect("POST", fmt.Sprintf("/api/items/%s/versions/%d/restore", key, seqs[0]), "", http.StatusForbidden)
	w.bert.expect("POST", fmt.Sprintf("/api/items/%s/versions/%d/restore", key, seqs[0]), "", http.StatusOK)
	now := w.bert.expect("GET", "/api/items/"+key, "", http.StatusOK)
	if b := field(now, "item", "body").([]any); len(b) != 1 || b[0].(map[string]any)["text"] != "first" {
		t.Fatalf("after restoring %v", now)
	}
}
