package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

// A catalog type (ADR-0074): url, unique and recommended fields, a review
// rule; items found by a property's value; reviews in the governance
// report and among one's findings.
func TestCatalogThroughTheAPI(t *testing.T) {
	w := newWorld(t)
	typ := w.admin.expect("POST", "/api/types", `{"name":"interface","body":true,"fields":[
		{"name":"source-id","kind":"text","unique":true},
		{"name":"specification","kind":"url"},
		{"name":"contact","kind":"text","recommended":true},
		{"name":"owner","kind":"user"},
		{"name":"reviewed","kind":"date"}],
		"review":{"everyDays":365,"field":"reviewed","owner":"owner"}}`, http.StatusCreated)
	if !strings.Contains(jsonOf(typ), `"unique":true`) || !strings.Contains(jsonOf(typ), `"everyDays":365`) {
		t.Fatalf("type %s", jsonOf(typ))
	}
	w.admin.expect("POST", "/api/types", `{"name":"bad","fields":[{"name":"d","kind":"text"}],"review":{"everyDays":30,"field":"d"}}`, http.StatusBadRequest)

	bert := w.ids["bert"]
	a := w.bert.expect("POST", "/api/items", `{"space":"PLAT","type":"interface","title":"Weather","properties":{"source-id":"meteo-1","specification":"https://example.org/openapi.json","owner":"`+bert+`"}}`, http.StatusCreated)
	key := field(a, "item", "key").(string)
	w.bert.expect("POST", "/api/items", `{"space":"PLAT","type":"interface","title":"Copy","properties":{"source-id":"meteo-1"}}`, http.StatusBadRequest)
	w.bert.expect("POST", "/api/items", `{"space":"PLAT","type":"interface","title":"Bad","properties":{"specification":"not a url"}}`, http.StatusBadRequest)
	w.bert.expect("POST", "/api/items", `{"space":"PLAT","type":"interface","title":"Other","properties":{"source-id":"meteo-2"}}`, http.StatusCreated)

	found := w.bert.expect("GET", "/api/items?space=PLAT&where=source-id%3Dmeteo-1", "", http.StatusOK)
	if items := found["items"].([]any); len(items) != 1 || items[0].(map[string]any)["key"] != key {
		t.Fatalf("where %s", jsonOf(found))
	}
	if found := w.bert.expect("GET", "/api/search?q=Weather&where=source-id%3Dmeteo-2", "", http.StatusOK); len(found["hits"].([]any)) != 0 {
		t.Fatalf("search with where %s", jsonOf(found))
	}
	w.bert.expect("GET", "/api/items?where=nothing", "", http.StatusBadRequest)
	w.bert.expect("GET", "/api/search?q=x&where==x", "", http.StatusBadRequest)

	gov := w.bert.expect("GET", "/api/spaces/PLAT/governance", "", http.StatusOK)
	reviews := gov["reviews"].([]any)
	if len(reviews) != 2 || reviews[0].(map[string]any)["responsible"] != bert || reviews[0].(map[string]any)["lastReview"] != nil {
		t.Fatalf("reviews %s", jsonOf(gov["reviews"]))
	}
	if !strings.Contains(jsonOf(gov["implausible"]), `recommended field \"contact\" is empty`) {
		t.Fatalf("implausible %s", jsonOf(gov["implausible"]))
	}
	notes := w.bert.expect("GET", "/api/notifications?limit=1&findings=1", "", http.StatusOK)
	if field(notes, "findings", "reviews").(float64) != 2 {
		t.Fatalf("bert's findings %s", jsonOf(notes["findings"]))
	}

	w.vera.expect("POST", "/api/items/"+key+"/reviewed", "", http.StatusForbidden)
	got := w.bert.expect("POST", "/api/items/"+key+"/reviewed", "", http.StatusOK)
	if !strings.HasPrefix(field(got, "item", "properties", "reviewed").(string), time.Now().UTC().Format(time.DateOnly)) {
		t.Fatalf("reviewed %s", jsonOf(got))
	}
	if gov := w.bert.expect("GET", "/api/spaces/PLAT/governance", "", http.StatusOK); len(gov["reviews"].([]any)) != 1 {
		t.Fatalf("after the review %s", jsonOf(gov["reviews"]))
	}
	w.bert.expect("POST", "/api/items/PLAT-999/reviewed", "", http.StatusNotFound)
}

func jsonOf(v any) string {
	data, _ := json.Marshal(v)
	return string(data)
}
