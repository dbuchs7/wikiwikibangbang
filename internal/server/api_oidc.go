package server

import (
	"crypto/subtle"
	"log/slog"
	"net/http"

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
		{Method: http.MethodGet, Pattern: "/api/oidc", Summary: "Whether single sign-on is configured, and its display name", Access: Public, Handler: http.HandlerFunc(h.info)},
		{Method: http.MethodGet, Pattern: "/api/oidc/login", Summary: "Start single sign-on; redirects to the identity provider", Access: Public, Handler: http.HandlerFunc(h.login)},
		{Method: http.MethodGet, Pattern: "/api/oidc/callback", Summary: "The identity provider's redirect back; starts a session", Access: Public, Handler: http.HandlerFunc(h.callback)},
	}
}

type oidcHandlers struct {
	d   Deps
	log *slog.Logger
}

func (h oidcHandlers) info(w http.ResponseWriter, _ *http.Request) {
	if h.d.SSO == nil {
		writeJSON(w, http.StatusOK, map[string]any{"enabled": false})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"enabled": true, "name": h.d.SSO.Name()})
}

func (h oidcHandlers) login(w http.ResponseWriter, r *http.Request) {
	if h.d.SSO == nil {
		writeError(w, http.StatusNotFound, "single sign-on is not configured")
		return
	}
	authURL, state, err := h.d.SSO.Begin(r.Context())
	if err != nil {
		h.log.Error("single sign-on cannot start", "err", err)
		writeError(w, http.StatusBadGateway, "the identity provider cannot be reached")
		return
	}
	// SameSite=Lax, not Strict: the provider's redirect back is a
	// cross-site navigation, and a Strict cookie would not be sent with it.
	http.SetCookie(w, &http.Cookie{Name: stateCookie, Value: state, Path: "/api/oidc/", MaxAge: int(sso.FlowLifetime.Seconds()),
		HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: isSecure(r)})
	http.Redirect(w, r, authURL, http.StatusFound)
}

// fail ends a sign-on attempt: audited, and the browser goes back to the UI
// with a generic error, never the provider's details.
func (h oidcHandlers) fail(w http.ResponseWriter, r *http.Request, reason string, err error) {
	h.d.DB.Audit.Record(audit.Entry{Event: audit.LoginFailed, Addr: clientAddr(r), Detail: map[string]string{"method": "oidc", "reason": reason}})
	if err != nil {
		h.log.Warn("single sign-on failed", "reason", reason, "err", err)
	}
	http.Redirect(w, r, "/?login_error=sso", http.StatusFound)
}

func (h oidcHandlers) callback(w http.ResponseWriter, r *http.Request) {
	if h.d.SSO == nil {
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
	if err != nil || state == "" || subtle.ConstantTimeCompare([]byte(c.Value), []byte(state)) != 1 {
		h.fail(w, r, "state does not belong to this browser", err)
		return
	}
	id, err := h.d.SSO.Finish(r.Context(), state, q.Get("code"))
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
			Detail: map[string]string{"username": u.Username, "method": "oidc", "issuer": id.Issuer}})
	}
	secret, p, err := accounts.StartSession(u.ID)
	if err != nil {
		h.fail(w, r, "session", err)
		return
	}
	h.d.DB.Audit.Record(audit.Entry{Event: audit.Login, Actor: string(p.User), Addr: clientAddr(r), Detail: map[string]string{"method": "oidc"}})
	setSessionCookie(w, r, secret)
	http.Redirect(w, r, "/", http.StatusFound)
}
