package metrics

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func scrape(t *testing.T, m *Metrics) string {
	t.Helper()
	rec := httptest.NewRecorder()
	m.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/metrics", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	return rec.Body.String()
}

func TestMetricsCountRequestsBySourceAndRoute(t *testing.T) {
	m := New(Sources{
		Build:   "test build",
		Items:   func() map[string]int { return map[string]int{"live": 3, "trashed": 1} },
		Live:    func() (int, int) { return 2, 5 },
		Storage: func() map[string]int64 { return map[string]int64{"events.wal": 4096} },
	})
	teapot := m.Instrument("/api/items/{ref}", false, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
		w.WriteHeader(http.StatusOK) // a second call does not change the recorded code
	}))
	for range 2 {
		teapot.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/api/items/PLAT-1", nil))
	}
	stream := m.Instrument("/api/items/{ref}/live", true, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if err := http.NewResponseController(w).Flush(); err != nil {
			t.Errorf("the wrapper hides the flusher: %v", err)
		}
	}))
	stream.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/api/items/x/live", nil))
	m.ObserveSearch(3 * time.Millisecond)

	out := scrape(t, m)
	for _, want := range []string{
		`wwbb_http_requests_total{code="418",method="GET",route="/api/items/{ref}"} 2`,
		`wwbb_http_request_duration_seconds_count{route="/api/items/{ref}"} 2`,
		`wwbb_http_requests_total{code="200",method="GET",route="/api/items/{ref}/live"} 1`,
		`wwbb_search_duration_seconds_count 1`,
		`wwbb_items{presence="live"} 3`,
		`wwbb_items{presence="trashed"} 1`,
		`wwbb_live_sessions 2`,
		`wwbb_live_clients 5`,
		`wwbb_storage_bytes{file="events.wal"} 4096`,
		`wwbb_build_info{version="test build"} 1`,
		`go_goroutines`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %s", want)
		}
	}
	if strings.Contains(out, `wwbb_http_request_duration_seconds_count{route="/api/items/{ref}/live"}`) {
		t.Error("an event stream is timed")
	}
	if strings.Contains(out, "PLAT-1") {
		t.Error("a path leaked into a label")
	}
}

func TestMissingSourcesAreLeftOut(t *testing.T) {
	out := scrape(t, New(Sources{}))
	for _, name := range []string{"wwbb_items", "wwbb_live_sessions", "wwbb_storage_bytes"} {
		if strings.Contains(out, name+"{") || strings.Contains(out, name+" ") {
			t.Errorf("%s without a source", name)
		}
	}
}
