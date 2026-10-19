package auth

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/dbuchs7/wikiwikibangbang/internal/budget"
)

const pw = "correct horse battery"

type memJournal struct {
	records [][]byte
	fail    error
}

func (m *memJournal) append(r []byte) error {
	if m.fail != nil {
		return m.fail
	}
	m.records = append(m.records, r)
	return nil
}

type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

func newAccounts(t *testing.T) (*Accounts, *memJournal, *clock) {
	t.Helper()
	j := &memJournal{}
	c := &clock{time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)}
	a, err := New(nil, j.append, WithClock(c.now), WithIterations(1000))
	if err != nil {
		t.Fatal(err)
	}
	return a, j, c
}

func noErr(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func TestLoginAndSession(t *testing.T) {
	a, _, c := newAccounts(t)
	u, err := a.CreateUser("Anna", pw)
	noErr(t, err)
	secret, p, err := a.Login("anna", pw, "10.0.0.1") // usernames are case-insensitive
	noErr(t, err)
	if p.User != u.ID || p.Via != "session" || p.Scope != ScopeFull {
		t.Fatalf("principal %+v", p)
	}
	got, ok := a.Session(secret)
	if !ok || got.User != u.ID || got.Username != "Anna" {
		t.Fatalf("session lookup: %+v %v", got, ok)
	}
	if _, ok := a.Session("forged"); ok {
		t.Fatal("a forged session secret resolved")
	}
	c.t = c.t.Add(SessionLifetime)
	if _, ok := a.Session(secret); ok {
		t.Fatal("an expired session resolved")
	}
	secret, p, err = a.Login("Anna", pw, "10.0.0.1")
	noErr(t, err)
	a.Logout(p.Session)
	if _, ok := a.Session(secret); ok {
		t.Fatal("a session survived logout")
	}
}

func TestWrongCredentialsLookTheSame(t *testing.T) {
	a, _, _ := newAccounts(t)
	_, err := a.CreateUser("anna", pw)
	noErr(t, err)
	_, _, errWrongPassword := a.Login("anna", "wrong password!", "a")
	_, _, errNoUser := a.Login("nobody", pw, "a")
	if !errors.Is(errWrongPassword, ErrUnauthorized) || errWrongPassword.Error() != errNoUser.Error() {
		t.Fatalf("errors differ: %v / %v", errWrongPassword, errNoUser)
	}
}

func TestLoginThrottle(t *testing.T) {
	a, _, c := newAccounts(t)
	_, err := a.CreateUser("anna", pw)
	noErr(t, err)
	for range MaxFailures {
		if _, _, err := a.Login("anna", "wrong password!", "1.1.1.1"); !errors.Is(err, ErrUnauthorized) {
			t.Fatalf("failure: %v", err)
		}
	}
	// The username is blocked from every address, even with the right password.
	if _, _, err := a.Login("anna", pw, "2.2.2.2"); !errors.Is(err, ErrThrottled) {
		t.Fatalf("after %d failures: err = %v, want ErrThrottled", MaxFailures, err)
	}
	// The address is blocked for every username, existing or not.
	if _, _, err := a.Login("nobody", pw, "1.1.1.1"); !errors.Is(err, ErrThrottled) {
		t.Fatalf("blocked address, unknown user: err = %v", err)
	}
	c.t = c.t.Add(FailureWindow)
	if _, _, err := a.Login("anna", pw, "1.1.1.1"); err != nil {
		t.Fatalf("after the window: %v", err)
	}
}

func TestThrottleMemoryIsBounded(t *testing.T) {
	var th throttle
	th.init()
	now := time.Now()
	maxKeys := budget.Get().LoginTracked
	for i := range maxKeys + 10 {
		th.fail(now, "k"+strings.Repeat("x", i%7)+string(rune('a'+i%26))+time.Duration(i).String())
	}
	if len(th.failures) > maxKeys+1 {
		t.Fatalf("%d keys tracked, limit %d", len(th.failures), maxKeys)
	}
}

func TestTokens(t *testing.T) {
	a, _, c := newAccounts(t)
	anna, err := a.CreateUser("anna", pw)
	noErr(t, err)
	bert, err := a.CreateUser("bert", pw)
	noErr(t, err)

	secret, tok, err := a.CreateToken(anna.ID, "ci", ScopeRead, time.Hour)
	noErr(t, err)
	if !strings.HasPrefix(secret, TokenPrefix) || tok.Scope != ScopeRead {
		t.Fatalf("token %q %+v", secret, tok)
	}
	p, ok := a.TokenPrincipal(secret)
	if !ok || p.User != anna.ID || p.Scope != ScopeRead || p.Via != "token" {
		t.Fatalf("principal %+v %v", p, ok)
	}
	for _, bad := range []string{"", "wwbb_nope", secret[len(TokenPrefix):]} {
		if _, ok := a.TokenPrincipal(bad); ok {
			t.Errorf("%q resolved", bad)
		}
	}
	if got := a.Tokens(anna.ID); len(got) != 1 || got[0].ID != tok.ID {
		t.Fatalf("listing: %+v", got)
	}
	if err := a.RevokeToken(bert.ID, tok.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("revoking someone else's token: %v", err)
	}
	c.t = c.t.Add(time.Hour)
	if _, ok := a.TokenPrincipal(secret); ok {
		t.Fatal("an expired token resolved")
	}
	noErr(t, a.RevokeToken(anna.ID, tok.ID))
	if len(a.Tokens(anna.ID)) != 0 {
		t.Fatal("revoked token still listed")
	}

	for name, call := range map[string]func() error{
		"scope":    func() error { _, _, err := a.CreateToken(anna.ID, "x", "admin", time.Hour); return err },
		"lifetime": func() error { _, _, err := a.CreateToken(anna.ID, "x", ScopeFull, MaxTokenLifetime+1); return err },
		"no name":  func() error { _, _, err := a.CreateToken(anna.ID, "", ScopeFull, time.Hour); return err },
	} {
		if err := call(); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: err = %v, want ErrInvalid", name, err)
		}
	}
	if _, _, err := a.CreateToken("ghost", "x", ScopeFull, time.Hour); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown user: %v", err)
	}
}

