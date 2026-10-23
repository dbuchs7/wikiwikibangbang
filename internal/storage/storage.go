// Package storage gives the item store a home on disk (ADR-0008, ADR-0025):
// it opens the data directory, rebuilds the store by replaying the event log,
// and makes every later command durable before it takes effect. Item content
// is encrypted with a key per item, and purging or redacting an item
// destroys its key (crypto-shredding, ADR-0026).
package storage

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/dbuchs7/wikiwikibangbang/internal/audit"
	"github.com/dbuchs7/wikiwikibangbang/internal/auth"
	"github.com/dbuchs7/wikiwikibangbang/internal/item"
	"github.com/dbuchs7/wikiwikibangbang/internal/notes"
	"github.com/dbuchs7/wikiwikibangbang/internal/site"
	"github.com/dbuchs7/wikiwikibangbang/internal/wal"
)

// AccountsFile holds the accounts and API tokens (ADR-0027).
const AccountsFile = "accounts.wal"

// AuditFile holds the security audit trail (ADR-0029).
const AuditFile = "audit.wal"

// SiteFile holds the installation's settings, its corporate design
// (ADR-0052).
const SiteFile = "site.wal"

// NotesFile holds the personal notes on passages of items (ADR-0107),
// sealed with each item's key.
const NotesFile = "notes.wal"

// DB is an open data directory.
type DB struct {
	// Store is the item store. It is not safe for concurrent use: callers
	// that may run concurrently go through Read and Write.
	Store    *item.Store
	Accounts *auth.Accounts
	Audit    *audit.Trail
	Site     *site.Settings
	// Notes are personal notes (ADR-0107). They are sealed with the item
	// keys, so they are changed only within Write.
	Notes *notes.Notes

	// mu lets reads run side by side and a command alone (ADR-0081).
	mu       sync.RWMutex
	lock     io.Closer
	log      *wal.Log
	keys     *keyring
	accounts *wal.Log
	audit    *wal.Log
	site     *wal.Log
	notes    *wal.Log
	dir      string
}

// Read runs fn with the item store while no command runs; reads run side
// by side (ADR-0081), so fn must not change the store. What a read
// remembers for later reads, the store guards itself.
func (db *DB) Read(fn func(*item.Store)) {
	db.mu.RLock()
	defer db.mu.RUnlock()
	fn(db.Store)
}

// WarmUp scans the items for sensitive data in parts of a few hundred,
// each a read of its own, so that commands are not held up, until done or
// ctx ends (ADR-0081). The server runs it after opening, so the first
// person to open a page does not wait for every item to be scanned.
func (db *DB) WarmUp(ctx context.Context) {
	var ids []item.ID
	db.Read(func(s *item.Store) { ids = s.LiveItems() })
	for start := 0; start < len(ids) && ctx.Err() == nil; start += warmPart {
		part := ids[start:min(start+warmPart, len(ids))]
		db.Read(func(s *item.Store) { s.ScanSensitive(part) })
	}
}

// warmPart is how many items one read of WarmUp scans.
const warmPart = 200

// Write runs fn with the item store as the only writer (ADR-0008): commands
// are applied one at a time, in the order of the log.
func (db *DB) Write(fn func(*item.Store) error) error {
	db.mu.Lock()
	defer db.mu.Unlock()
	return fn(db.Store)
}

// Recovery reports what opening the data directory found.
type Recovery struct {
	Batches int   // records in the log
	Events  int   // events replayed
	Erased  int   // events whose content was destroyed (ADR-0026)
	Torn    int64 // bytes of an interrupted append that were dropped
	// Shredded counts keys destroyed while opening: purges a crash
	// interrupted before their keys were destroyed, and keys of items that
	// never reached the log.
	Shredded int
	// Orphans counts attachment files removed while opening: uploads never
	// recorded, and files of attachments deleted or purged (ADR-0045).
	Orphans int
}

// Options configure Open.
type Options struct {
	Item []item.Option
	Auth []auth.Option
	// Log receives every audit entry as a log line, too.
	Log *slog.Logger
}

// Open opens or creates the data directory and returns the recovered store.
// Only one process can have a data directory open (wal.ErrLocked).
func Open(dir string, opts ...item.Option) (*DB, Recovery, error) {
	return OpenWith(dir, Options{Item: opts})
}

