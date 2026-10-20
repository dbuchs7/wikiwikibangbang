package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/dbuchs7/wikiwikibangbang/internal/site"
)

// TestDataProtectionReviewThroughTheAPI: an administrator adopts a template;
// content with an indicator holds an open item for editors only, until a
// second person releases it (ADR-0058).
func TestDataProtectionReviewThroughTheAPI(t *testing.T) {
	w := newWorld(t)
	tpl := site.Templates[0]
	tpl.Active = true
	body, _ := json.Marshal(tpl)
	w.bert.expect("PUT", "/api/regulations/dsg", string(body), http.StatusForbidden)
	w.admin.expect("GET", "/api/regulations/templates", "", http.StatusOK)
	w.admin.expect("PUT", "/api/regulations/dsg", string(body), http.StatusOK)
	if regs := field(w.vera.expect("GET", "/api/regulations", "", http.StatusOK), "regulations").([]any); len(regs) != 1 {
		t.Fatalf("regulations %v", regs)
	}
	if r := w.admin.put("/api/regulations/dsg/document", "application/octet-stream", []byte("not a pdf")); r.Code != http.StatusBadRequest {
		t.Errorf("a document that is no PDF: %d", r.Code)
	}
	if r := w.admin.put("/api/regulations/dsg/document", "application/octet-stream", []byte("%PDF-1.7 law")); r.Code != http.StatusOK {
		t.Fatalf("document: %d %s", r.Code, r.Body)
	}
	if doc := w.vera.do("GET", "/api/regulations/dsg/document", ""); doc.Code != http.StatusOK || !strings.Contains(doc.Header().Get("Content-Security-Policy"), "sandbox") {
		t.Errorf("document read: %d", doc.Code)
	}

	// carla becomes a second editor, to release.
	w.admin.expect("PUT", "/api/spaces/PLAT/roles/"+w.ids["carla"], `{"role":"editor"}`, http.StatusNoContent)
	it := w.bert.expect("POST", "/api/items", `{"space":"PLAT","type":"task","title":"Absence","properties":{"priority":"low"},
		"body":[{"kind":"paragraph","text":"Arztzeugnis bis Freitag"}]}`, http.StatusCreated)
	id := field(it, "item", "id").(string)
	if field(it, "item", "hold") == nil {
		t.Fatalf("not held: %v", it)
	}
	w.vera.expect("GET", "/api/items/"+id, "", http.StatusNotFound)
	w.bert.expect("POST", "/api/items/"+id+"/hold/release", `{"outcome":"false-alarm","reason":"x"}`, http.StatusForbidden)
	w.carla.expect("POST", "/api/items/"+id+"/hold/release", `{"outcome":"restricted"}`, http.StatusBadRequest)
	gov := w.carla.expect("GET", "/api/spaces/PLAT/governance", "", http.StatusOK)
	if p := gov["personal"].([]any); len(p) != 1 || p[0].(map[string]any)["mayRelease"] != true {
		t.Fatalf("governance personal %v", gov["personal"])
	}
	n := w.carla.expect("GET", "/api/notifications?limit=5&findings=1", "", http.StatusOK)
	if field(n, "findings", "personal").(float64) != 1 {
		t.Errorf("the reviewer is not reminded: %v", n["findings"])
	}
	w.bert.expect("PUT", "/api/items/"+id+"/restriction", `{"read":["`+w.ids["bert"]+`","`+w.ids["carla"]+`"],"edit":[]}`, http.StatusOK)
	got := w.carla.expect("POST", "/api/items/"+id+"/hold/release", `{"outcome":"restricted"}`, http.StatusOK)
	if field(got, "item", "hold") != nil {
		t.Fatalf("still held: %v", got)
	}
	w.admin.expect("DELETE", "/api/regulations/dsg", "", http.StatusNoContent)
}
