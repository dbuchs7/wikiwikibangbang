// Package storage gives the item store a home on disk (ADR-0008, ADR-0025):
// it opens the data directory, rebuilds the store by replaying the event log,
// and makes every later command durable before it takes effect.
package storage

import (
	"fmt"
	"path/filepath"

	"github.com/dbuchs7/wikiwikibangbang/internal/item"
	"github.com/dbuchs7/wikiwikibangbang/internal/wal"
)

// DB is an open data directory.
type DB struct {
	Store *item.Store
	log   *wal.Log
}

// Recovery reports what opening the data directory found.
type Recovery struct {
	Batches int   // records in the log
	Events  int   // events replayed
	Torn    int64 // bytes of an interrupted append that were dropped
}

// Open opens or creates the data directory and returns the recovered store.
// Only one process can have a data directory open (wal.ErrLocked).
func Open(dir string, opts ...item.Option) (*DB, Recovery, error) {
	l, sc, err := wal.Open(dir)
	if err != nil {
		return nil, Recovery{}, err
	}
	log, err := decode(sc)
	if err != nil {
		l.Close()
		return nil, Recovery{}, err
	}
	opts = append(opts, item.WithJournal(journal{l}))
	s, err := item.Replay(log, opts...)
	if err != nil {
		l.Close()
		return nil, Recovery{}, fmt.Errorf("storage: replaying %s: %w", dir, err)
	}
	return &DB{Store: s, log: l}, Recovery{Batches: len(sc.Records), Events: len(log), Torn: sc.Torn}, nil
}

// Close closes the log and releases the data directory.
func (db *DB) Close() error { return db.log.Close() }

// Inspect reads a data directory without changing or locking it and checks
// that its log decodes and replays. It is safe to run next to a server: an
// append in progress shows up as a torn tail.
func Inspect(dir string) (Recovery, error) {
	sc, err := wal.Read(filepath.Join(dir, wal.FileName))
	if err != nil {
		return Recovery{}, err
	}
	log, err := decode(sc)
	if err != nil {
		return Recovery{}, err
	}
	if _, err := item.Replay(log); err != nil {
		return Recovery{}, fmt.Errorf("storage: replaying %s: %w", dir, err)
	}
	return Recovery{Batches: len(sc.Records), Events: len(log), Torn: sc.Torn}, nil
}

func decode(sc wal.Scan) ([]item.Event, error) {
	var log []item.Event
	for i, rec := range sc.Records {
		batch, err := item.DecodeBatch(rec)
		if err != nil {
			return nil, fmt.Errorf("storage: record %d: %w", i+1, err)
		}
		log = append(log, batch...)
	}
	return log, nil
}

// journal writes each command's batch as one record, so a batch is durable
// as a whole or not at all.
type journal struct{ l *wal.Log }

func (j journal) Append(batch []item.Event) error {
	data, err := item.EncodeBatch(batch)
	if err != nil {
		return err
	}
	return j.l.Append(data)
}
