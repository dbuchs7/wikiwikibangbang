package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dbuchs7/wikiwikibangbang/internal/audit"
	"github.com/dbuchs7/wikiwikibangbang/internal/backup"
	"github.com/dbuchs7/wikiwikibangbang/internal/item"
	"github.com/dbuchs7/wikiwikibangbang/internal/storage"
)

// TestRestoreSpacesFromABackup covers import -from backup (ADR-0124): a
// dry run writes nothing, a restore brings a space back with its files and
// puts itself on record, and a second run skips what is there.
func TestRestoreSpacesFromABackup(t *testing.T) {
	backup.Iterations = 1000
	src, _, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	picture := []byte("\x89PNG\r\n\x1a\na picture")
	var page item.Item
	err = src.Write(func(s *item.Store) error {
		for _, key := range []string{"PLAT", "OPS"} {
			if err := s.CreateSpace("admin", key, key); err != nil {
				return err
			}
		}
		if err := s.DefineType("admin", item.TypeDef{Name: "page", Body: true}); err != nil {
			return err
		}
		if _, err := s.Create("admin", item.NewItem{Space: "OPS", Type: "page", Title: "Not asked for"}); err != nil {
			return err
		}
		page, err = s.Create("admin", item.NewItem{Space: "PLAT", Type: "page", Title: "Runbook"})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	a, err := src.StoreAttachment(page.ID, bytes.NewReader(picture), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	a.Name = "pic.png"
	err = src.Write(func(s *item.Store) error {
		if _, err := s.AddAttachment("admin", page.ID, a); err != nil {
			return err
		}
		return s.CommitBody("admin", page.ID, item.Doc{Blocks: []item.Block{{Kind: item.Image, Ref: a.ID}, {Kind: item.Paragraph, Text: "Steps"}}})
	})
	if err != nil {
		t.Fatal(err)
	}
	files, err := src.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "b.wwbb")
	out, _ := os.Create(file)
	if err := backup.Write(out, "a long passphrase", files, "test", time.Now()); err != nil {
		t.Fatal(err)
	}
	out.Close()
	src.Close()

	target := filepath.Join(t.TempDir(), "data")
	db, _, err := storage.Open(target)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	run := func(extra ...string) (int, string, string) {
		return runCmd(t, context.Background(), append([]string{"import", "-from", "backup", "-backup", file, "-data-dir", target}, extra...)...)
	}
	if code, _, errOut := run("-spaces", "PLAT"); code != 2 || !strings.Contains(errOut, "WWBB_BACKUP_PASSPHRASE") {
		t.Fatalf("no passphrase: exit %d, %q", code, errOut)
	}
	t.Setenv("WWBB_BACKUP_PASSPHRASE", "a long passphrase")
	if code, _, errOut := run(); code != 2 || !strings.Contains(errOut, "-spaces") {
		t.Fatalf("no spaces: exit %d, %q", code, errOut)
	}
	if code, _, errOut := run("-spaces", "NONE"); code != 1 || !strings.Contains(errOut, "no space NONE") {
		t.Fatalf("a space the backup lacks: exit %d, %q", code, errOut)
	}
	if code, _, errOut := run("-spaces", "PLAT", "-grant", "nobody=viewer"); code != 1 || !strings.Contains(errOut, "nobody") {
		t.Fatalf("a grant to no one: exit %d, %q", code, errOut)
	}
	code, got, errOut := run("-spaces", "PLAT", "-dry-run")
	if code != 0 || !strings.Contains(got, "dry run, nothing written") || !strings.Contains(got, "1 items restored") {
		t.Fatalf("dry run: exit %d, %q %q", code, got, errOut)
	}
	code, got, errOut = run("-spaces", "PLAT")
	if code != 0 || !strings.Contains(got, "1 items restored") || !strings.Contains(got, page.Key+" Runbook") || !strings.Contains(got, "spaces created: PLAT") {
		t.Fatalf("restore: exit %d, %q %q", code, got, errOut)
	}
	if left, _ := filepath.Glob(filepath.Join(filepath.Dir(target), ".wikiwikibangbang-restore-*")); len(left) != 0 {
		t.Fatalf("the unpacked backup is left: %v", left)
	}

	db, _, err = storage.Open(target)
	if err != nil {
		t.Fatal(err)
	}
	var back item.Item
	db.Read(func(s *item.Store) { back, err = s.Get(page.ID) })
	if err != nil || back.Key != page.Key || len(back.Attachments) != 1 || back.Body.Blocks[0].Ref != back.Attachments[0].ID {
		t.Fatalf("restored %+v, %v", back, err)
	}
	r, err := db.OpenAttachment(page.ID, back.Attachments[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	content, _ := io.ReadAll(r)
	r.Close()
	if !bytes.Equal(content, picture) {
		t.Fatalf("the file holds %q", content)
	}
	entries := db.Audit.List(0, 5)
	if len(entries) == 0 || entries[0].Event != audit.PartialRestore || entries[0].Detail["spaces"] != "PLAT" || entries[0].Detail["restored"] != "1" {
		t.Fatalf("audit %+v", entries)
	}
	var ops bool
	db.Read(func(s *item.Store) { ops = s.HasSpace("OPS") })
	db.Close()
	if ops {
		t.Fatal("a space not asked for came back")
	}

	code, got, _ = run("-spaces", "PLAT")
	if code != 0 || !strings.Contains(got, "0 items restored, 1 skipped") || !strings.Contains(got, "Runbook: exists") {
		t.Fatalf("again: exit %d, %q", code, got)
	}
}
