package server

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/dbuchs7/wikiwikibangbang/internal/audit"
	"github.com/dbuchs7/wikiwikibangbang/internal/auth"
	"github.com/dbuchs7/wikiwikibangbang/internal/notify"
)

// messageRoutes are the password reset and the delivery of messages
// (ADR-0064).
func messageRoutes(d Deps) []Route {
	h := authHandlers{d: d}
	return []Route{
		{Method: http.MethodPost, Pattern: "/api/password-reset", Summary: "Ask for a password reset link by mail; the answer is the same whether the account exists or not", Access: Public, Handler: http.HandlerFunc(h.requestReset)},
		{Method: http.MethodPost, Pattern: "/api/password-reset/confirm", Summary: "Choose a new password with the token of a reset link; ends the account's sessions", Access: Public, Handler: http.HandlerFunc(h.confirmReset)},
		{Method: http.MethodPost, Pattern: "/api/users/{id}/password-reset", Summary: "Make a reset link to hand to a user, valid for 24 hours (administrators)", Handler: http.HandlerFunc(h.handReset)},
		{Method: http.MethodPost, Pattern: "/api/me/contacts/test", Summary: "Send a test message on all of one's channels; at most one every 30 seconds", Handler: http.HandlerFunc(h.testMessage)},
		{Method: http.MethodGet, Pattern: "/api/settings/delivery", Summary: "How messages are sent: mail server, sender, public address, digest interval (administrators)", Handler: http.HandlerFunc(h.deliveryStatus)},
	}
}

// testInterval is how often one may send oneself a test message.
const testInterval = 30 * time.Second

func (h authHandlers) requestReset(w http.ResponseWriter, r *http.Request) {
	if !h.d.Messages.resetByMail() {
		writeError(w, http.StatusServiceUnavailable, "password reset by mail is not set up here; ask an administrator")
		return
	}
	var req struct {
		Username string `json:"username"`
	}
	if err := readJSON(w, r, &req); err != nil {
		badRequest(w, err)
		return
	}
	addr := clientAddr(r)
	token, u, email, err := h.d.DB.Accounts.RequestReset(req.Username, addr)
	var throttled *auth.ThrottledError
	switch {
	case errors.As(err, &throttled):
		w.Header().Set("Retry-After", strconv.Itoa(int(throttled.Wait.Seconds())+1))
		writeError(w, http.StatusTooManyRequests, "too many requests; try again later")
		return
	case err == nil:
		h.record(audit.Entry{Event: audit.PasswordResetRequested, Addr: addr, Subject: string(u.ID)})
		lang := h.d.DB.Accounts.Language(u.ID)
		h.d.Messages.mail(email, notify.Message{Subject: text(lang, "reset.subject"), Text: text(lang, "reset.text", u.Username),
			Link: h.d.Messages.n.Link("#/reset/" + token)})
	case !errors.Is(err, auth.ErrNotFound):
		h.d.Log.Error("password reset not started", "err", err)
	}
	// The same answer either way: nobody learns which accounts exist.
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "sent if the account has a mail address"})
}

func (h authHandlers) confirmReset(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token    string `json:"token"`
		Password string `json:"password"`
	}
	if err := readJSON(w, r, &req); err != nil {
		badRequest(w, err)
		return
	}
	u, err := h.d.DB.Accounts.CompleteReset(req.Token, req.Password)
	switch {
	case errors.Is(err, auth.ErrInvalid):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, auth.ErrNotFound):
		writeError(w, http.StatusNotFound, "this link is no longer valid; ask for a new one")
	case err != nil:
		writeError(w, http.StatusInternalServerError, "password not changed")
	default:
		h.record(audit.Entry{Event: audit.PasswordReset, Actor: string(u.ID), Addr: clientAddr(r), Subject: string(u.ID)})
		w.WriteHeader(http.StatusNoContent)
	}
}

func (h authHandlers) handReset(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.adminOnly(w, r)
	if !ok {
		return
	}
	id := auth.UserID(r.PathValue("id"))
	token, expires, err := h.d.DB.Accounts.HandReset(id)
	switch {
	case errors.Is(err, auth.ErrNotFound):
		writeError(w, http.StatusNotFound, "no such user")
	case err != nil:
		writeError(w, http.StatusInternalServerError, "no link made")
	default:
		h.record(audit.Entry{Event: audit.PasswordResetHanded, Actor: actor, Addr: clientAddr(r), Subject: string(id)})
		writeJSON(w, http.StatusCreated, map[string]any{"token": token, "expires": expires, "link": h.d.Messages.n.Link("#/reset/" + token)})
	}
}

func (h authHandlers) testMessage(w http.ResponseWriter, r *http.Request) {
	p, _ := PrincipalFrom(r.Context())
	m := h.d.Messages
	if !m.reachable(h.d.DB.Accounts.Contacts(p.User)) {
		writeError(w, http.StatusConflict, "no channel is available; set one first")
		return
	}
	m.mu.Lock()
	if last, ok := m.tested[p.User]; ok && time.Since(last) < testInterval {
		m.mu.Unlock()
		w.Header().Set("Retry-After", strconv.Itoa(int((testInterval-time.Since(last)).Seconds())+1))
		writeError(w, http.StatusTooManyRequests, "one test message every 30 seconds")
		return
	}
	m.tested[p.User] = time.Now()
	m.mu.Unlock()
	lang := h.d.DB.Accounts.Language(p.User)
	res := m.Send(r.Context(), p.User, notify.Message{Subject: text(lang, "test.subject"), Text: text(lang, "test.text"), Link: m.n.Link("#/account")})
	writeJSON(w, http.StatusOK, map[string]any{"results": res})
}

func (h authHandlers) deliveryStatus(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.adminOnly(w, r); !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": h.d.Messages.n.Status(), "digestMinutes": int(h.d.Messages.Every().Minutes())})
}
