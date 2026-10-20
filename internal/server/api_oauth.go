package server

import (
	"context"
	"encoding/json"
	"errors"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/dbuchs7/wikiwikibangbang/internal/audit"
	"github.com/dbuchs7/wikiwikibangbang/internal/auth"
	"github.com/dbuchs7/wikiwikibangbang/internal/budget"
)

// OAuth for AI assistants (ADR-0068): the MCP endpoint is an OAuth
// resource server (RFC 9728) and this server its authorization server
// (RFC 8414, RFC 6749 with PKCE). A hosted assistant such as Claude finds
// both from the 401 of the MCP endpoint, sends the person to the consent
// page and holds tokens that act as that person on the MCP endpoint only.

// OAuth paths.
const (
	ResourceMetadataPath = "/.well-known/oauth-protected-resource"
	ServerMetadataPath   = "/.well-known/oauth-authorization-server"
	AuthorizePath        = "/oauth/authorize"
	TokenPath            = "/oauth/token"
	RegisterPath         = "/oauth/register"
)

// Realm names this server in WWW-Authenticate.
const Realm = "wikiwikibangbang"

func oauthRoutes(d Deps) []Route {
	h := oauthHandlers{authHandlers{d: d}}
	routes := []Route{
		{Method: http.MethodGet, Pattern: ResourceMetadataPath, Summary: "OAuth protected resource metadata of the MCP endpoint (RFC 9728)", Access: Public, Handler: http.HandlerFunc(h.resourceMetadata)},
		{Method: http.MethodGet, Pattern: ResourceMetadataPath + MCPPath, Summary: "OAuth protected resource metadata of the MCP endpoint, at its path-suffixed address (RFC 9728)", Access: Public, Handler: http.HandlerFunc(h.resourceMetadata)},
		{Method: http.MethodGet, Pattern: ServerMetadataPath, Summary: "OAuth authorization server metadata (RFC 8414)", Access: Public, Handler: http.HandlerFunc(h.serverMetadata)},
		{Method: http.MethodGet, Pattern: AuthorizePath, Summary: "Start an OAuth authorization: checks the application and its redirect address, then leads the person to the consent page", Access: Public, Handler: http.HandlerFunc(h.authorize)},
		{Method: http.MethodPost, Pattern: TokenPath, Summary: "OAuth token endpoint: authorization_code (with PKCE) and refresh_token grants, form-encoded", Access: Public, Handler: http.HandlerFunc(h.token)},
		{Method: http.MethodGet, Pattern: "/api/oauth/request", Summary: "The application an authorization request names, for the consent page", Handler: http.HandlerFunc(h.request)},
		{Method: http.MethodPost, Pattern: "/api/oauth/approve", Summary: "Approve an application for oneself; answers the address to return to with a one-time code (sessions only)", Handler: http.HandlerFunc(h.approve)},
		{Method: http.MethodGet, Pattern: "/api/oauth/grants", Summary: "The applications one has approved", Handler: http.HandlerFunc(h.listGrants)},
		{Method: http.MethodDelete, Pattern: "/api/oauth/grants/{client}", Summary: "Withdraw one's approval of an application, ending its tokens", Handler: http.HandlerFunc(h.revokeGrant)},
		{Method: http.MethodGet, Pattern: "/api/oauth/clients", Summary: "The registered applications and this server's OAuth addresses (administrators)", Handler: http.HandlerFunc(h.listClients)},
		{Method: http.MethodPost, Pattern: "/api/oauth/clients", Summary: "Register an application; its secret is returned once (administrators)", Handler: http.HandlerFunc(h.createClient)},
		{Method: http.MethodDelete, Pattern: "/api/oauth/clients/{id}", Summary: "Remove an application with every approval and token of it (administrators)", Handler: http.HandlerFunc(h.removeClient)},
	}
	// Off means absent: a client that finds no registration endpoint in
	// the metadata knows to ask for credentials (ADR-0068).
	if d.OAuthRegistration {
		routes = append(routes, Route{Method: http.MethodPost, Pattern: RegisterPath, Summary: "Dynamic client registration (RFC 7591); the application is marked as self-registered", Access: Public, Handler: http.HandlerFunc(h.register)})
	}
	return routes
}

