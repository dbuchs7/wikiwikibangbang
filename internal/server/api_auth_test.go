package server

import (
	"bytes"
	"encoding/json"
	"github.com/dbuchs7/wikiwikibangbang/internal/buildinfo"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dbuchs7/wikiwikibangbang/internal/auth"
	"github.com/dbuchs7/wikiwikibangbang/internal/budget"
)

const adminPassword = "admin password!"

type client struct {
	t      *testing.T
	h      http.Handler
	cookie *http.Cookie
	bearer string
}

func (c *client) do(method, path, body string) *httptest.ResponseRecorder {
	c.t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.RemoteAddr = "192.0.2.1:4000"
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.cookie != nil {
		req.AddCookie(c.cookie)
	}
	if c.bearer != "" {
		req.Header.Set("Authorization", "Bearer "+c.bearer)
	}
	rec := httptest.NewRecorder()
	c.h.ServeHTTP(rec, req)
	return rec
}

func (c *client) expect(method, path, body string, status int) map[string]any {
	c.t.Helper()
	rec := c.do(method, path, body)
	if rec.Code != status {
		c.t.Fatalf("%s %s: status %d, want %d; body %s", method, path, rec.Code, status, rec.Body)
	}
	var out map[string]any
	if rec.Body.Len() > 0 {
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
	}
	return out
}

func (c *client) login(username, password string) {
	c.t.Helper()
	rec := c.do("POST", "/api/login", `{"username":"`+username+`","password":"`+password+`"}`)
	if rec.Code != http.StatusOK {
		c.t.Fatalf("login %s: %d %s", username, rec.Code, rec.Body)
	}
	for _, ck := range rec.Result().Cookies() {
		if ck.Name == SessionCookie {
			c.cookie = ck
		}
	}
}

func setupAuth(t *testing.T, opts ...func(*Deps)) (http.Handler, Deps) {
	t.Helper()
	d := testDeps(t)
	for _, o := range opts {
		o(&d)
	}
	if _, err := d.DB.EnsureAdmin("admin", adminPassword); err != nil {
		t.Fatal(err)
	}
	d.Collab = NewCollab(d.DB)
	return Handler(Routes(d), d.DB.Accounts), d
}

// publicAPI is every API route an anonymous caller may reach. Adding one is
// a security decision; this list makes it a visible one.
var publicAPI = map[string]bool{
	"/api/login":                  true,
	"/api/oidc":                   true, // whether single sign-on exists
	"/api/oidc/login":             true,
	"/api/oidc/callback":          true,
	"/api/signup":                 true, // asking for an account (ADR-0061)
	"/api/password-reset":         true, // a forgotten password (ADR-0064)
	"/api/password-reset/confirm": true,
}

func TestEveryAPIRouteButLoginNeedsAPrincipal(t *testing.T) {
	for _, rt := range Routes(testDeps(t)) {
		if !strings.HasPrefix(rt.Pattern, "/api/") {
			continue
		}
		if want := publicAPI[rt.Pattern]; (rt.Access == Public) != want {
			t.Errorf("%s %s is %s", rt.Method, rt.Pattern, rt.Access)
		}
	}
	h, _ := setupAuth(t)
	anon := &client{t: t, h: h}
	for _, rt := range Routes(testDeps(t)) {
		if strings.HasPrefix(rt.Pattern, "/api/") && rt.Access != Public {
			path := strings.ReplaceAll(rt.Pattern, "{id}", "x")
			anon.expect(rt.Method, path, "", http.StatusUnauthorized)
		}
	}
}

