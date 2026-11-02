package storage

import (
	"errors"
	"testing"

	"github.com/dbuchs7/orbithek/internal/diskspace"
	"github.com/dbuchs7/orbithek/internal/item"
)

// TestNothingNewFromTheCriticalLevelOn: the store of a data directory
// refuses commands that make something new from the critical level on,
// with an ErrNoSpace, and nothing reaches the log; what exists still
// changes; at warning, and with room again, everything is made
// (ADR-0163).
func TestNothingNewFromTheCriticalLevelOn(t *testing.T) {
	dir := t.TempDir()
	db, id := pageIn(t, dir)
	defer db.Close()
	create := func() error {
		return db.Write(func(s *item.Store) error {
			_, err := s.Create("admin", item.NewItem{Space: "PLAT", Type: "page", Title: "New"})
			return err
		})
	}
	for _, l := range []diskspace.Level{diskspace.Critical, diskspace.Full} {
		db.SetSpace(l)
		var before int
		db.Read(func(s *item.Store) { before = s.Seq() })
		if err := create(); !errors.Is(err, ErrNothingNew) || !errors.Is(err, ErrNoSpace) {
			t.Errorf("%v: create %v", l, err)
		}
		if err := db.Write(func(s *item.Store) error { return s.CreateSpace("admin", "NEW", "New") }); !errors.Is(err, ErrNothingNew) {
			t.Errorf("%v: space %v", l, err)
		}
		db.Read(func(s *item.Store) {
			if s.Seq() != before {
				t.Errorf("%v: %d events recorded", l, s.Seq()-before)
			}
		})
	}
	db.SetSpace(diskspace.Critical)
	if err := db.Write(func(s *item.Store) error { return s.SetTitle("admin", id, "Changed") }); err != nil {
		t.Errorf("an edit when critical: %v", err)
	}
	db.SetSpace(diskspace.Warning)
	if err := create(); err != nil {
		t.Errorf("warning: %v", err)
	}
	db.SetSpace(diskspace.OK)
	if err := create(); err != nil {
		t.Errorf("ok: %v", err)
	}
}
