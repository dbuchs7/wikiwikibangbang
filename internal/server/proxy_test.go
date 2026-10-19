package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dbuchs7/wikiwikibangbang/internal/audit"
)

func TestParseTrustedProxies(t *testing.T) {
	got, err := ParseTrustedProxies(" 172.17.0.1, 10.0.0.0/8 ,::1 ")
	if err != nil || len(got) != 3 || got[0].String() != "172.17.0.1/32" || got[1].String() != "10.0.0.0/8" || got[2].String() != "::1/128" {
		t.Fatalf("got %v, %v", got, err)
	}
	if got, err := ParseTrustedProxies(""); err != nil || got != nil {
		t.Fatalf("empty: %v, %v", got, err)
	}
	for _, bad := range []string{"proxy.local", "10.0.0.0/33", "1.2.3.4,", "0.0.0.0/0", "::/0"} {
		if _, err := ParseTrustedProxies(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

// seen answers with what the handler behind the proxy layer sees.
func seen(t *testing.T, trusted, peer string, header map[string]string) (addr string, secure bool) {
	t.Helper()
	nets, err := ParseTrustedProxies(trusted)
	if err != nil {
		t.Fatal(err)
	}
	h := BehindProxies(nets, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		addr, secure = clientAddr(r), isSecure(r)
	}))
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = peer
	for k, v := range header {
		req.Header.Set(k, v)
	}
	h.ServeHTTP(httptest.NewRecorder(), req)
	return addr, secure
}

func TestForwardedHeadersCountOnlyFromTrustedProxies(t *testing.T) {
	fwd := map[string]string{"X-Forwarded-For": "203.0.113.7", "X-Forwarded-Proto": "https"}
	cases := []struct {
		name, trusted, peer string
		header              map[string]string
		addr                string
		secure              bool
	}{
		{"no proxy configured", "", "172.17.0.1:5000", fwd, "172.17.0.1", false},
		{"untrusted peer", "10.0.0.1", "172.17.0.1:5000", fwd, "172.17.0.1", false},
		{"trusted peer", "172.17.0.0/16", "172.17.0.1:5000", fwd, "203.0.113.7", true},
		{"proxy without headers", "172.17.0.1", "172.17.0.1:5000", nil, "172.17.0.1", false},
		{"plain http behind the proxy", "172.17.0.1", "172.17.0.1:5000", map[string]string{"X-Forwarded-Proto": "http"}, "172.17.0.1", false},
		// A client cannot claim an address: the rightmost entry the
		// trusted proxy added is the one believed.
		{"spoofed chain", "172.17.0.1", "172.17.0.1:5000", map[string]string{"X-Forwarded-For": "198.51.100.1, 203.0.113.7"}, "203.0.113.7", false},
		{"chain of trusted proxies", "172.17.0.1, 10.0.0.2", "172.17.0.1:5000", map[string]string{"X-Forwarded-For": "203.0.113.7, 10.0.0.2"}, "203.0.113.7", false},
		{"garbage", "172.17.0.1", "172.17.0.1:5000", map[string]string{"X-Forwarded-For": "not-an-address"}, "172.17.0.1", false},
		{"ipv6 client", "172.17.0.1", "172.17.0.1:5000", map[string]string{"X-Forwarded-For": "2001:db8::1"}, "2001:db8::1", false},
	}
	for _, c := range cases {
		addr, secure := seen(t, c.trusted, c.peer, c.header)
		if addr != c.addr || secure != c.secure {
			t.Errorf("%s: addr %s secure %v, want %s %v", c.name, addr, secure, c.addr, c.secure)
		}
	}
}

// Behind a TLS-terminating proxy, the session cookie is Secure and the
// audit trail and login throttle see the client, not the proxy.
func TestLoginBehindAProxy(t *testing.T) {
	h, d := setupAuth(t)
	nets, _ := ParseTrustedProxies("172.17.0.1")
	h = BehindProxies(nets, h)
	req := httptest.NewRequest("POST", "/api/login", strings.NewReader(`{"username":"admin","password":"`+adminPassword+`"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Forwarded-For", "203.0.113.7")
	req.Header.Set("X-Forwarded-Proto", "https")
	req.RemoteAddr = "172.17.0.1:5000"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("login: %d %s", rec.Code, rec.Body)
	}
	if c := rec.Result().Cookies(); len(c) != 1 || !c[0].Secure {
		t.Fatalf("cookie %+v is not Secure", c)
	}
	entries := d.DB.Audit.List(0, 10)
	found := false
	for _, e := range entries {
		if e.Event == audit.Login && e.Addr == "203.0.113.7" {
			found = true
		}
	}
	if !found {
		t.Fatalf("no login from the client's address in %+v", entries)
	}
}