type oauthHandlers struct{ authHandlers }

type publicKey struct{}

// WithPublicURL makes public (WWBB_PUBLIC_URL, e.g.
// https://wiki.example.org) the address this server names itself by in
// OAuth; empty leaves the address each request arrived at (ADR-0068).
func WithPublicURL(public string, h http.Handler) http.Handler {
	public = strings.TrimRight(public, "/")
	if public == "" {
		return h
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), publicKey{}, public)))
	})
}

// origin is the address this server names itself by: the public address
// when one is set, which is what people and assistants use; otherwise the
// address the request arrived at, as the server sees it, with https behind
// a trusted proxy that says so (ADR-0037). The metadata names the MCP
// endpoint at this address, which must be the address the client used, as
// RFC 9728 requires.
func origin(r *http.Request) string {
	if public, ok := r.Context().Value(publicKey{}).(string); ok {
		return public
	}
	scheme := "http"
	if isSecure(r) {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

// resource is the MCP endpoint as an OAuth resource.
func resource(r *http.Request) string { return origin(r) + MCPPath }

// resourceMetadataURL is where a client learns how to get a token for the
// MCP endpoint.
func resourceMetadataURL(r *http.Request) string { return origin(r) + ResourceMetadataPath + MCPPath }

// challenge is the WWW-Authenticate value of a 401.
func challenge(r *http.Request) string {
	return `Bearer realm="` + Realm + `", resource_metadata="` + resourceMetadataURL(r) + `"`
}

func (h oauthHandlers) resourceMetadata(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"resource":                 resource(r),
		"authorization_servers":    []string{origin(r)},
		"bearer_methods_supported": []string{"header"},
		"resource_name":            "wikiwikibangbang",
	})
}

func (h oauthHandlers) serverMetadata(w http.ResponseWriter, r *http.Request) {
	o := origin(r)
	m := map[string]any{
		"issuer":                                         o,
		"authorization_endpoint":                         o + AuthorizePath,
		"token_endpoint":                                 o + TokenPath,
		"response_types_supported":                       []string{"code"},
		"grant_types_supported":                          []string{"authorization_code", "refresh_token"},
		"code_challenge_methods_supported":               []string{"S256"},
		"token_endpoint_auth_methods_supported":          []string{"client_secret_basic", "client_secret_post", "none"},
		"authorization_response_iss_parameter_supported": true,
	}
	if h.d.OAuthRegistration {
		m["registration_endpoint"] = o + RegisterPath
	}
	writeJSON(w, http.StatusOK, m)
}

// authRequest is an authorization request as the consent page passes it
// on.
type authRequest struct {
	ClientID  string `json:"client_id"`
	Redirect  string `json:"redirect_uri"`
	State     string `json:"state"`
	Challenge string `json:"code_challenge"`
	Method    string `json:"code_challenge_method"`
	Resource  string `json:"resource"`
	Scope     string `json:"scope"`
}

func authRequestFrom(q url.Values) authRequest {
	return authRequest{ClientID: q.Get("client_id"), Redirect: q.Get("redirect_uri"), State: q.Get("state"),
		Challenge: q.Get("code_challenge"), Method: q.Get("code_challenge_method"), Resource: q.Get("resource"), Scope: q.Get("scope")}
}

func (a authRequest) values() url.Values {
	v := url.Values{"client_id": {a.ClientID}, "redirect_uri": {a.Redirect}, "code_challenge": {a.Challenge}, "code_challenge_method": {a.Method}}
	for k, s := range map[string]string{"state": a.State, "resource": a.Resource, "scope": a.Scope} {
		if s != "" {
			v.Set(k, s)
		}
	}
	return v
}

