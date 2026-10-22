package server

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestLabelsThroughTheAPI(t *testing.T) {
	w := newWorld(t)
	mk := func(title string) string {
		it := w.bert.expect("POST", "/api/items", `{"space":"PLAT","type":"task","title":"`+title+`","properties":{"priority":"low"}}`, http.StatusCreated)
		return field(it, "item", "key").(string)
	}
	a, b := mk("Patch"), mk("Rotate")
	got := w.bert.expect("PUT", "/api/items/"+a+"/labels", `{"labels":["Ops","security"]}`, http.StatusOK)
	if fmt.Sprint(field(got, "item", "labels")) != "[ops security]" {
		t.Fatalf("labels %v", got)
	}
	w.bert.expect("PUT", "/api/items/"+b+"/labels", `{"labels":["ops"]}`, http.StatusOK)
	w.bert.expect("PUT", "/api/items/"+b+"/labels", `{"labels":["no way!"]}`, http.StatusBadRequest)
	w.vera.expect("PUT", "/api/items/"+b+"/labels", `{"labels":["x"]}`, http.StatusForbidden)

	counts := w.vera.expect("GET", "/api/labels?space=PLAT", "", http.StatusOK)
	if fmt.Sprint(counts["labels"]) != "[map[count:2 label:ops] map[count:1 label:security]]" {
		t.Fatalf("counts %v", counts)
	}
	// Suggestions while typing: the labels holding what is typed.
	if got := w.vera.expect("GET", "/api/labels?q=Secu", "", http.StatusOK); fmt.Sprint(got["labels"]) != "[map[count:1 label:security]]" {
		t.Fatalf("suggested %v", got)
	}
	found := w.vera.expect("GET", "/api/items?label=security", "", http.StatusOK)["items"].([]any)
	if len(found) != 1 || found[0].(map[string]any)["key"] != a {
		t.Fatalf("found %v", found)
	}
	hits := w.vera.expect("GET", "/api/search?q=rotate&label=ops", "", http.StatusOK)["hits"].([]any)
	if len(hits) != 1 {
		t.Fatalf("hits %v", hits)
	}
	if got := w.carla.expect("GET", "/api/labels", "", http.StatusOK)["labels"].([]any); len(got) != 0 {
		t.Fatalf("an outsider counts %v", got)
	}
	hist := fmt.Sprint(w.vera.expect("GET", "/api/items/"+a+"/history", "", http.StatusOK))
	if !strings.Contains(hist, "LabelsSet") || !strings.Contains(hist, "security") {
		t.Fatalf("history %s", hist)
	}
}
