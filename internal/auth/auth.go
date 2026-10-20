// Package auth holds the local accounts, API tokens and login sessions of
// the principal boundary (ADR-0013, ADR-0027). It answers one question for
// the HTTP layer: which principal, if any, is making this request.
//
// Accounts and tokens are event-sourced into their own journal; sessions
// live in memory and end with the process.
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// UserID is a user's permanent identity. It never changes and is never
// reused; usernames can change without affecting it (ADR-0013).
type UserID string

// Scope limits what a credential may do.
type Scope string

const (
	// ScopeFull allows everything the user may do.
	ScopeFull Scope = "full"
	// ScopeRead allows only reading requests (GET, HEAD).
	ScopeRead Scope = "read"
	// ScopeMetrics allows only reading the operational metrics, the
	// narrowest credential to hand to a scraper (ADR-0016).
	ScopeMetrics Scope = "metrics"
)

// Principal is the authenticated caller of a request.
type Principal struct {
	User     UserID
	Username string
	Scope    Scope
	// Via names the credential: "session" or "token".
	Via string
	// Session is the session's ID when Via is "session", so logout can end it.
	Session string
}

// User is an account as listed.
type User struct {
	ID       UserID
	Username string
	Created  time.Time
}

// Token is an API token as listed; its secret is shown only at creation.
type Token struct {
	ID      string
	User    UserID
	Name    string
	Scope   Scope
	Created time.Time
	Expires time.Time
}

var (
	ErrInvalid      = errors.New("invalid")
	ErrExists       = errors.New("already exists")
	ErrNotFound     = errors.New("not found")
	ErrUnauthorized = errors.New("wrong username or password")
	ErrThrottled    = errors.New("too many failed attempts")
)

// ThrottledError refuses a login during a throttle; it matches ErrThrottled.
type ThrottledError struct{ Wait time.Duration }

func (e *ThrottledError) Error() string {
	return fmt.Sprintf("%v: retry in %s", ErrThrottled, e.Wait.Round(time.Second))
}

func (e *ThrottledError) Is(target error) bool { return target == ErrThrottled }

// TokenPrefix starts every API token secret, so secret scanners can find
// leaked tokens.
const TokenPrefix = "wwbb_"

// SessionLifetime is how long a login lasts.
const SessionLifetime = 12 * time.Hour

// MinPasswordLength is the shortest password accepted.
const MinPasswordLength = 12

// MaxTokenLifetime bounds the expiry of an API token.
const MaxTokenLifetime = 366 * 24 * time.Hour

// Journal makes account events durable; an error means the change did not
// happen.
type Journal func(record []byte) error

// Accounts is safe for concurrent use.
type Accounts struct {
	mu      sync.Mutex
	journal Journal
	now     func() time.Time
	random  func([]byte) error
	iter    int

	users    map[UserID]*user
	byName   map[string]UserID
	requests map[string]*request // pending account requests (ADR-0061)
	resets   map[UserID]*reset   // password reset tokens (ADR-0064)
	tokens   map[string]*token   // by secret hash
	tokenIDs map[string]*token
	sessions map[string]*session // by secret hash
	external map[string]UserID   // issuer + "\x00" + subject -> user (ADR-0030)
	throttle throttle
	dummy    string // hash checked for unknown usernames, at the same cost
}

type user struct {
	User
	hash string
	// language is the UI language the user chose (ADR-0017), or empty.
	language string
	// seen is the sequence number of the newest item event whose
	// notification the user has seen (ADR-0046).
	seen int
	// avatar is the user's profile picture and its media type, or empty
	// (ADR-0050).
	avatar     []byte
	avatarType string
	// contacts are the channels messages reach the user on (ADR-0061).
	contacts Contacts
	// delivered is the sequence number of the newest item event a digest
	// of notifications was sent for (ADR-0064).
	delivered int
}

type token struct {
	Token
	hash string
}

type session struct {
	user    UserID
	id      string
	expires time.Time
}

// Option configures Accounts.
type Option func(*Accounts)