func TestUsersAndPasswords(t *testing.T) {
	a, _, _ := newAccounts(t)
	for name, call := range map[string]func() error{
		"short password": func() error { _, err := a.CreateUser("x", "short"); return err },
		"long password":  func() error { _, err := a.CreateUser("x", strings.Repeat("p", 1025)); return err },
		"empty name":     func() error { _, err := a.CreateUser("", pw); return err },
		"spaced name":    func() error { _, err := a.CreateUser("a b", pw); return err },
		"padded name":    func() error { _, err := a.CreateUser(" a", pw); return err },
		"long name":      func() error { _, err := a.CreateUser(strings.Repeat("n", 65), pw); return err },
	} {
		if err := call(); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: err = %v, want ErrInvalid", name, err)
		}
	}
	u, err := a.CreateUser("anna", pw)
	noErr(t, err)
	if _, err := a.CreateUser("ANNA", pw); !errors.Is(err, ErrExists) {
		t.Fatalf("duplicate username: %v", err)
	}
	secret, _, err := a.Login("anna", pw, "x")
	noErr(t, err)
	if err := a.SetPassword(u.ID, "wrong password!", "a new password!"); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("wrong current password: %v", err)
	}
	if err := a.SetPassword(u.ID, pw, "short"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("short new password: %v", err)
	}
	if err := a.SetPassword("ghost", pw, "a new password!"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown user: %v", err)
	}
	noErr(t, a.SetPassword(u.ID, pw, "a new password!"))
	if _, ok := a.Session(secret); ok {
		t.Fatal("a session survived the password change")
	}
	if _, _, err := a.Login("anna", "a new password!", "x"); err != nil {
		t.Fatal(err)
	}
	if got := a.Users(); len(got) != 1 || got[0].ID != u.ID || !a.HasUsers() {
		t.Fatalf("users: %+v", got)
	}
}

