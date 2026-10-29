package server

import (
	"crypto/subtle"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/dbuchs7/wikiwikibangbang/internal/audit"
	"github.com/dbuchs7/wikiwikibangbang/internal/engineauth"
	"github.com/dbuchs7/wikiwikibangbang/internal/engineconn"
	"github.com/dbuchs7/wikiwikibangbang/internal/site"
	"github.com/dbuchs7/wikiwikibangbang/internal/storage"
)

// connectCookie binds a begun connection to an engine to the browser that
// began it, as the single sign-on's state cookie does (ADR-0030).
const connectCookie = "wwbb_engine_state"

// engineConnRoutes connect a person's account to a process engine, so the
// engine's tasks are read and worked with the person's own grant
// (ADR-0151).
func engineConnRoutes(d Deps) []Route {
	h := engineConnHandlers{authHandlers: authHandlers{d: d}, log: d.Log}
	if h.log == nil {
		h.log = slog.New(slog.DiscardHandler)
	}
	return []Route{
		{Method: http.MethodGet, Pattern: "/api/me/engines", Summary: "The process engines one may connect one's account to, and who one is at each one connected: the engine's username, name, mail address and groups, and whether that mail address differs from one's own here (ADR-0151)", Handler: http.HandlerFunc(h.list)},
		{Method: http.MethodGet, Pattern: "/api/engines/{id}/connect", Summary: "Begin connecting one's account to a process engine; redirects to the engine, where one signs in and approves this server for one's tasks (ADR-0151)", Handler: http.HandlerFunc(h.connect)},
		{Method: http.MethodGet, Pattern: "/api/engines/{id}/connect/callback", Summary: "The engine's redirect back after one approved or refused this server; keeps the connection, bound to the browser that began it, and goes on to one's account page", Access: Public, Handler: http.HandlerFunc(h.callback)},
		{Method: http.MethodDelete, Pattern: "/api/me/engines/{id}", Summary: "End one's connection to a process engine; its grant is revoked at the engine where the engine offers that (ADR-0151)", Handler: http.HandlerFunc(h.disconnect)},
	}
}

type engineConnHandlers struct {
	authHandlers
	log *slog.Logger
}

// connectionJSON is a connection as its person sees it.
type connectionJSON struct {
	engineconn.Connection
	// EmailDiffers says the engine knows the person by another mail
	// address than the one the person gave here: the mail address is a
	// suggestion, the person's sign-in at the engine the proof.
	EmailDiffers bool `json:"emailDiffers"`
}

type myEngineJSON struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Connectable says this server is registered at the engine, so one
	// may connect.
	Connectable bool            `json:"connectable"`
	Connection  *connectionJSON `json:"connection,omitempty"`
	// TaskApp is where the engine shows its tasks, and where the grant is
	// withdrawn as well.
	TaskApp string `json:"taskApp"`
}

func (h engineConnHandlers) list(w http.ResponseWriter, r *http.Request) {
	p, _ := PrincipalFrom(r.Context())
	mine := h.d.DB.Accounts.Contacts(p.User).Email
	out := []myEngineJSON{}
	for _, e := range h.d.DB.Site.Engines() {
		if e.Kind != site.EngineAtlas {
			continue
		}
		x := myEngineJSON{ID: e.ID, Name: e.Name, Connectable: h.d.Connect.Configured(e.ID), TaskApp: e.BaseURL + "/#/tasks"}
		if c, ok := h.d.Connect.Connections().Get(string(p.User), e.ID); ok {
			x.Connection = &connectionJSON{Connection: c, EmailDiffers: c.Email != "" && mine != "" && !strings.EqualFold(c.Email, mine)}
		}
		if x.Connectable || x.Connection != nil {
			out = append(out, x)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"engines": out})
}

// toAccount sends the browser to one's account page, saying how the connection
// went.
func toAccount(w http.ResponseWriter, r *http.Request, param, value string) {
	http.Redirect(w, r, "/?"+url.Values{param: {value}}.Encode()+"#/account", http.StatusFound)
}

