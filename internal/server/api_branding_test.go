package server

import (
	"bytes"
	"net/http"
	"strings"
	"testing"
)

func TestTheCorporateDesignThroughTheAPI(t *testing.T) {
	w := newWorld(t)
	anon := &client{t: t, h: w.admin.h}
	w.bert.expect("PUT", "/api/branding", `{"accent":"#d40000"}`, http.StatusForbidden)
	w.admin.expect("PUT", "/api/branding", `{"accent":"red"}`, http.StatusBadRequest)
	got := w.admin.expect("PUT", "/api/branding", `{"name":"ACME","accent":"#d40000","font":"serif","css":".brand{gap:2px}"}`, http.StatusOK)
	if got["name"] != "ACME" || len(got["contrast"].([]any)) != 0 {
		t.Fatalf("set %v", got)
	}
	// Colours hard to read are kept, with a warning naming the pair.
	warned := w.admin.expect("PUT", "/api/branding", `{"accent":"#ffcc00","headerBackground":"#336699"}`, http.StatusOK)
	if c := warned["contrast"].([]any); len(c) != 2 || c[0].(map[string]any)["pair"] != "headerText" || c[1].(map[string]any)["pair"] != "accentOnSurface" {
		t.Fatalf("contrast %v", warned["contrast"])
	}
	w.admin.expect("PUT", "/api/branding", `{"name":"ACME","accent":"#d40000","font":"serif","css":".brand{gap:2px}"}`, http.StatusOK)
	// Reading is public: the sign-in page carries the design.
	if b := anon.expect("GET", "/branding.json", "", http.StatusOK); b["accent"] != "#d40000" {
		t.Fatalf("branding.json %v", b)
	}
	css := anon.do("GET", "/branding.css", "")
	if css.Code != http.StatusOK || !strings.HasPrefix(css.Header().Get("Content-Type"), "text/css") || !strings.Contains(css.Body.String(), "--accent: #d40000") || !strings.Contains(css.Body.String(), ".brand{gap:2px}") {
		t.Fatalf("branding.css %d %s", css.Code, css.Body)
	}

	png := append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{1}, 40)...)
	if r := w.admin.put("/api/branding/logo", "application/octet-stream", png); r.Code != http.StatusOK {
		t.Fatalf("logo %d %s", r.Code, r.Body)
	}
	if r := w.admin.put("/api/branding/font", "application/octet-stream", png); r.Code != http.StatusBadRequest {
		t.Fatalf("a picture as font: %d", r.Code)
	}
	if r := w.bert.put("/api/branding/logo", "application/octet-stream", png); r.Code != http.StatusForbidden {
		t.Fatalf("bert sets the logo: %d", r.Code)
	}
	svg := []byte(`<?xml version="1.0"?><svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`)
	if r := w.admin.put("/api/branding/logo", "application/octet-stream", svg); r.Code != http.StatusOK {
		t.Fatalf("an SVG logo: %d %s", r.Code, r.Body)
	}
	logo := anon.do("GET", "/branding/logo", "")
	if logo.Code != http.StatusOK || logo.Header().Get("Content-Type") != "image/svg+xml" || !strings.Contains(logo.Header().Get("Content-Security-Policy"), "sandbox") ||
		logo.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("logo %d %v", logo.Code, logo.Header())
	}
	w.admin.expect("DELETE", "/api/branding/logo", "", http.StatusNoContent)
	anon.expect("GET", "/branding/logo", "", http.StatusNotFound)
	if audit := w.admin.expect("GET", "/api/audit", "", http.StatusOK); !strings.Contains(strings.Join(eventsOf(audit), ","), "design.change") {
		t.Fatal("the audit trail has no design change")
	}
}

func eventsOf(audit map[string]any) []string {
	var out []string
	for _, e := range audit["entries"].([]any) {
		out = append(out, e.(map[string]any)["event"].(string))
	}
	return out
}
