// Package audit keeps the security audit trail (ADR-0013, ADR-0029): who
// logged in or failed to, who created or revoked a credential, granted a
// role, purged or redacted an item. Entries are appended to their own
// journal and survive restarts; recent entries are kept in memory for
// listing.
package audit

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"time"
)

// Entry is one audited event.
type Entry struct {
	Seq   int       `json:"seq"`
	At    time.Time `json:"at"`
	Event string    `json:"event"`
	// Actor is the user ID that acted, empty for an anonymous caller.
	Actor string `json:"actor,omitempty"`
	// Addr is the client address, where the event came from a request.
	Addr string `json:"addr,omitempty"`
	// Subject is what the event is about: a user, token, item or principal.
	Subject string `json:"subject,omitempty"`
	// Detail holds further facts, e.g. a role or a username. It never holds
	// a secret.
	Detail map[string]string `json:"detail,omitempty"`
}

// The audited events.
const (
	Login           = "login"
	LoginFailed     = "login.failed"
	LoginThrottled  = "login.throttled"
	Logout          = "logout"
	PasswordChanged = "password.change"
	PasswordFailed  = "password.change.failed"
	TokenCreated    = "token.create"
	TokenRevoked    = "token.revoke"
	UserCreated     = "user.create"
	RoleGranted     = "role.grant"
	SystemRole      = "system-role.grant"
	GroupMember     = "group.add"
	ItemPurged      = "item.purge"
	ItemRedacted    = "item.redact"
)

// Retained bounds how many recent entries the trail keeps in memory for
// listing; the journal keeps all of them.
const Retained = 10_000

// Trail is safe for concurrent use.
type Trail struct {
	mu      sync.Mutex
	journal func([]byte) error
	now     func() time.Time
	log     *slog.Logger
	seq     int
	recent  []Entry
	failed  int
}

// New rebuilds a trail from its journal records. log, if not nil, also
// receives every entry as a structured log line.
func New(records [][]byte, journal func([]byte) error, log *slog.Logger, now func() time.Time) (*Trail, error) {
	if now == nil {
		now = time.Now
	}
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	t := &Trail{journal: journal, now: now, log: log}
	for i, r := range records {
		var e Entry
		if err := json.Unmarshal(r, &e); err != nil {
			return nil, fmt.Errorf("audit: record %d: %w", i+1, err)
		}
		if e.Seq != t.seq+1 {
			return nil, fmt.Errorf("audit: record %d has sequence number %d", i+1, e.Seq)
		}
		t.seq = e.Seq
		t.keep(e)
	}
	return t, nil
}

func (t *Trail) keep(e Entry) {
	t.recent = append(t.recent, e)
	if len(t.recent) > Retained {
		t.recent = slices.Clone(t.recent[len(t.recent)-Retained:])
	}
}

// Record appends an entry. The audited action has already happened, so a
// failure to record it does not undo it; it is logged and counted
// (Failures), never silently dropped.
func (t *Trail) Record(e Entry) {
	t.mu.Lock()
	defer t.mu.Unlock()
	e.Seq, e.At = t.seq+1, t.now().UTC()
	data, err := json.Marshal(e)
	if err == nil {
		err = t.journal(data)
	}
	attrs := []any{"event", e.Event, "actor", e.Actor, "subject", e.Subject, "addr", e.Addr}
	for k, v := range e.Detail {
		attrs = append(attrs, k, v)
	}
	if err != nil {
		t.failed++
		t.log.Error("audit entry not persisted", append(attrs, "err", err)...)
		return
	}
	t.seq = e.Seq
	t.keep(e)
	t.log.Info("audit", attrs...)
}

// Failures counts entries that could not be persisted since the start.
func (t *Trail) Failures() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.failed
}

// List returns up to limit entries, newest first, older than sequence
// number before (0: from the newest).
func (t *Trail) List(before, limit int) []Entry {
	t.mu.Lock()
	defer t.mu.Unlock()
	var out []Entry
	for i := len(t.recent) - 1; i >= 0 && len(out) < limit; i-- {
		if before == 0 || t.recent[i].Seq < before {
			out = append(out, t.recent[i])
		}
	}
	return out
}