// OpenWith is Open with options for every part of the data directory.
func OpenWith(dir string, o Options) (_ *DB, _ Recovery, err error) {
	opts := o.Item
	db := &DB{}
	defer func() {
		if err != nil {
			db.close()
		}
	}()
	if db.lock, err = wal.Lock(dir); err != nil {
		return nil, Recovery{}, err
	}
	if db.keys, err = openKeyring(dir); err != nil {
		return nil, Recovery{}, err
	}
	l, sc, err := wal.OpenFile(dir, wal.FileName)
	if err != nil {
		return nil, Recovery{}, err
	}
	db.log = l
	log, err := decode(sc, db.keys)
	if err != nil {
		return nil, Recovery{}, err
	}
	opts = append(opts, item.WithJournal(journal{db}))
	if db.Store, err = item.Replay(log, opts...); err != nil {
		return nil, Recovery{}, fmt.Errorf("storage: replaying %s: %w", dir, err)
	}
	al, asc, err := wal.OpenFile(dir, AccountsFile)
	if err != nil {
		return nil, Recovery{}, err
	}
	db.accounts = al
	if db.Accounts, err = auth.New(asc.Records, al.Append, o.Auth...); err != nil {
		return nil, Recovery{}, err
	}
	db.dir = dir
	tl, tsc, err := wal.OpenFile(dir, AuditFile)
	if err != nil {
		return nil, Recovery{}, err
	}
	db.audit = tl
	// Expired entries leave the journal before the trail is built (ADR-0029).
	kept, dropped, err := audit.Unexpired(tsc.Records, audit.Cutoff(time.Now()))
	if err != nil {
		return nil, Recovery{}, err
	}
	if dropped > 0 {
		if err := db.rewriteAudit(kept); err != nil {
			return nil, Recovery{}, err
		}
	}
	// The journal is reached through db.audit, which ExpireAudit replaces.
	if db.Audit, err = audit.New(kept, func(b []byte) error { return db.audit.Append(b) }, o.Log, nil); err != nil {
		return nil, Recovery{}, err
	}
	sl, ssc, err := wal.OpenFile(dir, SiteFile)
	if err != nil {
		return nil, Recovery{}, err
	}
	db.site = sl
	if db.Site, err = site.New(ssc.Records, sl.Append); err != nil {
		return nil, Recovery{}, err
	}
	nl, nsc, err := wal.OpenFile(dir, NotesFile)
	if err != nil {
		return nil, Recovery{}, err
	}
	db.notes = nl
	if db.Notes, err = notes.New(nsc.Records, nl.Append, notesSealer{db.keys}, newNoteID); err != nil {
		return nil, Recovery{}, fmt.Errorf("storage: reading %s: %w", NotesFile, err)
	}
	// Content is assessed against the installation's data protection
	// regulations on save (ADR-0058); replay reads the outcome from the log.
	settings := db.Site
	db.Store.SetAssessor(func(texts []string) []item.DPFinding {
		var out []item.DPFinding
		for _, f := range settings.Assess(texts) {
			out = append(out, item.DPFinding{Regulation: f.Regulation, Category: f.Category, Indicator: f.Indicator})
		}
		return out
	})
	rec := recovery(sc, log)
	if rec.Shredded, err = db.heal(); err != nil {
		return nil, Recovery{}, fmt.Errorf("storage: destroying leftover keys: %w", err)
	}
	if rec.Orphans, err = db.healAttachments(); err != nil {
		return nil, Recovery{}, fmt.Errorf("storage: removing unneeded attachment files: %w", err)
	}
	return db, rec, nil
}

