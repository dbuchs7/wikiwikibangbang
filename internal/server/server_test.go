package server

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dbuchs7/wikiwikibangbang/internal/auth"
	"github.com/dbuchs7/wikiwikibangbang/internal/storage"
	"github.com/dbuchs7/wikiwikibangbang/internal/web"
)

func get(t *testing.T, h http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

// testDeps opens a data directory with cheap password hashing.
func testDeps(t *testing.T) Deps {
	t.Helper()
	db, _, err := storage.OpenWith(t.TempDir(), storage.Options{Auth: []auth.Option{auth.WithIterations(1000)}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return Deps{UI: web.FS(), DB: db}
}

func testHandler(t *testing.T) http.Handler {
	t.Helper()
	d := testDeps(t)
	return Handler(Routes(d), d.DB.Accounts)
}

func TestRouteTableIsComplete(t *testing.T) {
	seen := map[string]bool{}
	for _, rt := range Routes(testDeps(t)) {
		key := rt.Method + " " + rt.Pattern
		if rt.Method == "" || rt.Pattern == "" || rt.Summary == "" || rt.Handler == nil {
			t.Errorf("%s: method, pattern, summary and handler are required", key)
		}
		if seen[key] {
			t.Errorf("%s: registered twice", key)
		}
		seen[key] = true
	}
}

func TestPublicRoutes(t *testing.T) {
	h := testHandler(t)
	cases := []struct {
		path, status, contentType, body string
	}{
		{"/healthz", "200", "text/plain", "ok"},
		{"/", "200", "text/html", "<h1>wikiwikibangbang</h1>"},
		{"/assets/app.js", "200", "javascript", "/healthz"},
		{"/assets/style.css", "200", "text/css", "color-scheme"},
	}
	for _, c := range cases {
		rec := get(t, h, c.path)
		if got := rec.Result().Status; !strings.HasPrefix(got, c.status) {
			t.Errorf("GET %s: status %s, want %s", c.path, got, c.status)
		}
		if got := rec.Header().Get("Content-Type"); !strings.Contains(got, c.contentType) {
			t.Errorf("GET %s: Content-Type %q, want %q", c.path, got, c.contentType)
		}
		if !strings.Contains(rec.Body.String(), c.body) {
			t.Errorf("GET %s: body does not contain %q", c.path, c.body)
		}
	}
}

func TestUnknownPathsAndListingsAreNotFound(t *testing.T) {
	h := testHandler(t)
	for _, path := range []string{"/nope", "/assets/", "/assets/missing.js", "/index.html/x"} {
		if rec := get(t, h, path); rec.Code != http.StatusNotFound {
			t.Errorf("GET %s: status %d, want 404", path, rec.Code)
		}
	}
}

func TestWrongMethodIsRejected(t *testing.T) {
	h := testHandler(t)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/healthz", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST /healthz: status %d, want 405", rec.Code)
	}
}

func TestUndeclaredAccessIsGated(t *testing.T) {
	called := false
	secret := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true })
	// No Access field: the zero value must be Authenticated.
	h := Handler([]Route{{Method: http.MethodGet, Pattern: "/secret", Summary: "s", Handler: secret}}, nil)
	if rec := get(t, h, "/secret"); rec.Code != http.StatusUnauthorized {
		t.Errorf("status %d, want 401", rec.Code)
	}
	if called {
		t.Error("the handler of an authenticated route ran without a principal")
	}
}

func TestEveryResponseCarriesSecurityHeaders(t *testing.T) {
	h := testHandler(t)
	for _, path := range []string{"/", "/healthz", "/nope"} {
		hd := get(t, h, path).Header()
		if !strings.Contains(hd.Get("Content-Security-Policy"), "default-src 'self'") {
			t.Errorf("GET %s: missing content security policy", path)
		}
		if hd.Get("X-Content-Type-Options") != "nosniff" {
			t.Errorf("GET %s: missing nosniff", path)
		}
	}
}

func TestAccessString(t *testing.T) {
	if Public.String() != "public" || Authenticated.String() != "authenticated" {
		t.Error("unexpected access class names")
	}
}

func TestServeAnswersAndShutsDownWhenContextEnds(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	h := testHandler(t)
	go func() {
		done <- Serve(ctx, ln, h, slog.New(slog.DiscardHandler))
	}()

	res, err := http.Get("http://" + ln.Addr().String() + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if string(body) != "ok\n" {
		t.Errorf("body %q, want ok", body)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Serve returned %v, want nil after shutdown", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Serve did not return after the context ended")
	}
}

func TestServeReportsListenerFailure(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ln.Close()
	err = Serve(context.Background(), ln, http.NotFoundHandler(), slog.New(slog.DiscardHandler))
	if err == nil {
		t.Fatal("Serve on a closed listener returned nil")
	}
}
