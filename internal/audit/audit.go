// Package audit keeps the security audit trail (ADR-0013, ADR-0029): who
// logged in or failed to, who created or revoked a credential, granted a
// role, purged or redacted an item. Entries are appended to their own
// journal and survive restarts; recent entries are kept in memory for
// listing.
package audit

import (
	"encoding/json"
	"errors"
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
	// Prev links the entry to the one before it: the hash of that entry's
	// record, or Genesis for the first (ADR-0123). Entries recorded before
	// the chain began have none.
	Prev string `json:"prev,omitempty"`
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
	RoleRevoked     = "role.revoke"
	SystemRoleTaken = "system-role.revoke"
	GroupLeft       = "group.remove"
	DesignChanged   = "design.change"
	// RegulationChanged records a change of a data protection regulation
	// (ADR-0058).
	RegulationChanged = "regulation.change"
	ItemPurged        = "item.purge"
	ItemRedacted      = "item.redact"
	BackupCreated     = "backup.create"
	// BackupFailed and BackupChanged are automatic backups that failed
	// and changes to their schedule (ADR-0073).
	BackupFailed  = "backup.failed"
	BackupChanged = "backup.change"
	// CatalogSynced and CatalogSyncChanged are runs of a connector that
	// fills a catalog and changes to its settings (ADR-0075, ADR-0077).
	CatalogSynced      = "catalog.sync"
	CatalogSyncChanged = "catalog.sync.change"
	// AlbumRetentionChanged and AlbumCleaned are changes to the album's
	// default rule for older versions and the clean-ups that removed
	// versions by it, automatic or started by an administrator (ADR-0104).
	AlbumRetentionChanged = "album.retention.change"
	AlbumCleaned          = "album.clean"
	// The account requests and contacts (ADR-0061).
	AccountRequested = "account.request"
	AccountApproved  = "account.approve"
	AccountRejected  = "account.reject"
	AccessChanged    = "access.change"
	ContactsChanged  = "contacts.change"
	// Password resets (ADR-0064).
	PasswordResetRequested = "password.reset.request"
	PasswordReset          = "password.reset"
	PasswordResetHanded    = "password.reset.link"
	// Deleting spaces (ADR-0069).
	SpaceTrashed  = "space.trash"
	SpaceRestored = "space.restore"
	SpacePurged   = "space.purge"
	// OAuth for AI assistants (ADR-0068).
	OAuthClientCreated = "oauth.client.create"
	OAuthClientRemoved = "oauth.client.remove"
	OAuthApproved      = "oauth.approve"
	OAuthRevoked       = "oauth.revoke"
	// ChainBroken is written at a start that finds the chain broken
	// (ADR-0123).
	ChainBroken = "audit.chain.broken"
	// PartialRestore is a restore of spaces from a backup into a running
	// installation's data (ADR-0124).
	PartialRestore = "restore.partial"
	OAuthFailed    = "oauth.token.failed"
	// EngineChanged records a process engine added, changed or removed
	// (ADR-0128).
	EngineChanged = "engine.change"
)

// Retained bounds how many recent entries the trail keeps in memory for
// listing; the journal keeps all of them, and ListAll reaches them there.
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
	// trimmed is whether older entries than the in-memory ones are in the
	// journal; read, if set, reads them (ListAll).
	trimmed bool
	read    func(each func([]byte) error) error
	// head is the link of the newest record, which the next entry carries.
	head string
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
	t := &Trail{journal: journal, now: now, log: log, head: Genesis}
	for i, r := range records {
		var e Entry
		if err := json.Unmarshal(r, &e); err != nil {
			return nil, fmt.Errorf("audit: record %d: %w", i+1, err)
		}
		// Expiry removes the oldest entries, so the first may be any number;
		// after it, numbers have no gaps.
		if (i == 0 && e.Seq < 1) || (i > 0 && e.Seq != t.seq+1) {
			return nil, fmt.Errorf("audit: record %d has sequence number %d", i+1, e.Seq)
		}
		t.seq, t.head = e.Seq, link(e, r)
		t.keep(e)
	}
	return t, nil
}

// Retention is how long an entry is kept (ADR-0029: ten years, the
// maintainer's decision of 2026-10-17).
const RetentionYears = 10

// Cutoff is the time before which entries have expired.
func Cutoff(now time.Time) time.Time { return now.UTC().AddDate(-RetentionYears, 0, 0) }

// Expired marks where expired entries were removed. It takes the sequence
// number and time of the last removed entry, so numbering stays gapless
// and the trail stays in time order; it expires in turn like any entry.
const Expired = "audit.expired"

// Unexpired returns the records to keep and how many were dropped. Entries
// are in time order, so the expired ones are a prefix; when there are any,
// the kept records start with an Expired entry in their place.
func Unexpired(records [][]byte, cutoff time.Time) ([][]byte, int, error) {
	var last Entry
	var lastRaw []byte
	for i, r := range records {
		var e Entry
		if err := json.Unmarshal(r, &e); err != nil {
			return nil, 0, fmt.Errorf("audit: record %d: %w", i+1, err)
		}
		if !e.At.Before(cutoff) {
			if i == 0 {
				return records, 0, nil
			}
			return marked(last, lastRaw, i, cutoff, records[i:])
		}
		last, lastRaw = e, r
	}
	if len(records) == 0 {
		return nil, 0, nil
	}
	return marked(last, lastRaw, len(records), cutoff, nil)
}

