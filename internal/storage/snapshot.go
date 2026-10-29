package storage

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/dbuchs7/wikiwikibangbang/internal/item"

	"github.com/dbuchs7/wikiwikibangbang/internal/wal"
)

// BackupFiles are the files of a data directory that a backup contains
// (ADR-0016). It is an allowlist: a file added to the data directory later
// is left out until it is added here deliberately, and TestEveryFileIsBackedUpOrExcluded
// fails until that decision is made.
var BackupFiles = []string{wal.FileName, KeysFile, AccountsFile, AuditFile}

// OptionalBackupFiles are files a backup contains when they exist: a
// backup made before they were introduced restores without them.
var OptionalBackupFiles = []string{SiteFile, NotesFile}

// BackedUpDirs are the directories of a data directory whose files a
// backup contains, each file under its path relative to the data directory:
// the content of attachments (ADR-0045).
var BackedUpDirs = []string{AttachmentsDir}

// NotBackedUp are the files of a data directory a backup leaves out on
// purpose, with the reason.
var NotBackedUp = map[string]string{
	"LOCK": "the directory lock of the running process",
	BackupStateFile: "which automatic backup the next incremental one builds on (ADR-0073); " +
		"a restored instance starts with a full backup",
	BackupStateFile + ".tmp": "the state of automatic backups while it is replaced",
	CredentialsFile: "the credentials of other systems that administrators stored (ADR-0151); like the backup's own " +
		"passphrase (ADR-0073), they are not part of a backup, and are stored again after a restore",
}

// SnapshotFile is one file of a consistent snapshot: Size bytes of R.
type SnapshotFile struct {
	Name string
	Size int64
	R    io.ReadCloser
	// At reads the same bytes at any offset, for a log, so an incremental
	// backup can compare its start and take only what was appended
	// (ADR-0073); nil for an attachment's file, which never changes.
	At io.ReaderAt
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
		files = append(files, SnapshotFile{Name: name, Size: size, R: limitCloser{io.LimitReader(f, size), f}, At: f})
		return nil
	}
	db.mu.Lock()
	err = open(wal.FileName, db.log.Size())
	if err == nil {
		err = open(KeysFile, db.keys.log.Size())
	}
	live := db.Store.LiveAttachments()
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
		r := bytes.NewReader(data[:sc.Valid])
		files = append(files, SnapshotFile{Name: name, Size: sc.Valid, R: io.NopCloser(r), At: r})
	}
	if len(files) != len(BackupFiles) {
		return files, errors.New("storage: snapshot does not cover every backed-up file")
	}
	for _, name := range OptionalBackupFiles {
		data, err := os.ReadFile(filepath.Join(db.dir, name))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return files, err
		}
		sc, err := wal.ReadBytes(data)
		if err != nil {
			return files, fmt.Errorf("storage: snapshot of %s: %w", name, err)
		}
		r := bytes.NewReader(data[:sc.Valid])
		files = append(files, SnapshotFile{Name: name, Size: sc.Valid, R: io.NopCloser(r), At: r})
	}
	// The attachments the captured log describes. Their files never change,
	// so they are opened only when read, not all at once. One deleted or
	// purged since the capture is left out: its content was meant to go.
	for id, list := range live {
		for _, a := range list {
			path := attachmentPath(db.dir, id, a)
			fi, err := os.Stat(path)
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			if err != nil {
				return files, err
			}
			rel := filepath.ToSlash(filepath.Join(AttachmentsDir, string(id), string(a)))
			files = append(files, SnapshotFile{Name: rel, Size: fi.Size(), R: &lazyFile{path: path, size: fi.Size()}})
		}
	}
	return files, nil
}

// lazyFile opens its file on the first read.
type lazyFile struct {
	path string
	size int64
	f    *os.File
	r    io.Reader
}

func (l *lazyFile) Read(p []byte) (int, error) {
	if l.r == nil {
		f, err := os.Open(l.path)
		if err != nil {
			return 0, err
		}
		l.f, l.r = f, io.LimitReader(f, l.size)
	}
	return l.r.Read(p)
}

func (l *lazyFile) Close() error {
	if l.f == nil {
		return nil
	}
	return l.f.Close()
}

// BackedUpPath reports whether a backup may hold a file under this path
// (slash-separated, relative to the data directory): one of BackupFiles,
// or an attachment's file.
func BackedUpPath(name string) bool {
	if slices.Contains(BackupFiles, name) || slices.Contains(OptionalBackupFiles, name) {
		return true
	}
	parts := strings.Split(name, "/")
	return len(parts) == 3 && parts[0] == AttachmentsDir && item.ValidItemID(parts[1]) && item.ValidAttachmentID(parts[2])
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
	for _, name := range []string{AccountsFile, AuditFile, SiteFile, NotesFile} {
		if st, err := os.Stat(filepath.Join(db.dir, name)); err == nil {
			out[name] = st.Size()
		}
	}
	return out
}

// Dir is the data directory.
func (db *DB) Dir() string { return db.dir }

// BackupStateFile records the automatic backups (ADR-0073).
const BackupStateFile = "backups.json"

// BackupState returns the state of automatic backups, or nil before the
// first.
func (db *DB) BackupState() ([]byte, error) {
	data, err := os.ReadFile(filepath.Join(db.dir, BackupStateFile))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	return data, err
}

// SetBackupState replaces the state of automatic backups; a crash leaves
// the old state or the new one.
func (db *DB) SetBackupState(data []byte) error {
	path := filepath.Join(db.dir, BackupStateFile)
	f, err := os.OpenFile(path+".tmp", os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(path + ".tmp")
		return err
	}
	return os.Rename(path+".tmp", path)
}
