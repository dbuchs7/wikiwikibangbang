package storage

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/dbuchs7/wikiwikibangbang/internal/item"
)

// pageIn opens a data directory with a page to attach files to.
func pageIn(t *testing.T, dir string) (*DB, item.ID) {
	t.Helper()
	db, _, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	var id item.ID
	err = db.Write(func(s *item.Store) error {
		if err := s.DefineType("admin", item.TypeDef{Name: "page", Body: true}); err != nil {
			return err
		}
		if err := s.CreateSpace("admin", "PLAT", "Platform"); err != nil {
			return err
		}
		it, err := s.Create("admin", item.NewItem{Space: "PLAT", Type: "page", Title: "Runbook"})
		id = it.ID
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return db, id
}

func attach(t *testing.T, db *DB, id item.ID, content []byte, name string) item.Attachment {
	t.Helper()
	a, err := db.StoreAttachment(id, bytes.NewReader(content), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	a.Name = name
	err = db.Write(func(s *item.Store) error {
		a, err = s.AddAttachment("admin", id, a)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func readAttachment(db *DB, id, a item.ID) ([]byte, error) {
	r, err := db.OpenAttachment(id, a)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return io.ReadAll(r)
}

func TestAttachmentsRoundTripEncrypted(t *testing.T) {
	dir := t.TempDir()
	db, id := pageIn(t, dir)
	defer db.Close()
	png := append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte("secret pixels "), 9000)...) // over one chunk
	for _, size := range []int{0, 1, chunkSize - 1, chunkSize, chunkSize + 1, 3 * chunkSize} {
		content := bytes.Repeat([]byte("x"), size)
		if size > 8 {
			content = png[:min(size, len(png))]
			content = append(content, bytes.Repeat([]byte("y"), size-len(content))...)
		}
		a := attach(t, db, id, content, "file.bin")
		sum := sha256.Sum256(content)
		if a.Size != int64(size) || a.SHA256 != hex.EncodeToString(sum[:]) {
			t.Fatalf("size %d: %+v", size, a)
		}
		if size > 8 && a.Type != "image/png" {
			t.Errorf("size %d: type %s", size, a.Type)
		}
		got, err := readAttachment(db, id, a.ID)
		if err != nil || !bytes.Equal(got, content) {
			t.Fatalf("size %d: read %d bytes, %v", size, len(got), err)
		}
		raw, _ := os.ReadFile(attachmentPath(dir, id, a.ID))
		if size > 20 && bytes.Contains(raw, content[len(content)-20:]) {
			t.Fatalf("size %d: stored in the clear", size)
		}
	}
	if _, err := db.StoreAttachment(id, bytes.NewReader(make([]byte, 101)), 100); !errors.Is(err, ErrAttachmentTooLarge) {
		t.Errorf("too large: %v", err)
	}
	if _, err := db.OpenAttachment(id, "../../keys.wal"); err == nil {
		t.Error("a path as attachment ID was opened")
	}
}

func TestAttachmentsRefuseTampering(t *testing.T) {
	dir := t.TempDir()
	db, id := pageIn(t, dir)
	defer db.Close()
	content := bytes.Repeat([]byte("0123456789"), chunkSize/5) // two chunks
	a := attach(t, db, id, content, "two-chunks.txt")
	path := attachmentPath(dir, id, a.ID)
	orig, _ := os.ReadFile(path)
	sealed := chunkSize + 16
	head := len(attachmentMagic) + 8
	for name, change := range map[string]func([]byte) []byte{
		"a flipped byte":    func(b []byte) []byte { b[len(b)/2] ^= 1; return b },
		"cut after a chunk": func(b []byte) []byte { return b[:head+sealed] },
		"an extra chunk":    func(b []byte) []byte { return append(b, b[head:head+sealed]...) },
		"chunks swapped": func(b []byte) []byte {
			return append(append(append([]byte{}, b[:head]...), b[head+sealed:]...), b[head:head+sealed]...)
		},
		"a different magic":     func(b []byte) []byte { b[0] = 'X'; return b },
		"cut inside the header": func(b []byte) []byte { return b[:4] },
	} {
		os.WriteFile(path, change(append([]byte{}, orig...)), 0o640)
		if got, err := readAttachment(db, id, a.ID); err == nil {
			t.Errorf("%s: read %d bytes without error", name, len(got))
		}
	}
	// A file of another item does not decrypt under this item's name.
	os.WriteFile(path, orig, 0o640)
	other := attach(t, db, id, []byte("other"), "o.txt")
	os.WriteFile(attachmentPath(dir, id, other.ID), orig, 0o640)
	if _, err := readAttachment(db, id, other.ID); err == nil {
		t.Error("a file moved to another attachment decrypts")
	}
}

func TestAttachmentFilesGoWithTheirAttachments(t *testing.T) {
	dir := t.TempDir()
	db, id := pageIn(t, dir)
	kept := attach(t, db, id, []byte("kept"), "kept.txt")
	deleted := attach(t, db, id, []byte("deleted"), "deleted.txt")
	if err := db.Write(func(s *item.Store) error { return s.DeleteAttachment("admin", id, deleted.ID) }); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(attachmentPath(dir, id, deleted.ID)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a deleted attachment's file stays: %v", err)
	}
	// An upload never recorded, and a crash's temporary file, go on the
	// next start.
	orphan, err := db.StoreAttachment(id, bytes.NewReader([]byte("never recorded")), 100)
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, AttachmentsDir, string(id), "upload-1.tmp"), []byte("x"), 0o640)
	db.Close()
	db, rec, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Orphans != 2 {
		t.Errorf("removed %d orphans, want 2", rec.Orphans)
	}
	if _, err := os.Stat(attachmentPath(dir, id, orphan.ID)); !errors.Is(err, os.ErrNotExist) {
		t.Error("an unrecorded upload stays")
	}
	if got, err := readAttachment(db, id, kept.ID); err != nil || string(got) != "kept" {
		t.Fatalf("kept file %q, %v", got, err)
	}
	// Purging the item removes its files and its key.
	err = db.Write(func(s *item.Store) error {
		if err := s.Trash("admin", id); err != nil {
			return err
		}
		return s.Purge("admin", id)
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, AttachmentsDir, string(id))); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a purged item's files stay: %v", err)
	}
	if _, err := db.StoreAttachment(id, bytes.NewReader([]byte("x")), 100); err == nil {
		t.Error("stored a file for a purged item")
	}
	db.Close()
}