// TestAccountsSurviveARestart rebuilds the accounts from their journal.
func TestAccountsSurviveARestart(t *testing.T) {
	a, j, _ := newAccounts(t)
	u, err := a.CreateUser("anna", pw)
	noErr(t, err)
	secret, _, err := a.CreateToken(u.ID, "ci", ScopeFull, time.Hour)
	noErr(t, err)
	_, revoked, err := a.CreateToken(u.ID, "old", ScopeFull, time.Hour)
	noErr(t, err)
	noErr(t, a.RevokeToken(u.ID, revoked.ID))

	b, err := New(j.records, j.append, WithClock(a.now))
	noErr(t, err)
	if _, _, err := b.Login("anna", pw, "x"); err != nil {
		t.Fatalf("login after restart: %v", err)
	}
	if p, ok := b.TokenPrincipal(secret); !ok || p.User != u.ID {
		t.Fatal("token lost in restart")
	}
	if len(b.Tokens(u.ID)) != 1 {
		t.Fatal("revocation lost in restart")
	}
	for _, bad := range [][]byte{[]byte("{"), []byte(`{"v":2,"kind":"UserCreated"}`), []byte(`{"v":1,"kind":"Nope"}`), []byte(`{"v":1,"kind":"PasswordSet","user":"ghost"}`)} {
		if _, err := New([][]byte{bad}, j.append); err == nil {
			t.Errorf("%s: loaded", bad)
		}
	}
}

func TestAFailedJournalChangesNothing(t *testing.T) {
	a, j, _ := newAccounts(t)
	j.fail = errors.New("disk full")
	if _, err := a.CreateUser("anna", pw); err == nil || a.HasUsers() {
		t.Fatalf("err = %v, users exist: %v", err, a.HasUsers())
	}
}

func TestPasswordHashes(t *testing.T) {
	rnd := func(b []byte) error { return nil }
	h, err := hashPassword(pw, 1000, rnd)
	noErr(t, err)
	if !checkPassword(h, pw) || checkPassword(h, pw+"x") {
		t.Fatal("hash does not verify exactly")
	}
	for _, bad := range []string{"", "md5$1$a$b", "pbkdf2-sha256$x$a$b", "pbkdf2-sha256$0$AA$AA", "pbkdf2-sha256$1$!!$AA", dummyHash(1000)} {
		if checkPassword(bad, pw) {
			t.Errorf("%q matched", bad)
		}
	}
	if _, err := hashPassword(pw, 1000, func([]byte) error { return errors.New("no entropy") }); err == nil {
		t.Fatal("hashing without randomness succeeded")
	}
}

func TestExternalUsers(t *testing.T) {
	a, j, _ := newAccounts(t)
	_, err := a.CreateUser("anna", pw)
	noErr(t, err)
	u, err := a.CreateExternalUser("anna", "https://idp", "sub-1")
	noErr(t, err)
	if u.Username != "anna-2" {
		t.Fatalf("username %q, want a suffix when taken", u.Username)
	}
	if _, err := a.CreateExternalUser("x", "https://idp", "sub-1"); !errors.Is(err, ErrExists) {
		t.Fatalf("second link: %v", err)
	}
	if _, err := a.CreateExternalUser("x", "", "sub"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("no issuer: %v", err)
	}
	odd, err := a.CreateExternalUser("has space", "https://idp", "sub-2")
	noErr(t, err)
	if odd.Username != "user" {
		t.Fatalf("invalid preferred name became %q", odd.Username)
	}
	if got, ok := a.UserByIdentity("https://idp", "sub-1"); !ok || got.ID != u.ID {
		t.Fatal("identity not found")
	}
	if _, ok := a.UserByIdentity("https://other", "sub-1"); ok {
		t.Fatal("the issuer is not part of the match")
	}
	// An external account has no password; the password login refuses it.
	if _, _, err := a.Login("anna-2", "", "x"); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("password login of an external account: %v", err)
	}
	secret, p, err := a.StartSession(u.ID)
	noErr(t, err)
	if got, ok := a.Session(secret); !ok || got.User != u.ID || p.Via != "session" {
		t.Fatal("external session does not resolve")
	}
	if _, _, err := a.StartSession("ghost"); !errors.Is(err, ErrNotFound) {
		t.Fatal("session for an unknown user")
	}
	b, err := New(j.records, j.append, WithIterations(1000))
	noErr(t, err)
	if got, ok := b.UserByIdentity("https://idp", "sub-1"); !ok || got.ID != u.ID {
		t.Fatal("identity link lost in restart")
	}
	if _, err := New([][]byte{[]byte(`{"v":1,"kind":"IdentityLinked","user":"ghost"}`)}, j.append); err == nil {
		t.Fatal("a link to an unknown user loaded")
	}
}

