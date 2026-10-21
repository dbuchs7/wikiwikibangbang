// Package backup writes and restores encrypted backups of a data directory
// (ADR-0016, ADR-0034, ADR-0073).
//
// A backup is a tar archive of the files storage.BackupFiles names and of
// the attachments' files (ADR-0045), taken from a consistent snapshot, followed by a manifest with their sizes and
// SHA-256 sums; the archive is encrypted with a key derived from a
// passphrase. It holds the item keys and the account hashes, because a
// backup without them could not be restored to a working instance; the
// passphrase is what protects them.
//
// A backup is full or incremental (ADR-0073). An incremental backup builds
// on the manifest of the backup before it: of a log that only grew since,
// it holds the appended bytes; an attachment's file, which never changes,
// it leaves out when the base has it; anything else it holds whole. Its
// manifest still lists every file as it is after the backup is applied, so
// a chain restores to exactly that state.
package backup

import (
	"archive/tar"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/dbuchs7/wikiwikibangbang/internal/storage"
)

// Format is the version of the archive layout. Format 1 backups, which
// were all full, still restore.
const Format = 2

const manifestName = "manifest.json"

// maxManifest bounds the manifest a restore reads; it lists every file,
// attachments included.
const maxManifest = 64 << 20

// fromRecord is the PAX record of an entry that holds the bytes appended
// to a file from this offset on, instead of the whole file.
const fromRecord = "WWBB.from"

// The kinds of a backup.
const (
	Full        = "full"
	Incremental = "incremental"
)

// Manifest describes a backup.
type Manifest struct {
	Format  int       `json:"format"`
	ID      string    `json:"id,omitempty"`
	Kind    string    `json:"kind,omitempty"`
	Chain   string    `json:"chain,omitempty"` // the ID of the full backup the chain starts with
	Base    string    `json:"base,omitempty"`  // the ID of the backup an incremental one builds on
	Seq     int       `json:"seq,omitempty"`   // the position in the chain; 0 for the full backup
	Created time.Time `json:"created"`
	Build   string    `json:"build"`
	// Files are every file of the data directory as it is once the backup
	// is applied, whether the archive holds it, part of it or none of it.
	Files []File `json:"files"`
}

// IsFull tells whether a backup restores on its own.
func (m Manifest) IsFull() bool { return m.Kind != Incremental }