// WithClock sets the clock.
func WithClock(now func() time.Time) Option { return func(a *Accounts) { a.now = now } }

// WithIterations sets the PBKDF2 iteration count of new password hashes;
// tests lower it. Existing hashes keep the count they were made with.
func WithIterations(n int) Option { return func(a *Accounts) { a.iter = n } }

// New returns the accounts rebuilt from records, journaling later changes
// through j.
func New(records [][]byte, j Journal, opts ...Option) (*Accounts, error) {
	a := &Accounts{
		journal: j, now: time.Now, random: func(b []byte) error { _, err := rand.Read(b); return err },
		iter:  DefaultIterations,
		users: map[UserID]*user{}, byName: map[string]UserID{},
		tokens: map[string]*token{}, tokenIDs: map[string]*token{}, sessions: map[string]*session{},
		external: map[string]UserID{}, requests: map[string]*request{}, resets: map[UserID]*reset{},
	}
	for _, o := range opts {
		o(a)
	}
	a.throttle.init()
	a.dummy = dummyHash(a.iter)
	for i, r := range records {
		var e event
		if err := json.Unmarshal(r, &e); err != nil {
			return nil, fmt.Errorf("auth: record %d: %w", i+1, err)
		}
		if err := a.apply(e); err != nil {
			return nil, fmt.Errorf("auth: record %d: %w", i+1, err)
		}
	}
	return a, nil
}

// event is one persisted change of the accounts.
type event struct {
	V        int       `json:"v"`
	Kind     string    `json:"kind"`
	At       time.Time `json:"at"`
	User     UserID    `json:"user,omitempty"`
	Username string    `json:"username,omitempty"`
	Hash     string    `json:"hash,omitempty"`
	Token    string    `json:"token,omitempty"`
	Name     string    `json:"name,omitempty"`
	Scope    Scope     `json:"scope,omitempty"`
	Expires  time.Time `json:"expires,omitzero"`
	Issuer   string    `json:"issuer,omitempty"`
	Subject  string    `json:"subject,omitempty"`
	Language string    `json:"language,omitempty"`
	Seen     int       `json:"seen,omitempty"`
	// Avatar is a profile picture; an AvatarSet without one removes it.
	Avatar     []byte `json:"avatar,omitempty"`
	AvatarType string `json:"avatarType,omitempty"`
	// Contacts are set by ContactsSet and carry the address of an
	// AccountRequested (ADR-0061).
	Contacts *Contacts `json:"contacts,omitempty"`
	// Request names an account request; Reason is the requester's note.
	Request string `json:"request,omitempty"`
	Reason  string `json:"reason,omitempty"`
}

const (
	userCreated  = "UserCreated"
	passwordSet  = "PasswordSet"
	tokenCreated = "TokenCreated"
	tokenRevoked = "TokenRevoked"
	// identityLinked ties an account to an identity at an external
	// provider (OIDC issuer and subject).
	identityLinked = "IdentityLinked"
	// languageSet records the UI language a user chose (ADR-0017).
	languageSet = "LanguageSet"
	// notificationsSeen records how far a user has seen their
	// notifications (ADR-0046).
	notificationsSeen = "NotificationsSeen"
	// avatarSet records a user's profile picture, or its removal
	// (ADR-0050).
	avatarSet = "AvatarSet"
	// contactsSet records the channels messages reach a user on
	// (ADR-0061).
	contactsSet = "ContactsSet"
	// accountRequested records a request for an account made at the
	// sign-in page; requestApproved and requestRejected decide it. An
	// approval is followed by the account's own events (ADR-0061).
	accountRequested = "AccountRequested"
	requestApproved  = "AccountRequestApproved"
	requestRejected  = "AccountRequestRejected"
	// digestSent records how far notifications were sent to a user's
	// channels (ADR-0064).
	digestSent = "DigestSent"
)

