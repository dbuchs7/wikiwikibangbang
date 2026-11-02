package server

import (
	"cmp"
	"crypto/subtle"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/dbuchs7/orbithek/internal/audit"
	"github.com/dbuchs7/orbithek/internal/engineauth"
	"github.com/dbuchs7/orbithek/internal/engineconn"
	"github.com/dbuchs7/orbithek/internal/site"
	"github.com/dbuchs7/orbithek/internal/storage"
)

// connectCookie binds a begun connection to an engine to the browser that
// began it, as the single sign-on's state cookie does (ADR-0030).
const connectCookie = "ot_engine_state"

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
		{Method: http.MethodGet, Pattern: "/api/engines/{id}/connect", Summary: "Begin connecting one's account to a process engine; redirects to the engine, where one signs in and approves this server for one's tasks (ADR-0151). With silent=1 the engine shows no page — where its administrator approved people's grants beforehand, one comes back connected at once (ADR-0152) —, and back names the page of the web UI to go on to", Handler: http.HandlerFunc(h.connect)},
		{Method: http.MethodGet, Pattern: "/api/engines/{id}/connect/callback", Summary: "The engine's redirect back after one approved or refused this server; keeps the connection, bound to the browser that began it, and goes on to one's account page, the page it was begun from, or the administration", Access: Public, Handler: http.HandlerFunc(h.callback)},
		{Method: http.MethodDelete, Pattern: "/api/me/engines/{id}", Summary: "End one's connection to a process engine; its grant is revoked at the engine where the engine offers that (ADR-0151), and one is not connected again without asking (ADR-0152)", Handler: http.HandlerFunc(h.disconnect)},
		{Method: http.MethodGet, Pattern: "/api/engines/client-metadata", Summary: "This server's client metadata document: the client ID it names itself by at process engines, its name, where people are sent back to and the public key of its signed assertions (ADR-0152)", Access: Public, Handler: http.HandlerFunc(h.clientMetadata)},
		{Method: http.MethodGet, Pattern: "/api/engines/{id}/system/connect", Summary: "Administrators: begin connecting this server to a process engine; redirects to the engine, where an administrator of the engine approves a system grant and chooses what it reaches (ADR-0152)", Handler: http.HandlerFunc(h.connectSystem)},
		{Method: http.MethodDelete, Pattern: "/api/engines/{id}/system", Summary: "Administrators: end this server's connection to a process engine; its system grant is revoked at the engine (ADR-0152)", Handler: http.HandlerFunc(h.disconnectSystem)},
		{Method: http.MethodPost, Pattern: "/api/engines/client-key", Summary: "Administrators: replace the signing key of this server's client metadata document; engines learn the new one when they next read it (ADR-0152)", Handler: http.HandlerFunc(h.replaceKey)},
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
	// Connectable says this server is registered at the engine, or
	// connected to it, so one may connect.
	Connectable bool `json:"connectable"`
	// Silent says one may be connected without a page of one's own: the
	// engine's administrator approved people's grants beforehand, and one
	// has not ended a connection oneself (ADR-0152).
	Silent     bool            `json:"silent"`
	Connection *connectionJSON `json:"connection,omitempty"`
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
		x := myEngineJSON{ID: e.ID, Name: e.Name, Connectable: h.d.Connect.Configured(e.ID), TaskApp: e.BaseURL + "/#/tasks",
			Silent: p.Via == "session" && h.d.Connect.Silent(e.ID, string(p.User))}
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

// backRoute is a page of the web UI a connection may go on to: a hash
// route of this server, never an address of another.
var backRoute = regexp.MustCompile(`^#/[A-Za-z0-9/_.~%=&+,:;@?-]{0,300}$`)

// toBack sends the browser on to the page a connection was begun from.
func toBack(w http.ResponseWriter, r *http.Request, back string) {
	http.Redirect(w, r, "/"+back, http.StatusFound)
}

// toAdmin sends the browser to the administration, saying how this
// server's connection to an engine went.
func toAdmin(w http.ResponseWriter, r *http.Request, param, value string) {
	http.Redirect(w, r, "/?"+url.Values{param: {value}}.Encode()+"#/admin", http.StatusFound)
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
	o := engineauth.Flow{Silent: r.URL.Query().Get("silent") == "1"}
	if back := r.URL.Query().Get("back"); backRoute.MatchString(back) {
		o.Back = back
	}
	authURL, state, err := h.d.Connect.Begin(r.Context(), e, string(p.User), o)
	if err != nil {
		if !errors.Is(err, engineauth.ErrNotConfigured) {
			h.log.Warn("connecting to an engine cannot begin", "engine", e.ID, "err", err)
		}
		switch {
		case o.Silent && o.Back != "":
			toBack(w, r, o.Back)
		case errors.Is(err, engineauth.ErrNotConfigured):
			toAccount(w, r, "engine_error", "config")
		default:
			toAccount(w, r, "engine_error", "unavailable")
		}
		return
	}
	h.toEngine(w, r, authURL, state)
}

