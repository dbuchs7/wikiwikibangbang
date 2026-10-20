package backup

import (
	"archive/tar"
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dbuchs7/wikiwikibangbang/internal/audit"
	"github.com/dbuchs7/wikiwikibangbang/internal/auth"
	"github.com/dbuchs7/wikiwikibangbang/internal/item"
	"github.com/dbuchs7/wikiwikibangbang/internal/storage"
)

func init() { Iterations = 1000 }

func openDB(t *testing.T, dir string) *storage.DB {
	t.Helper()
	db, _, err := storage.OpenWith(dir, storage.Options{Auth: []auth.Option{auth.WithIterations(1000)}})
	if err != nil {
		t.Fatal(err)
	}
	return db
}

// populate fills a data directory with items (one purged), a user and
// audit entries.
func populate(t *testing.T, db *storage.DB) {
	t.Helper()
	err := db.Write(func(s *item.Store) error {
		for _, err := range []error{
			s.DefineType("admin", item.TypeDef{Name: "page", Body: true}),
			s.CreateSpace("admin", "PLAT", "Platform"),
		} {
			if err != nil {
				return err
			}
		}
		for _, title := range []string{"kept", "purged"} {
			it, err := s.Create("admin", item.NewItem{Space: "PLAT", Type: "page", Title: title})
			if err != nil {
				return err
			}
			if err := s.CommitBody("admin", it.ID, item.Doc{Blocks: []item.Block{{Kind: item.Paragraph, Text: "secret of " + title}}}); err != nil {
				return err
			}
			if title == "purged" {
				if err := s.Trash("admin", it.ID); err != nil {
					return err
				}
				if err := s.Purge("admin", it.ID); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Accounts.CreateUser("anna", "anna's password"); err != nil {
		t.Fatal(err)
	}
	db.Audit.Record(audit.Entry{Event: audit.Login, Actor: "anna"})
}

func backupOf(t *testing.T, db *storage.DB) []byte {
	t.Helper()
	files, err := db.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := Write(&buf, testPass, files, "test", time.Now()); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

type state struct {
	Items []item.Item
	Users []string
	Audit int
}

func stateOf(db *storage.DB) state {
	var st state
	db.Read(func(s *item.Store) { st.Items = s.Find(item.Query{IncludeDeleted: true}) })
	for _, u := range db.Accounts.Users() {
		st.Users = append(st.Users, u.Username)
	}
	st.Audit = len(db.Audit.List(0, 1000))
	return st
}

func TestBackupAndRestoreGiveTheSameState(t *testing.T) {
	src := openDB(t, t.TempDir())
	defer src.Close()
	populate(t, src)
	want := stateOf(src)
	data := backupOf(t, src)
	if bytes.Contains(data, []byte("secret of")) || bytes.Contains(data, []byte("anna")) {
		t.Fatal("the backup is not encrypted")
	}

	dir := filepath.Join(t.TempDir(), "restored")
	m, err := Restore(bytes.NewReader(data), testPass, dir)
	if err != nil {
		t.Fatal(err)
	}
	if m.Format != Format || m.Build != "test" || len(m.Files) != len(storage.BackupFiles)+len(storage.OptionalBackupFiles) {
		t.Fatalf("manifest %+v", m)
	}
	dst := openDB(t, dir)
	defer dst.Close()
	if got := stateOf(dst); !reflect.DeepEqual(got, want) {
		t.Fatalf("restored state differs\n got %+v\nwant %+v", got, want)
	}
	// The content decrypts with the restored keys; the purged item stays erased.
	for _, it := range stateOf(dst).Items {
		switch it.Title {
		case "kept":
			if it.Body.Blocks[0].Text != "secret of kept" {
				t.Fatalf("body %v", it.Body)
			}
		case "":
			if it.Presence != item.Purged || len(it.Body.Blocks) != 0 {
				t.Fatalf("purged item %+v", it)
			}
		}
	}
	if _, _, err := dst.Accounts.Login("anna", "anna's password", "x"); err != nil {
		t.Fatalf("restored account does not log in: %v", err)
	}
}

func TestABackupTakenWhileWritingIsConsistent(t *testing.T) {
	src := openDB(t, t.TempDir())
	defer src.Close()
	populate(t, src)
	stop := make(chan struct{})
	var wg sync.WaitGroup
	var writes atomic.Int64
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			_ = src.Write(func(s *item.Store) error {
				it, err := s.Create("admin", item.NewItem{Space: "PLAT", Type: "page", Title: "busy"})
				if err == nil {
					err = s.CommitBody("admin", it.ID, item.Doc{Blocks: []item.Block{{Kind: item.Paragraph, Text: "busy body"}}})
				}
				return err
			})
			src.Audit.Record(audit.Entry{Event: audit.Login, Actor: "anna"})
			writes.Add(1)
		}
	}()
	// Each backup starts after the writer has written again, so every one
	// is taken while writes are going on.
	var backups [][]byte
	for range 5 {
		seen := writes.Load()
		for deadline := time.Now().Add(10 * time.Second); writes.Load() == seen; {
			if time.Now().After(deadline) {
				t.Fatal("the writer does not write")
			}
			runtime.Gosched()
		}
		backups = append(backups, backupOf(t, src))
	}
	close(stop)
	wg.Wait()
	for i, data := range backups {
		dir := filepath.Join(t.TempDir(), "r")
		if _, err := Restore(bytes.NewReader(data), testPass, dir); err != nil {
			t.Fatalf("backup %d: %v", i, err)
		}
		db := openDB(t, dir)
		// An item and its body are written in one command, so a consistent
		// snapshot holds both, with the key to read them.
		busy := 0
		for _, it := range stateOf(db).Items {
			if it.Title != "busy" {
				continue
			}
			busy++
			if len(it.Body.Blocks) != 1 || it.Body.Blocks[0].Text != "busy body" {
				t.Fatalf("backup %d: body %+v", i, it.Body)
			}
		}
		if busy == 0 {
			t.Fatalf("backup %d holds none of the writer's items", i)
		}
		db.Close()
	}
}

func TestRestoreRefusesWhatItShouldNot(t *testing.T) {
	src := openDB(t, t.TempDir())
	defer src.Close()
	populate(t, src)
	data := backupOf(t, src)

	full := t.TempDir()
	os.WriteFile(filepath.Join(full, "x"), []byte("x"), 0o600)
	if _, err := Restore(bytes.NewReader(data), testPass, full); err == nil || !strings.Contains(err.Error(), "not empty") {
		t.Errorf("non-empty target: %v", err)
	}
	target := filepath.Join(t.TempDir(), "r")
	if _, err := Restore(bytes.NewReader(data), "wrong passphrase!", target); !errors.Is(err, ErrPassphrase) {
		t.Errorf("wrong passphrase: %v", err)
	}
	if _, err := os.Stat(target); !errors.Is(err, os.ErrNotExist) {
		t.Error("a failed restore left the target behind")
	}

	// Archives that were not written by Write.
	forge := func(entries map[string]string, order ...string) []byte {
		var buf bytes.Buffer
		enc, _ := encrypt(&buf, testPass, 1000)
		tw := tar.NewWriter(enc)
		for _, name := range order {
			tw.WriteHeader(&tar.Header{Name: name, Size: int64(len(entries[name])), Mode: 0o600, Typeflag: tar.TypeReg})
			tw.Write([]byte(entries[name]))
		}
		tw.Close()
		enc.Close()
		return buf.Bytes()
	}
	for name, archive := range map[string][]byte{
		"an unknown file":  forge(map[string]string{"../../etc/passwd": "x"}, "../../etc/passwd"),
		"no manifest":      forge(map[string]string{"events.wal": "WWBBLOG1"}, "events.wal"),
		"a lying manifest": forge(map[string]string{"events.wal": "WWBBLOG1", "manifest.json": `{"format":1,"files":[{"name":"events.wal","size":8,"sha256":"00"}]}`}, "events.wal", "manifest.json"),
		"a future format":  forge(map[string]string{"manifest.json": `{"format":99}`}, "manifest.json"),
	} {
		if _, err := Restore(bytes.NewReader(archive), testPass, filepath.Join(t.TempDir(), "r")); err == nil {
			t.Errorf("%s was restored", name)
		}
	}
}

// Attachments' files travel with the backup, encrypted twice, and open
// after a restore; a deleted attachment's file is not in it (ADR-0045).
func TestBackupsHoldAttachments(t *testing.T) {
	src := openDB(t, t.TempDir())
	defer src.Close()
	populate(t, src)
	var page item.ID
	src.Read(func(s *item.Store) { page = s.Find(item.Query{Space: "PLAT"})[0].ID })
	store := func(content, name string) item.Attachment {
		a, err := src.StoreAttachment(page, strings.NewReader(content), 1<<20)
		if err != nil {
			t.Fatal(err)
		}
		a.Name = name
		if err := src.Write(func(s *item.Store) error { a, err = s.AddAttachment("admin", page, a); return err }); err != nil {
			t.Fatal(err)
		}
		return a
	}
	kept := store("the diagram", "diagram.png")
	gone := store("deleted content", "old.txt")
	if err := src.Write(func(s *item.Store) error { return s.DeleteAttachment("admin", page, gone.ID) }); err != nil {
		t.Fatal(err)
	}
	data := backupOf(t, src)
	if bytes.Contains(data, []byte("the diagram")) {
		t.Fatal("an attachment is in the backup in the clear")
	}
	dir := filepath.Join(t.TempDir(), "restored")
	m, err := Restore(bytes.NewReader(data), testPass, dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Files) != len(storage.BackupFiles)+len(storage.OptionalBackupFiles)+1 {
		t.Fatalf("manifest %+v", m.Files)
	}
	dst := openDB(t, dir)
	defer dst.Close()
	r, err := dst.OpenAttachment(page, kept.ID)
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(r)
	r.Close()
	if err != nil || string(got) != "the diagram" {
		t.Fatalf("restored attachment %q, %v", got, err)
	}
}