// apply is the only code that changes accounts; it runs live and on load.
func (a *Accounts) apply(e event) error {
	if e.V != 1 {
		return fmt.Errorf("format version %d is not supported", e.V)
	}
	switch e.Kind {
	case userCreated:
		u := &user{User: User{ID: e.User, Username: e.Username, Created: e.At}}
		a.users[e.User] = u
		a.byName[fold(e.Username)] = e.User
	case passwordSet:
		u, ok := a.users[e.User]
		if !ok {
			return fmt.Errorf("password for unknown user %s", e.User)
		}
		u.hash = e.Hash
	case tokenCreated:
		t := &token{Token: Token{ID: e.Token, User: e.User, Name: e.Name, Scope: e.Scope, Created: e.At, Expires: e.Expires}, hash: e.Hash}
		a.tokens[e.Hash] = t
		a.tokenIDs[e.Token] = t
	case identityLinked:
		if _, ok := a.users[e.User]; !ok {
			return fmt.Errorf("identity for unknown user %s", e.User)
		}
		a.external[e.Issuer+"\x00"+e.Subject] = e.User
	case languageSet:
		u, ok := a.users[e.User]
		if !ok {
			return fmt.Errorf("language for unknown user %s", e.User)
		}
		u.language = e.Language
	case notificationsSeen:
		u, ok := a.users[e.User]
		if !ok {
			return fmt.Errorf("notifications seen by unknown user %s", e.User)
		}
		u.seen = max(u.seen, e.Seen)
	case avatarSet:
		u, ok := a.users[e.User]
		if !ok {
			return fmt.Errorf("avatar of unknown user %s", e.User)
		}
		u.avatar, u.avatarType = e.Avatar, e.AvatarType
	case contactsSet:
		u, ok := a.users[e.User]
		if !ok || e.Contacts == nil {
			return fmt.Errorf("contacts of unknown user %s", e.User)
		}
		u.contacts = *e.Contacts
	case digestSent:
		u, ok := a.users[e.User]
		if !ok {
			return fmt.Errorf("digest of unknown user %s", e.User)
		}
		u.delivered = max(u.delivered, e.Seen)
	case accountRequested:
		var c Contacts
		if e.Contacts != nil {
			c = *e.Contacts
		}
		a.requests[e.Request] = &request{Request: Request{ID: e.Request, Username: e.Username, Email: c.Email, Reason: e.Reason, Created: e.At}, hash: e.Hash}
	case requestApproved, requestRejected:
		if _, ok := a.requests[e.Request]; !ok {
			return fmt.Errorf("decision on unknown account request %s", e.Request)
		}
		delete(a.requests, e.Request)
	case tokenRevoked:
		if t, ok := a.tokenIDs[e.Token]; ok {
			delete(a.tokens, t.hash)
			delete(a.tokenIDs, e.Token)
		}
	default:
		return fmt.Errorf("unknown event %q", e.Kind)
	}
	return nil
}

// record journals an event and applies it. Callers hold a.mu.
func (a *Accounts) record(e event) error {
	e.V, e.At = 1, a.now().UTC()
	data, err := json.Marshal(e)
	if err != nil {
		return err
	}
	if err := a.journal(data); err != nil {
		return fmt.Errorf("auth: change not persisted: %w", err)
	}
	return a.apply(e)
}

func fold(username string) string { return strings.ToLower(strings.TrimSpace(username)) }

func validUsername(name string) bool {
	if name != strings.TrimSpace(name) || name == "" || utf8.RuneCountInString(name) > 64 {
		return false
	}
	for _, r := range name {
		if r < 0x21 || r == 0x7f {
			return false
		}
	}
	return true
}

func validPassword(pw string) error {
	if utf8.RuneCountInString(pw) < MinPasswordLength {
		return fmt.Errorf("%w: a password needs at least %d characters", ErrInvalid, MinPasswordLength)
	}
	if len(pw) > 1024 {
		return fmt.Errorf("%w: password too long", ErrInvalid)
	}
	return nil
}

