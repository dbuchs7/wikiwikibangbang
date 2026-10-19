package server

import (
	"errors"
	"fmt"
	"github.com/dbuchs7/wikiwikibangbang/internal/budget"
	"github.com/dbuchs7/wikiwikibangbang/internal/buildinfo"
	"github.com/dbuchs7/wikiwikibangbang/internal/web"
	"net"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/dbuchs7/wikiwikibangbang/internal/audit"
	"github.com/dbuchs7/wikiwikibangbang/internal/auth"
	"github.com/dbuchs7/wikiwikibangbang/internal/item"
)

// authRoutes are the routes of the principal boundary (ADR-0013, ADR-0027).
func authRoutes(d Deps) []Route {
	h := authHandlers{d: d}
	return []Route{
		{Method: http.MethodPost, Pattern: "/api/login", Summary: "Log in with username and password; sets the session cookie", Access: Public, Handler: http.HandlerFunc(h.login)},
		{Method: http.MethodPost, Pattern: "/api/logout", Summary: "End the current session", Handler: http.HandlerFunc(h.logout)},
		{Method: http.MethodGet, Pattern: "/api/me", Summary: "The authenticated principal", Handler: http.HandlerFunc(h.me)},
		{Method: http.MethodPut, Pattern: "/api/me/password", Summary: "Change one's own password", Handler: http.HandlerFunc(h.setPassword)},
		{Method: http.MethodPut, Pattern: "/api/me/language", Summary: "Choose one's UI language (one of the web UI's languages, or empty)", Handler: http.HandlerFunc(h.setLanguage)},
		{Method: http.MethodGet, Pattern: "/api/tokens", Summary: "List one's own API tokens", Handler: http.HandlerFunc(h.listTokens)},
		{Method: http.MethodPost, Pattern: "/api/tokens", Summary: "Create an API token; the secret is returned once", Handler: http.HandlerFunc(h.createToken)},
		{Method: http.MethodDelete, Pattern: "/api/tokens/{id}", Summary: "Revoke one of one's own API tokens", Handler: http.HandlerFunc(h.revokeToken)},
		{Method: http.MethodGet, Pattern: "/api/users", Summary: "List accounts (administrators and space administrators)", Handler: http.HandlerFunc(h.listUsers)},
		{Method: http.MethodGet, Pattern: "/api/users/names", Summary: "Usernames of the user IDs given as id parameters, for showing authors, and which of them have a profile picture", Handler: http.HandlerFunc(h.userNames)},
		{Method: http.MethodPost, Pattern: "/api/users", Summary: "Create an account (administrators)", Handler: http.HandlerFunc(h.createUser)},
		{Method: http.MethodPost, Pattern: "/api/backup", Summary: "Download an encrypted backup of the data directory (administrators; passphrase in the body)", Handler: http.HandlerFunc(h.backup)},
	}
}

func setSessionCookie(w http.ResponseWriter, r *http.Request, secret string) {
	http.SetCookie(w, &http.Cookie{
		Name: SessionCookie, Value: secret, Path: "/",
		MaxAge: int(auth.SessionLifetime.Seconds()), HttpOnly: true,
		SameSite: http.SameSiteStrictMode, Secure: isSecure(r),
	})
}

type authHandlers struct {
	d Deps
}

func (h authHandlers) record(e audit.Entry) { h.d.DB.Audit.Record(e) }

type userJSON struct {
	ID       auth.UserID `json:"id"`
	Username string      `json:"username"`
}

type tokenJSON struct {
	ID      string     `json:"id"`
	Name    string     `json:"name"`
	Scope   auth.Scope `json:"scope"`
	Created time.Time  `json:"created"`
	Expires time.Time  `json:"expires"`
}

func toTokenJSON(t auth.Token) tokenJSON {
	return tokenJSON{ID: t.ID, Name: t.Name, Scope: t.Scope, Created: t.Created, Expires: t.Expires}
}

