package auth

import (
	"time"
)

// Password resets (ADR-0064): a link with a secret token lets one choose a
// new password. A token is kept in memory as its hash, one per account,
// and ends with the process, its lifetime, its use, or a new token.

// ResetLifetime is how long a reset link mailed on request is valid.
const ResetLifetime = 30 * time.Minute

// HandedResetLifetime is how long a reset link an administrator hands
// over is valid.
const HandedResetLifetime = 24 * time.Hour

type reset struct {
	user    UserID
	hash    string
	expires time.Time
}

// RequestReset starts a reset for the account named username when it has
// a password and a mail address, and returns the token and where to send
// it. ErrNotFound covers every other case, which the caller must not tell
// apart from success. An address and a username may ask MaxFailures times
// within FailureWindow.
func (a *Accounts) RequestReset(username, addr string) (token string, u User, email string, err error) {
	now := a.now()
	a.mu.Lock()
	defer a.mu.Unlock()
	keys := []string{"reset-addr:" + addr, "reset-user:" + fold(username)}
	if wait := a.throttle.blocked(now, keys...); wait > 0 {
		return "", User{}, "", &ThrottledError{Wait: wait}
	}
	a.throttle.fail(now, keys...)
	id, ok := a.byName[fold(username)]
	if !ok {
		return "", User{}, "", ErrNotFound
	}
	usr := a.users[id]
	// An account that signs in elsewhere has no password to reset.
	if usr.hash == "" || usr.contacts.Email == "" {
		return "", User{}, "", ErrNotFound
	}
	token, err = a.newReset(id, ResetLifetime)
	if err != nil {
		return "", User{}, "", err
	}
	return token, usr.User, usr.contacts.Email, nil
}

// HandReset makes a reset token for an administrator to hand over, valid
// for HandedResetLifetime.
func (a *Accounts) HandReset(id UserID) (string, time.Time, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, ok := a.users[id]; !ok {
		return "", time.Time{}, ErrNotFound
	}
	token, err := a.newReset(id, HandedResetLifetime)
	if err != nil {
		return "", time.Time{}, err
	}
	return token, a.resets[id].expires, nil
}

// newReset replaces the account's token; callers hold a.mu.
func (a *Accounts) newReset(id UserID, lifetime time.Duration) (string, error) {
	secret, err := a.newID(32)
	if err != nil {
		return "", err
	}
	a.resets[id] = &reset{user: id, hash: hashSecret(secret), expires: a.now().Add(lifetime)}
	return secret, nil
}

// CompleteReset sets a new password with a token and ends the account's
// sessions; the token is used up. ErrNotFound means the token is unknown,
// used or expired.
func (a *Accounts) CompleteReset(token, password string) (User, error) {
	if err := validPassword(password); err != nil {
		return User{}, err
	}
	if a.resetUser(token) == "" {
		return User{}, ErrNotFound
	}
	hash, err := hashPassword(password, a.iter, a.random)
	if err != nil {
		return User{}, err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	// Checked again: the token may have been used while hashing.
	id := a.resetUserLocked(token)
	if id == "" {
		return User{}, ErrNotFound
	}
	if err := a.record(event{Kind: passwordSet, User: id, Hash: hash}); err != nil {
		return User{}, err
	}
	delete(a.resets, id)
	for k, s := range a.sessions {
		if s.user == id {
			delete(a.sessions, k)
		}
	}
	a.throttle.reset("user:" + fold(a.users[id].Username))
	return a.users[id].User, nil
}

func (a *Accounts) resetUser(token string) UserID {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.resetUserLocked(token)
}

// resetUserLocked finds the account of a valid token; callers hold a.mu.
func (a *Accounts) resetUserLocked(token string) UserID {
	if token == "" {
		return ""
	}
	h := hashSecret(token)
	now := a.now()
	for id, r := range a.resets {
		if !now.Before(r.expires) {
			delete(a.resets, id)
			continue
		}
		if equalStrings(r.hash, h) {
			return id
		}
	}
	return ""
}
