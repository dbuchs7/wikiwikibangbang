package storage

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/dbuchs7/wikiwikibangbang/internal/item"
	"github.com/dbuchs7/wikiwikibangbang/internal/wal"
)

func mustOpen(t *testing.T, dir string) (*DB, Recovery) {
	t.Helper()
	db, rec, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return db, rec
}

func noErr(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func populate(t *testing.T, s *item.Store) item.Item {
	t.Helper()
	noErr(t, s.DefineType("admin", item.TypeDef{Name: "task", Body: true, Fields: []item.FieldDef{
		{Name: "due", Kind: item.KindDate}, {Name: "points", Kind: item.KindNumber},
	}}))
	noErr(t, s.CreateSpace("admin", "PLAT", "Platform"))
	page, err := s.Create("anna", item.NewItem{Space: "PLAT", Type: "task", Title: "Host"})
	noErr(t, err)
	// A multi-event command: one record in the log.
	_, err = s.CreateEmbedded("anna", page.ID, item.NewItem{Space: "PLAT", Type: "task", Title: "Inline",
		Properties: item.Props{"points": 3, "due": time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)}})
	noErr(t, err)
	return page
}

// TestRestartRecoversEverything is the point of the package: what was
// acknowledged before a restart is there after it, identically.
func TestRestartRecoversEverything(t *testing.T) {
	dir := t.TempDir()
	db, rec := mustOpen(t, dir)
	if rec.Events != 0 {
		t.Fatalf("new directory recovered %d events", rec.Events)
	}
	page := populate(t, db.Store)
	all := item.Query{IncludeDeleted: true}
	before := db.Store.Find(all)
	beforeLog := db.Store.Log()
	noErr(t, db.Close())

	db, rec = mustOpen(t, dir)
	defer db.Close()
	if rec.Events != len(beforeLog) || rec.Batches != len(beforeLog)-1 {
		t.Fatalf("recovered %d events in %d batches, want %d in %d", rec.Events, rec.Batches, len(beforeLog), len(beforeLog)-1)
	}
	if after := db.Store.Find(all); !reflect.DeepEqual(after, before) {
		t.Fatalf("state after restart differs\n got: %#v\nwant: %#v", after, before)
	}
	// The recovered store keeps writing to the same log.
	noErr(t, db.Store.SetTitle("anna", page.ID, "Renamed"))
	noErr(t, db.Close())
	db, _ = mustOpen(t, dir)
	if got, _ := db.Store.Get(page.ID); got.Title != "Renamed" {
		t.Fatalf("title after second restart: %q", got.Title)
	}
	rec2, err := Inspect(dir)
	noErr(t, err)
	if rec2.Events != len(beforeLog)+1 {
		t.Fatalf("Inspect: %d events", rec2.Events)
	}
}

func TestAnInterruptedCommandIsNotRecovered(t *testing.T) {
	dir := t.TempDir()
	db, _ := mustOpen(t, dir)
	populate(t, db.Store)
	want := len(db.Store.Log())
	noErr(t, db.Close())

	// Simulate a crash in the middle of the last append: cut its record.
	path := filepath.Join(dir, wal.FileName)
	data, err := os.ReadFile(path)
	noErr(t, err)
	noErr(t, os.WriteFile(path, data[:len(data)-5], 0o640))

	rec, err := Inspect(dir)
	noErr(t, err)
	if rec.Torn == 0 {
		t.Fatal("Inspect did not report the torn tail")
	}
	db, rec = mustOpen(t, dir)
	defer db.Close()
	// The last command was the two-event CreateEmbedded: both events go.
	if rec.Events != want-2 || rec.Torn == 0 {
		t.Fatalf("recovered %d events (torn %d), want %d", rec.Events, rec.Torn, want-2)
	}
	if n := len(db.Store.Find(item.Query{})); n != 1 {
		t.Fatalf("%d items after recovery, want only the host", n)
	}
}

func TestUnreadableDataIsRefused(t *testing.T) {
	cases := map[string]func(l *wal.Log) error{
		"not an event batch": func(l *wal.Log) error { return l.Append([]byte(`{"hello":1}`)) },
		"sequence gap": func(l *wal.Log) error {
			return l.Append([]byte(`[{"v":1,"seq":2,"at":"2026-01-01T00:00:00Z","kind":"SpaceCreated","space":"X"}]`))
		},
	}
	for name, write := range cases {
		dir := t.TempDir()
		l, _, err := wal.Open(dir)
		noErr(t, err)
		noErr(t, write(l))
		noErr(t, l.Close())
		if _, _, err := Open(dir); err == nil {
			t.Errorf("%s: Open succeeded", name)
		}
		if _, err := Inspect(dir); err == nil {
			t.Errorf("%s: Inspect succeeded", name)
		}
	}
}

