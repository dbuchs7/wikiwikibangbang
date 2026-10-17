package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dbuchs7/wikiwikibangbang/internal/sso"
	"github.com/dbuchs7/wikiwikibangbang/internal/sso/ssotest"
)

func ssoSetup(t *testing.T) (http.Handler, Deps, *ssotest.IdP) {
	t.Helper()
	idp := ssotest.New(t, "wiki")
	p, err := sso.New(sso.Config{Issuer: idp.URL, ClientID: "wiki", ClientSecret: "s",
		RedirectURL: "http://localhost/api/oidc/callback", Name: "Corp SSO"}, idp.Client())
	if err != nil {
		t.Fatal(err)
	}
	d := testDeps(t)
	d.SSO = p
	if _, err := d.DB.EnsureAdmin("admin", adminPassword); err != nil {
		t.Fatal(err)
	}
	return Handler(Routes(d), d.DB.Accounts), d, idp
}

func getWith(h http.Handler, target string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	req := httptest.NewRequest("GET", target, nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func cookie(rec *httptest.ResponseRecorder, name string) *http.Cookie {
	for _, c := range rec.Result().Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

// signOn runs the browser's side of a login and returns the callback answer.
func signOn(t *testing.T, h http.Handler, idp *ssotest.IdP, c ssotest.Claims) *httptest.ResponseRecorder {
	t.Helper()
	start := getWith(h, "/api/oidc/login")
	if start.Code != http.StatusFound {
		t.Fatalf("login start: %d %s", start.Code, start.Body)
	}
	st := cookie(start, stateCookie)
	if st == nil || !st.HttpOnly || st.SameSite != http.SameSiteLaxMode {
		t.Fatalf("state cookie %+v", st)
	}
	cb := idp.Authorize(t, start.Header().Get("Location"), c)
	return getWith(h, "/api/oidc/callback?"+cb.Encode(), st)
}

func TestSingleSignOn(t *testing.T) {
	h, d, idp := ssoSetup(t)
	info := (&client{t: t, h: h}).expect("GET", "/api/oidc", "", http.StatusOK)
	if info["enabled"] != true || info["name"] != "Corp SSO" {
		t.Fatalf("info %v", info)
	}

	rec := signOn(t, h, idp, ssotest.Claims{Subject: "s-1", PreferredUsername: "admin"})
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/" {
		t.Fatalf("callback: %d %s", rec.Code, rec.Header().Get("Location"))
	}
	c := &client{t: t, h: h, cookie: cookie(rec, SessionCookie)}
	me := c.expect("GET", "/api/me", "", http.StatusOK)
	user := me["user"].(map[string]any)
	// The local administrator "admin" exists; the provider's "admin" is a
	// different, new account without roles.
	if user["username"] != "admin-2" || me["admin"] != false {
		t.Fatalf("me %v", me)
	}

	// The same identity logs into the same account again.
	rec = signOn(t, h, idp, ssotest.Claims{Subject: "s-1", PreferredUsername: "renamed"})
	again := (&client{t: t, h: h, cookie: cookie(rec, SessionCookie)}).expect("GET", "/api/me", "", http.StatusOK)
	if again["user"].(map[string]any)["id"] != user["id"] {
		t.Fatal("a returning identity got a new account")
	}

	events := map[string]int{}
	for _, e := range d.DB.Audit.List(0, 100) {
		if e.Detail["method"] == "oidc" {
			events[e.Event]++
		}
	}
	if events["login"] != 2 || events["user.create"] != 1 {
		t.Fatalf("audit %v", events)
	}
}

func TestSingleSignOnRefusals(t *testing.T) {
	h, d, idp := ssoSetup(t)
	failed := func(rec *httptest.ResponseRecorder, name string) {
		t.Helper()
		if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/?login_error=sso" || cookie(rec, SessionCookie) != nil {
			t.Errorf("%s: %d %s", name, rec.Code, rec.Header().Get("Location"))
		}
	}
	failed(signOn(t, h, idp, ssotest.Claims{Subject: "s", Audience: "other"}), "token for another client")

	// A callback without the browser's state cookie (login CSRF).
	start := getWith(h, "/api/oidc/login")
	cb := idp.Authorize(t, start.Header().Get("Location"), ssotest.Claims{Subject: "s"})
	failed(getWith(h, "/api/oidc/callback?"+cb.Encode()), "no state cookie")
	failed(getWith(h, "/api/oidc/callback?"+cb.Encode(), &http.Cookie{Name: stateCookie, Value: "other"}), "other state cookie")
	failed(getWith(h, "/api/oidc/callback?error=access_denied"), "provider error")

	// Replaying a completed callback.
	start = getWith(h, "/api/oidc/login")
	st := cookie(start, stateCookie)
	cb = idp.Authorize(t, start.Header().Get("Location"), ssotest.Claims{Subject: "s"})
	if rec := getWith(h, "/api/oidc/callback?"+cb.Encode(), st); cookie(rec, SessionCookie) == nil {
		t.Fatal("the genuine callback failed")
	}
	failed(getWith(h, "/api/oidc/callback?"+cb.Encode(), st), "replayed callback")

	n := 0
	for _, e := range d.DB.Audit.List(0, 100) {
		if e.Event == "login.failed" && e.Detail["method"] == "oidc" {
			n++
		}
	}
	if n != 5 {
		t.Fatalf("%d failed sign-ons audited, want 5", n)
	}
}

func TestSingleSignOnDisabledOrUnreachable(t *testing.T) {
	h := testHandler(t)
	c := &client{t: t, h: h}
	if info := c.expect("GET", "/api/oidc", "", http.StatusOK); info["enabled"] != false {
		t.Fatalf("info %v", info)
	}
	c.expect("GET", "/api/oidc/login", "", http.StatusNotFound)
	c.expect("GET", "/api/oidc/callback", "", http.StatusNotFound)

	p, err := sso.New(sso.Config{Issuer: "http://127.0.0.1:1", ClientID: "c", RedirectURL: "http://localhost/cb"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	d := testDeps(t)
	d.SSO = p
	if rec := getWith(Handler(Routes(d), d.DB.Accounts), "/api/oidc/login"); rec.Code != http.StatusBadGateway || strings.Contains(rec.Body.String(), "127.0.0.1") {
		t.Fatalf("unreachable provider: %d %s", rec.Code, rec.Body)
	}
}
