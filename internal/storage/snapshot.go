package storage

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/dbuchs7/wikiwikibangbang/internal/wal"
)

// BackupFiles are the files of a data directory that a backup contains
// (ADR-0016). It is an allowlist: a file added to the data directory later
// is left out until it is added here deliberately, and TestEveryFileIsBackedUpOrExcluded
// fails until that decision is made.
var BackupFiles = []string{wal.FileName, KeysFile, AccountsFile, AuditFile}

// NotBackedUp are the files of a data directory a backup leaves out on
// purpose, with the reason.
var NotBackedUp = map[string]string{
	"LOCK": "the directory lock of the running process",
}

// SnapshotFile is one file of a consistent snapshot: Size bytes of R.
type SnapshotFile struct {
	Name string
	Size int64
	R    io.ReadCloser
}

// Snapshot captures the data directory at one point in time, without
// stopping the writer: the logs are append-only, so the bytes up to each
// log's durable end at that moment are a complete copy (ADR-0016).
//
// The event log and the keys are captured together under the writer's
// lock, because events need their keys. The files are opened before the
// lock is released, so a later rewrite of a file (a destroyed key, expired
// audit entries) replaces the name, not the content being read. The
// accounts and audit logs are written independently of items; each is
// read to the end of its last complete record. The caller closes every R.
func (db *DB) Snapshot() (files []SnapshotFile, err error) {
	defer func() {
		if err != nil {
			for _, f := range files {
				f.R.Close()
			}
			files = nil
		}
	}()
	open := func(name string, size int64) error {
		f, err := os.Open(filepath.Join(db.dir, name))
		if err != nil {
			return err
		}
		files = append(files, SnapshotFile{Name: name, Size: size, R: limitCloser{io.LimitReader(f, size), f}})
		return nil
	}
	db.mu.Lock()
	err = open(wal.FileName, db.log.Size())
	if err == nil {
		err = open(KeysFile, db.keys.log.Size())
	}
	db.mu.Unlock()
	if err != nil {
		return files, err
	}
	for _, name := range []string{AccountsFile, AuditFile} {
		data, err := os.ReadFile(filepath.Join(db.dir, name))
		if err != nil {
			return files, err
		}
		sc, err := wal.ReadBytes(data)
		if err != nil {
			return files, fmt.Errorf("storage: snapshot of %s: %w", name, err)
		}
		files = append(files, SnapshotFile{Name: name, Size: sc.Valid, R: io.NopCloser(bytes.NewReader(data[:sc.Valid]))})
	}
	if len(files) != len(BackupFiles) {
		return files, errors.New("storage: snapshot does not cover every backed-up file")
	}
	return files, nil
}

type limitCloser struct {
	io.Reader
	f *os.File
}

func (l limitCloser) Close() error { return l.f.Close() }

// Sizes reports the bytes of each backed-up file, for operational metrics
// (ADR-0016).
func (db *DB) Sizes() map[string]int64 {
	out := map[string]int64{}
	db.mu.Lock()
	out[wal.FileName], out[KeysFile] = db.log.Size(), db.keys.log.Size()
	db.mu.Unlock()
	for _, name := range []string{AccountsFile, AuditFile} {
		if st, err := os.Stat(filepath.Join(db.dir, name)); err == nil {
			out[name] = st.Size()
		}
	}
	return out
}