// toEngine sends the browser to the engine's consent, the state bound to
// it.
func (h engineConnHandlers) toEngine(w http.ResponseWriter, r *http.Request, authURL, state string) {
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
	f, ok := h.d.Connect.Pending(state)
	e, exists := h.d.DB.Site.Engine(r.PathValue("id"))
	if !ok || !exists || f.Engine != e.ID {
		toAccount(w, r, "engine_error", "state")
		return
	}
	// failed answers where the browser goes when the connection did not
	// come about: a silent one back to its page, which then offers to
	// connect; this server's own to the administration.
	failed := func(code string) {
		switch {
		case f.Silent && f.Back != "":
			toBack(w, r, f.Back)
		case f.System:
			toAdmin(w, r, "engine_error", code)
		default:
			toAccount(w, r, "engine_error", code)
		}
	}
	if q.Get("error") != "" {
		// Refused at the engine — or, silently, not possible without a
		// page —; the begun connection is used up.
		h.d.Connect.Cancel(state)
		failed("denied")
		return
	}
	conn, err := h.d.Connect.Finish(r.Context(), e, state, q.Get("code"))
	if err != nil {
		h.log.Warn("connecting to an engine failed", "engine", e.ID, "err", err)
		failed("unavailable")
		return
	}
	// The tasks are shown soon, not at the next scheduled round.
	h.d.TaskLinks.Nudge(e.ID)
	if f.System {
		detail := map[string]string{"approvedBy": conn.System.ApprovedBy, "preconsent": strconv.FormatBool(conn.System.Preconsent)}
		for _, c := range conn.System.Capabilities {
			detail[c.Scope] = cmp.Or(strings.Join(c.Projects, ","), "all")
		}
		h.record(audit.Entry{Event: audit.EngineSystemConnected, Actor: f.User, Addr: clientAddr(r), Subject: e.ID, Detail: detail})
		toAdmin(w, r, "engine_system", e.ID)
		return
	}
	h.record(audit.Entry{Event: audit.EngineConnected, Actor: f.User, Addr: clientAddr(r), Subject: e.ID,
		Detail: map[string]string{"username": conn.Username, "subject": conn.Subject}})
	if f.Back != "" {
		toBack(w, r, f.Back)
		return
	}
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
	// Ended by the person: not made again without asking (ADR-0152).
	if err := h.d.Connect.Connections().Decline(string(p.User), id); err != nil {
		h.log.Warn("a declined connection not recorded", "engine", id, "err", err)
	}
	h.record(audit.Entry{Event: audit.EngineDisconnected, Actor: string(p.User), Addr: clientAddr(r), Subject: id})
	h.d.TaskLinks.Nudge(id)
	w.WriteHeader(http.StatusNoContent)
}

func (h engineConnHandlers) clientMetadata(w http.ResponseWriter, r *http.Request) {
	doc, err := h.d.Connect.ClientMetadata(h.d.DB.Site.Design().Name, h.d.DB.Site.Engines())
	if errors.Is(err, engineauth.ErrNotConfigured) {
		writeError(w, http.StatusNotFound, "this server has no public address (OT_PUBLIC_URL)")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "the client metadata document is not available")
		return
	}
	// Engines read it again when it may have changed: a new key, another
	// engine's callback.
	w.Header().Set("Cache-Control", "max-age=300")
	writeJSON(w, http.StatusOK, doc)
}

func (h engineConnHandlers) connectSystem(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.adminOnly(w, r)
	if !ok {
		return
	}
	p, _ := PrincipalFrom(r.Context())
	e, exists := h.d.DB.Site.Engine(r.PathValue("id"))
	if !exists {
		writeError(w, http.StatusNotFound, "no engine "+r.PathValue("id")+" is set up")
		return
	}
	if p.Via != "session" {
		writeError(w, http.StatusForbidden, "connect an engine from the web UI, signed in")
		return
	}
	authURL, state, err := h.d.Connect.Begin(r.Context(), e, actor, engineauth.Flow{System: true})
	switch {
	case errors.Is(err, engineauth.ErrNotConfigured):
		toAdmin(w, r, "engine_error", "config")
		return
	case errors.Is(err, engineauth.ErrNoSystem):
		toAdmin(w, r, "engine_error", "nosystem")
		return
	case err != nil:
		h.log.Warn("connecting this server to an engine cannot begin", "engine", e.ID, "err", err)
		toAdmin(w, r, "engine_error", "unavailable")
		return
	}
	h.toEngine(w, r, authURL, state)
}

func (h engineConnHandlers) disconnectSystem(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.adminOnly(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	e, exists := h.d.DB.Site.Engine(id)
	if !exists {
		writeError(w, http.StatusNotFound, "no engine "+id+" is set up")
		return
	}
	err := h.d.Connect.Disconnect(r.Context(), e, engineconn.SystemUser)
	if errors.Is(err, engineconn.ErrNotConnected) {
		writeError(w, http.StatusNotFound, "this server is not connected to engine "+id)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "the connection was not ended")
		return
	}
	h.record(audit.Entry{Event: audit.EngineSystemDisconnected, Actor: actor, Addr: clientAddr(r), Subject: id})
	h.d.TaskLinks.Nudge(id)
	w.WriteHeader(http.StatusNoContent)
}

func (h engineConnHandlers) replaceKey(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.adminOnly(w, r)
	if !ok {
		return
	}
	if err := h.d.Connect.ReplaceKey(); err != nil {
		writeError(w, http.StatusInternalServerError, "the key was not replaced")
		return
	}
	h.record(audit.Entry{Event: audit.ClientKeyReplaced, Actor: actor, Addr: clientAddr(r)})
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
