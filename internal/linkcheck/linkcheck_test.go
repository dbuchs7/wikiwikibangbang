package linkcheck

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"
)

// loopbackAllowed lets the tests reach httptest servers, which listen on
// loopback; everything else stays refused.
func loopbackAllowed(a netip.Addr) bool { return a.IsLoopback() || Public(a) }

func TestPublicAddresses(t *testing.T) {
	for addr, want := range map[string]bool{
		"93.184.216.34": true, "2606:2800:220:1:248:1893:25c8:1946": true,
		"127.0.0.1": false, "::1": false, "10.1.2.3": false, "172.16.0.1": false, "192.168.1.10": false,
		"169.254.169.254": false, "100.64.0.1": false, "0.0.0.0": false, "224.0.0.1": false,
		"fd00::1": false, "fe80::1": false, "::ffff:192.168.1.1": false, "255.255.255.255": false,
	} {
		if got := Public(netip.MustParseAddr(addr)); got != want {
			t.Errorf("%s: public %v, want %v", addr, got, want)
		}
	}
}

func TestCheck(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/ok", func(w http.ResponseWriter, r *http.Request) {})
	mux.HandleFunc("/gone", func(w http.ResponseWriter, r *http.Request) { http.Error(w, "gone", http.StatusNotFound) })
	mux.HandleFunc("/moved", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/ok", http.StatusMovedPermanently) })
	mux.HandleFunc("/loop", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/loop", http.StatusFound) })
	mux.HandleFunc("/nohead", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})
	mux.HandleFunc("/slow", func(w http.ResponseWriter, r *http.Request) { time.Sleep(2 * time.Second) })
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := New(Options{Allowed: loopbackAllowed, Timeout: 500 * time.Millisecond})
	for path, want := range map[string]string{
		"/ok": "", "/moved": "", "/nohead": "",
		"/gone": "404 Not Found",
		"/loop": "too many redirects",
		"/slow": "no answer within",
	} {
		r := c.Check(context.Background(), srv.URL+path)
		if want == "" && !r.OK || want != "" && (r.OK || !strings.Contains(r.Problem, want)) {
			t.Errorf("%s: %+v, want problem %q", path, r, want)
		}
	}
}

// The default guard refuses every address that is not public, also when a
// name resolves to one; and a redirect from an allowed server to an
// internal address is refused as well.
func TestInternalAddressesAreRefused(t *testing.T) {
	c := New(Options{Timeout: time.Second})
	for _, u := range []string{"http://127.0.0.1:1/", "http://localhost:1/", "http://[::1]:1/", "http://169.254.169.254/latest/meta-data/", "http://192.168.1.1/"} {
		if r := c.Check(context.Background(), u); r.OK || !strings.Contains(r.Problem, "internal address") {
			t.Errorf("%s: %+v, want refused as internal", u, r)
		}
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://10.0.0.1/admin", http.StatusFound)
	}))
	defer srv.Close()
	viaRedirect := New(Options{Timeout: time.Second, Allowed: loopbackAllowed})
	if r := viaRedirect.Check(context.Background(), srv.URL); r.OK || !strings.Contains(r.Problem, "internal address") {
		t.Errorf("redirect to an internal address: %+v", r)
	}
}

func TestAClosedPortIsNoConnection(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	addr := srv.URL
	srv.Close() // the port is free now: nothing answers
	r := New(Options{Allowed: loopbackAllowed}).Check(context.Background(), addr)
	if r.OK || !strings.HasPrefix(r.Problem, "no connection: ") || strings.Contains(r.Problem, addr) {
		t.Fatalf("%+v", r)
	}
}

func TestOnlyWebLinksAreChecked(t *testing.T) {
	c := New(Options{Allowed: loopbackAllowed})
	for _, u := range []string{"mailto:a@example.org", "ftp://example.org/x", "javascript:alert(1)", "not a url"} {
		if r := c.Check(context.Background(), u); r.OK || r.Problem == "" {
			t.Errorf("%s: %+v", u, r)
		}
	}
}

func TestCheckAllKeepsOrderAndBound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "bad") {
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer srv.Close()
	c := New(Options{Allowed: loopbackAllowed, Concurrency: 3})
	urls := []string{srv.URL + "/a", srv.URL + "/bad", srv.URL + "/a", srv.URL + "/c"}
	got := c.CheckAll(context.Background(), urls)
	if len(got) != 3 || got[srv.URL+"/bad"].OK || !got[srv.URL+"/a"].OK || !got[srv.URL+"/c"].OK {
		t.Fatalf("%+v", got)
	}
}