// check validates what can be validated before the person is asked. A
// problem with the client or its redirect address is shown to the person,
// never sent to an address that is not known to be the client's; anything
// else goes back to the client (RFC 6749 §4.1.2.1).
func (h oauthHandlers) check(r *http.Request, a authRequest) (auth.OAuthClient, string, error) {
	c, err := h.d.DB.Accounts.CheckAuthorization(a.ClientID, a.Redirect)
	if err != nil {
		return c, "", err
	}
	switch {
	case a.Challenge == "" || a.Method != "S256":
		return c, "invalid_request", nil
	case a.Resource != "" && a.Resource != resource(r):
		return c, "invalid_target", nil
	}
	return c, "", nil
}

// back sends the person to the client's redirect address with params.
func back(redirect string, params url.Values) string {
	u, _ := url.Parse(redirect)
	q := u.Query()
	for k, v := range params {
		q[k] = v
	}
	u.RawQuery = q.Encode()
	return u.String()
}

func (h oauthHandlers) authorize(w http.ResponseWriter, r *http.Request) {
	a := authRequestFrom(r.URL.Query())
	if r.URL.Query().Get("response_type") != "code" && a.ClientID != "" {
		if _, err := h.d.DB.Accounts.CheckAuthorization(a.ClientID, a.Redirect); err == nil {
			http.Redirect(w, r, back(a.Redirect, url.Values{"error": {"unsupported_response_type"}, "state": {a.State}, "iss": {origin(r)}}), http.StatusFound)
			return
		}
	}
	_, problem, err := h.check(r, a)
	if err != nil {
		// The consent page explains the refusal; nothing goes back.
		http.Redirect(w, r, "/#/oauth?"+url.Values{"error": {"client"}}.Encode(), http.StatusFound)
		return
	}
	if problem != "" {
		http.Redirect(w, r, back(a.Redirect, url.Values{"error": {problem}, "state": {a.State}, "iss": {origin(r)}}), http.StatusFound)
		return
	}
	http.Redirect(w, r, "/#/oauth?"+a.values().Encode(), http.StatusFound)
}

// request shows the consent page whom it is asked for.
func (h oauthHandlers) request(w http.ResponseWriter, r *http.Request) {
	a := authRequestFrom(r.URL.Query())
	c, problem, err := h.check(r, a)
	if err != nil || problem != "" {
		writeError(w, http.StatusBadRequest, "this authorization request cannot be approved: the application or its return address is not registered here")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"client": map[string]any{"id": c.ID, "name": c.Name, "self": c.Self}, "redirect_host": hostOf(a.Redirect)})
}

func hostOf(s string) string {
	u, err := url.Parse(s)
	if err != nil {
		return ""
	}
	return u.Host
}