func (a *Accounts) newID(n int) (string, error) {
	b := make([]byte, n)
	if err := a.random(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// CreateUser creates an account with a password.
func (a *Accounts) CreateUser(username, password string) (User, error) {
	if !validUsername(username) {
		return User{}, fmt.Errorf("%w: a username is 1 to 64 visible characters without spaces", ErrInvalid)
	}
	if err := validPassword(password); err != nil {
		return User{}, err
	}
	hash, err := hashPassword(password, a.iter, a.random)
	if err != nil {
		return User{}, err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, taken := a.byName[fold(username)]; taken {
		return User{}, fmt.Errorf("%w: username %q", ErrExists, username)
	}
	id, err := a.newID(16)
	if err != nil {
		return User{}, err
	}
	uid := UserID(id)
	if err := a.record(event{Kind: userCreated, User: uid, Username: username}); err != nil {
		return User{}, err
	}
	if err := a.record(event{Kind: passwordSet, User: uid, Hash: hash}); err != nil {
		return User{}, err
	}
	return a.users[uid].User, nil
}

// languageRE is a language tag as the UI names its catalogues: "de",
// "en", or with a region, "de-CH".
var languageRE = regexp.MustCompile(`^[a-z]{2}(-[A-Z]{2})?$`)

// SetLanguage records the UI language a user chose (ADR-0017); empty
// clears it. Which languages the UI offers is the caller's concern.
func (a *Accounts) SetLanguage(id UserID, language string) error {
	if language != "" && !languageRE.MatchString(language) {
		return fmt.Errorf("%w: language %q", ErrInvalid, language)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	u, ok := a.users[id]
	if !ok {
		return ErrNotFound
	}
	if u.language == language {
		return nil
	}
	return a.record(event{Kind: languageSet, User: id, Language: language})
}

// SetAvatar records a user's profile picture of the given media type;
// empty data removes it. Checking the picture is the caller's concern.
func (a *Accounts) SetAvatar(id UserID, data []byte, mediaType string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	u, ok := a.users[id]
	if !ok {
		return ErrNotFound
	}
	if len(data) == 0 {
		if len(u.avatar) == 0 {
			return nil
		}
		mediaType = ""
	}
	return a.record(event{Kind: avatarSet, User: id, Avatar: slices.Clone(data), AvatarType: mediaType})
}

// Avatar returns a user's profile picture and its media type, or nil.
func (a *Accounts) Avatar(id UserID) ([]byte, string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	u, ok := a.users[id]
	if !ok || len(u.avatar) == 0 {
		return nil, ""
	}
	return u.avatar, u.avatarType
}

// SetSeen records that a user has seen their notifications up to the item
// event seq (ADR-0046); the mark only moves forward.
func (a *Accounts) SetSeen(id UserID, seq int) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	u, ok := a.users[id]
	if !ok {
		return ErrNotFound
	}
	if seq <= u.seen {
		return nil
	}
	return a.record(event{Kind: notificationsSeen, User: id, Seen: seq})
}

// Seen is how far a user has seen their notifications.
func (a *Accounts) Seen(id UserID) int {
	a.mu.Lock()
	defer a.mu.Unlock()
	if u, ok := a.users[id]; ok {
		return u.seen
	}
	return 0
}

// Usernames returns the usernames of the given IDs; unknown IDs are left
// out.
func (a *Accounts) Usernames(ids []UserID) map[UserID]string {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make(map[UserID]string, len(ids))
	for _, id := range ids {
		if u, ok := a.users[id]; ok {
			out[id] = u.Username
		}
	}
	return out
}

// Language is the UI language a user chose, or empty.
func (a *Accounts) Language(id UserID) string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if u, ok := a.users[id]; ok {
		return u.language
	}
	return ""
}

// SetPassword replaces a user's password after checking the current one.
func (a *Accounts) SetPassword(id UserID, current, next string) error {
	if err := validPassword(next); err != nil {
		return err
	}
	a.mu.Lock()
	u, ok := a.users[id]
	var old string
	if ok {
		old = u.hash
	}
	a.mu.Unlock()
	if !ok {
		return ErrNotFound
	}
	if !checkPassword(old, current) {
		return ErrUnauthorized
	}
	hash, err := hashPassword(next, a.iter, a.random)
	if err != nil {
		return err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.record(event{Kind: passwordSet, User: id, Hash: hash}); err != nil {
		return err
	}
	// A new password ends every other way in that relied on the old one.
	delete(a.resets, id)
	for k, s := range a.sessions {
		if s.user == id {
			delete(a.sessions, k)
		}
	}
	return nil
}

// Users lists the accounts, ordered by username.
func (a *Accounts) Users() []User {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]User, 0, len(a.users))
	for _, u := range a.users {
		out = append(out, u.User)
	}
	slices.SortFunc(out, func(x, y User) int { return strings.Compare(fold(x.Username), fold(y.Username)) })
	return out
}

// Login checks a username and password from a client address and returns a
// new session secret. Failures are throttled per address and per username,
// and charged before the account is looked up, so the answer and its timing
// do not reveal which usernames exist.
func (a *Accounts) Login(username, password, addr string) (secret string, p Principal, err error) {
	now := a.now()
	a.mu.Lock()
	if wait := a.throttle.blocked(now, "addr:"+addr, "user:"+fold(username)); wait > 0 {
		a.mu.Unlock()
		return "", Principal{}, &ThrottledError{Wait: wait}
	}
	id, ok := a.byName[fold(username)]
	hash := a.dummy
	if ok && a.users[id].hash != "" {
		hash = a.users[id].hash
	}
	a.mu.Unlock()

	// An account without a password (external identity) costs the same
	// derivation and never matches.
	good := checkPassword(hash, password) && ok && hash != a.dummy

	a.mu.Lock()
	defer a.mu.Unlock()
	if !good {
		a.throttle.fail(now, "addr:"+addr, "user:"+fold(username))
		return "", Principal{}, ErrUnauthorized
	}
	a.throttle.reset("user:" + fold(username))
	return a.openSession(id)
}

// openSession creates a session; callers hold a.mu.
func (a *Accounts) openSession(id UserID) (string, Principal, error) {
	now := a.now()
	secret, err := a.newID(32)
	if err != nil {
		return "", Principal{}, err
	}
	sid, err := a.newID(8)
	if err != nil {
		return "", Principal{}, err
	}
	for k, s := range a.sessions {
		if !now.Before(s.expires) {
			delete(a.sessions, k)
		}
	}
	a.sessions[hashSecret(secret)] = &session{user: id, id: sid, expires: now.Add(SessionLifetime)}
	return secret, a.principal(id, ScopeFull, "session", sid), nil
}

// Logout ends the session with the given ID.
func (a *Accounts) Logout(sessionID string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for k, s := range a.sessions {
		if s.id == sessionID {
			delete(a.sessions, k)
		}
	}
}

func (a *Accounts) principal(id UserID, scope Scope, via, sid string) Principal {
	return Principal{User: id, Username: a.users[id].Username, Scope: scope, Via: via, Session: sid}
}

// Session resolves a session secret.
func (a *Accounts) Session(secret string) (Principal, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	h := hashSecret(secret)
	s, ok := a.sessions[h]
	if !ok {
		return Principal{}, false
	}
	if !a.now().Before(s.expires) {
		delete(a.sessions, h)
		return Principal{}, false
	}
	return a.principal(s.user, ScopeFull, "session", s.id), true
}

// CreateToken issues an API token for a user. The secret is returned once
// and only its SHA-256 hash is kept.
func (a *Accounts) CreateToken(id UserID, name string, scope Scope, lifetime time.Duration) (string, Token, error) {
	if scope != ScopeFull && scope != ScopeRead && scope != ScopeMetrics {
		return "", Token{}, fmt.Errorf("%w: scope %q", ErrInvalid, scope)
	}
	if lifetime <= 0 || lifetime > MaxTokenLifetime {
		return "", Token{}, fmt.Errorf("%w: a token expires after 1 second to %d days", ErrInvalid, int(MaxTokenLifetime.Hours()/24))
	}
	if n := utf8.RuneCountInString(name); n == 0 || n > 100 {
		return "", Token{}, fmt.Errorf("%w: a token needs a name of up to 100 characters", ErrInvalid)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, ok := a.users[id]; !ok {
		return "", Token{}, ErrNotFound
	}
	raw, err := a.newID(32)
	if err != nil {
		return "", Token{}, err
	}
	tid, err := a.newID(8)
	if err != nil {
		return "", Token{}, err
	}
	secret := TokenPrefix + raw
	err = a.record(event{Kind: tokenCreated, Token: tid, User: id, Name: name, Scope: scope,
		Hash: hashSecret(secret), Expires: a.now().Add(lifetime).UTC()})
	if err != nil {
		return "", Token{}, err
	}
	return secret, a.tokenIDs[tid].Token, nil
}

// Tokens lists a user's tokens, newest first.
func (a *Accounts) Tokens(id UserID) []Token {
	a.mu.Lock()
	defer a.mu.Unlock()
	var out []Token
	for _, t := range a.tokenIDs {
		if t.User == id {
			out = append(out, t.Token)
		}
	}
	slices.SortFunc(out, func(x, y Token) int { return y.Created.Compare(x.Created) })
	return out
}

// RevokeToken deletes one of a user's tokens.
func (a *Accounts) RevokeToken(user UserID, tokenID string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	t, ok := a.tokenIDs[tokenID]
	if !ok || t.User != user {
		return ErrNotFound
	}
	return a.record(event{Kind: tokenRevoked, Token: tokenID})
}

// TokenPrincipal resolves an API token secret.
func (a *Accounts) TokenPrincipal(secret string) (Principal, bool) {
	if !strings.HasPrefix(secret, TokenPrefix) {
		return Principal{}, false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	t, ok := a.tokens[hashSecret(secret)]
	if !ok || !a.now().Before(t.Expires) {
		return Principal{}, false
	}
	return a.principal(t.User, t.Scope, "token", ""), true
}

// HasUsers reports whether any account exists.
func (a *Accounts) HasUsers() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.users) > 0
}

// hashSecret hashes a high-entropy secret for lookup. Comparing hashes in a
// map is safe: the attacker does not control the stored side.
func hashSecret(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

func equalStrings(a, b string) bool { return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1 }

// UserByIdentity returns the account linked to an external identity.
func (a *Accounts) UserByIdentity(issuer, subject string) (User, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	id, ok := a.external[issuer+"\x00"+subject]
	if !ok {
		return User{}, false
	}
	return a.users[id].User, true
}

// CreateExternalUser creates an account without a password for an identity
// at an external provider (just-in-time provisioning, ADR-0030). The
// username is the preferred one if it is valid and free, otherwise it gets
// a numeric suffix; usernames are only labels, the ID is the identity.
func (a *Accounts) CreateExternalUser(preferred, issuer, subject string) (User, error) {
	if issuer == "" || subject == "" {
		return User{}, fmt.Errorf("%w: an external identity needs issuer and subject", ErrInvalid)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, ok := a.external[issuer+"\x00"+subject]; ok {
		return User{}, fmt.Errorf("%w: identity already linked", ErrExists)
	}
	base := preferred
	if !validUsername(base) || utf8.RuneCountInString(base) > 56 {
		base = "user"
	}
	name := base
	for n := 2; ; n++ {
		if _, taken := a.byName[fold(name)]; !taken {
			break
		}
		name = fmt.Sprintf("%s-%d", base, n)
	}
	id, err := a.newID(16)
	if err != nil {
		return User{}, err
	}
	uid := UserID(id)
	if err := a.record(event{Kind: userCreated, User: uid, Username: name}); err != nil {
		return User{}, err
	}
	if err := a.record(event{Kind: identityLinked, User: uid, Issuer: issuer, Subject: subject}); err != nil {
		return User{}, err
	}
	return a.users[uid].User, nil
}

// StartSession opens a session for a user an external provider has
// authenticated.
func (a *Accounts) StartSession(id UserID) (string, Principal, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, ok := a.users[id]; !ok {
		return "", Principal{}, ErrNotFound
	}
	return a.openSession(id)
}
