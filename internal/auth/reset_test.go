package auth

import (
	"errors"
	"testing"
	"time"
)

func TestPasswordReset(t *testing.T) {
	a, j, c := newAccounts(t)
	u, err := a.CreateUser("Gina", pw)
	noErr(t, err)
	if _, _, _, err := a.RequestReset("gina", "10.0.0.1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("an account without a mail address: %v", err)
	}
	noErr(t, a.SetEmail(u.ID, "gina@example.org"))
	secret, _, err := a.Login("Gina", pw, "10.0.0.1")
	noErr(t, err)
	token, got, email, err := a.RequestReset("GINA", "10.0.0.1")
	noErr(t, err)
	if got.ID != u.ID || email != "gina@example.org" || len(token) != 64 {
		t.Fatalf("reset %v %q %d", got, email, len(token))
	}
	if _, err := a.CompleteReset(token, "short"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("a short password: %v", err)
	}
	if _, err := a.CompleteReset("wrong", "a new long password"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a wrong token: %v", err)
	}
	if _, err := a.CompleteReset(token, "a new long password"); err != nil {
		t.Fatal(err)
	}
	if _, ok := a.Session(secret); ok {
		t.Fatal("a reset ends the sessions")
	}
	if _, _, err := a.Login("Gina", "a new long password", "10.0.0.1"); err != nil {
		t.Fatalf("the new password: %v", err)
	}
	if _, err := a.CompleteReset(token, "another long password"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a token is used once: %v", err)
	}
	// The new password is journaled; tokens are not.
	b, err := New(j.records, j.append, WithIterations(1000))
	noErr(t, err)
	if _, _, err := b.Login("Gina", "a new long password", "10.0.0.1"); err != nil {
		t.Fatalf("replayed: %v", err)
	}

	// Tokens expire; a newer one replaces an older one.
	old, _, _, err := a.RequestReset("gina", "10.0.0.2")
	noErr(t, err)
	newer, _, _, err := a.RequestReset("gina", "10.0.0.2")
	noErr(t, err)
	if _, err := a.CompleteReset(old, "yet another password"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a replaced token: %v", err)
	}
	c.t = c.t.Add(ResetLifetime)
	if _, err := a.CompleteReset(newer, "yet another password"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("an expired token: %v", err)
	}
	handed, expires, err := a.HandReset(u.ID)
	noErr(t, err)
	if !expires.Equal(c.t.Add(HandedResetLifetime)) {
		t.Fatalf("expires %v", expires)
	}
	c.t = c.t.Add(time.Hour)
	if _, err := a.CompleteReset(handed, "handed over password"); err != nil {
		t.Fatalf("a handed token lasts longer: %v", err)
	}
}

func TestPasswordResetIsThrottled(t *testing.T) {
	a, _, _ := newAccounts(t)
	for range MaxFailures {
		_, _, _, _ = a.RequestReset("nobody", "10.0.0.3")
	}
	if _, _, _, err := a.RequestReset("someone", "10.0.0.3"); !errors.Is(err, ErrThrottled) {
		t.Fatalf("per address: %v", err)
	}
	for i := range MaxFailures {
		_, _, _, _ = a.RequestReset("victim", "10.0.1."+string(rune('0'+i)))
	}
	if _, _, _, err := a.RequestReset("victim", "10.0.2.1"); !errors.Is(err, ErrThrottled) {
		t.Fatalf("per username: %v", err)
	}
}

func TestDeliveredOnlyMovesForward(t *testing.T) {
	a, j, _ := newAccounts(t)
	u, err := a.CreateUser("Hans", pw)
	noErr(t, err)
	noErr(t, a.SetDelivered(u.ID, 7))
	noErr(t, a.SetDelivered(u.ID, 3))
	n := len(j.records)
	noErr(t, a.SetDelivered(u.ID, 7))
	if a.Delivered(u.ID) != 7 || len(j.records) != n {
		t.Fatalf("delivered %d", a.Delivered(u.ID))
	}
	b, _ := New(j.records, j.append, WithIterations(1000))
	if b.Delivered(u.ID) != 7 {
		t.Fatal("replayed")
	}
}

func TestUnknownAccounts(t *testing.T) {
	a, _, _ := newAccounts(t)
	if _, _, err := a.HandReset("nobody"); !errors.Is(err, ErrNotFound) {
		t.Errorf("hand reset: %v", err)
	}
	if err := a.SetEmail("nobody", "a@example.org"); !errors.Is(err, ErrNotFound) {
		t.Errorf("set email: %v", err)
	}
	if err := a.SetDelivered("nobody", 3); !errors.Is(err, ErrNotFound) {
		t.Errorf("set delivered: %v", err)
	}
	if a.Contacts("nobody") != (Contacts{}) || a.Delivered("nobody") != 0 {
		t.Error("an unknown account has nothing")
	}
	u, err := a.CreateUser("Ida", pw)
	noErr(t, err)
	if err := a.SetEmail(u.ID, "not an address"); !errors.Is(err, ErrInvalid) {
		t.Errorf("a bad address: %v", err)
	}
	noErr(t, a.SetEmail(u.ID, "ida@example.org"))
	noErr(t, a.SetEmail(u.ID, "ida@example.org")) // unchanged
}