func (h oauthHandlers) approve(w http.ResponseWriter, r *http.Request) {
	p, _ := PrincipalFrom(r.Context())
	// Approving is the person's own act: a token, which an application
	// holds, cannot approve an application.
	if p.Via != "session" {
		writeError(w, http.StatusForbidden, "only a signed-in person approves an application")
		return
	}
	var req struct {
		authRequest
		Deny bool `json:"deny"`
		// Access is what the person allows: full (read and write, the
		// default) or read.
		Access auth.Scope `json:"access"`
	}
	if !decode(w, r, &req) {
		return
	}
	a := req.authRequest
	_, problem, err := h.check(r, a)
	if err != nil {
		writeError(w, http.StatusBadRequest, "the application or its return address is not registered here")
		return
	}
	iss := origin(r)
	if problem != "" || req.Deny {
		if problem == "" {
			problem = "access_denied"
		}
		writeJSON(w, http.StatusOK, map[string]string{"redirect": back(a.Redirect, url.Values{"error": {problem}, "state": {a.State}, "iss": {iss}})})
		return
	}
	target := a.Resource
	if target == "" {
		target = resource(r)
	}
	access := req.Access
	if access == "" {
		access = auth.ScopeFull
	}
	code, err := h.d.DB.Accounts.IssueCode(p.User, a.ClientID, a.Redirect, a.Challenge, target, access)
	switch {
	case errors.Is(err, auth.ErrFull):
		writeError(w, http.StatusServiceUnavailable, "too many approvals are pending; try again in a minute")
		return
	case err != nil:
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	h.record(audit.Entry{Event: audit.OAuthApproved, Actor: string(p.User), Addr: clientAddr(r), Subject: a.ClientID, Detail: map[string]string{"scope": string(access)}})
	writeJSON(w, http.StatusOK, map[string]string{"redirect": back(a.Redirect, url.Values{"code": {code}, "state": {a.State}, "iss": {iss}})})
}

// oauthError answers the token or registration endpoint as RFC 6749 §5.2
// says.
func oauthError(w http.ResponseWriter, status int, code, desc string) {
	if status == http.StatusUnauthorized {
		w.Header().Set("WWW-Authenticate", `Basic realm="`+Realm+`"`)
	}
	w.Header().Set("Pragma", "no-cache")
	writeJSON(w, status, map[string]string{"error": code, "error_description": desc})
}

func (h oauthHandlers) token(w http.ResponseWriter, r *http.Request) {
	if mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || mt != "application/x-www-form-urlencoded" {
		oauthError(w, http.StatusBadRequest, "invalid_request", "the body must be application/x-www-form-urlencoded")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, budget.Get().RequestBody)
	if err := r.ParseForm(); err != nil {
		oauthError(w, http.StatusBadRequest, "invalid_request", "the form cannot be read")
		return
	}
	f := r.PostForm
	id, secret := f.Get("client_id"), f.Get("client_secret")
	if u, s, ok := r.BasicAuth(); ok {
		// RFC 6749 §2.3.1: the credentials are form-encoded inside Basic.
		u, _ = url.QueryUnescape(u)
		s, _ = url.QueryUnescape(s)
		if id != "" && id != u {
			oauthError(w, http.StatusBadRequest, "invalid_request", "two different client identifiers")
			return
		}
		id, secret = u, s
	}
	accounts := h.d.DB.Accounts
	var run func() (auth.OAuthTokens, error)
	switch f.Get("grant_type") {
	case "authorization_code":
		run = func() (auth.OAuthTokens, error) {
			return accounts.ExchangeCode(id, secret, f.Get("code"), f.Get("redirect_uri"), f.Get("code_verifier"), f.Get("resource"))
		}
	case "refresh_token":
		run = func() (auth.OAuthTokens, error) { return accounts.Refresh(id, secret, f.Get("refresh_token")) }
	default:
		oauthError(w, http.StatusBadRequest, "unsupported_grant_type", "authorization_code and refresh_token are supported")
		return
	}
	addr := clientAddr(r)
	tok, err := accounts.ThrottleOAuth(addr, run)
	var throttled *auth.ThrottledError
	switch {
	case errors.As(err, &throttled):
		w.Header().Set("Retry-After", strconv.Itoa(int(throttled.Wait.Seconds())+1))
		oauthError(w, http.StatusTooManyRequests, "slow_down", "too many failed attempts; try again later")
		return
	case errors.Is(err, auth.ErrInvalidClient):
		h.record(audit.Entry{Event: audit.OAuthFailed, Addr: addr, Subject: id, Detail: map[string]string{"error": "invalid_client"}})
		oauthError(w, http.StatusUnauthorized, "invalid_client", "the application is unknown or its secret is wrong")
		return
	case errors.Is(err, auth.ErrInvalidGrant):
		h.record(audit.Entry{Event: audit.OAuthFailed, Addr: addr, Subject: id, Detail: map[string]string{"error": "invalid_grant"}})
		oauthError(w, http.StatusBadRequest, "invalid_grant", "the code or refresh token is unknown, used, expired or not this application's")
		return
	case err != nil:
		h.d.Log.Error("token not issued", "err", err)
		oauthError(w, http.StatusInternalServerError, "server_error", "the token was not issued")
		return
	}
	w.Header().Set("Pragma", "no-cache")
	writeJSON(w, http.StatusOK, map[string]any{
		"access_token":  tok.Access,
		"token_type":    "Bearer",
		"expires_in":    int(tok.ExpiresIn / time.Second),
		"refresh_token": tok.Refresh,
	})
}

// register is dynamic client registration (RFC 7591). It is open to
// anyone when switched on; what it creates is marked self-registered all
// the way to the consent page, and bounded (ADR-0068).
func (h oauthHandlers) register(w http.ResponseWriter, r *http.Request) {
	if mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || mt != "application/json" {
		oauthError(w, http.StatusBadRequest, "invalid_client_metadata", "the body must be application/json")
		return
	}
	var req struct {
		Name       string   `json:"client_name"`
		Redirects  []string `json:"redirect_uris"`
		AuthMethod string   `json:"token_endpoint_auth_method"`
		Grants     []string `json:"grant_types"`
		Responses  []string `json:"response_types"`
	}
	// Unknown metadata is ignored, as RFC 7591 §2 allows.
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, budget.Get().RequestBody)).Decode(&req); err != nil {
		oauthError(w, http.StatusBadRequest, "invalid_client_metadata", "the body is no JSON object")
		return
	}
	method := req.AuthMethod
	if method == "client_secret_basic" {
		method = auth.AuthClientSecret
	}
	for _, g := range req.Grants {
		if g != "authorization_code" && g != "refresh_token" {
			oauthError(w, http.StatusBadRequest, "invalid_client_metadata", "grant_types are authorization_code and refresh_token")
			return
		}
	}
	name := req.Name
	if strings.TrimSpace(name) == "" {
		name = hostOf(firstOf(req.Redirects))
	}
	c, secret, err := h.d.DB.Accounts.CreateOAuthClient(name, req.Redirects, method, true)
	switch {
	case errors.Is(err, auth.ErrInvalid):
		code := "invalid_client_metadata"
		if strings.Contains(err.Error(), "redirect") {
			code = "invalid_redirect_uri"
		}
		oauthError(w, http.StatusBadRequest, code, err.Error())
		return
	case errors.Is(err, auth.ErrFull):
		oauthError(w, http.StatusServiceUnavailable, "temporarily_unavailable", "no more applications can register themselves now")
		return
	case err != nil:
		h.d.Log.Error("application not registered", "err", err)
		oauthError(w, http.StatusInternalServerError, "server_error", "the application was not registered")
		return
	}
	h.record(audit.Entry{Event: audit.OAuthClientCreated, Addr: clientAddr(r), Subject: c.ID, Detail: map[string]string{"name": c.Name, "self": "true"}})
	out := map[string]any{
		"client_id":                  c.ID,
		"client_id_issued_at":        c.Created.Unix(),
		"client_name":                c.Name,
		"redirect_uris":              c.Redirects,
		"grant_types":                []string{"authorization_code", "refresh_token"},
		"response_types":             []string{"code"},
		"token_endpoint_auth_method": c.Method,
	}
	if secret != "" {
		out["client_secret"] = secret
		out["client_secret_expires_at"] = 0
	}
	writeJSON(w, http.StatusCreated, out)
}

