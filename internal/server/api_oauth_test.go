package server

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

const (
	oauthCallback = "https://claude.ai/api/mcp/auth_callback"
	oauthVerifier = "a-verifier-of-at-least-forty-three-characters-0123456789"
	oauthResource = "http://example.com/mcp"
)

func oauthChallenge() string {
	sum := sha256.Sum256([]byte(oauthVerifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// form posts a form to the token endpoint, with Basic credentials when
// user is not empty.
func form(t *testing.T, h http.Handler, path string, v url.Values, user, pass string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(v.Encode()))
	req.RemoteAddr = "198.51.100.7:5000"
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if user != "" {
		req.SetBasicAuth(url.QueryEscape(user), url.QueryEscape(pass))
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec, out
}

// authorizeParams is a well-formed authorization request.
func authorizeParams(client string) url.Values {
	return url.Values{"response_type": {"code"}, "client_id": {client}, "redirect_uri": {oauthCallback},
		"state": {"st-1"}, "code_challenge": {oauthChallenge()}, "code_challenge_method": {"S256"}, "resource": {oauthResource}}
}

// approveBody is the consent page's approval of params.
func approveBody(t *testing.T, params url.Values, deny bool) string {
	t.Helper()
	m := map[string]any{"deny": deny}
	for _, k := range []string{"client_id", "redirect_uri", "state", "code_challenge", "code_challenge_method", "resource"} {
		m[k] = params.Get(k)
	}
	return mustJSON(t, m)
}

func TestOAuthMetadataAndChallenge(t *testing.T) {
	h, _ := setupAuth(t)
	anon := &client{t: t, h: h}
	rec := anon.do("POST", MCPPath, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	want := `Bearer realm="wikiwikibangbang", resource_metadata="http://example.com/.well-known/oauth-protected-resource/mcp"`
	if rec.Code != http.StatusUnauthorized || rec.Header().Get("WWW-Authenticate") != want {
		t.Fatalf("anonymous MCP call: %d, WWW-Authenticate %q", rec.Code, rec.Header().Get("WWW-Authenticate"))
	}
	for _, p := range []string{ResourceMetadataPath, ResourceMetadataPath + MCPPath} {
		m := anon.expect("GET", p, "", http.StatusOK)
		if m["resource"] != oauthResource || m["authorization_servers"].([]any)[0] != "http://example.com" {
			t.Fatalf("%s: %v", p, m)
		}
	}
	m := anon.expect("GET", ServerMetadataPath, "", http.StatusOK)
	if m["issuer"] != "http://example.com" || m["token_endpoint"] != "http://example.com/oauth/token" ||
		mustJSON(t, m["code_challenge_methods_supported"]) != `["S256"]` {
		t.Fatalf("server metadata: %v", m)
	}
	// Registration is off: neither advertised nor served.
	if _, ok := m["registration_endpoint"]; ok {
		t.Fatalf("registration advertised while off: %v", m)
	}
	if rec := anon.do("POST", RegisterPath, `{"redirect_uris":["https://x.example/cb"]}`); rec.Code == http.StatusCreated {
		t.Fatalf("registration served while off")
	}
}

func TestOAuthFlowOverHTTP(t *testing.T) {
	w := newWorld(t)
	h := w.admin.h
	// The administrator registers Claude; bert may not.
	w.bert.expect("POST", "/api/oauth/clients", `{"name":"Claude","redirects":["`+oauthCallback+`"]}`, http.StatusForbidden)
	created := w.admin.expect("POST", "/api/oauth/clients", `{"name":"Claude","redirects":["`+oauthCallback+`"]}`, http.StatusCreated)
	id := field(created, "client", "id").(string)
	secret := created["secret"].(string)
	if listed := w.admin.expect("GET", "/api/oauth/clients", "", http.StatusOK); listed["mcp"] != oauthResource || strings.Contains(mustJSON(t, listed), secret) {
		t.Fatalf("clients: %v", listed)
	}

	// The authorize endpoint leads to the consent page, or refuses a
	// redirect address that is not the client's without following it.
	params := authorizeParams(id)
	anon := &client{t: t, h: h}
	rec := anon.do("GET", AuthorizePath+"?"+params.Encode(), "")
	if loc := rec.Header().Get("Location"); rec.Code != http.StatusFound || !strings.HasPrefix(loc, "/#/oauth?") || !strings.Contains(loc, "client_id="+id) {
		t.Fatalf("authorize: %d %q", rec.Code, loc)
	}
	bad := authorizeParams(id)
	bad.Set("redirect_uri", "https://evil.example/cb")
	if loc := anon.do("GET", AuthorizePath+"?"+bad.Encode(), "").Header().Get("Location"); loc != "/#/oauth?error=client" {
		t.Fatalf("foreign redirect: %q", loc)
	}
	noPKCE := authorizeParams(id)
	noPKCE.Del("code_challenge")
	if loc := anon.do("GET", AuthorizePath+"?"+noPKCE.Encode(), "").Header().Get("Location"); !strings.HasPrefix(loc, oauthCallback+"?") || !strings.Contains(loc, "error=invalid_request") {
		t.Fatalf("without PKCE: %q", loc)
	}
	if got := w.bert.expect("GET", "/api/oauth/request?"+params.Encode(), "", http.StatusOK); field(got, "client", "name") != "Claude" || got["redirect_host"] != "claude.ai" {
		t.Fatalf("request: %v", got)
	}

	// Only a signed-in person approves; an API token cannot.
	tok := w.bert.expect("POST", "/api/tokens", `{"name":"t","scope":"full","days":1}`, http.StatusCreated)["secret"].(string)
	(&client{t: t, h: h, bearer: tok}).expect("POST", "/api/oauth/approve", approveBody(t, params, false), http.StatusForbidden)
	denied := w.bert.expect("POST", "/api/oauth/approve", approveBody(t, params, true), http.StatusOK)["redirect"].(string)
	if !strings.Contains(denied, "error=access_denied") || !strings.Contains(denied, "state=st-1") {
		t.Fatalf("denied: %s", denied)
	}
	back, _ := url.Parse(w.bert.expect("POST", "/api/oauth/approve", approveBody(t, params, false), http.StatusOK)["redirect"].(string))
	code := back.Query().Get("code")
	if code == "" || back.Query().Get("state") != "st-1" || back.Query().Get("iss") != "http://example.com" {
		t.Fatalf("approved: %s", back)
	}

	// The code turns into tokens once, for the right verifier.
	exchange := url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {oauthCallback}, "code_verifier": {oauthVerifier}, "resource": {oauthResource}}
	if rec, out := form(t, h, TokenPath, exchange, id, "wrong"); rec.Code != http.StatusUnauthorized || out["error"] != "invalid_client" {
		t.Fatalf("wrong secret: %d %v", rec.Code, out)
	}
	rec, out := form(t, h, TokenPath, exchange, id, secret)
	if rec.Code != http.StatusOK || out["token_type"] != "Bearer" {
		t.Fatalf("exchange: %d %v", rec.Code, out)
	}
	access, refresh := out["access_token"].(string), out["refresh_token"].(string)
	if rec, out := form(t, h, TokenPath, exchange, id, secret); rec.Code != http.StatusBadRequest || out["error"] != "invalid_grant" {
		t.Fatalf("code used twice: %d %v", rec.Code, out)
	}

	// The access token acts as bert on the MCP endpoint, tool calls
	// included, and nowhere else.
	agent := &client{t: t, h: h, bearer: access}
	text, isErr := agent.tool("create_item", `{"space":"PLAT","type":"task","title":"Via OAuth","properties":{"priority":"low"}}`)
	if isErr {
		t.Fatalf("create_item over OAuth: %s", text)
	}
	if !strings.Contains(mustJSON(t, w.bert.expect("GET", "/api/items?space=PLAT", "", http.StatusOK)), "Via OAuth") {
		t.Fatalf("the item made over OAuth is missing")
	}
	agent.expect("GET", "/api/me", "", http.StatusUnauthorized)
	agent.expect("GET", "/api/oauth/grants", "", http.StatusUnauthorized)

	// A token issued for another resource is refused.
	other := &client{t: t, h: h}
	req := httptest.NewRequest("POST", MCPPath, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	req.Host = "other.example"
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+access)
	r2 := httptest.NewRecorder()
	other.h.ServeHTTP(r2, req)
	if r2.Code != http.StatusUnauthorized {
		t.Fatalf("token at another resource: %d", r2.Code)
	}

	// Refreshing turns the refresh token over.
	rec, out = form(t, h, TokenPath, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refresh}, "client_id": {id}, "client_secret": {secret}}, "", "")
	if rec.Code != http.StatusOK || out["refresh_token"] == refresh {
		t.Fatalf("refresh: %d %v", rec.Code, out)
	}
	access = out["access_token"].(string)

	// bert sees the approval and withdraws it; the token stops.
	grants := w.bert.expect("GET", "/api/oauth/grants", "", http.StatusOK)["grants"].([]any)
	if len(grants) != 1 || grants[0].(map[string]any)["name"] != "Claude" {
		t.Fatalf("grants: %v", grants)
	}
	w.bert.expect("DELETE", "/api/oauth/grants/"+id, "", http.StatusNoContent)
	if rec := (&client{t: t, h: h, bearer: access}).do("POST", MCPPath, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`); rec.Code != http.StatusUnauthorized {
		t.Fatalf("after withdrawal: %d", rec.Code)
	}
	w.admin.expect("DELETE", "/api/oauth/clients/"+id, "", http.StatusNoContent)
	w.admin.expect("DELETE", "/api/oauth/clients/"+id, "", http.StatusNotFound)
}

func TestOAuthTokenEndpointIsThrottled(t *testing.T) {
	h, _ := setupAuth(t)
	bad := url.Values{"grant_type": {"authorization_code"}, "code": {"nope"}, "code_verifier": {oauthVerifier}}
	var rec *httptest.ResponseRecorder
	for range 11 {
		rec, _ = form(t, h, TokenPath, bad, "wwbb-unknown", "secret")
	}
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") == "" {
		t.Fatalf("after many failures: %d", rec.Code)
	}
	if rec, out := form(t, h, TokenPath, url.Values{"grant_type": {"password"}}, "", ""); rec.Code != http.StatusBadRequest || out["error"] != "unsupported_grant_type" {
		t.Fatalf("password grant: %d %v", rec.Code, out)
	}
}

func TestOAuthDynamicRegistration(t *testing.T) {
	h, _ := setupAuth(t, func(d *Deps) { d.OAuthRegistration = true })
	anon := &client{t: t, h: h}
	if m := anon.expect("GET", ServerMetadataPath, "", http.StatusOK); m["registration_endpoint"] != "http://example.com/oauth/register" {
		t.Fatalf("metadata: %v", m)
	}
	anon.expect("POST", RegisterPath, `{"client_name":"Bad","redirect_uris":["ftp://x.example/cb"]}`, http.StatusBadRequest)
	out := anon.expect("POST", RegisterPath, `{"client_name":"Some agent","redirect_uris":["`+oauthCallback+`"],"token_endpoint_auth_method":"none","logo_uri":"https://x.example/l.png"}`, http.StatusCreated)
	if out["client_secret"] != nil || out["token_endpoint_auth_method"] != "none" {
		t.Fatalf("public registration: %v", out)
	}
	admin := &client{t: t, h: h}
	admin.login("admin", adminPassword)
	got := admin.expect("GET", "/api/oauth/request?"+authorizeParams(out["client_id"].(string)).Encode(), "", http.StatusOK)
	if field(got, "client", "self") != true {
		t.Fatalf("a self-registered client is not marked: %v", got)
	}
}

func TestOAuthRefusals(t *testing.T) {
	w := newWorld(t, func(d *Deps) { d.OAuthRegistration = true })
	h := w.admin.h
	w.admin.expect("POST", "/api/oauth/clients", `{"name":"Bad","redirects":["http://x.example/cb"]}`, http.StatusBadRequest)
	pub := w.admin.expect("POST", "/api/oauth/clients", `{"name":"Desktop app","redirects":["http://127.0.0.1:3000/cb"],"public":true}`, http.StatusCreated)
	if pub["secret"] != "" || field(pub, "client", "public") != true {
		t.Fatalf("a public client: %v", pub)
	}
	w.bert.expect("GET", "/api/oauth/clients", "", http.StatusForbidden)
	w.bert.expect("DELETE", "/api/oauth/clients/x", "", http.StatusForbidden)
	w.bert.expect("DELETE", "/api/oauth/grants/x", "", http.StatusNotFound)
	w.bert.expect("GET", "/api/oauth/request?client_id=nobody", "", http.StatusBadRequest)

	id := field(w.admin.expect("POST", "/api/oauth/clients", `{"name":"Claude","redirects":["`+oauthCallback+`"]}`, http.StatusCreated), "client", "id").(string)
	anon := &client{t: t, h: h}
	wrongType := authorizeParams(id)
	wrongType.Set("response_type", "token")
	if loc := anon.do("GET", AuthorizePath+"?"+wrongType.Encode(), "").Header().Get("Location"); !strings.Contains(loc, "error=unsupported_response_type") {
		t.Fatalf("response_type token: %q", loc)
	}
	foreign := authorizeParams(id)
	foreign.Set("resource", "https://other.example/mcp")
	if loc := anon.do("GET", AuthorizePath+"?"+foreign.Encode(), "").Header().Get("Location"); !strings.Contains(loc, "error=invalid_target") {
		t.Fatalf("foreign resource: %q", loc)
	}
	w.bert.expect("POST", "/api/oauth/approve", approveBody(t, url.Values{"client_id": {"nobody"}}, false), http.StatusBadRequest)
	if got := w.bert.expect("POST", "/api/oauth/approve", approveBody(t, foreign, false), http.StatusOK)["redirect"].(string); !strings.Contains(got, "error=invalid_target") {
		t.Fatalf("approve a foreign resource: %s", got)
	}

	// The token endpoint takes forms only, and one client identity.
	req := httptest.NewRequest(http.MethodPost, TokenPath, strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("JSON at the token endpoint: %d", rec.Code)
	}
	if rec, out := form(t, h, TokenPath, url.Values{"grant_type": {"refresh_token"}, "client_id": {"other"}}, id, "s"); rec.Code != http.StatusBadRequest || out["error"] != "invalid_request" {
		t.Fatalf("two client identities: %d %v", rec.Code, out)
	}

	// Registration refuses what it cannot take.
	for _, body := range []string{`not json`, `{"redirect_uris":["` + oauthCallback + `"],"grant_types":["client_credentials"]}`, `{"redirect_uris":[]}`} {
		anon.expect("POST", RegisterPath, body, http.StatusBadRequest)
	}
	req = httptest.NewRequest(http.MethodPost, RegisterPath, strings.NewReader(`a=b`))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("a form at registration: %d", rec.Code)
	}
	// Without a name, the redirect host names it; Basic means a secret.
	out := anon.expect("POST", RegisterPath, `{"redirect_uris":["`+oauthCallback+`"],"token_endpoint_auth_method":"client_secret_basic"}`, http.StatusCreated)
	if out["client_name"] != "claude.ai" || !strings.HasPrefix(out["client_secret"].(string), "wwbbc_") {
		t.Fatalf("registration without a name: %v", out)
	}
}

func TestOAuthReadOnlyApproval(t *testing.T) {
	w := newWorld(t)
	h := w.admin.h
	created := w.admin.expect("POST", "/api/oauth/clients", `{"name":"Claude","redirects":["`+oauthCallback+`"]}`, http.StatusCreated)
	id, secret := field(created, "client", "id").(string), created["secret"].(string)
	params := authorizeParams(id)
	body := strings.Replace(approveBody(t, params, false), `{`, `{"access":"read",`, 1)
	w.bert.expect("POST", "/api/oauth/approve", strings.Replace(body, `"read"`, `"metrics"`, 1), http.StatusBadRequest)
	back, _ := url.Parse(w.bert.expect("POST", "/api/oauth/approve", body, http.StatusOK)["redirect"].(string))
	rec, out := form(t, h, TokenPath, url.Values{"grant_type": {"authorization_code"}, "code": {back.Query().Get("code")},
		"redirect_uri": {oauthCallback}, "code_verifier": {oauthVerifier}, "resource": {oauthResource}}, id, secret)
	if rec.Code != http.StatusOK {
		t.Fatalf("exchange: %d %v", rec.Code, out)
	}
	// A read-only approval reads through the tools and changes nothing.
	agent := &client{t: t, h: h, bearer: out["access_token"].(string)}
	if text, isErr := agent.tool("list_spaces", `{}`); isErr || !strings.Contains(text, "PLAT") {
		t.Fatalf("list_spaces read-only: %s", text)
	}
	if text, isErr := agent.tool("create_item", `{"space":"PLAT","type":"task","title":"Not allowed","properties":{"priority":"low"}}`); !isErr {
		t.Fatalf("create_item read-only succeeded: %s", text)
	}
	if g := w.bert.expect("GET", "/api/oauth/grants", "", http.StatusOK)["grants"].([]any); g[0].(map[string]any)["scope"] != "read" {
		t.Fatalf("grants: %v", g)
	}
}
