// Package engineauthtest is a process engine's authorization for tests
// (ADR-0151), as atlas offers it: metadata of its tasks resource and of
// its authorization server, a consent that approves at once, a token
// endpoint that takes the client's credentials in the form only, checks
// PKCE and replaces the refresh token on every use, a revocation endpoint,
// and who a grant belongs to. It also connects a system as atlas's
// connected systems do (ADR-0152): a client named by its metadata
// document and authenticated by a signed assertion, a system grant an
// administrator approves with the capabilities they keep, pre-consent for
// people's grants, prompt=none, and a task that ended told from one out
// of reach.
package engineauthtest

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// Client is the registration of the server under test.
const (
	ClientID     = "wiki-client"
	ClientSecret = "atlascs_wiki-secret"
)

// Person is who signs in at the engine and approves.
type Person struct {
	ID, Username, Email, DisplayName string
	Groups                           []string
}

// Engine is the fake engine. Its fields may be changed between requests.
type Engine struct {
	*httptest.Server

	mu sync.Mutex
	// Person approves every consent.
	Person Person
	// Deny refuses every consent, as a person who clicks Deny.
	Deny bool
	// Busy answers the token endpoint 429, as atlas's shared throttle.
	Busy bool
	// NoRevocation leaves the revocation endpoint out of the metadata.
	NoRevocation bool
	// ExpiresIn is the access tokens' lifetime in seconds.
	ExpiresIn int
	// Refreshes counts the renewals the token endpoint granted;
	// Revoked the refresh tokens revoked.
	Refreshes, Revoked int
	// LastAuthorize is the query of the last consent asked for.
	LastAuthorize url.Values
	// Tasks are the open user tasks, as GET /api/v1/tasks answers them:
	// all of them for ListToken, and to a person's grant those whose key
	// Sees lists for the person's ID.
	Tasks     []map[string]any
	ListToken string
	Sees      map[string][]uint64
	// Forms are the forms by ID, their schemas; Values the variables of
	// every task's scope. Completed records each completion: the task's
	// key, who completed it and the variables sent.
	Forms     map[string]map[string]any
	Values    map[string]any
	Completed []Completion

	// Documents answers a client metadata document by its client ID, as
	// the engine reads it from the client's address (ADR-0152); without
	// it only the registered client is known.
	Documents func(clientID string) (map[string]any, error)
	// Admin approves system grants; Keep is the capabilities the admin
	// keeps of those asked for, all when nil; Preconsent approves
	// people's grants beforehand; NoSession answers prompt=none as for a
	// person without a session; NoSystem offers no system grant.
	Admin      Person
	Keep       []string
	Preconsent bool
	NoSession  bool
	NoSystem   bool
	// Ended says how a task no longer open ended, by key; a key not in
	// Tasks nor here is out of reach.
	Ended map[uint64]string
	// Assertions counts the client assertions the engine accepted.
	Assertions int
	// System is the system grant standing, nil before one.
	System *SystemGrant

	codes   map[string]code
	refresh map[string]string // refresh token -> person ID
	access  map[string]string // access token -> person ID
	jtis    map[string]bool
}

// SystemGrant is a system grant as the fake engine keeps it.
type SystemGrant struct {
	Client       string
	Capabilities []string
	Preconsent   bool
}

// systemPrincipal is whose a token of the system grant is.
const systemPrincipal = "system"

// Completion is a task completed at the fake engine.
type Completion struct {
	Key       string
	Person    string
	Variables map[string]any
}

type code struct {
	challenge, redirect, resource, person, client string
	scopes                                        []string
}

func token(prefix string) string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return prefix + hex.EncodeToString(b)
}

// New starts the fake engine; it stops with the test.
func New(t *testing.T) *Engine {
	t.Helper()
	e := &Engine{Person: Person{ID: "u-17", Username: "anna", Email: "anna@example.org", DisplayName: "Anna Muster", Groups: []string{"hr", "Team Lead"}},
		Admin:     Person{ID: "u-1", Username: "patrick", Email: "patrick@example.org", DisplayName: "Patrick Admin"},
		ExpiresIn: 7200, codes: map[string]code{}, refresh: map[string]string{}, access: map[string]string{}, jtis: map[string]bool{}}
	e.Server = httptest.NewServer(e)
	t.Cleanup(e.Close)
	return e
}

