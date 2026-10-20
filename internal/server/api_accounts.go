package server

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/dbuchs7/wikiwikibangbang/internal/audit"
	"github.com/dbuchs7/wikiwikibangbang/internal/auth"
	"github.com/dbuchs7/wikiwikibangbang/internal/notify"
	"github.com/dbuchs7/wikiwikibangbang/internal/site"
)

// accountRoutes are the account requests made at the sign-in page and the
// contacts of the accounts (ADR-0061).
func accountRoutes(d Deps) []Route {
	h := authHandlers{d: d}
	return []Route{
		{Method: http.MethodGet, Pattern: "/api/signup", Summary: "Whether one may ask for an account at the sign-in page", Access: Public, Handler: http.HandlerFunc(h.signupOpen)},
		{Method: http.MethodPost, Pattern: "/api/signup", Summary: "Ask for an account: username, password, mail address and a note; an administrator decides", Access: Public, Handler: http.HandlerFunc(h.signup)},
		{Method: http.MethodGet, Pattern: "/api/account-requests", Summary: "The account requests waiting for a decision (administrators)", Handler: http.HandlerFunc(h.listRequests)},
		{Method: http.MethodPost, Pattern: "/api/account-requests/{id}/approve", Summary: "Approve an account request: the account is created (administrators)", Handler: http.HandlerFunc(h.approveRequest)},
		{Method: http.MethodDelete, Pattern: "/api/account-requests/{id}", Summary: "Reject an account request (administrators)", Handler: http.HandlerFunc(h.rejectRequest)},
		{Method: http.MethodGet, Pattern: "/api/settings/access", Summary: "How people come to an account (administrators)", Handler: http.HandlerFunc(h.getAccess)},
		{Method: http.MethodPut, Pattern: "/api/settings/access", Summary: "Allow or stop account requests at the sign-in page (administrators)", Handler: http.HandlerFunc(h.setAccess)},
		{Method: http.MethodGet, Pattern: "/api/me/contacts", Summary: "One's own channels for messages: mail, Slack, Teams, webhook; empty is not available", Handler: http.HandlerFunc(h.myContacts)},
		{Method: http.MethodPut, Pattern: "/api/me/contacts", Summary: "Set one's own channels for messages", Handler: http.HandlerFunc(h.setMyContacts)},
		{Method: http.MethodPut, Pattern: "/api/users/{id}/email", Summary: "Set an account's mail address (administrators)", Handler: http.HandlerFunc(h.setUserEmail)},
	}
}

// langOf is a user's UI language. A requester has none yet; mail to them
// is written in the deciding administrator's language, the best guess
// for the installation's.
func (h authHandlers) langOf(id string) string { return h.d.DB.Accounts.Language(auth.UserID(id)) }

type requestJSON struct {
	ID       string    `json:"id"`
	Username string    `json:"username"`
	Email    string    `json:"email"`
	Reason   string    `json:"reason"`
	Created  time.Time `json:"created"`
}

// waitingRequests counts the account requests for an administrator, and
// is 0 for everyone else.
func (h authHandlers) waitingRequests(p auth.Principal) int {
	if !h.isAdmin(p) {
		return 0
	}
	return len(h.d.DB.Accounts.Requests())
}

func (h authHandlers) signupOpen(w http.ResponseWriter, r *http.Request) {
	// passwordReset says whether a forgotten password can be reset by
	// mail (ADR-0064).
	writeJSON(w, http.StatusOK, map[string]bool{"open": h.d.DB.Site.Access().AccountRequests, "passwordReset": h.d.Messages.resetByMail()})
}

func (h authHandlers) signup(w http.ResponseWriter, r *http.Request) {
	if !h.d.DB.Site.Access().AccountRequests {
		writeError(w, http.StatusForbidden, "account requests are not accepted here; ask an administrator")
		return
	}
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Email    string `json:"email"`
		Reason   string `json:"reason"`
	}
	if err := readJSON(w, r, &req); err != nil {
		badRequest(w, err)
		return
	}
	addr := clientAddr(r)
	rq, err := h.d.DB.Accounts.RequestAccount(req.Username, req.Password, req.Email, req.Reason, addr)
	var throttled *auth.ThrottledError
	switch {
	case errors.As(err, &throttled):
		w.Header().Set("Retry-After", strconv.Itoa(int(throttled.Wait.Seconds())+1))
		writeError(w, http.StatusTooManyRequests, "too many requests; try again later")
	case errors.Is(err, auth.ErrFull):
		w.Header().Set("Retry-After", "3600")
		writeError(w, http.StatusServiceUnavailable, "too many requests are waiting; try again later")
	case errors.Is(err, auth.ErrInvalid):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, auth.ErrExists):
		writeError(w, http.StatusConflict, "username taken")
	case err != nil:
		writeError(w, http.StatusInternalServerError, "request not recorded")
	default:
		h.record(audit.Entry{Event: audit.AccountRequested, Addr: addr, Subject: rq.ID, Detail: map[string]string{"username": rq.Username}})
		h.d.Messages.tellAdmins(rq.Username)
		writeJSON(w, http.StatusAccepted, map[string]string{"status": "pending"})
	}
}

func (h authHandlers) listRequests(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.adminOnly(w, r); !ok {
		return
	}
	out := []requestJSON{}
	for _, rq := range h.d.DB.Accounts.Requests() {
		out = append(out, requestJSON{ID: rq.ID, Username: rq.Username, Email: rq.Email, Reason: rq.Reason, Created: rq.Created})
	}
	writeJSON(w, http.StatusOK, map[string]any{"requests": out})
}

