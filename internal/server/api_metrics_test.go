package server

import (
	"net/http"
	"strings"
	"testing"
)

func TestMetricsAreForAdministratorsAndScrapers(t *testing.T) {
	w := newWorld(t)
	w.bert.expect("POST", "/api/items", `{"space":"PLAT","type":"task","title":"t","properties":{"priority":"low"}}`, http.StatusCreated)
	w.bert.expect("GET", "/api/search?q=t", "", http.StatusOK)

	if rec := w.bert.do("GET", "/metrics", ""); rec.Code != http.StatusForbidden {
		t.Fatalf("an editor reads metrics: %d", rec.Code)
	}
	if rec := (&client{t: t, h: w.bert.h}).do("GET", "/metrics", ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous: %d", rec.Code)
	}
	rec := w.admin.do("GET", "/metrics", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("admin: %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		`wwbb_items{presence="live"} 1`,
		`wwbb_http_requests_total{code="201",method="POST",route="/api/items"} 1`,
		`wwbb_search_duration_seconds_count 1`,
		`wwbb_storage_bytes{file="events.wal"}`,
		`wwbb_live_sessions 0`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %s", want)
		}
	}

	// Only administrators make metrics tokens, and such a token reads the
	// metrics and nothing else.
	w.bert.expect("POST", "/api/tokens", `{"name":"scraper","scope":"metrics","days":30}`, http.StatusForbidden)
	tok := w.admin.expect("POST", "/api/tokens", `{"name":"scraper","scope":"metrics","days":30}`, http.StatusCreated)
	scraper := &client{t: t, h: w.admin.h, bearer: tok["secret"].(string)}
	if rec := scraper.do("GET", "/metrics", ""); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "wwbb_build_info") {
		t.Fatalf("scraper: %d", rec.Code)
	}
	for _, c := range []struct{ method, path, body string }{
		{"GET", "/api/me", ""},
		{"GET", "/api/items/PLAT-1", ""},
		{"GET", "/api/search?q=t", ""},
		{"POST", "/api/backup", `{"passphrase":"a long passphrase"}`},
		{"POST", "/api/tokens", `{"name":"x","scope":"full","days":1}`},
	} {
		if rec := scraper.do(c.method, c.path, c.body); rec.Code != http.StatusForbidden {
			t.Errorf("scraper %s %s: %d, want 403", c.method, c.path, rec.Code)
		}
	}
}
