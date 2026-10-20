// Package storage gives the item store a home on disk (ADR-0008, ADR-0025):
// it opens the data directory, rebuilds the store by replaying the event log,
// and makes every later command durable before it takes effect. Item content
// is encrypted with a key per item, and purging or redacting an item
// destroys its key (crypto-shredding, ADR-0026).
package storage

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"sync"
	"time"

	"github.com/dbuchs7/wikiwikibangbang/internal/audit"
	"github.com/dbuchs7/wikiwikibangbang/internal/auth"
	"github.com/dbuchs7/wikiwikibangbang/internal/item"
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

// DB is an open data directory.
type DB struct {
	// Store is the item store. It is not safe for concurrent use: callers
	// that may run concurrently go through Read and Write.
	Store    *item.Store
	Accounts *auth.Accounts
	Audit    *audit.Trail
	Site     *site.Settings

	mu       sync.Mutex
	lock     io.Closer
	log      *wal.Log
	keys     *keyring
	accounts *wal.Log
	audit    *wal.Log
	site     *wal.Log
	dir      string
}

// Read runs fn with the item store while no command runs.
func (db *DB) Read(fn func(*item.Store)) {
	db.mu.Lock()
	defer db.mu.Unlock()
	fn(db.Store)
}

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
// (a crash between the purge and the key's destruction) and those of items
// the log does not know (a crash between storing a key and its batch).
func (db *DB) heal() (int, error) {
	var ids []item.ID
	for id := range db.keys.keys {
		it, err := db.Store.Get(id)
		if err != nil || it.Presence == item.Purged {
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
// the batch purges are destroyed after it is durable.
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
		case item.ItemPurged:
			gone = append(gone, e.Members...)
		case item.ItemRedacted:
			gone = append(gone, e.Item)
		case item.AttachmentDeleted:
			deleted[e.Item] = append(deleted[e.Item], e.Attachment.ID)
		}
	}
	if len(gone) > 0 || len(kr.unshred) > 0 {
		// The batch is durable and will take effect. A failure to destroy
		// the keys now is retried on the next purge and on the next start;
		// it must not fail a command that is already on disk.
		_ = kr.shred(gone...)
	}
	if len(gone) > 0 || len(deleted) > 0 {
		j.db.removeAttachments(gone, deleted) // likewise healed on the next start
	}
	return nil
}
