package server

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/dbuchs7/wikiwikibangbang/internal/auth"
	"github.com/dbuchs7/wikiwikibangbang/internal/item"
	"github.com/dbuchs7/wikiwikibangbang/internal/notify"
	"github.com/dbuchs7/wikiwikibangbang/internal/storage"
)

// Messenger sends messages to people over the channels on their accounts
// (ADR-0064): a digest of their notifications, word of account requests
// and decisions, password reset links and test messages. It keeps the
// outcome of the last message per person, for their account page.
type Messenger struct {
	n     *notify.Notifier
	db    *storage.DB
	log   *slog.Logger
	every time.Duration

	mu     sync.Mutex
	last   map[auth.UserID]delivery
	tested map[auth.UserID]time.Time
	wg     sync.WaitGroup
}

type delivery struct {
	At      time.Time       `json:"at"`
	Subject string          `json:"subject"`
	Results []notify.Result `json:"results"`
}

// NewMessenger returns a Messenger; with a nil Notifier it sends nothing.
// every is how often the digest is sent; 0 means never.
func NewMessenger(n *notify.Notifier, db *storage.DB, log *slog.Logger, every time.Duration) *Messenger {
	if log == nil {
		log = slog.Default()
	}
	return &Messenger{n: n, db: db, log: log, every: every, last: map[auth.UserID]delivery{}, tested: map[auth.UserID]time.Time{}}
}

// Every is how often the digest is sent; 0 means never.
func (m *Messenger) Every() time.Duration { return m.every }

// resetByMail reports whether a forgotten password can be reset by mail.
func (m *Messenger) resetByMail() bool { return m.n.Mail() && m.n.PublicURL() != "" }

// reachable reports whether any channel of c can be used.
func (m *Messenger) reachable(c auth.Contacts) bool {
	return (c.Email != "" && m.n.Mail()) || c.Slack != "" || c.Teams != "" || c.Webhook != ""
}

// Send delivers a message to a user on all their channels and keeps the
// outcome.
func (m *Messenger) Send(ctx context.Context, id auth.UserID, msg notify.Message) []notify.Result {
	res := m.n.Send(ctx, m.db.Accounts.Contacts(id), msg)
	if len(res) == 0 {
		return res
	}
	for _, r := range res {
		if !r.OK {
			m.log.Warn("message not delivered", "user", string(id), "channel", r.Channel, "problem", r.Problem)
		}
	}
	m.mu.Lock()
	m.last[id] = delivery{At: time.Now().UTC(), Subject: msg.Subject, Results: res}
	m.mu.Unlock()
	return res
}

// Last is the outcome of the last message to a user, if any.
func (m *Messenger) Last(id auth.UserID) (delivery, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, ok := m.last[id]
	return d, ok
}

// later runs fn in the background, so a request does not wait for, and
// cannot be timed by, a mail server.
func (m *Messenger) later(fn func(ctx context.Context)) {
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		fn(ctx)
	}()
}

// Wait waits for the messages sent in the background, for tests and for
// shutting down.
func (m *Messenger) Wait() { m.wg.Wait() }

// Run sends the digest at every tick until ctx ends.
func (m *Messenger) Run(ctx context.Context, tick <-chan time.Time) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick:
			m.Digest(ctx)
		}
	}
}

// Digest tells everyone with a channel how many notifications arrived
// since the last digest and since they last looked. The first round for
// a person only marks where they stand, so a new channel does not bring
// the whole past.
func (m *Messenger) Digest(ctx context.Context) {
	for _, u := range m.db.Accounts.Users() {
		if ctx.Err() != nil {
			return
		}
		if !m.reachable(m.db.Accounts.Contacts(u.ID)) {
			continue
		}
		delivered := m.db.Accounts.Delivered(u.ID)
		after := max(delivered, m.db.Accounts.Seen(u.ID))
		var latest, unread, newest int
		m.db.Read(func(s *item.Store) {
			latest = s.Seq()
			x := s.As(string(u.ID)).Named(u.Username)
			if unread = x.Unread(after, unreadCap); unread > 0 {
				if list, _ := x.Notifications(0, 1); len(list) > 0 {
					newest = list[0].Seq
				}
			}
		})
		if delivered == 0 {
			if latest > 0 {
				m.mark(u.ID, latest)
			}
			continue
		}
		if unread == 0 {
			continue
		}
		lang := m.db.Accounts.Language(u.ID)
		n := fmt.Sprint(unread)
		if unread >= unreadCap {
			n = fmt.Sprintf("%d+", unreadCap-1)
		}
		subject, body := "digest.subject", "digest.text"
		if unread == 1 {
			subject, body = "digest.subject.one", "digest.text.one"
		}
		m.Send(ctx, u.ID, notify.Message{
			Subject: text(lang, subject, n),
			Text:    text(lang, body, n),
			Link:    m.n.Link("#/notifications"),
		})
		m.mark(u.ID, newest)
	}
}