func TestLoginSessionAndLogout(t *testing.T) {
	h, _ := setupAuth(t)
	c := &client{t: t, h: h}
	c.expect("POST", "/api/login", `{"username":"admin","password":"wrong password!"}`, http.StatusUnauthorized)
	c.expect("POST", "/api/login", `{"username":"nobody","password":"wrong password!"}`, http.StatusUnauthorized)

	c.login("admin", adminPassword)
	if !c.cookie.HttpOnly || c.cookie.SameSite != http.SameSiteStrictMode || c.cookie.Path != "/" {
		t.Fatalf("cookie %+v", c.cookie)
	}
	me := c.expect("GET", "/api/me", "", http.StatusOK)
	if me["admin"] != true || me["via"] != "session" {
		t.Fatalf("me %v", me)
	}
	// Signed-in users see which build runs, for the footer; anonymous
	// callers do not (the next request after logout is refused).
	if v, _ := me["version"].(string); v != buildinfo.Get().Version || v == "" {
		t.Fatalf("version %v, want %q", me["version"], buildinfo.Get().Version)
	}
	c.expect("POST", "/api/logout", "", http.StatusNoContent)
	c.expect("GET", "/api/me", "", http.StatusUnauthorized)
}

func TestRequestBodiesAreStrict(t *testing.T) {
	h, _ := setupAuth(t)
	c := &client{t: t, h: h}
	req := httptest.NewRequest("POST", "/api/login", strings.NewReader(`{"username":"admin","password":"x"}`))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded") // what a cross-site form sends
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("form-encoded login: %d", rec.Code)
	}
	c.expect("POST", "/api/login", `{"username":"admin","password":"x","admin":true}`, http.StatusBadRequest)
	c.expect("POST", "/api/login", `{"username":"admin"} {}`, http.StatusBadRequest)
	c.expect("POST", "/api/login", `{"username":"admin"} x`, http.StatusBadRequest)
	big := `{"username":"` + strings.Repeat("a", int(budget.Get().RequestBody)) + `"}`
	c.expect("POST", "/api/login", big, http.StatusRequestEntityTooLarge)
}

func TestLoginIsThrottled(t *testing.T) {
	h, _ := setupAuth(t)
	c := &client{t: t, h: h}
	for range auth.MaxFailures {
		c.expect("POST", "/api/login", `{"username":"admin","password":"wrong password!"}`, http.StatusUnauthorized)
	}
	rec := c.do("POST", "/api/login", `{"username":"admin","password":"`+adminPassword+`"}`)
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") == "" {
		t.Fatalf("status %d, Retry-After %q", rec.Code, rec.Header().Get("Retry-After"))
	}
}

func TestTokens(t *testing.T) {
	h, _ := setupAuth(t)
	c := &client{t: t, h: h}
	c.login("admin", adminPassword)
	c.expect("POST", "/api/tokens", `{"name":"x","scope":"root","days":1}`, http.StatusBadRequest)
	c.expect("POST", "/api/tokens", `{"name":"x"`, http.StatusBadRequest)

	read := c.expect("POST", "/api/tokens", `{"name":"reader","scope":"read","days":1}`, http.StatusCreated)
	full := c.expect("POST", "/api/tokens", `{"name":"ci","scope":"full","days":30}`, http.StatusCreated)
	if !strings.HasPrefix(read["secret"].(string), auth.TokenPrefix) {
		t.Fatalf("secret %v", read["secret"])
	}
	list := c.expect("GET", "/api/tokens", "", http.StatusOK)
	if n := len(list["tokens"].([]any)); n != 2 {
		t.Fatalf("%d tokens listed", n)
	}
	if bytes.Contains([]byte(c.do("GET", "/api/tokens", "").Body.String()), []byte(auth.TokenPrefix)) {
		t.Fatal("the listing shows a secret")
	}

	r := &client{t: t, h: h, bearer: read["secret"].(string)}
	if me := r.expect("GET", "/api/me", "", http.StatusOK); me["scope"] != "read" || me["via"] != "token" {
		t.Fatalf("read token: %v", me)
	}
	r.expect("POST", "/api/tokens", `{"name":"y","scope":"full","days":1}`, http.StatusForbidden)
	r.expect("POST", "/api/logout", "", http.StatusForbidden)

	f := &client{t: t, h: h, bearer: full["secret"].(string)}
	f.expect("GET", "/api/me", "", http.StatusOK)
	f.expect("POST", "/api/logout", "", http.StatusNoContent) // a token has no session to end
	id := full["token"].(map[string]any)["id"].(string)
	c.expect("DELETE", "/api/tokens/"+id, "", http.StatusNoContent)
	c.expect("DELETE", "/api/tokens/"+id, "", http.StatusNotFound)
	f.expect("GET", "/api/me", "", http.StatusUnauthorized)

	// A credential that does not resolve is not replaced by the cookie.
	both := &client{t: t, h: h, cookie: c.cookie, bearer: "wwbb_forged"}
	both.expect("GET", "/api/me", "", http.StatusUnauthorized)
	basic := httptest.NewRequest("GET", "/api/me", nil)
	basic.Header.Set("Authorization", "Basic YWRtaW46eA==")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, basic)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("basic auth: %d", rec.Code)
	}
}