// heal destroys the keys that should no longer exist: those of purged items
// and erased passages (a crash between the purge or erasure and the key's
// destruction, ADR-0089) and those of items the log does not know (a crash
// between storing a key and its batch).
func (db *DB) heal() (int, error) {
	var ids []item.ID
	for id := range db.keys.keys {
		// A protected passage's key belongs to its item (ADR-0088).
		// A protected passage's key, {item}~{passage}, belongs to its item.
		owner, passage, ok := strings.Cut(string(id), "~")
		it, err := db.Store.Get(item.ID(owner))
		if err != nil || it.Presence == item.Purged || ok && slices.Contains(it.ErasedSecrets, item.ID(passage)) {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 && len(db.keys.unshred) == 0 {
		return 0, nil
	}
	return len(ids), db.keys.shred(ids...)
}

// rewriteAudit replaces the audit journal with records and reopens it.
func (db *DB) rewriteAudit(records [][]byte) error {
	if err := db.audit.Close(); err != nil {
		return err
	}
	db.audit = nil
	rerr := wal.Rewrite(db.dir, AuditFile, records)
	l, _, err := wal.OpenFile(db.dir, AuditFile)
	if err != nil {
		return errors.Join(rerr, err)
	}
	db.audit = l
	return rerr
}

// ExpireAudit removes audit entries older than the retention period from the
// journal and the in-memory listing, and reports how many went. The server
// calls it once a day; Open does it at every start.
func (db *DB) ExpireAudit(now time.Time) (int, error) {
	cutoff := audit.Cutoff(now)
	dropped := 0
	err := db.Audit.Exclusive(func() ([][]byte, bool, error) {
		sc, err := wal.Read(filepath.Join(db.dir, AuditFile))
		if err != nil {
			return nil, false, err
		}
		kept, n, err := audit.Unexpired(sc.Records, cutoff)
		if err != nil || n == 0 {
			return nil, false, err
		}
		dropped = n
		return kept, true, db.rewriteAudit(kept)
	})
	return dropped, err
}

// Close closes the data directory.
func (db *DB) Close() error { return db.close() }

func (db *DB) close() error {
	var errs []error
	if db.log != nil {
		errs = append(errs, db.log.Close())
	}
	if db.keys != nil {
		errs = append(errs, db.keys.close())
	}
	if db.accounts != nil {
		errs = append(errs, db.accounts.Close())
	}
	if db.site != nil {
		errs = append(errs, db.site.Close())
	}
	if db.notes != nil {
		errs = append(errs, db.notes.Close())
	}
	if db.audit != nil {
		errs = append(errs, db.audit.Close())
	}
	if db.lock != nil {
		errs = append(errs, db.lock.Close())
	}
	return errors.Join(errs...)
}

// Inspect reads a data directory without changing or locking it and checks
// that its log decodes and replays. It is safe to run next to a server: an
// append in progress shows up as a torn tail.
func Inspect(dir string) (Recovery, error) {
	ksc, err := wal.Read(filepath.Join(dir, KeysFile))
	if err != nil {
		return Recovery{}, err
	}
	kr := &keyring{keys: map[item.ID][]byte{}, shredded: map[item.ID]bool{}}
	if err := kr.load(ksc.Records); err != nil {
		return Recovery{}, err
	}
	sc, err := wal.Read(filepath.Join(dir, wal.FileName))
	if err != nil {
		return Recovery{}, err
	}
	log, err := decode(sc, kr)
	if err != nil {
		return Recovery{}, err
	}
	if _, err := item.Replay(log); err != nil {
		return Recovery{}, fmt.Errorf("storage: replaying %s: %w", dir, err)
	}
	return recovery(sc, log), nil
}

func recovery(sc wal.Scan, log []item.Event) Recovery {
	rec := Recovery{Batches: len(sc.Records), Events: len(log), Torn: sc.Torn}
	for _, e := range log {
		if e.Erased {
			rec.Erased++
		}
	}
	return rec
}

func decode(sc wal.Scan, kr *keyring) ([]item.Event, error) {
	var log []item.Event
	for i, rec := range sc.Records {
		batch, err := item.DecodeBatchSealed(rec, kr)
		if err != nil {
			return nil, fmt.Errorf("storage: record %d: %w", i+1, err)
		}
		log = append(log, batch...)
	}
	return log, nil
}

// journal writes each command's batch as one record, so a batch is durable
// as a whole or not at all. New keys are made durable first; keys of items
// the batch purges, and of passages it erases, are destroyed after it is
// durable.
type journal struct{ db *DB }

func (j journal) Append(batch []item.Event) error {
	kr := j.db.keys
	data, err := item.EncodeBatchSealed(batch, kr)
	if err != nil {
		kr.pending = map[item.ID][]byte{}
		return err
	}
	if err := kr.persistPending(); err != nil {
		return err
	}
	if err := j.db.log.Append(data); err != nil {
		return err
	}
	var gone []item.ID
	deleted := map[item.ID][]item.ID{}
	for _, e := range batch {
		switch e.Kind {
		case item.ItemPurged, item.SpacePurged:
			gone = append(gone, e.Members...)
		case item.ItemRedacted:
			gone = append(gone, e.Item)
		case item.SecretErased:
			gone = append(gone, item.SecretKey(e.Item, e.Secret.ID))
		case item.AttachmentDeleted:
			deleted[e.Item] = append(deleted[e.Item], e.Attachment.ID)
		}
	}
	// An item's protected passages go with it (ADR-0088).
	gone = append(gone, kr.passagesOf(gone)...)
	if len(gone) > 0 || len(kr.unshred) > 0 {
		// The batch is durable and will take effect. A failure to destroy
		// the keys now is retried on the next purge and on the next start;
		// it must not fail a command that is already on disk.
		_ = kr.shred(gone...)
	}
	if len(gone) > 0 || len(deleted) > 0 {
		j.db.removeAttachments(gone, deleted) // likewise healed on the next start
	}
	// Notes go with their item's key (ADR-0107); their records can no
	// longer be read, and are dropped on the next start too.
	if j.db.Notes != nil {
		j.db.Notes.Forget(gone...)
	}
	return nil
}

// notesSealer seals notes with their item's key (ADR-0107). A key it
// creates is made durable at once; notes are changed within Write, under
// the lock that guards the keys.
type notesSealer struct{ kr *keyring }

func (n notesSealer) Seal(id item.ID, pt, aad []byte) ([]byte, error) {
	ct, err := n.kr.Seal(id, pt, aad)
	if err != nil {
		return nil, err
	}
	return ct, n.kr.persistPending()
}

func (n notesSealer) Open(id item.ID, ct, aad []byte) ([]byte, error) { return n.kr.Open(id, ct, aad) }

// newNoteID is a random ID for a note.
func newNoteID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err) // crypto/rand does not fail on supported systems
	}
	return hex.EncodeToString(b)
}