// Resource is the engine's tasks resource.
func (e *Engine) Resource() string { return e.URL + "/tasks" }

// Expire forgets every access token, as a restart of the engine's
// clock would; the next call must renew.
func (e *Engine) Expire() {
	e.mu.Lock()
	e.access = map[string]string{}
	e.mu.Unlock()
}

// Withdraw revokes every grant of the person, as an account disabled at
// the engine.
func (e *Engine) Withdraw() {
	e.mu.Lock()
	e.refresh, e.access = map[string]string{}, map[string]string{}
	e.mu.Unlock()
}

// AccessToken is an access token the engine issued and still accepts,
// for tests that call its API with a person's grant.
func (e *Engine) AccessToken() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	for tok := range e.access {
		return tok
	}
	return ""
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func oauthError(w http.ResponseWriter, status int, code string) {
	writeJSON(w, status, map[string]string{"error": code})
}

func (e *Engine) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	e.mu.Lock()
	defer e.mu.Unlock()
	switch r.URL.Path {
	case "/.well-known/oauth-protected-resource/tasks":
		writeJSON(w, http.StatusOK, map[string]any{"resource": e.Resource(), "authorization_servers": []string{e.URL}, "bearer_methods_supported": []string{"header"}})
	case "/.well-known/oauth-protected-resource/system":
		if e.NoSystem {
			http.NotFound(w, r)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"resource": e.URL + "/system", "authorization_servers": []string{e.URL}, "bearer_methods_supported": []string{"header"}})
	case "/.well-known/oauth-authorization-server":
		m := map[string]any{"issuer": e.URL, "authorization_endpoint": e.URL + "/oauth/authorize", "token_endpoint": e.URL + "/oauth/token",
			"code_challenge_methods_supported": []string{"S256"}, "token_endpoint_auth_methods_supported": []string{"client_secret_post", "private_key_jwt"}}
		if !e.NoRevocation {
			m["revocation_endpoint"] = e.URL + "/oauth/revoke"
		}
		writeJSON(w, http.StatusOK, m)
	case "/oauth/authorize":
		e.authorize(w, r)
	case "/oauth/token":
		e.token(w, r)
	case "/oauth/revoke":
		if !e.authenticated(r) {
			oauthError(w, http.StatusUnauthorized, "invalid_client")
			return
		}
		if p, ok := e.refresh[r.PostFormValue("token")]; ok {
			delete(e.refresh, r.PostFormValue("token"))
			e.Revoked++
			if p == systemPrincipal {
				e.System = nil
			}
		}
		w.WriteHeader(http.StatusOK)
	case "/api/v1/tasks":
		bearer := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		person, ok := e.access[bearer]
		if ok && person == systemPrincipal {
			if !e.System.holds("tasklist") {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			ok = false // the system grant lists every open task
		} else if !ok && (e.ListToken == "" || bearer != e.ListToken) {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		items := []map[string]any{}
		for _, t := range e.Tasks {
			key, _ := t["key"].(int)
			if !ok || slices.Contains(e.Sees[person], uint64(key)) {
				items = append(items, t)
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items, "truncated": false})
	case "/api/v1/auth/me":
		e.me(w, r)
	default:
		if strings.HasPrefix(r.URL.Path, "/api/v1/") {
			e.work(w, r)
			return
		}
		http.NotFound(w, r)
	}
}