// marked makes the marker. It carries the link of the last removed entry,
// which the next entry names as its Prev, so the chain stays checkable
// from the marker on (ADR-0123).
func marked(last Entry, lastRaw []byte, n int, cutoff time.Time, rest [][]byte) ([][]byte, int, error) {
	m, err := json.Marshal(Entry{Seq: last.Seq, At: last.At, Event: Expired,
		Detail: map[string]string{"removed": fmt.Sprint(n), "before": cutoff.Format(time.RFC3339), "hash": link(last, lastRaw)}})
	if err != nil {
		return nil, 0, err
	}
	return append([][]byte{m}, rest...), n, nil
}

// Exclusive runs fn while no entry is recorded, e.g. to rewrite the
// journal without expired entries. When fn returns the records now in the
// journal, the in-memory listing is rebuilt from them, so it shows what a
// restart would.
func (t *Trail) Exclusive(fn func() (kept [][]byte, changed bool, err error)) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	kept, changed, err := fn()
	if err != nil || !changed {
		return err
	}
	t.recent, t.trimmed, t.head = nil, false, Genesis
	for i, r := range kept {
		var e Entry
		if err := json.Unmarshal(r, &e); err != nil {
			return fmt.Errorf("audit: record %d: %w", i+1, err)
		}
		t.head = link(e, r)
		t.keep(e)
	}
	return nil
}

func (t *Trail) keep(e Entry) {
	t.recent = append(t.recent, e)
	if len(t.recent) > Retained {
		t.recent = slices.Clone(t.recent[len(t.recent)-Retained:])
		t.trimmed = true
	}
}

// Record appends an entry. The audited action has already happened, so a
// failure to record it does not undo it; it is logged and counted
// (Failures), never silently dropped.
func (t *Trail) Record(e Entry) {
	t.mu.Lock()
	defer t.mu.Unlock()
	e.Seq, e.At, e.Prev = t.seq+1, t.now().UTC(), t.head
	data, err := json.Marshal(e)
	if err == nil {
		err = t.journal(data)
	}
	attrs := []any{"event", e.Event, "actor", e.Actor, "subject", e.Subject, "addr", e.Addr}
	// The log line carries the entry's own link: where log lines are kept
	// apart from the data directory, they witness the chain (ADR-0123).
	head := link(e, data)
	attrs = append(attrs, "seq", e.Seq, "hash", head)
	for k, v := range e.Detail {
		attrs = append(attrs, k, v)
	}
	if err != nil {
		t.failed++
		t.log.Error("audit entry not persisted", append(attrs, "err", err)...)
		return
	}
	t.seq, t.head = e.Seq, head
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
func (t *Trail) List(before, limit int) []Entry { return t.ListWhere(before, limit, nil) }

// ListWhere is List narrowed by keep, before paging, as a filter of the
// trail asks (ADR-0098); nil keeps every entry.
func (t *Trail) ListWhere(before, limit int, keep func(Entry) bool) []Entry {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.listRecent(before, limit, keep)
}

func (t *Trail) listRecent(before, limit int, keep func(Entry) bool) []Entry {
	var out []Entry
	for i := len(t.recent) - 1; i >= 0 && len(out) < limit; i-- {
		if (before == 0 || t.recent[i].Seq < before) && (keep == nil || keep(t.recent[i])) {
			out = append(out, t.recent[i])
		}
	}
	return out
}

// ReadJournal lets ListAll read the whole journal: read passes its records
// to each in order and stops when each returns an error.
func (t *Trail) ReadJournal(read func(each func([]byte) error) error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.read = read
}

// errOlder stops reading the journal at the first in-memory entry.
var errOlder = errors.New("audit: reached the in-memory entries")

// ListAll is ListWhere reaching past the in-memory window: when the window
// holds fewer than limit entries that keep accepts and older entries exist,
// the rest come from the journal (ADR-0122), so an entry stays listable for
// as long as it is kept, however many came after it. Reading the journal
// takes time in proportion to its size; it runs without holding the lock,
// so entries are still recorded meanwhile.
func (t *Trail) ListAll(before, limit int, keep func(Entry) bool) ([]Entry, error) {
	t.mu.Lock()
	out := t.listRecent(before, limit, keep)
	read, trimmed, bound := t.read, t.trimmed, 0
	if len(t.recent) > 0 {
		bound = t.recent[0].Seq
	}
	t.mu.Unlock()
	if len(out) >= limit || read == nil || !trimmed {
		return out, nil
	}
	if before != 0 && before < bound {
		bound = before
	}
	// The journal is in sequence order: keep the newest matches in a ring.
	want := limit - len(out)
	ring, n := make([]Entry, want), 0
	err := read(func(p []byte) error {
		var e Entry
		if err := json.Unmarshal(p, &e); err != nil {
			return err
		}
		if e.Seq >= bound {
			return errOlder
		}
		if keep == nil || keep(e) {
			ring[n%want] = e
			n++
		}
		return nil
	})
	if err != nil && !errors.Is(err, errOlder) {
		return nil, fmt.Errorf("audit: reading the journal: %w", err)
	}
	for i := n - 1; i >= 0 && i >= n-want; i-- {
		out = append(out, ring[i%want])
	}
	return out, nil
}