func TestUsersAndPasswords(t *testing.T) {
	h, _ := setupAuth(t)
	admin := &client{t: t, h: h}
	admin.login("admin", adminPassword)
	admin.expect("POST", "/api/users", `{"username":"bert","password":"short"}`, http.StatusBadRequest)
	admin.expect("POST", "/api/users", `{"username":"bert","password":"bert's password"}`, http.StatusCreated)
	admin.expect("POST", "/api/users", `{"username":"Bert","password":"bert's password"}`, http.StatusConflict)
	admin.expect("POST", "/api/users", `{"username":1}`, http.StatusBadRequest)
	if users := admin.expect("GET", "/api/users", "", http.StatusOK); len(users["users"].([]any)) != 2 {
		t.Fatalf("users %v", users)
	}

	bert := &client{t: t, h: h}
	bert.login("bert", "bert's password")
	if me := bert.expect("GET", "/api/me", "", http.StatusOK); me["admin"] != false {
		t.Fatalf("bert is admin: %v", me)
	}
	bert.expect("GET", "/api/users", "", http.StatusForbidden)
	bert.expect("POST", "/api/users", `{"username":"carla","password":"carla's password"}`, http.StatusForbidden)

	bert.expect("PUT", "/api/me/password", `{"current":"wrong password!","new":"a new password!"}`, http.StatusForbidden)
	bert.expect("PUT", "/api/me/password", `{"current":"bert's password","new":"short"}`, http.StatusBadRequest)
	bert.expect("PUT", "/api/me/password", `{"current":"bert's password"`, http.StatusBadRequest)
	bert.expect("PUT", "/api/me/password", `{"current":"bert's password","new":"a new password!"}`, http.StatusNoContent)
	bert.expect("GET", "/api/me", "", http.StatusUnauthorized) // sessions end with the old password
	bert.login("bert", "a new password!")
}

func TestLanguageThroughTheAPI(t *testing.T) {
	w := newWorld(t)
	me := w.bert.expect("GET", "/api/me", "", http.StatusOK)
	if me["language"] != "" || len(me["languages"].([]any)) < 2 {
		t.Fatalf("me %v", me)
	}
	w.bert.expect("PUT", "/api/me/language", `{"language":"de"}`, http.StatusNoContent)
	if got := w.bert.expect("GET", "/api/me", "", http.StatusOK)["language"]; got != "de" {
		t.Fatalf("language %v", got)
	}
	if got := w.vera.expect("GET", "/api/me", "", http.StatusOK)["language"]; got != "" {
		t.Fatalf("another user's language changed: %v", got)
	}
	w.bert.expect("PUT", "/api/me/language", `{"language":"fr"}`, http.StatusBadRequest)
	w.bert.expect("PUT", "/api/me/language", `{"language":""}`, http.StatusNoContent)
}

func TestUserNamesForAnyoneSignedIn(t *testing.T) {
	w := newWorld(t)
	got := w.carla.expect("GET", "/api/users/names?id="+w.ids["bert"]+"&id="+w.ids["vera"]+"&id=nobody", "", http.StatusOK)
	names := got["names"].(map[string]any)
	if names[w.ids["bert"]] != "bert" || names[w.ids["vera"]] != "vera" || len(names) != 2 {
		t.Fatalf("names %v", names)
	}
	if rec := (&client{t: t, h: w.carla.h}).do("GET", "/api/users/names?id="+w.ids["bert"], ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous: %d", rec.Code)
	}
	many := strings.Repeat("&id=x", budget.Get().PageItems+1)
	w.carla.expect("GET", "/api/users/names?"+many[1:], "", http.StatusBadRequest)
}
