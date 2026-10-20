package auth

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/dbuchs7/wikiwikibangbang/internal/budget"
)

// Request is a request for an account made at the sign-in page, waiting
// for an administrator (ADR-0061).
type Request struct {
	ID       string
	Username string
	Email    string
	Reason   string
	Created  time.Time
}

type request struct {
	Request
	// hash is the password the requester chose, hashed; an approval gives
	// it to the account, so no password is ever handed over.
	hash string
}

// ErrFull refuses a request while too many wait for a decision.
var ErrFull = errors.New("too many requests are waiting")

// MaxReasonLength bounds the note of an account request, in characters.
const MaxReasonLength = 500

// RequestAccount records a request for an account. The requester chooses
// the password now and must give a mail address, so an administrator can
// tell who is asking. A client address may make MaxFailures requests
// within FailureWindow.
func (a *Accounts) RequestAccount(username, password, email, reason, addr string) (Request, error) {
	email, reason = strings.TrimSpace(email), strings.TrimSpace(reason)
	switch {
	case !validUsername(username):
		return Request{}, fmt.Errorf("%w: a username is 1 to 64 visible characters without spaces", ErrInvalid)
	case !validEmail(email):
		return Request{}, fmt.Errorf("%w: a mail address is needed", ErrInvalid)
	case utf8.RuneCountInString(reason) > MaxReasonLength:
		return Request{}, fmt.Errorf("%w: the note has at most %d characters", ErrInvalid, MaxReasonLength)
	}
	if err := validPassword(password); err != nil {
		return Request{}, err
	}
	now := a.now()
	key := "request:" + addr
	a.mu.Lock()
	if wait := a.throttle.blocked(now, key); wait > 0 {
		a.mu.Unlock()
		return Request{}, &ThrottledError{Wait: wait}
	}
	// Each request counts, so one address cannot fill the list; the
	// count is taken before the costly hashing.
	a.throttle.fail(now, key)
	a.mu.Unlock()

	hash, err := hashPassword(password, a.iter, a.random)
	if err != nil {
		return Request{}, err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.nameTaken(username) {
		return Request{}, fmt.Errorf("%w: username %q", ErrExists, username)
	}
	if len(a.requests) >= budget.Get().AccountRequests {
		return Request{}, ErrFull
	}
	id, err := a.newID(16)
	if err != nil {
		return Request{}, err
	}
	err = a.record(event{Kind: accountRequested, Request: id, Username: username, Hash: hash,
		Contacts: &Contacts{Email: email}, Reason: reason})
	if err != nil {
		return Request{}, err
	}
	return a.requests[id].Request, nil
}

// nameTaken says whether an account or a waiting request has the username;
// callers hold a.mu.
func (a *Accounts) nameTaken(username string) bool {
	if _, taken := a.byName[fold(username)]; taken {
		return true
	}
	for _, r := range a.requests {
		if fold(r.Username) == fold(username) {
			return true
		}
	}
	return false
}

// Requests lists the waiting account requests, oldest first.
func (a *Accounts) Requests() []Request {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]Request, 0, len(a.requests))
	for _, r := range a.requests {
		out = append(out, r.Request)
	}
	slices.SortFunc(out, func(x, y Request) int {
		if c := x.Created.Compare(y.Created); c != 0 {
			return c
		}
		return strings.Compare(x.ID, y.ID)
	})
	return out
}

// ApproveRequest creates the account a request asked for, with the
// password and mail address the requester gave.
func (a *Accounts) ApproveRequest(id string) (User, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	r, ok := a.requests[id]
	if !ok {
		return User{}, ErrNotFound
	}
	if _, taken := a.byName[fold(r.Username)]; taken {
		return User{}, fmt.Errorf("%w: username %q", ErrExists, r.Username)
	}
	uid, err := a.newID(16)
	if err != nil {
		return User{}, err
	}
	user := UserID(uid)
	contacts := Contacts{Email: r.Email}
	for _, e := range []event{
		{Kind: requestApproved, Request: id, User: user},
		{Kind: userCreated, User: user, Username: r.Username},
		{Kind: passwordSet, User: user, Hash: r.hash},
		{Kind: contactsSet, User: user, Contacts: &contacts},
	} {
		if err := a.record(e); err != nil {
			return User{}, err
		}
	}
	return a.users[user].User, nil
}

// RejectRequest turns a request down; the username is free again.
func (a *Accounts) RejectRequest(id string) (Request, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	r, ok := a.requests[id]
	if !ok {
		return Request{}, ErrNotFound
	}
	if err := a.record(event{Kind: requestRejected, Request: id}); err != nil {
		return Request{}, err
	}
	return r.Request, nil
}
