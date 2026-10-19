package server

import (
	"errors"
	"net"
	"net/http"
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
		{Method: http.MethodGet, Pattern: "/api/tokens", Summary: "List one's own API tokens", Handler: http.HandlerFunc(h.listTokens)},
		{Method: http.MethodPost, Pattern: "/api/tokens", Summary: "Create an API token; the secret is returned once", Handler: http.HandlerFunc(h.createToken)},
		{Method: http.MethodDelete, Pattern: "/api/tokens/{id}", Summary: "Revoke one of one's own API tokens", Handler: http.HandlerFunc(h.revokeToken)},
		{Method: http.MethodGet, Pattern: "/api/users", Summary: "List accounts (administrators)", Handler: http.HandlerFunc(h.listUsers)},
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

func (h authHandlers) isAdmin(p auth.Principal) bool {
	admin := false
	h.d.DB.Read(func(s *item.Store) { admin = s.HasSystemRole(string(p.User), item.RoleAdmin) })
	return admin
}

func (h authHandlers) me(w http.ResponseWriter, r *http.Request) {
	p, _ := PrincipalFrom(r.Context())
	writeJSON(w, http.StatusOK, map[string]any{
		"user":  userJSON{ID: p.User, Username: p.Username},
		"via":   p.Via,
		"scope": p.Scope,
		"admin": h.isAdmin(p),
	})
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

func (h authHandlers) listUsers(w http.ResponseWriter, r *http.Request) {
	p, _ := PrincipalFrom(r.Context())
	if !h.isAdmin(p) {
		writeError(w, http.StatusForbidden, "administrators only")
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
