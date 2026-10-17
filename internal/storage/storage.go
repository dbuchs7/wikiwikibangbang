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
	"path/filepath"

	"github.com/dbuchs7/wikiwikibangbang/internal/item"
	"github.com/dbuchs7/wikiwikibangbang/internal/wal"
)

// DB is an open data directory.
type DB struct {
	Store *item.Store
	lock  io.Closer
	log   *wal.Log
	keys  *keyring
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
}

// Open opens or creates the data directory and returns the recovered store.
// Only one process can have a data directory open (wal.ErrLocked).
func Open(dir string, opts ...item.Option) (_ *DB, _ Recovery, err error) {
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
	rec := recovery(sc, log)
	if rec.Shredded, err = db.heal(); err != nil {
		return nil, Recovery{}, fmt.Errorf("storage: destroying leftover keys: %w", err)
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
	for _, e := range batch {
		switch e.Kind {
		case item.ItemPurged:
			gone = append(gone, e.Members...)
		case item.ItemRedacted:
			gone = append(gone, e.Item)
		}
	}
	if len(gone) > 0 || len(kr.unshred) > 0 {
		// The batch is durable and will take effect. A failure to destroy
		// the keys now is retried on the next purge and on the next start;
		// it must not fail a command that is already on disk.
		_ = kr.shred(gone...)
	}
	return nil
}