func firstOf(s []string) string {
	if len(s) == 0 {
		return ""
	}
	return s[0]
}

type clientJSON struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Redirects []string  `json:"redirects"`
	Self      bool      `json:"self"`
	Public    bool      `json:"public"`
	Created   time.Time `json:"created"`
}

func toClientJSON(c auth.OAuthClient) clientJSON {
	return clientJSON{ID: c.ID, Name: c.Name, Redirects: c.Redirects, Self: c.Self, Public: c.Method == auth.AuthNone, Created: c.Created}
}

func (h oauthHandlers) admin(w http.ResponseWriter, r *http.Request) (auth.Principal, bool) {
	p, _ := PrincipalFrom(r.Context())
	if !h.isAdmin(p) {
		writeError(w, http.StatusForbidden, "administrators only")
		return p, false
	}
	return p, true
}

// listClients also names this server's OAuth addresses as it sees them,
// so the page can show what to enter in the assistant and check that the
// address it was opened at is the one the server publishes.
func (h oauthHandlers) listClients(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.admin(w, r); !ok {
		return
	}
	out := []clientJSON{}
	for _, c := range h.d.DB.Accounts.OAuthClients() {
		out = append(out, toClientJSON(c))
	}
	writeJSON(w, http.StatusOK, map[string]any{"clients": out, "origin": origin(r), "mcp": resource(r), "registration": h.d.OAuthRegistration})
}