// File is one file of a backup.
type File struct {
	Name   string `json:"name"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// Options set what WriteBackup writes.
type Options struct {
	Passphrase string
	Build      string // names the program version for the manifest
	Now        time.Time
	// ID names the backup; NewID makes one when it is empty.
	ID string
	// Base is the manifest of the backup an incremental one builds on; nil
	// writes a full backup.
	Base *Manifest
}

// Write writes an encrypted full backup of a snapshot to w and closes the
// snapshot's files. build names the program version for the manifest.
func Write(w io.Writer, passphrase string, files []storage.SnapshotFile, build string, now time.Time) error {
	_, err := WriteBackup(w, files, Options{Passphrase: passphrase, Build: build, Now: now})
	return err
}

// WriteBackup writes an encrypted backup of a snapshot to w, full or
// incremental as o says, closes the snapshot's files and returns the
// backup's manifest.
func WriteBackup(w io.Writer, files []storage.SnapshotFile, o Options) (m Manifest, err error) {
	defer func() {
		for _, f := range files {
			if cerr := f.R.Close(); err == nil {
				err = cerr
			}
		}
	}()
	id := o.ID
	if id == "" {
		if id, err = NewID(); err != nil {
			return m, err
		}
	}
	m = Manifest{Format: Format, ID: id, Kind: Full, Chain: id, Created: o.Now.UTC(), Build: o.Build}
	base := map[string]File{}
	if o.Base != nil {
		if o.Base.ID == "" || o.Base.Chain == "" {
			return m, errors.New("backup: an incremental backup needs a base of format 2")
		}
		m.Kind, m.Chain, m.Base, m.Seq = Incremental, o.Base.Chain, o.Base.ID, o.Base.Seq+1
		for _, f := range o.Base.Files {
			base[f.Name] = f
		}
	}
	enc, err := encrypt(w, o.Passphrase, Iterations)
	if err != nil {
		return m, err
	}
	tw := tar.NewWriter(enc)
	for _, f := range files {
		file, err := writeFile(tw, f, base, o.Now)
		if err != nil {
			return m, err
		}
		m.Files = append(m.Files, file)
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return m, err
	}
	if err := tw.WriteHeader(&tar.Header{Name: manifestName, Size: int64(len(data)), Mode: 0o640, ModTime: o.Now, Typeflag: tar.TypeReg}); err != nil {
		return m, err
	}
	if _, err := tw.Write(data); err != nil {
		return m, err
	}
	if err := tw.Close(); err != nil {
		return m, err
	}
	return m, enc.Close()
}

// writeFile writes what the backup needs of one file: nothing of an
// attachment the base has or of a log that did not change, the appended
// bytes of a log whose start is the base's file, the whole file otherwise.
func writeFile(tw *tar.Writer, f storage.SnapshotFile, base map[string]File, now time.Time) (File, error) {
	b, inBase := base[f.Name]
	if inBase && f.At == nil && b.Size == f.Size {
		return b, nil
	}
	h := sha256.New()
	var from int64
	if inBase && f.At != nil && b.Size > 0 && b.Size <= f.Size {
		if _, err := io.Copy(h, io.NewSectionReader(f.At, 0, b.Size)); err != nil {
			return File{}, err
		}
		if hex.EncodeToString(h.Sum(nil)) == b.SHA256 {
			if b.Size == f.Size {
				return b, nil
			}
			from = b.Size
		} else {
			h.Reset()
		}
	}
	hdr := &tar.Header{Name: f.Name, Size: f.Size - from, Mode: 0o640, ModTime: now, Typeflag: tar.TypeReg}
	var r io.Reader = f.R
	if from > 0 {
		hdr.Format, hdr.ModTime = tar.FormatPAX, now.Truncate(time.Second)
		hdr.PAXRecords = map[string]string{fromRecord: strconv.FormatInt(from, 10)}
		r = io.NewSectionReader(f.At, from, f.Size-from)
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return File{}, err
	}
	n, err := io.Copy(io.MultiWriter(tw, h), r)
	if err != nil {
		return File{}, err
	}
	if n != f.Size-from {
		return File{}, fmt.Errorf("backup: %s: read %d of %d bytes", f.Name, from+n, f.Size)
	}
	return File{Name: f.Name, Size: f.Size, SHA256: hex.EncodeToString(h.Sum(nil))}, nil
}

// NewID returns a new random ID for a backup.
func NewID() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// Restore decrypts a full backup into dir, which must not exist or be
// empty. It takes only the files storage.BackedUpPath allows, checks each
// against the manifest, and checks that the result opens as a data
// directory before it moves it into place; on any error dir is left as it
// was.
func Restore(r io.Reader, passphrase, dir string) (Manifest, error) {
	return RestoreChain([]Source{func() (io.ReadCloser, error) { return io.NopCloser(r), nil }}, passphrase, dir)
}

// Source opens one backup of a chain when it is its turn.
type Source func() (io.ReadCloser, error)

// RestoreChain restores a full backup and the incremental backups that
// follow it, in order, into dir, as Restore does with one. Each backup
// must build on the one before it; after each, the files must be exactly
// those its manifest lists.
func RestoreChain(chain []Source, passphrase, dir string) (Manifest, error) {
	var m Manifest
	if len(chain) == 0 {
		return m, errors.New("backup: nothing to restore")
	}
	if entries, err := os.ReadDir(dir); err == nil && len(entries) > 0 {
		return m, fmt.Errorf("backup: %s is not empty; restore into a new directory", dir)
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return m, err
	}
	stage := filepath.Clean(dir) + ".restoring"
	if err := os.RemoveAll(stage); err != nil {
		return m, err
	}
	if err := os.MkdirAll(stage, 0o750); err != nil {
		return m, err
	}
	ok := false
	defer func() {
		if !ok {
			os.RemoveAll(stage)
		}
	}()
	have := map[string]File{}
	var prev *Manifest
	for i, open := range chain {
		next, err := applyOne(open, passphrase, stage, have)
		if err == nil {
			err = settle(stage, next, have)
		}
		if err != nil {
			if len(chain) > 1 {
				err = fmt.Errorf("backup %d of %d: %w", i+1, len(chain), err)
			}
			return m, err
		}
		switch {
		case prev == nil && !next.IsFull():
			return m, errors.New("backup: a chain starts with a full backup")
		case prev != nil && next.IsFull():
			return m, fmt.Errorf("backup %d of %d is a full backup, not one that builds on the one before", i+1, len(chain))
		case prev != nil && (next.Base != prev.ID || next.Chain != prev.Chain):
			return m, fmt.Errorf("backup %d of %d does not build on the one before it", i+1, len(chain))
		}
		m, prev = next, &next
	}
	for _, name := range storage.BackupFiles {
		if _, ok := have[name]; !ok {
			return m, fmt.Errorf("backup: %s is missing", name)
		}
	}
	if _, err := storage.Inspect(stage); err != nil {
		return m, fmt.Errorf("backup: the restored data does not open: %w", err)
	}
	if err := os.Remove(dir); err != nil && !errors.Is(err, os.ErrNotExist) {
		return m, err
	}
	if err := os.Rename(stage, dir); err != nil {
		return m, err
	}
	ok = true
	return m, nil
}

// applyOne writes one backup's files into stage and records in have what
// each file now holds.
func applyOne(open Source, passphrase, stage string, have map[string]File) (Manifest, error) {
	var m Manifest
	rc, err := open()
	if err != nil {
		return m, err
	}
	defer rc.Close()
	plain, err := decrypt(rc, passphrase)
	if err != nil {
		return m, err
	}
	written := map[string]bool{}
	tr := tar.NewReader(plain)
	haveManifest := false
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return m, fmt.Errorf("backup: reading the archive: %w", err)
		}
		if hdr.Typeflag != tar.TypeReg {
			return m, fmt.Errorf("backup: unexpected entry %q", hdr.Name)
		}
		if hdr.Name == manifestName && !haveManifest {
			if err := json.NewDecoder(io.LimitReader(tr, maxManifest)).Decode(&m); err != nil {
				return m, fmt.Errorf("backup: reading the manifest: %w", err)
			}
			haveManifest = true
			continue
		}
		if !storage.BackedUpPath(hdr.Name) {
			return m, fmt.Errorf("backup: unexpected file %q", hdr.Name)
		}
		if written[hdr.Name] {
			return m, fmt.Errorf("backup: %q appears twice", hdr.Name)
		}
		written[hdr.Name] = true
		path := filepath.Join(stage, filepath.FromSlash(hdr.Name))
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			return m, err
		}
		var f File
		if s, ok := hdr.PAXRecords[fromRecord]; ok {
			from, err := strconv.ParseInt(s, 10, 64)
			if err != nil || from <= 0 || have[hdr.Name].Size != from {
				return m, fmt.Errorf("backup: %s continues a file the backup before did not leave", hdr.Name)
			}
			f, err = extend(tr, path)
			if err != nil {
				return m, err
			}
		} else {
			os.Remove(path)
			f, err = extract(tr, path)
			if err != nil {
				return m, err
			}
		}
		f.Name = hdr.Name
		have[hdr.Name] = f
	}
	if !haveManifest {
		return m, errors.New("backup: the manifest is missing")
	}
	if m.Format != 1 && m.Format != Format {
		return m, fmt.Errorf("backup: format %d is not supported by this version (it reads 1 to %d)", m.Format, Format)
	}
	if m.Format == 1 {
		m.Kind = Full
	}
	return m, nil
}

// settle removes from stage the files a backup's manifest does not list
// and checks that every file it lists is there as listed.
func settle(stage string, m Manifest, have map[string]File) error {
	want := map[string]File{}
	for _, f := range m.Files {
		want[f.Name] = f
	}
	for name := range have {
		if _, ok := want[name]; !ok {
			if err := os.Remove(filepath.Join(stage, filepath.FromSlash(name))); err != nil && !errors.Is(err, fs.ErrNotExist) {
				return err
			}
			delete(have, name)
		}
	}
	if len(want) != len(have) {
		return errors.New("backup: the files do not match the manifest")
	}
	for name, f := range want {
		if have[name] != f {
			return fmt.Errorf("backup: %s does not match the manifest", name)
		}
	}
	return nil
}

func extract(r io.Reader, path string) (File, error) {
	out, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o640)
	if err != nil {
		return File{}, err
	}
	return finish(out, sha256.New(), 0, r)
}

// extend appends r to the file at path, hashing the whole of it.
func extend(r io.Reader, path string) (File, error) {
	out, err := os.OpenFile(path, os.O_RDWR, 0o640)
	if err != nil {
		return File{}, err
	}
	h := sha256.New()
	n, err := io.Copy(h, out)
	if err != nil {
		out.Close()
		return File{}, err
	}
	return finish(out, h, n, r)
}

func finish(out *os.File, h hash.Hash, n int64, r io.Reader) (File, error) {
	m, err := io.Copy(io.MultiWriter(out, h), r)
	if err == nil {
		err = out.Sync()
	}
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return File{}, err
	}
	return File{Size: n + m, SHA256: hex.EncodeToString(h.Sum(nil))}, nil
}