// clientAddr is the peer address. Forwarding headers are not trusted: they
// are set by whoever sends the request (ADR-0027).
func clientAddr(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func (h authHandlers) login(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := readJSON(w, r, &req); err != nil {
		badRequest(w, err)
		return
	}
	addr := clientAddr(r)
	secret, p, err := h.d.DB.Accounts.Login(req.Username, req.Password, addr)
	var throttled *auth.ThrottledError
	switch {
	case errors.As(err, &throttled):
		h.record(audit.Entry{Event: audit.LoginThrottled, Addr: addr, Detail: map[string]string{"username": req.Username}})
		w.Header().Set("Retry-After", strconv.Itoa(int(throttled.Wait.Seconds())+1))
		writeError(w, http.StatusTooManyRequests, "too many failed attempts; try again later")
		return
	case err != nil:
		h.record(audit.Entry{Event: audit.LoginFailed, Addr: addr, Detail: map[string]string{"username": req.Username}})
		writeError(w, http.StatusUnauthorized, "wrong username or password")
		return
	}
	h.record(audit.Entry{Event: audit.Login, Actor: string(p.User), Addr: addr})
	setSessionCookie(w, r, secret)
	writeJSON(w, http.StatusOK, map[string]userJSON{"user": {ID: p.User, Username: p.Username}})
}

func (h authHandlers) logout(w http.ResponseWriter, r *http.Request) {
	p, _ := PrincipalFrom(r.Context())
	if p.Via == "session" {
		h.d.DB.Accounts.Logout(p.Session)
		h.record(audit.Entry{Event: audit.Logout, Actor: string(p.User), Addr: clientAddr(r)})
	}
	http.SetCookie(w, &http.Cookie{Name: SessionCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteStrictMode, Secure: isSecure(r)})
	w.WriteHeader(http.StatusNoContent)
}

func (h authHandlers) isAdmin(p auth.Principal) bool { return h.hasSystemRole(p, item.RoleAdmin) }

func (h authHandlers) hasSystemRole(p auth.Principal, role string) bool {
	held := false
	h.d.DB.Read(func(s *item.Store) { held = s.HasSystemRole(string(p.User), role) })
	return held
}

func (h authHandlers) me(w http.ResponseWriter, r *http.Request) {
	p, _ := PrincipalFrom(r.Context())
	writeJSON(w, http.StatusOK, map[string]any{
		"user":  userJSON{ID: p.User, Username: p.Username},
		"via":   p.Via,
		"scope": p.Scope,
		"admin": h.isAdmin(p),
		// Whether the caller may erase content for good (ADR-0026), so the
		// UI offers redaction only to them.
		"erasure": h.hasSystemRole(p, item.RoleErasure),
		// avatar names the caller's profile picture's content, or is empty
		// when they have none (ADR-0050).
		"avatar": h.avatarTagOf(p.User),
		// The UI language the user chose (ADR-0017), or empty: the UI
		// then suggests the browser's language once.
		"language":  h.d.DB.Accounts.Language(p.User),
		"languages": web.Languages,
		// The running build, for the UI's footer. Only signed-in callers
		// learn it; an anonymous one cannot probe for a vulnerable version.
		"version":  buildinfo.Get().Version,
		"revision": shortRevision(buildinfo.Get()),
	})
}

// shortRevision is the commit the binary was built from, shortened, or
// empty when the build carries none (a container image is built without
// the repository's history).
func shortRevision(in buildinfo.Info) string {
	r := in.Revision
	if len(r) > 12 {
		r = r[:12]
	}
	if r != "" && in.Modified {
		r += "+dirty"
	}
	return r
}

func (h authHandlers) setPassword(w http.ResponseWriter, r *http.Request) {
	p, _ := PrincipalFrom(r.Context())
	var req struct {
		Current string `json:"current"`
		New     string `json:"new"`
	}
	if err := readJSON(w, r, &req); err != nil {
		badRequest(w, err)
		return
	}
	switch err := h.d.DB.Accounts.SetPassword(p.User, req.Current, req.New); {
	case errors.Is(err, auth.ErrInvalid):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, auth.ErrUnauthorized):
		h.record(audit.Entry{Event: audit.PasswordFailed, Actor: string(p.User), Addr: clientAddr(r)})
		writeError(w, http.StatusForbidden, "the current password is wrong")
	case err != nil:
		writeError(w, http.StatusInternalServerError, "password not changed")
	default:
		h.record(audit.Entry{Event: audit.PasswordChanged, Actor: string(p.User), Addr: clientAddr(r)})
		w.WriteHeader(http.StatusNoContent)
	}
}

func (h authHandlers) listTokens(w http.ResponseWriter, r *http.Request) {
	p, _ := PrincipalFrom(r.Context())
	out := []tokenJSON{}
	for _, t := range h.d.DB.Accounts.Tokens(p.User) {
		out = append(out, toTokenJSON(t))
	}
	writeJSON(w, http.StatusOK, map[string]any{"tokens": out})
}