func (h oauthHandlers) createClient(w http.ResponseWriter, r *http.Request) {
	p, ok := h.admin(w, r)
	if !ok {
		return
	}
	var req struct {
		Name      string   `json:"name"`
		Redirects []string `json:"redirects"`
		Public    bool     `json:"public"`
	}
	if !decode(w, r, &req) {
		return
	}
	method := auth.AuthClientSecret
	if req.Public {
		method = auth.AuthNone
	}
	c, secret, err := h.d.DB.Accounts.CreateOAuthClient(req.Name, req.Redirects, method, false)
	switch {
	case errors.Is(err, auth.ErrInvalid):
		writeError(w, http.StatusBadRequest, err.Error())
		return
	case err != nil:
		writeError(w, http.StatusInternalServerError, "the application was not registered")
		return
	}
	h.record(audit.Entry{Event: audit.OAuthClientCreated, Actor: string(p.User), Addr: clientAddr(r), Subject: c.ID, Detail: map[string]string{"name": c.Name}})
	writeJSON(w, http.StatusCreated, map[string]any{"client": toClientJSON(c), "secret": secret})
}

func (h oauthHandlers) removeClient(w http.ResponseWriter, r *http.Request) {
	p, ok := h.admin(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	switch err := h.d.DB.Accounts.RemoveOAuthClient(id); {
	case errors.Is(err, auth.ErrNotFound):
		writeError(w, http.StatusNotFound, "no such application")
	case err != nil:
		writeError(w, http.StatusInternalServerError, "the application was not removed")
	default:
		h.record(audit.Entry{Event: audit.OAuthClientRemoved, Actor: string(p.User), Addr: clientAddr(r), Subject: id})
		w.WriteHeader(http.StatusNoContent)
	}
}

func (h oauthHandlers) listGrants(w http.ResponseWriter, r *http.Request) {
	p, _ := PrincipalFrom(r.Context())
	type grantJSON struct {
		Client   string     `json:"client"`
		Name     string     `json:"name"`
		Self     bool       `json:"self"`
		Scope    auth.Scope `json:"scope"`
		Created  time.Time  `json:"created"`
		LastUsed time.Time  `json:"last_used"`
	}
	out := []grantJSON{}
	for _, g := range h.d.DB.Accounts.Grants(p.User) {
		c, _ := h.d.DB.Accounts.OAuthClientByID(g.Client)
		out = append(out, grantJSON{Client: g.Client, Name: c.Name, Self: c.Self, Scope: g.Scope, Created: g.Created, LastUsed: g.LastUsed})
	}
	writeJSON(w, http.StatusOK, map[string]any{"grants": out})
}

func (h oauthHandlers) revokeGrant(w http.ResponseWriter, r *http.Request) {
	p, _ := PrincipalFrom(r.Context())
	id := r.PathValue("client")
	switch err := h.d.DB.Accounts.RevokeGrant(p.User, id); {
	case errors.Is(err, auth.ErrNotFound):
		writeError(w, http.StatusNotFound, "no such approval")
	case err != nil:
		writeError(w, http.StatusInternalServerError, "the approval was not withdrawn")
	default:
		h.record(audit.Entry{Event: audit.OAuthRevoked, Actor: string(p.User), Addr: clientAddr(r), Subject: id})
		w.WriteHeader(http.StatusNoContent)
	}
}