func TestOpenPropagatesLogErrors(t *testing.T) {
	dir := t.TempDir()
	db, _ := mustOpen(t, dir)
	defer db.Close()
	if _, _, err := Open(dir); !errors.Is(err, wal.ErrLocked) {
		t.Fatalf("second Open: err = %v, want wal.ErrLocked", err)
	}
	bad := t.TempDir()
	noErr(t, os.WriteFile(filepath.Join(bad, wal.FileName), []byte("garbage-garbage"), 0o640))
	if _, err := Inspect(bad); !errors.Is(err, wal.ErrCorrupt) {
		t.Fatalf("Inspect: err = %v, want wal.ErrCorrupt", err)
	}
}

func TestAnUnstorableValueFailsTheCommand(t *testing.T) {
	db, _ := mustOpen(t, t.TempDir())
	defer db.Close()
	noErr(t, db.Store.CreateSpace("admin", "PLAT", "Platform"))
	// TypeDefs are validated, so provoke an encoding failure through the
	// journal directly.
	if err := (journal{db}).Append([]item.Event{{Kind: item.PropertySet, Value: struct{}{}}}); err == nil {
		t.Fatal("an unencodable event was journaled")
	}
}

// TestEveryFileIsBackedUpOrExcluded keeps the backup allowlist honest
// (ADR-0016): a new file in the data directory fails this test until it
// is added to BackupFiles or, with a reason, to NotBackedUp.
func TestEveryFileIsBackedUpOrExcluded(t *testing.T) {
	dir := t.TempDir()
	db, _, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	s := db.Store
	steps := []error{
		s.DefineType("admin", item.TypeDef{Name: "page", Body: true}),
		s.CreateSpace("admin", "PLAT", "Platform"),
	}
	it, err := s.Create("admin", item.NewItem{Space: "PLAT", Type: "page", Title: "t"})
	steps = append(steps, err, s.Trash("admin", it.ID), s.Purge("admin", it.ID))
	_, err = db.EnsureAdmin("admin", "a long enough password")
	steps = append(steps, err)
	_, err = db.ExpireAudit(time.Now().AddDate(20, 0, 0))
	steps = append(steps, err, db.SetCredential("WWBB_ENGINE_ATLAS_TOKEN", "t", "admin"))
	for _, err := range steps {
		if err != nil {
			t.Fatal(err)
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		_, excluded := NotBackedUp[e.Name()]
		if !excluded && !slices.Contains(BackupFiles, e.Name()) && !slices.Contains(OptionalBackupFiles, e.Name()) && !slices.Contains(BackedUpDirs, e.Name()) {
			t.Errorf("%s is neither backed up nor excluded on purpose", e.Name())
		}
	}
	files, err := db.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if f.Name == CredentialsFile {
			t.Error("a snapshot carries the stored credentials (ADR-0151)")
		}
		f.R.Close()
	}
	db.Close()
}

// WarmUp scans every live item ahead, in parts, and stops when told to;
// the reports then find what a scan finds.
func TestWarmUp(t *testing.T) {
	db, _ := mustOpen(t, t.TempDir())
	defer db.Close()
	noErr(t, db.Write(func(s *item.Store) error {
		populate(t, s)
		for i := range warmPart + 5 {
			if _, err := s.Create("anna", item.NewItem{Space: "PLAT", Type: "task", Title: fmt.Sprintf("Item %d", i)}); err != nil {
				return err
			}
		}
		return s.GrantRole("admin", "PLAT", "anna", item.SpaceAdmin)
	}))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	db.WarmUp(ctx) // stops at once
	db.WarmUp(context.Background())
	var n int
	db.Read(func(s *item.Store) {
		rep, err := s.As("anna").Governance("PLAT", time.Now(), time.Hour)
		noErr(t, err)
		n = len(rep.Sensitive)
		if len(s.LiveItems()) < warmPart+5 {
			t.Errorf("%d live items", len(s.LiveItems()))
		}
	})
	if n != 0 {
		t.Errorf("%d sensitive findings in plain titles", n)
	}
}

// TestChangedWakesOnlyOnACommand: the change feed's waiting requests wake
// when the log grows, not on a command that fails (ADR-0121).
func TestChangedWakesOnlyOnACommand(t *testing.T) {
	db, _ := mustOpen(t, t.TempDir())
	defer db.Close()
	ch := db.Changed()
	if ch != db.Changed() {
		t.Fatal("two waiters before a change wait on different channels")
	}
	if err := db.Write(func(s *item.Store) error { return s.CreateSpace("admin", "lower", "x") }); err == nil {
		t.Fatal("an invalid space was created")
	}
	select {
	case <-ch:
		t.Fatal("a failed command woke the waiters")
	default:
	}
	noErr(t, db.Write(func(s *item.Store) error { return s.CreateSpace("admin", "PLAT", "Platform") }))
	select {
	case <-ch:
	default:
		t.Fatal("a command did not wake the waiters")
	}
	if db.Changed() == ch {
		t.Fatal("a closed channel is handed out again")
	}
	// A command with nobody waiting closes nothing.
	noErr(t, db.Write(func(s *item.Store) error { return s.CreateSpace("admin", "OPS", "Ops") }))
}