func (h authHandlers) createToken(w http.ResponseWriter, r *http.Request) {
	p, _ := PrincipalFrom(r.Context())
	var req struct {
		Name  string     `json:"name"`
		Scope auth.Scope `json:"scope"`
		Days  int        `json:"days"`
	}
	if err := readJSON(w, r, &req); err != nil {
		badRequest(w, err)
		return
	}
	if req.Scope == auth.ScopeMetrics && !h.isAdmin(p) {
		writeError(w, http.StatusForbidden, "only administrators create metrics tokens")
		return
	}
	secret, t, err := h.d.DB.Accounts.CreateToken(p.User, req.Name, req.Scope, time.Duration(req.Days)*24*time.Hour)
	if errors.Is(err, auth.ErrInvalid) {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "token not created")
		return
	}
	h.record(audit.Entry{Event: audit.TokenCreated, Actor: string(p.User), Addr: clientAddr(r), Subject: t.ID, Detail: map[string]string{"scope": string(t.Scope), "name": t.Name}})
	writeJSON(w, http.StatusCreated, map[string]any{"token": toTokenJSON(t), "secret": secret})
}

func (h authHandlers) revokeToken(w http.ResponseWriter, r *http.Request) {
	p, _ := PrincipalFrom(r.Context())
	id := r.PathValue("id")
	switch err := h.d.DB.Accounts.RevokeToken(p.User, id); {
	case errors.Is(err, auth.ErrNotFound):
		writeError(w, http.StatusNotFound, "no such token")
	case err != nil:
		writeError(w, http.StatusInternalServerError, "token not revoked")
	default:
		h.record(audit.Entry{Event: audit.TokenRevoked, Actor: string(p.User), Addr: clientAddr(r), Subject: id})
		w.WriteHeader(http.StatusNoContent)
	}
}

// listUsers lists the accounts for administrators and for space
// administrators, who choose among them whom to grant a role.
func (h authHandlers) listUsers(w http.ResponseWriter, r *http.Request) {
	p, _ := PrincipalFrom(r.Context())
	may := false
	h.d.DB.Read(func(s *item.Store) { may = s.As(string(p.User)).AdministersASpace() })
	if !may {
		writeError(w, http.StatusForbidden, "administrators and space administrators only")
		return
	}
	out := []userJSON{}
	for _, u := range h.d.DB.Accounts.Users() {
		out = append(out, userJSON{ID: u.ID, Username: u.Username})
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": out})
}

func (h authHandlers) createUser(w http.ResponseWriter, r *http.Request) {
	p, _ := PrincipalFrom(r.Context())
	if !h.isAdmin(p) {
		writeError(w, http.StatusForbidden, "administrators only")
		return
	}
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := readJSON(w, r, &req); err != nil {
		badRequest(w, err)
		return
	}
	u, err := h.d.DB.Accounts.CreateUser(req.Username, req.Password)
	switch {
	case errors.Is(err, auth.ErrInvalid):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, auth.ErrExists):
		writeError(w, http.StatusConflict, "username taken")
	case err != nil:
		writeError(w, http.StatusInternalServerError, "account not created")
	default:
		h.record(audit.Entry{Event: audit.UserCreated, Actor: string(p.User), Addr: clientAddr(r), Subject: string(u.ID), Detail: map[string]string{"username": u.Username}})
		writeJSON(w, http.StatusCreated, map[string]userJSON{"user": {ID: u.ID, Username: u.Username}})
	}
}

func (h authHandlers) setLanguage(w http.ResponseWriter, r *http.Request) {
	p, _ := PrincipalFrom(r.Context())
	var req struct {
		Language string `json:"language"`
	}
	if err := readJSON(w, r, &req); err != nil {
		badRequest(w, err)
		return
	}
	if req.Language != "" && !slices.Contains(web.Languages, req.Language) {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("language must be one of %v, or empty", web.Languages))
		return
	}
	if err := h.d.DB.Accounts.SetLanguage(p.User, req.Language); err != nil {
		writeError(w, http.StatusInternalServerError, "language not saved")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// userNames resolves user IDs to usernames for anyone signed in, so the UI
// can show who wrote or changed something instead of an ID. IDs are random
// and appear only in what the caller may read, so this reveals the names
// of people whose work the caller sees, and cannot list the accounts.
func (h authHandlers) userNames(w http.ResponseWriter, r *http.Request) {
	raw := r.URL.Query()["id"]
	if max := budget.Get().PageItems; len(raw) > max {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("at most %d ids per request", max))
		return
	}
	ids := make([]auth.UserID, len(raw))
	for i, id := range raw {
		ids[i] = auth.UserID(id)
	}
	names := map[auth.UserID]string{}
	// avatars names the content of the profile pictures of those who have
	// one (ADR-0050), so the UI shows them beside the names.
	avatars := map[auth.UserID]string{}
	for id, name := range h.d.DB.Accounts.Usernames(ids) {
		names[id] = name
		if tag := h.avatarTagOf(id); tag != "" {
			avatars[id] = tag
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"names": names, "avatars": avatars})
}

func (h authHandlers) avatarTagOf(id auth.UserID) string {
	if data, _ := h.d.DB.Accounts.Avatar(id); data != nil {
		return avatarTag(data)
	}
	return ""
}