func (h engineConnHandlers) connect(w http.ResponseWriter, r *http.Request) {
	p, _ := PrincipalFrom(r.Context())
	e, ok := h.d.DB.Site.Engine(r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, "no engine "+r.PathValue("id")+" is set up")
		return
	}
	if p.Via != "session" {
		writeError(w, http.StatusForbidden, "connect to an engine from the web UI, signed in")
		return
	}
	authURL, state, err := h.d.Connect.Begin(r.Context(), e, string(p.User))
	if errors.Is(err, engineauth.ErrNotConfigured) {
		toAccount(w, r, "engine_error", "config")
		return
	}
	if err != nil {
		h.log.Warn("connecting to an engine cannot begin", "engine", e.ID, "err", err)
		toAccount(w, r, "engine_error", "unavailable")
		return
	}
	// SameSite=Lax, not Strict: the engine's redirect back is a cross-site
	// navigation, which a Strict cookie would not be sent with — and the
	// session cookie is not, so the begun connection names its person.
	http.SetCookie(w, &http.Cookie{Name: connectCookie, Value: state, Path: "/api/engines/", MaxAge: int(engineauth.FlowLifetime / time.Second),
		HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: isSecure(r)})
	http.Redirect(w, r, authURL, http.StatusFound)
}

func (h engineConnHandlers) callback(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: connectCookie, Value: "", Path: "/api/engines/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: isSecure(r)})
	q := r.URL.Query()
	state := q.Get("state")
	c, err := r.Cookie(connectCookie)
	if err != nil || state == "" || subtle.ConstantTimeCompare([]byte(c.Value), []byte(state)) != 1 {
		toAccount(w, r, "engine_error", "state")
		return
	}
	user, engineID, ok := h.d.Connect.Pending(state)
	e, exists := h.d.DB.Site.Engine(r.PathValue("id"))
	if !ok || !exists || engineID != e.ID {
		toAccount(w, r, "engine_error", "state")
		return
	}
	if q.Get("error") != "" {
		// Refused at the engine; the begun connection is used up.
		h.d.Connect.Cancel(state)
		toAccount(w, r, "engine_error", "denied")
		return
	}
	conn, err := h.d.Connect.Finish(r.Context(), e, state, q.Get("code"))
	if err != nil {
		h.log.Warn("connecting to an engine failed", "engine", e.ID, "err", err)
		toAccount(w, r, "engine_error", "unavailable")
		return
	}
	h.record(audit.Entry{Event: audit.EngineConnected, Actor: user, Addr: clientAddr(r), Subject: e.ID,
		Detail: map[string]string{"username": conn.Username, "subject": conn.Subject}})
	// The person's tasks are shown soon, not at the next scheduled round.
	h.d.TaskLinks.Nudge(e.ID)
	toAccount(w, r, "engine_connected", e.ID)
}

func (h engineConnHandlers) disconnect(w http.ResponseWriter, r *http.Request) {
	p, _ := PrincipalFrom(r.Context())
	id := r.PathValue("id")
	e, ok := h.d.DB.Site.Engine(id)
	if !ok {
		// An engine removed ended its connections with it.
		writeError(w, http.StatusNotFound, "no engine "+id+" is set up")
		return
	}
	err := h.d.Connect.Disconnect(r.Context(), e, string(p.User))
	if errors.Is(err, engineconn.ErrNotConnected) {
		writeError(w, http.StatusNotFound, "not connected to engine "+id)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "the connection was not ended")
		return
	}
	h.record(audit.Entry{Event: audit.EngineDisconnected, Actor: string(p.User), Addr: clientAddr(r), Subject: id})
	h.d.TaskLinks.Nudge(id)
	w.WriteHeader(http.StatusNoContent)
}

// endConnections ends every connection to an engine that was removed or
// moved to another address, recording each.
func endConnections(d Deps, actor, addr, engineID, reason string) {
	users, err := d.Connect.EndEngine(engineID, reason)
	for _, u := range users {
		d.DB.Audit.Record(audit.Entry{Event: audit.EngineConnectionEnded, Actor: actor, Addr: addr, Subject: engineID,
			Detail: map[string]string{"user": u, "reason": reason}})
	}
	if err != nil && d.Log != nil {
		d.Log.Error("connections to an engine not ended", "engine", engineID, "err", err)
	}
}

// RecordEnded records a connection the engine ended by refusing its
// grant.
func RecordEnded(db *storage.DB) func(user, engineID string) {
	return func(user, engineID string) {
		db.Audit.Record(audit.Entry{Event: audit.EngineConnectionEnded, Actor: storage.SystemActor, Subject: engineID,
			Detail: map[string]string{"user": user, "reason": "refused by the engine"}})
	}
}
