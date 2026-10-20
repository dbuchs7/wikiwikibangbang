package auth

import (
	"errors"
	"testing"

	"github.com/dbuchs7/wikiwikibangbang/internal/budget"
)

func TestAccountRequestApproved(t *testing.T) {
	a, j, c := newAccounts(t)
	r, err := a.RequestAccount("Berta", pw, "berta@example.org", "Team Finance", "10.0.0.1")
	noErr(t, err)
	if got := a.Requests(); len(got) != 1 || got[0].ID != r.ID || got[0].Email != "berta@example.org" || got[0].Reason != "Team Finance" || !got[0].Created.Equal(c.t) {
		t.Fatalf("requests %+v", got)
	}
	if _, err := a.RequestAccount("berta", pw, "x@example.org", "", "10.0.0.2"); !errors.Is(err, ErrExists) {
		t.Fatalf("a waiting username is taken: %v", err)
	}
	if _, _, err := a.Login("Berta", pw, "10.0.0.1"); err == nil {
		t.Fatal("a request is no account")
	}
	u, err := a.ApproveRequest(r.ID)
	noErr(t, err)
	if len(a.Requests()) != 0 {
		t.Fatal("an approved request is decided")
	}
	// The password chosen at the request signs in; the address is kept.
	if _, p, err := a.Login("Berta", pw, "10.0.0.1"); err != nil || p.User != u.ID {
		t.Fatalf("login after approval: %v", err)
	}
	if a.Contacts(u.ID).Email != "berta@example.org" {
		t.Fatalf("contacts %+v", a.Contacts(u.ID))
	}
	if _, err := a.ApproveRequest(r.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("decided twice: %v", err)
	}
	// Everything replays.
	b, err := New(j.records, j.append, WithIterations(1000))
	noErr(t, err)
	if _, _, err := b.Login("Berta", pw, "10.0.0.1"); err != nil || b.Contacts(u.ID).Email != "berta@example.org" || len(b.Requests()) != 0 {
		t.Fatalf("replayed: %v %+v", err, b.Contacts(u.ID))
	}
}

func TestAccountRequestRejected(t *testing.T) {
	a, j, _ := newAccounts(t)
	r, err := a.RequestAccount("Cara", pw, "cara@example.org", "", "10.0.0.1")
	noErr(t, err)
	b, err := New(j.records, j.append, WithIterations(1000))
	noErr(t, err)
	if len(b.Requests()) != 1 {
		t.Fatal("a waiting request survives a restart")
	}
	if _, err := a.RejectRequest(r.ID); err != nil {
		t.Fatal(err)
	}
	if len(a.Requests()) != 0 || len(a.Users()) != 0 {
		t.Fatal("a rejected request leaves nothing")
	}
	if _, err := a.RequestAccount("Cara", pw, "cara@example.org", "", "10.0.0.1"); err != nil {
		t.Fatalf("the name is free again: %v", err)
	}
}

func TestAccountRequestIsChecked(t *testing.T) {
	a, _, _ := newAccounts(t)
	_, err := a.CreateUser("Dora", pw)
	noErr(t, err)
	for name, args := range map[string][3]string{
		"taken":      {"dora", pw, "d@example.org"},
		"no mail":    {"Emil", pw, ""},
		"bad mail":   {"Emil", pw, "Emil <e@example.org>"},
		"short pw":   {"Emil", "short", "e@example.org"},
		"bad name":   {"E mil", pw, "e@example.org"},
		"local mail": {"Emil", pw, "e@localhost"},
	} {
		if _, err := a.RequestAccount(args[0], args[1], args[2], "", "10.0.0.1"); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if len(a.Requests()) != 0 {
		t.Fatal("refused requests are not kept")
	}
}

func TestAccountRequestsAreBounded(t *testing.T) {
	a, _, _ := newAccounts(t)
	// One address may ask MaxFailures times within the window.
	for i := range MaxFailures {
		if _, err := a.RequestAccount("u"+string(rune('a'+i)), pw, "u@example.org", "", "10.0.0.9"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := a.RequestAccount("late", pw, "u@example.org", "", "10.0.0.9"); !errors.Is(err, ErrThrottled) {
		t.Fatalf("throttle: %v", err)
	}
	b := budget.Default()
	b.AccountRequests = MaxFailures
	defer budget.Set(budget.Default())
	budget.Set(b)
	if _, err := a.RequestAccount("other", pw, "u@example.org", "", "10.0.0.10"); !errors.Is(err, ErrFull) {
		t.Fatalf("waiting requests are bounded: %v", err)
	}
}

func TestContacts(t *testing.T) {
	a, j, _ := newAccounts(t)
	u, err := a.CreateUser("Fritz", pw)
	noErr(t, err)
	if got := a.Contacts(u.ID).Channels(); len(got) != 0 {
		t.Fatalf("a new account has no channels: %v", got)
	}
	c := Contacts{Email: " fritz@example.org ", Slack: "https://hooks.slack.com/services/T0/B0/x", Teams: "https://example.webhook.office.com/webhookb2/x"}
	noErr(t, a.SetContacts(u.ID, c))
	got := a.Contacts(u.ID)
	if got.Email != "fritz@example.org" || len(got.Channels()) != 3 || got.Channels()[2] != ChannelTeams {
		t.Fatalf("contacts %+v %v", got, got.Channels())
	}
	noErr(t, a.SetEmail(u.ID, "f@example.org"))
	if got := a.Contacts(u.ID); got.Email != "f@example.org" || got.Slack == "" {
		t.Fatalf("setting the address keeps the rest: %+v", got)
	}
	for name, bad := range map[string]Contacts{
		"slack elsewhere": {Slack: "https://example.org/hook"},
		"plain http":      {Teams: "http://example.webhook.office.com/x"},
		"credentials":     {Webhook: "https://user:pw@example.org/x"},
		"not an address":  {Email: "fritz"},
	} {
		if err := a.SetContacts(u.ID, bad); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: %v", name, err)
		}
	}
	b, err := New(j.records, j.append, WithIterations(1000))
	noErr(t, err)
	if b.Contacts(u.ID) != a.Contacts(u.ID) {
		t.Fatalf("replayed contacts %+v", b.Contacts(u.ID))
	}
	if err := a.SetContacts("nobody", Contacts{}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown user: %v", err)
	}
}
