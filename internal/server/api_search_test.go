package server

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func searchKeys(t *testing.T, res map[string]any) []string {
	t.Helper()
	var keys []string
	for _, h := range res["hits"].([]any) {
		keys = append(keys, field(h.(map[string]any), "item", "key").(string))
	}
	return keys
}

func TestSearchThroughTheAPI(t *testing.T) {
	w := newWorld(t)
	b := w.bert
	for _, title := range []string{"Patch the kernel", "Kernel upgrade notes", "Unrelated"} {
		b.expect("POST", "/api/items", `{"space":"PLAT","type":"task","title":"`+title+`","properties":{"priority":"low"}}`, http.StatusCreated)
	}
	b.expect("PUT", "/api/items/PLAT-3/body", `{"blocks":[{"kind":"paragraph","text":"Mentions the kernel only in passing."}]}`, http.StatusOK)

	res := w.vera.expect("GET", "/api/search?q=kernel", "", http.StatusOK)
	if got := strings.Join(searchKeys(t, res), ","); got != "PLAT-2,PLAT-1,PLAT-3" {
		t.Fatalf("hits %s", got) // titles first, newer first among equals
	}
	last := res["hits"].([]any)[2].(map[string]any)
	if last["snippet"] != "Mentions the kernel only in passing." {
		t.Fatalf("snippet %v", last["snippet"])
	}
	if field(res, "total", "value") != 3.0 {
		t.Fatalf("total %v", res["total"])
	}

	page := w.vera.expect("GET", "/api/search?q=kernel&limit=2", "", http.StatusOK)
	next := page["next"].(string)
	rest := w.vera.expect("GET", "/api/search?q=kernel&limit=2&after="+url.QueryEscape(next), "", http.StatusOK)
	if got := strings.Join(searchKeys(t, rest), ","); got != "PLAT-3" {
		t.Fatalf("second page %s", got)
	}

	// Someone without a role finds nothing, not even that items exist.
	if got := w.carla.expect("GET", "/api/search?q=kernel", "", http.StatusOK); len(got["hits"].([]any)) != 0 || field(got, "total", "value") != 0.0 {
		t.Fatalf("carla finds %v", got)
	}

	w.vera.expect("GET", "/api/search?q=", "", http.StatusBadRequest)
	w.vera.expect("GET", "/api/search?q=kernel&limit=0", "", http.StatusBadRequest)
	w.vera.expect("GET", "/api/search?q=kernel&after=nope", "", http.StatusBadRequest)
	anon := &client{t: t, h: b.h}
	anon.expect("GET", "/api/search?q=kernel", "", http.StatusUnauthorized)
}