func TestLanguageIsKeptPerUserAndSurvivesReload(t *testing.T) {
	a, j, _ := newAccounts(t)
	u, err := a.CreateUser("anna", "anna's password")
	noErr(t, err)
	if got := a.Language(u.ID); got != "" {
		t.Fatalf("a new user has language %q", got)
	}
	noErr(t, a.SetLanguage(u.ID, "de"))
	n := len(j.records)
	noErr(t, a.SetLanguage(u.ID, "de")) // unchanged: nothing recorded
	if len(j.records) != n {
		t.Error("an unchanged language was recorded again")
	}
	for _, bad := range []string{"german", "DE", "de-ch", "de_CH", "x"} {
		if err := a.SetLanguage(u.ID, bad); !errors.Is(err, ErrInvalid) {
			t.Errorf("%q: %v", bad, err)
		}
	}
	if err := a.SetLanguage("nobody", "de"); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown user: %v", err)
	}
	b, err := New(j.records, j.append, WithIterations(1000))
	noErr(t, err)
	if got := b.Language(u.ID); got != "de" {
		t.Fatalf("after reload: %q", got)
	}
	noErr(t, b.SetLanguage(u.ID, ""))
	if got := b.Language(u.ID); got != "" {
		t.Fatalf("cleared: %q", got)
	}
}

func TestNotificationsSeenOnlyMovesForward(t *testing.T) {
	var journal [][]byte
	a, err := New(nil, func(b []byte) error { journal = append(journal, append([]byte{}, b...)); return nil }, WithIterations(1000))
	if err != nil {
		t.Fatal(err)
	}
	u, err := a.CreateUser("anna", "anna's password")
	if err != nil {
		t.Fatal(err)
	}
	for _, seq := range []int{10, 5, 12} {
		if err := a.SetSeen(u.ID, seq); err != nil {
			t.Fatal(err)
		}
	}
	if a.Seen(u.ID) != 12 {
		t.Fatalf("seen %d", a.Seen(u.ID))
	}
	again, err := New(journal, func([]byte) error { return nil }, WithIterations(1000))
	if err != nil || again.Seen(u.ID) != 12 {
		t.Fatalf("after reload %d, %v", again.Seen(u.ID), err)
	}
	if err := a.SetSeen("nobody", 1); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown user: %v", err)
	}
}

func TestAvatarsAreKeptAndReplayed(t *testing.T) {
	a, j, _ := newAccounts(t)
	u, err := a.CreateUser("anna", pw)
	noErr(t, err)
	if data, _ := a.Avatar(u.ID); data != nil {
		t.Fatal("a new account has a picture")
	}
	pic := []byte("\x89PNG picture")
	noErr(t, a.SetAvatar(u.ID, pic, "image/png"))
	pic[0] = 0 // the caller's buffer is not kept
	if data, typ := a.Avatar(u.ID); string(data) != "\x89PNG picture" || typ != "image/png" {
		t.Fatalf("avatar %q %q", data, typ)
	}
	again, err := New(j.records, j.append, WithIterations(1000))
	noErr(t, err)
	if data, _ := again.Avatar(u.ID); string(data) != "\x89PNG picture" {
		t.Fatalf("after a restart: %q", data)
	}
	noErr(t, a.SetAvatar(u.ID, nil, ""))
	count := len(j.records)
	noErr(t, a.SetAvatar(u.ID, nil, "")) // removing nothing records nothing
	if data, _ := a.Avatar(u.ID); data != nil || len(j.records) != count {
		t.Fatal("the picture was not removed, or removing twice was recorded")
	}
	if err := a.SetAvatar("nobody", pic, "image/png"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown user: %v", err)
	}
}