func (m *Messenger) mark(id auth.UserID, seq int) {
	if err := m.db.Accounts.SetDelivered(id, seq); err != nil {
		m.log.Error("digest position not saved", "user", string(id), "err", err)
	}
}

// tellAdmins tells every administrator of a new account request.
func (m *Messenger) tellAdmins(username string) {
	m.later(func(ctx context.Context) {
		for _, u := range m.db.Accounts.Users() {
			admin := false
			m.db.Read(func(s *item.Store) { admin = s.HasSystemRole(string(u.ID), item.RoleAdmin) })
			if !admin {
				continue
			}
			lang := m.db.Accounts.Language(u.ID)
			m.Send(ctx, u.ID, notify.Message{Subject: text(lang, "request.subject", username), Text: text(lang, "request.text", username), Link: m.n.Link("#/admin")})
		}
	})
}

// mail sends one message by mail only, in the background.
func (m *Messenger) mail(to string, msg notify.Message) {
	if to == "" || !m.n.Mail() {
		return
	}
	m.later(func(ctx context.Context) {
		if err := m.n.SendMail(ctx, to, msg); err != nil {
			m.log.Warn("mail not delivered", "subject", msg.Subject, "problem", err)
		}
	})
}

// texts are the messages in the languages of the UI; English when the
// user chose none or another.
var texts = map[string]map[string]string{
	"en": {
		"digest.subject":     "%s new changes on what you watch",
		"digest.subject.one": "%s new change on what you watch",
		"digest.text.one":    "Others made %s change on pages, tasks or spaces you watch since your last look. Open your notifications to see it.",
		"digest.text":        "Others made %s changes on pages, tasks or spaces you watch since your last look. Open your notifications to see them.",
		"request.subject":    "Account request from %s",
		"request.text":       "%s asked for an account. Approve or reject the request in the administration.",
		"approved.subject":   "Your account was approved",
		"approved.text":      "Your account %s was approved. Sign in with the username and the password you chose.",
		"rejected.subject":   "Your account request",
		"rejected.text":      "Your request for the account %s was not approved.",
		"reset.subject":      "Choose a new password",
		"reset.text":         "Someone asked to reset the password of the account %s. Open the link within 30 minutes to choose a new one. If it was not you, ignore this message; your password stays as it is.",
		"test.subject":       "Test message",
		"test.text":          "This message reached you on this channel.",
	},
	"de": {
		"digest.subject":     "%s neue Änderungen in Abonniertem",
		"digest.subject.one": "%s neue Änderung in Abonniertem",
		"digest.text.one":    "Seit Ihrem letzten Besuch gab es %s Änderung an Seiten, Aufgaben oder Bereichen, die Sie abonniert haben. Öffnen Sie Ihre Benachrichtigungen, um sie zu sehen.",
		"digest.text":        "Seit Ihrem letzten Besuch gab es %s Änderungen an Seiten, Aufgaben oder Bereichen, die Sie abonniert haben. Öffnen Sie Ihre Benachrichtigungen, um sie zu sehen.",
		"request.subject":    "Kontoantrag von %s",
		"request.text":       "%s hat ein Konto beantragt. Geben Sie den Antrag in der Administration frei oder lehnen Sie ihn ab.",
		"approved.subject":   "Ihr Konto wurde freigegeben",
		"approved.text":      "Ihr Konto %s wurde freigegeben. Melden Sie sich mit dem Benutzernamen und dem gewählten Passwort an.",
		"rejected.subject":   "Ihr Kontoantrag",
		"rejected.text":      "Ihr Antrag für das Konto %s wurde nicht freigegeben.",
		"reset.subject":      "Neues Passwort wählen",
		"reset.text":         "Für das Konto %s wurde das Zurücksetzen des Passworts angefordert. Öffnen Sie den Link innert 30 Minuten, um ein neues zu wählen. Falls Sie dies nicht waren, ignorieren Sie diese Nachricht; Ihr Passwort bleibt unverändert.",
		"test.subject":       "Testnachricht",
		"test.text":          "Diese Nachricht hat Sie über diesen Kanal erreicht.",
	},
}

// text is a message in a language ("de", "de-CH", …), formatted.
func text(lang, key string, args ...any) string {
	base, _, _ := strings.Cut(lang, "-")
	t, ok := texts[base][key]
	if !ok {
		t = texts["en"][key]
	}
	return fmt.Sprintf(t, args...)
}