// person is whose access token a request carries.
func (e *Engine) person(r *http.Request) (string, bool) {
	p, ok := e.access[strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")]
	return p, ok
}

// work answers a person's work on a task, as atlas does: a task one may
// not see is 404, one held by another is 403, completing it ends it.
func (e *Engine) work(w http.ResponseWriter, r *http.Request) {
	person, ok := e.person(r)
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/v1/"), "/")
	if !ok && e.ListToken != "" && strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ") == e.ListToken && len(parts) == 2 && parts[0] == "tasks" {
		e.oneTask(w, parts[1]) // the task list credential reads one task too
		return
	}
	if !ok {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	if person == systemPrincipal {
		if len(parts) == 2 && parts[0] == "tasks" && r.Method == http.MethodGet && e.System.holds("tasklist") {
			e.oneTask(w, parts[1])
			return
		}
		w.WriteHeader(http.StatusForbidden)
		return
	}
	switch {
	case len(parts) == 2 && parts[0] == "forms" && r.Method == http.MethodGet:
		f, ok := e.Forms[parts[1]]
		if !ok {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "no form with that id"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"id": parts[1], "name": "Form " + parts[1], "schema": f})
		return
	case len(parts) == 3 && parts[0] == "instances" && parts[2] == "variables":
		writeJSON(w, http.StatusOK, e.Values)
		return
	case len(parts) < 2 || parts[0] != "tasks":
		http.NotFound(w, r)
		return
	}
	var task map[string]any
	idx := -1
	for i, t := range e.Tasks {
		if k, _ := t["key"].(int); strconv.Itoa(k) == parts[1] && slices.Contains(e.Sees[person], uint64(k)) {
			task, idx = t, i
		}
	}
	if task == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no open task with that key"})
		return
	}
	if len(parts) == 2 {
		writeJSON(w, http.StatusOK, task)
		return
	}
	if a, _ := task["assignee"].(string); a != "" && a != e.usernameOf(person) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "this task is assigned to somebody else; ask them, or an operator, to act on it"})
		return
	}
	switch parts[2] {
	case "claim":
		task["assignee"] = e.usernameOf(person)
	case "unclaim":
		delete(task, "assignee")
	case "complete":
		var body struct {
			Variables map[string]any `json:"variables"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.Variables["approved"] == "maybe" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "approved: not a value of the list", "code": "invalid_value"})
			return
		}
		e.Completed = append(e.Completed, Completion{Key: parts[1], Person: person, Variables: body.Variables})
		e.Tasks = slices.Delete(e.Tasks, idx, idx+1)
		writeJSON(w, http.StatusOK, map[string]any{"taskKey": task["key"]})
		return
	default:
		http.NotFound(w, r)
		return
	}
	a, _ := task["assignee"].(string)
	writeJSON(w, http.StatusOK, map[string]any{"taskKey": task["key"], "assignee": a})
}

// usernameOf is the username of the person with the ID: Person's, or the
// ID itself for the others.
func (e *Engine) usernameOf(id string) string {
	if id == e.Person.ID {
		return e.Person.Username
	}
	return id
}

// oneTask answers one task to the system grant or the task list
// credential: open, ended (410 with how), or not found (ADR-0152).
func (e *Engine) oneTask(w http.ResponseWriter, key string) {
	for _, t := range e.Tasks {
		if k, _ := t["key"].(int); strconv.Itoa(k) == key {
			writeJSON(w, http.StatusOK, t)
			return
		}
	}
	k, _ := strconv.ParseUint(key, 10, 64)
	if how, ok := e.Ended[k]; ok {
		writeJSON(w, http.StatusGone, map[string]any{"key": key, "ended": how, "endedAt": "2026-10-30T10:00:00Z"})
		return
	}
	writeJSON(w, http.StatusNotFound, map[string]string{"error": "no task with that key"})
}

func (g *SystemGrant) holds(scope string) bool {
	return g != nil && slices.Contains(g.Capabilities, scope)
}

func (e *Engine) me(w http.ResponseWriter, r *http.Request) {
	person, ok := e.person(r)
	if !ok {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	if person == systemPrincipal {
		if e.System == nil {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		caps := []map[string]any{}
		for _, c := range e.System.Capabilities {
			caps = append(caps, map[string]any{"scope": c, "projects": []string{}})
		}
		writeJSON(w, http.StatusOK, map[string]any{"authEnabled": true,
			"user": map[string]any{"id": e.Admin.ID, "username": e.Admin.Username, "email": e.Admin.Email, "displayName": e.Admin.DisplayName, "roles": []string{"admin"}, "groups": []any{}},
			"system": map[string]any{"clientId": e.System.Client, "clientName": "Example wiki", "approvedBy": e.Admin.Username,
				"approvedAt": "2026-10-30T09:00:00Z", "capabilities": caps, "preconsent": e.System.Preconsent}})
		return
	}
	if person != e.Person.ID {
		writeJSON(w, http.StatusOK, map[string]any{"authEnabled": true, "user": map[string]any{"id": person, "username": person, "groups": []any{}}})
		return
	}
	groups := []map[string]string{}
	for i, g := range e.Person.Groups {
		groups = append(groups, map[string]string{"id": "g" + string(rune('1'+i)), "name": g})
	}
	writeJSON(w, http.StatusOK, map[string]any{"authEnabled": true, "user": map[string]any{"id": e.Person.ID, "username": e.Person.Username,
		"email": e.Person.Email, "displayName": e.Person.DisplayName, "roles": []string{"user"}, "groups": groups}})
}

// authorize is the consent: the person approves at once, or refuses. A
// client named by its metadata document must list the redirect; a system
// grant is approved by Admin with the capabilities Keep keeps; with
// prompt=none no page is shown, so a person's grant comes about only
// when it was approved beforehand and they have a session.
func (e *Engine) authorize(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	e.LastAuthorize = q
	redirect, err := url.Parse(q.Get("redirect_uri"))
	known := q.Get("client_id") == ClientID
	if !known && e.Documents != nil && strings.HasPrefix(q.Get("client_id"), "http") {
		if doc, derr := e.Documents(q.Get("client_id")); derr == nil {
			uris, _ := doc["redirect_uris"].([]string)
			known = doc["client_id"] == q.Get("client_id") && slices.Contains(uris, q.Get("redirect_uri"))
		}
	}
	if !known || err != nil || redirect.Host == "" {
		http.Error(w, "unknown client or redirect", http.StatusBadRequest)
		return
	}
	back := redirect.Query()
	back.Set("state", q.Get("state"))
	system := q.Get("resource") == e.URL+"/system"
	switch {
	case e.Deny:
		back.Set("error", "access_denied")
	case q.Get("response_type") != "code" || q.Get("code_challenge_method") != "S256" || q.Get("code_challenge") == "":
		back.Set("error", "invalid_request")
	case system && (e.NoSystem || q.Get("client_id") == ClientID):
		back.Set("error", "invalid_target")
	case !system && q.Get("resource") != e.Resource():
		back.Set("error", "invalid_target")
	case q.Get("prompt") == "none" && e.NoSession:
		back.Set("error", "login_required")
	case q.Get("prompt") == "none" && (system || e.System == nil || !e.System.Preconsent || e.System.Client != q.Get("client_id")):
		back.Set("error", "consent_required")
	default:
		c := token("atlasoc_")
		cd := code{challenge: q.Get("code_challenge"), redirect: q.Get("redirect_uri"), resource: q.Get("resource"), person: e.Person.ID, client: q.Get("client_id")}
		if system {
			cd.person = systemPrincipal
			for _, sc := range strings.Fields(q.Get("scope")) {
				if e.Keep == nil || slices.Contains(e.Keep, sc) {
					cd.scopes = append(cd.scopes, sc)
				}
			}
		}
		e.codes[c] = cd
		back.Set("code", c)
	}
	redirect.RawQuery = back.Encode()
	http.Redirect(w, r, redirect.String(), http.StatusFound)
}

// authenticated checks a request's client credentials: the registered
// client's secret, or a client assertion signed with a key of the client's
// metadata document (RFC 7523), for this engine's token endpoint, used
// once.
func (e *Engine) authenticated(r *http.Request) bool {
	if _, _, basic := r.BasicAuth(); basic {
		return false
	}
	id := r.PostFormValue("client_id")
	if id == ClientID {
		return r.PostFormValue("client_secret") == ClientSecret
	}
	if e.Documents == nil || r.PostFormValue("client_assertion_type") != "urn:ietf:params:oauth:client-assertion-type:jwt-bearer" {
		return false
	}
	doc, err := e.Documents(id)
	if err != nil {
		return false
	}
	parts := strings.Split(r.PostFormValue("client_assertion"), ".")
	if len(parts) != 3 {
		return false
	}
	var header struct{ Alg, Kid string }
	var claims struct {
		Iss, Sub, Aud, Jti string
		Exp                int64
	}
	h, _ := base64.RawURLEncoding.DecodeString(parts[0])
	c, _ := base64.RawURLEncoding.DecodeString(parts[1])
	sig, _ := base64.RawURLEncoding.DecodeString(parts[2])
	if json.Unmarshal(h, &header) != nil || json.Unmarshal(c, &claims) != nil || header.Alg != "EdDSA" {
		return false
	}
	keys, _ := doc["jwks"].(map[string]any)["keys"].([]map[string]string)
	for _, k := range keys {
		x, _ := base64.RawURLEncoding.DecodeString(k["x"])
		if k["kid"] != header.Kid || len(x) != ed25519.PublicKeySize || !ed25519.Verify(x, []byte(parts[0]+"."+parts[1]), sig) {
			continue
		}
		if claims.Iss != id || claims.Sub != id || claims.Aud != e.URL+"/oauth/token" || claims.Exp < time.Now().Unix() || e.jtis[claims.Jti] {
			return false
		}
		e.jtis[claims.Jti] = true
		e.Assertions++
		return true
	}
	return false
}

func (e *Engine) token(w http.ResponseWriter, r *http.Request) {
	if e.Busy {
		oauthError(w, http.StatusTooManyRequests, "temporarily_unavailable")
		return
	}
	if !e.authenticated(r) {
		oauthError(w, http.StatusUnauthorized, "invalid_client")
		return
	}
	var person string
	switch r.PostFormValue("grant_type") {
	case "authorization_code":
		c, ok := e.codes[r.PostFormValue("code")]
		delete(e.codes, r.PostFormValue("code"))
		sum := sha256.Sum256([]byte(r.PostFormValue("code_verifier")))
		if !ok || c.redirect != r.PostFormValue("redirect_uri") || base64.RawURLEncoding.EncodeToString(sum[:]) != c.challenge {
			oauthError(w, http.StatusBadRequest, "invalid_grant")
			return
		}
		if res := r.PostFormValue("resource"); res != "" && res != c.resource || c.client != r.PostFormValue("client_id") {
			oauthError(w, http.StatusBadRequest, "invalid_target")
			return
		}
		person = c.person
		if person == systemPrincipal {
			// A new system grant replaces the one before.
			for tok, p := range e.refresh {
				if p == systemPrincipal {
					delete(e.refresh, tok)
				}
			}
			e.System = &SystemGrant{Client: c.client, Capabilities: c.scopes, Preconsent: e.Preconsent}
		}
	case "refresh_token":
		p, ok := e.refresh[r.PostFormValue("refresh_token")]
		if !ok {
			oauthError(w, http.StatusBadRequest, "invalid_grant")
			return
		}
		delete(e.refresh, r.PostFormValue("refresh_token")) // replaced on every use
		person = p
		e.Refreshes++
	default:
		oauthError(w, http.StatusBadRequest, "unsupported_grant_type")
		return
	}
	access, refresh := token("atlasoa_"), token("atlasor_")
	e.access[access], e.refresh[refresh] = person, person
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{"access_token": access, "token_type": "Bearer", "expires_in": e.ExpiresIn, "refresh_token": refresh})
}

// Lock and Unlock guard changes of the fields while requests may run.
func (e *Engine) Lock()   { e.mu.Lock() }
func (e *Engine) Unlock() { e.mu.Unlock() }
