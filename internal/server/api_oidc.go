package server

import (
	"crypto/subtle"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/dbuchs7/wikiwikibangbang/internal/audit"
	"github.com/dbuchs7/wikiwikibangbang/internal/sso"
)

// stateCookie binds a started single sign-on to the browser that started
// it, so a callback cannot be replayed into another browser (login CSRF).
const stateCookie = "wwbb_oidc_state"

// oidcRoutes are the single sign-on routes (ADR-0030). They are public:
// the caller has no principal yet.
func oidcRoutes(d Deps) []Route {
	h := oidcHandlers{d: d, log: d.Log}
	if h.log == nil {
		h.log = slog.New(slog.DiscardHandler)
	}
	return []Route{
		{Method: http.MethodGet, Pattern: "/api/oidc", Summary: "Whether single sign-on is configured, and its providers (id, name, kind)", Access: Public, Handler: http.HandlerFunc(h.info)},
		{Method: http.MethodGet, Pattern: "/api/oidc/login", Summary: "Start single sign-on with the provider given as provider (the first by default); redirects to it", Access: Public, Handler: http.HandlerFunc(h.login)},
		{Method: http.MethodGet, Pattern: "/api/oidc/callback", Summary: "The identity provider's redirect back; starts a session", Access: Public, Handler: http.HandlerFunc(h.callback)},
	}
}

type oidcHandlers struct {
	d   Deps
	log *slog.Logger
}

// provider finds a configured provider by its ID; an empty ID is the
// first.
func (h oidcHandlers) provider(id string) *sso.Provider {
	for _, p := range h.d.SSO {
		if id == "" || p.ID() == id {
			return p
		}
	}
	return nil
}

func (h oidcHandlers) info(w http.ResponseWriter, _ *http.Request) {
	if len(h.d.SSO) == 0 {
		writeJSON(w, http.StatusOK, map[string]any{"enabled": false, "providers": []any{}})
		return
	}
	type providerJSON struct {
		ID   string `json:"id"`
		Name string `json:"name"`
		Kind string `json:"kind,omitempty"`
	}
	list := []providerJSON{}
	for _, p := range h.d.SSO {
		list = append(list, providerJSON{ID: p.ID(), Name: p.Name(), Kind: p.Kind()})
	}
	// name is the first provider's, as before several were possible.
	writeJSON(w, http.StatusOK, map[string]any{"enabled": true, "name": list[0].Name, "providers": list})
}

func (h oidcHandlers) login(w http.ResponseWriter, r *http.Request) {
	p := h.provider(r.URL.Query().Get("provider"))
	if p == nil {
		writeError(w, http.StatusNotFound, "single sign-on is not configured")
		return
	}
	authURL, state, err := p.Begin(r.Context())
	if err != nil {
		h.log.Error("single sign-on cannot start", "provider", p.ID(), "err", err)
		writeError(w, http.StatusBadGateway, "the identity provider cannot be reached")
		return
	}
	// The cookie names the provider beside the state, so the one callback
	// address serves every provider. SameSite=Lax, not Strict: the
	// provider's redirect back is a cross-site navigation, and a Strict
	// cookie would not be sent with it.
	http.SetCookie(w, &http.Cookie{Name: stateCookie, Value: p.ID() + "." + state, Path: "/api/oidc/", MaxAge: int(sso.FlowLifetime.Seconds()),
		HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: isSecure(r)})
	http.Redirect(w, r, authURL, http.StatusFound)
}

// fail ends a sign-on attempt: audited, and the browser goes back to the UI
// with a generic error, never the provider's details. code tells the UI
// which message to show.
func (h oidcHandlers) fail(w http.ResponseWriter, r *http.Request, reason string, err error, code ...string) {
	h.d.DB.Audit.Record(audit.Entry{Event: audit.LoginFailed, Addr: clientAddr(r), Detail: map[string]string{"method": "oidc", "reason": reason}})
	if err != nil {
		h.log.Warn("single sign-on failed", "reason", reason, "err", err)
	}
	c := "sso"
	if len(code) > 0 {
		c = code[0]
	}
	http.Redirect(w, r, "/?login_error="+c, http.StatusFound)
}

func (h oidcHandlers) callback(w http.ResponseWriter, r *http.Request) {
	if len(h.d.SSO) == 0 {
		writeError(w, http.StatusNotFound, "single sign-on is not configured")
		return
	}
	http.SetCookie(w, &http.Cookie{Name: stateCookie, Value: "", Path: "/api/oidc/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: isSecure(r)})
	q := r.URL.Query()
	if e := q.Get("error"); e != "" {
		h.fail(w, r, "provider: "+e, nil)
		return
	}
	state := q.Get("state")
	c, err := r.Cookie(stateCookie)
	var prov *sso.Provider
	var bound string
	if err == nil {
		id, s, ok := strings.Cut(c.Value, ".")
		if ok && id != "" {
			prov, bound = h.provider(id), s
		}
	}
	if prov == nil || state == "" || subtle.ConstantTimeCompare([]byte(bound), []byte(state)) != 1 {
		h.fail(w, r, "state does not belong to this browser", err)
		return
	}
	id, err := prov.Finish(r.Context(), state, q.Get("code"))
	if errors.Is(err, sso.ErrDomain) {
		h.fail(w, r, "domain not allowed", err, "domain")
		return
	}
	if err != nil {
		h.fail(w, r, "verification", err)
		return
	}
	accounts := h.d.DB.Accounts
	u, ok := accounts.UserByIdentity(id.Issuer, id.Subject)
	if !ok {
		// Just-in-time provisioning (ADR-0013): a new account without roles.
		// It is never linked to an existing account by e-mail address,
		// which would let whoever controls the provider take that account.
		if u, err = accounts.CreateExternalUser(id.Username, id.Issuer, id.Subject); err != nil {
			h.fail(w, r, "provisioning", err)
			return
		}
		h.d.DB.Audit.Record(audit.Entry{Event: audit.UserCreated, Addr: clientAddr(r), Subject: string(u.ID),
			Detail: map[string]string{"username": u.Username, "method": "oidc", "provider": prov.ID(), "issuer": id.Issuer}})
	}
	secret, p, err := accounts.StartSession(u.ID)
	if err != nil {
		h.fail(w, r, "session", err)
		return
	}
	h.d.DB.Audit.Record(audit.Entry{Event: audit.Login, Actor: string(p.User), Addr: clientAddr(r), Detail: map[string]string{"method": "oidc", "provider": prov.ID()}})
	setSessionCookie(w, r, secret)
	http.Redirect(w, r, "/", http.StatusFound)
}
