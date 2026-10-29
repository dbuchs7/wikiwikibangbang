// Package engineauthtest is a process engine's authorization for tests
// (ADR-0151), as atlas offers it: metadata of its tasks resource and of
// its authorization server, a consent that approves at once, a token
// endpoint that takes the client's credentials in the form only, checks
// PKCE and replaces the refresh token on every use, a revocation endpoint,
// and who a grant belongs to.
package engineauthtest

import (
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

	codes   map[string]code
	refresh map[string]string // refresh token -> person ID
	access  map[string]string // access token -> person ID
}

// Completion is a task completed at the fake engine.
type Completion struct {
	Key       string
	Person    string
	Variables map[string]any
}

type code struct {
	challenge, redirect, resource, person string
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
		ExpiresIn: 7200, codes: map[string]code{}, refresh: map[string]string{}, access: map[string]string{}}
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
	case "/.well-known/oauth-authorization-server":
		m := map[string]any{"issuer": e.URL, "authorization_endpoint": e.URL + "/oauth/authorize", "token_endpoint": e.URL + "/oauth/token",
			"code_challenge_methods_supported": []string{"S256"}, "token_endpoint_auth_methods_supported": []string{"client_secret_post"}}
		if !e.NoRevocation {
			m["revocation_endpoint"] = e.URL + "/oauth/revoke"
		}
		writeJSON(w, http.StatusOK, m)
	case "/oauth/authorize":
		e.authorize(w, r)
	case "/oauth/token":
		e.token(w, r)
	case "/oauth/revoke":
		if r.PostFormValue("client_id") != ClientID || r.PostFormValue("client_secret") != ClientSecret {
			oauthError(w, http.StatusUnauthorized, "invalid_client")
			return
		}
		if _, ok := e.refresh[r.PostFormValue("token")]; ok {
			delete(e.refresh, r.PostFormValue("token"))
			e.Revoked++
		}
		w.WriteHeader(http.StatusOK)
	case "/api/v1/tasks":
		bearer := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		person, ok := e.access[bearer]
		if !ok && (e.ListToken == "" || bearer != e.ListToken) {
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
	if !ok {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/v1/"), "/")
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

func (e *Engine) me(w http.ResponseWriter, r *http.Request) {
	person, ok := e.person(r)
	if !ok {
		w.WriteHeader(http.StatusUnauthorized)
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

// authorize is the consent: the person approves at once, or refuses.
func (e *Engine) authorize(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	e.LastAuthorize = q
	redirect, err := url.Parse(q.Get("redirect_uri"))
	if q.Get("client_id") != ClientID || err != nil || redirect.Host == "" {
		http.Error(w, "unknown client or redirect", http.StatusBadRequest)
		return
	}
	back := redirect.Query()
	back.Set("state", q.Get("state"))
	switch {
	case e.Deny:
		back.Set("error", "access_denied")
	case q.Get("response_type") != "code" || q.Get("code_challenge_method") != "S256" || q.Get("code_challenge") == "":
		back.Set("error", "invalid_request")
	case q.Get("resource") != e.Resource():
		back.Set("error", "invalid_target")
	default:
		c := token("atlasoc_")
		e.codes[c] = code{challenge: q.Get("code_challenge"), redirect: q.Get("redirect_uri"), resource: q.Get("resource"), person: e.Person.ID}
		back.Set("code", c)
	}
	redirect.RawQuery = back.Encode()
	http.Redirect(w, r, redirect.String(), http.StatusFound)
}

func (e *Engine) token(w http.ResponseWriter, r *http.Request) {
	if e.Busy {
		oauthError(w, http.StatusTooManyRequests, "temporarily_unavailable")
		return
	}
	if _, _, basic := r.BasicAuth(); basic || r.PostFormValue("client_id") != ClientID || r.PostFormValue("client_secret") != ClientSecret {
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
		if res := r.PostFormValue("resource"); res != "" && res != c.resource {
			oauthError(w, http.StatusBadRequest, "invalid_target")
			return
		}
		person = c.person
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