func (h authHandlers) approveRequest(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.adminOnly(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	u, err := h.d.DB.Accounts.ApproveRequest(id)
	switch {
	case errors.Is(err, auth.ErrNotFound):
		writeError(w, http.StatusNotFound, "no such request")
	case errors.Is(err, auth.ErrExists):
		writeError(w, http.StatusConflict, "an account has this username meanwhile; reject the request")
	case err != nil:
		writeError(w, http.StatusInternalServerError, "account not created")
	default:
		h.record(audit.Entry{Event: audit.AccountApproved, Actor: actor, Addr: clientAddr(r), Subject: string(u.ID), Detail: map[string]string{"username": u.Username, "request": id}})
		h.d.Messages.mail(h.d.DB.Accounts.Contacts(u.ID).Email, notify.Message{Subject: text(h.langOf(actor), "approved.subject"),
			Text: text(h.langOf(actor), "approved.text", u.Username), Link: h.d.Messages.n.Link("#/")})
		writeJSON(w, http.StatusCreated, map[string]userJSON{"user": {ID: u.ID, Username: u.Username}})
	}
}

func (h authHandlers) rejectRequest(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.adminOnly(w, r)
	if !ok {
		return
	}
	rq, err := h.d.DB.Accounts.RejectRequest(r.PathValue("id"))
	switch {
	case errors.Is(err, auth.ErrNotFound):
		writeError(w, http.StatusNotFound, "no such request")
	case err != nil:
		writeError(w, http.StatusInternalServerError, "request not rejected")
	default:
		h.record(audit.Entry{Event: audit.AccountRejected, Actor: actor, Addr: clientAddr(r), Subject: rq.ID, Detail: map[string]string{"username": rq.Username}})
		h.d.Messages.mail(rq.Email, notify.Message{Subject: text(h.langOf(actor), "rejected.subject"), Text: text(h.langOf(actor), "rejected.text", rq.Username)})
		w.WriteHeader(http.StatusNoContent)
	}
}

func (h authHandlers) getAccess(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.adminOnly(w, r); !ok {
		return
	}
	writeJSON(w, http.StatusOK, h.d.DB.Site.Access())
}

func (h authHandlers) setAccess(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.adminOnly(w, r)
	if !ok {
		return
	}
	var a site.Access
	if err := readJSON(w, r, &a); err != nil {
		badRequest(w, err)
		return
	}
	if err := h.d.DB.Site.SetAccess(actor, a); err != nil {
		writeError(w, http.StatusInternalServerError, "setting not saved")
		return
	}
	h.record(audit.Entry{Event: audit.AccessChanged, Actor: actor, Addr: clientAddr(r), Detail: map[string]string{"accountRequests": strconv.FormatBool(a.AccountRequests)}})
	writeJSON(w, http.StatusOK, a)
}

// contactsJSON is one's channels with whether mail is sent at all and the
// outcome of the last message (ADR-0064).
type contactsJSON struct {
	auth.Contacts
	Mail bool      `json:"mail"`
	Last *delivery `json:"last,omitempty"`
}

func (h authHandlers) contactsOf(id auth.UserID) contactsJSON {
	out := contactsJSON{Contacts: h.d.DB.Accounts.Contacts(id), Mail: h.d.Messages.n.Mail()}
	if d, ok := h.d.Messages.Last(id); ok {
		out.Last = &d
	}
	return out
}

func (h authHandlers) myContacts(w http.ResponseWriter, r *http.Request) {
	p, _ := PrincipalFrom(r.Context())
	writeJSON(w, http.StatusOK, h.contactsOf(p.User))
}

func (h authHandlers) setMyContacts(w http.ResponseWriter, r *http.Request) {
	p, _ := PrincipalFrom(r.Context())
	actor := string(p.User)
	var c auth.Contacts
	if err := readJSON(w, r, &c); err != nil {
		badRequest(w, err)
		return
	}
	switch err := h.d.DB.Accounts.SetContacts(p.User, c); {
	case errors.Is(err, auth.ErrInvalid):
		writeError(w, http.StatusBadRequest, err.Error())
		return
	case err != nil:
		writeError(w, http.StatusInternalServerError, "contacts not saved")
		return
	}
	got := h.d.DB.Accounts.Contacts(p.User)
	// The addresses are not audited: a webhook address is a credential.
	h.record(audit.Entry{Event: audit.ContactsChanged, Actor: actor, Addr: clientAddr(r), Subject: actor, Detail: map[string]string{"channels": strings.Join(got.Channels(), ",")}})
	writeJSON(w, http.StatusOK, h.contactsOf(p.User))
}

func (h authHandlers) setUserEmail(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.adminOnly(w, r)
	if !ok {
		return
	}
	var req struct {
		Email string `json:"email"`
	}
	if err := readJSON(w, r, &req); err != nil {
		badRequest(w, err)
		return
	}
	id := auth.UserID(r.PathValue("id"))
	switch err := h.d.DB.Accounts.SetEmail(id, req.Email); {
	case errors.Is(err, auth.ErrInvalid):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, auth.ErrNotFound):
		writeError(w, http.StatusNotFound, "no such user")
	case err != nil:
		writeError(w, http.StatusInternalServerError, "address not saved")
	default:
		h.record(audit.Entry{Event: audit.ContactsChanged, Actor: actor, Addr: clientAddr(r), Subject: string(id), Detail: map[string]string{"channels": "email"}})
		w.WriteHeader(http.StatusNoContent)
	}
}
