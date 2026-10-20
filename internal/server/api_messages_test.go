package server

import (
	"context"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"testing"

	"github.com/dbuchs7/wikiwikibangbang/internal/auth"
	"github.com/dbuchs7/wikiwikibangbang/internal/notify"
)

// outbox keeps the mail a test server would have sent.
type outbox struct {
	mu   sync.Mutex
	mail []sentMail
}

type sentMail struct {
	to string
	m  notify.Message
}

func (o *outbox) send(_ context.Context, to string, m notify.Message) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.mail = append(o.mail, sentMail{to, m})
	return nil
}

func (o *outbox) to(addr string) []notify.Message {
	o.mu.Lock()
	defer o.mu.Unlock()
	var out []notify.Message
	for _, s := range o.mail {
		if s.to == addr {
			out = append(out, s.m)
		}
	}
	return out
}

// messagingWorld is a world whose server sends mail into an outbox and
// may post to webhooks on this host.
func messagingWorld(t *testing.T) (world, *outbox) {
	t.Helper()
	box := &outbox{}
	n, err := notify.New(notify.Config{PublicURL: "https://wiki.example.org", Mail: &notify.MailConfig{Transport: box.send},
		AllowedHook: func(a netip.Addr) bool { return a.IsLoopback() }})
	if err != nil {
		t.Fatal(err)
	}
	w := newWorld(t, func(d *Deps) { d.Messages = NewMessenger(n, d.DB, nil, 0) })
	return w, box
}

func TestPasswordResetByMail(t *testing.T) {
	w, box := messagingWorld(t)
	anon := &client{t: t, h: w.admin.h}
	if got := anon.expect("GET", "/api/signup", "", http.StatusOK); got["passwordReset"] != true {
		t.Fatalf("signup %v", got)
	}
	// Unknown and known accounts get the same answer.
	anon.expect("POST", "/api/password-reset", `{"username":"nobody"}`, http.StatusAccepted)
	anon.expect("POST", "/api/password-reset", `{"username":"bert"}`, http.StatusAccepted)
	w.deps.Messages.Wait()
	if len(box.mail) != 0 {
		t.Fatalf("bert has no mail address yet: %v", box.mail)
	}
	w.admin.expect("PUT", "/api/users/"+w.ids["bert"]+"/email", `{"email":"bert@example.org"}`, http.StatusNoContent)
	anon.expect("POST", "/api/password-reset", `{"username":"bert"}`, http.StatusAccepted)
	w.deps.Messages.Wait()
	got := box.to("bert@example.org")
	if len(got) != 1 || !strings.HasPrefix(got[0].Link, "https://wiki.example.org/#/reset/") || !strings.Contains(got[0].Text, "bert") {
		t.Fatalf("mail %+v", got)
	}
	token := strings.TrimPrefix(got[0].Link, "https://wiki.example.org/#/reset/")
	anon.expect("POST", "/api/password-reset/confirm", `{"token":"`+token+`","password":"short"}`, http.StatusBadRequest)
	anon.expect("POST", "/api/password-reset/confirm", `{"token":"`+token+`","password":"bert's new password"}`, http.StatusNoContent)
	anon.expect("POST", "/api/password-reset/confirm", `{"token":"`+token+`","password":"bert's newer password"}`, http.StatusNotFound)
	// Bert's old session ended; the new password signs in.
	w.bert.expect("GET", "/api/me", "", http.StatusUnauthorized)
	w.bert.login("bert", "bert's new password")

	// An administrator hands a link over.
	w.bert.expect("POST", "/api/users/"+w.ids["vera"]+"/password-reset", "", http.StatusForbidden)
	handed := w.admin.expect("POST", "/api/users/"+w.ids["vera"]+"/password-reset", "", http.StatusCreated)
	anon.expect("POST", "/api/password-reset/confirm", `{"token":"`+handed["token"].(string)+`","password":"vera's new password"}`, http.StatusNoContent)

	trail := strings.Join(eventsOf(w.admin.expect("GET", "/api/audit", "", http.StatusOK)), ",")
	for _, e := range []string{"password.reset.request", "password.reset", "password.reset.link"} {
		if !strings.Contains(trail, e) {
			t.Errorf("the audit trail has no %s", e)
		}
	}
}

func TestNoResetWithoutMail(t *testing.T) {
	w := newWorld(t)
	anon := &client{t: t, h: w.admin.h}
	if got := anon.expect("GET", "/api/signup", "", http.StatusOK); got["passwordReset"] != false {
		t.Fatalf("signup %v", got)
	}
	anon.expect("POST", "/api/password-reset", `{"username":"bert"}`, http.StatusServiceUnavailable)
	w.bert.expect("POST", "/api/me/contacts/test", "", http.StatusConflict)
}

func TestTestMessageAndDigest(t *testing.T) {
	w, box := messagingWorld(t)
	if err := w.db.Accounts.SetContacts(auth.UserID(w.ids["bert"]), auth.Contacts{Email: "bert@example.org"}); err != nil {
		t.Fatal(err)
	}
	res := w.bert.expect("POST", "/api/me/contacts/test", "", http.StatusOK)["results"].([]any)
	if len(res) != 1 || res[0].(map[string]any)["ok"] != true || len(box.to("bert@example.org")) != 1 {
		t.Fatalf("test %v", res)
	}
	w.bert.expect("POST", "/api/me/contacts/test", "", http.StatusTooManyRequests)
	if last := w.bert.expect("GET", "/api/me/contacts", "", http.StatusOK)["last"].(map[string]any); last["subject"] != "Test message" {
		t.Fatalf("last %v", last)
	}

	// The first digest round only marks where bert stands.
	m := w.deps.Messages
	m.Digest(context.Background())
	if len(box.to("bert@example.org")) != 1 {
		t.Fatal("the first round sends nothing")
	}
	// Admin changes an item bert watches: the next round tells bert, once.
	it := w.bert.expect("POST", "/api/items", `{"space":"PLAT","type":"task","title":"Patch","properties":{"priority":"low"}}`, http.StatusCreated)
	key := field(it, "item", "key").(string)
	m.Digest(context.Background())
	w.admin.expect("PUT", "/api/items/"+key+"/title", `{"title":"Patch now"}`, http.StatusOK)
	w.admin.expect("PUT", "/api/items/"+key+"/title", `{"title":"Patch today"}`, http.StatusOK)
	m.Digest(context.Background())
	m.Digest(context.Background())
	got := box.to("bert@example.org")
	if len(got) != 2 || got[1].Subject != "2 new changes on what you watch" || got[1].Link != "https://wiki.example.org/#/notifications" {
		t.Fatalf("digest %+v", got)
	}
}
