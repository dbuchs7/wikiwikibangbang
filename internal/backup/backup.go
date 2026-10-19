// Package backup writes and restores encrypted backups of a data directory
// (ADR-0016, ADR-0034).
//
// A backup is a tar archive of the files storage.BackupFiles names and of
// the attachments' files (ADR-0045), taken from a consistent snapshot, followed by a manifest with their sizes and
// SHA-256 sums; the archive is encrypted with a key derived from a
// passphrase. It holds the item keys and the account hashes, because a
// backup without them could not be restored to a working instance; the
// passphrase is what protects them.
package backup

import (
	"archive/tar"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/dbuchs7/wikiwikibangbang/internal/storage"
)

// Format is the version of the archive layout.
const Format = 1

const manifestName = "manifest.json"

// Manifest describes a backup.
type Manifest struct {
	Format  int       `json:"format"`
	Created time.Time `json:"created"`
	Build   string    `json:"build"`
	Files   []File    `json:"files"`
}

// File is one file of a backup.
type File struct {
	Name   string `json:"name"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// Write writes an encrypted backup of a snapshot to w and closes the
// snapshot's files. build names the program version for the manifest.
func Write(w io.Writer, passphrase string, files []storage.SnapshotFile, build string, now time.Time) (err error) {
	defer func() {
		for _, f := range files {
			if cerr := f.R.Close(); err == nil {
				err = cerr
			}
		}
	}()
	enc, err := encrypt(w, passphrase, Iterations)
	if err != nil {
		return err
	}
	tw := tar.NewWriter(enc)
	m := Manifest{Format: Format, Created: now.UTC(), Build: build}
	for _, f := range files {
		if err := tw.WriteHeader(&tar.Header{Name: f.Name, Size: f.Size, Mode: 0o640, ModTime: now, Typeflag: tar.TypeReg}); err != nil {
			return err
		}
		h := sha256.New()
		n, err := io.Copy(io.MultiWriter(tw, h), f.R)
		if err != nil {
			return err
		}
		if n != f.Size {
			return fmt.Errorf("backup: %s: read %d of %d bytes", f.Name, n, f.Size)
		}
		m.Files = append(m.Files, File{Name: f.Name, Size: f.Size, SHA256: hex.EncodeToString(h.Sum(nil))})
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	if err := tw.WriteHeader(&tar.Header{Name: manifestName, Size: int64(len(data)), Mode: 0o640, ModTime: now, Typeflag: tar.TypeReg}); err != nil {
		return err
	}
	if _, err := tw.Write(data); err != nil {
		return err
	}
	if err := tw.Close(); err != nil {
		return err
	}
	return enc.Close()
}

// Restore decrypts a backup into dir, which must not exist or be empty. It
// takes only the files storage.BackedUpPath allows, checks each against the
// manifest, and checks that the result opens as a data directory before it
// moves it into place; on any error dir is left as it was.
func Restore(r io.Reader, passphrase, dir string) (Manifest, error) {
	var m Manifest
	if entries, err := os.ReadDir(dir); err == nil && len(entries) > 0 {
		return m, fmt.Errorf("backup: %s is not empty; restore into a new directory", dir)
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return m, err
	}
	plain, err := decrypt(r, passphrase)
	if err != nil {
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

	got := map[string]File{}
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
			if err := json.NewDecoder(io.LimitReader(tr, 1<<20)).Decode(&m); err != nil {
				return m, fmt.Errorf("backup: reading the manifest: %w", err)
			}
			haveManifest = true
			continue
		}
		if !storage.BackedUpPath(hdr.Name) {
			return m, fmt.Errorf("backup: unexpected file %q", hdr.Name)
		}
		if _, dup := got[hdr.Name]; dup {
			return m, fmt.Errorf("backup: %q appears twice", hdr.Name)
		}
		path := filepath.Join(stage, filepath.FromSlash(hdr.Name))
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			return m, err
		}
		f, err := extract(tr, path)
		if err != nil {
			return m, err
		}
		f.Name = hdr.Name
		got[hdr.Name] = f
	}
	if !haveManifest {
		return m, errors.New("backup: the manifest is missing")
	}
	if m.Format != Format {
		return m, fmt.Errorf("backup: format %d is not supported by this version (it reads %d)", m.Format, Format)
	}
	if len(m.Files) != len(got) {
		return m, errors.New("backup: the files do not match the manifest")
	}
	for _, want := range m.Files {
		if got[want.Name] != want {
			return m, fmt.Errorf("backup: %s does not match the manifest", want.Name)
		}
	}
	for _, name := range storage.BackupFiles {
		if _, ok := got[name]; !ok {
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

func extract(r io.Reader, path string) (File, error) {
	out, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o640)
	if err != nil {
		return File{}, err
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(out, h), r)
	if err == nil {
		err = out.Sync()
	}
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return File{}, err
	}
	return File{Size: n, SHA256: hex.EncodeToString(h.Sum(nil))}, nil
}
