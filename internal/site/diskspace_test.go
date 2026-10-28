package site

import (
	"errors"
	"testing"

	"github.com/dbuchs7/wikiwikibangbang/internal/diskspace"
)

// TestDiskSpaceIsKeptAndReplayed: the thresholds are the defaults until
// set, are checked, journaled once and replayed (ADR-0150).
func TestDiskSpaceIsKeptAndReplayed(t *testing.T) {
	var records [][]byte
	j := func(r []byte) error { records = append(records, r); return nil }
	s, err := New(nil, j)
	if err != nil {
		t.Fatal(err)
	}
	if s.DiskSpace() != diskspace.Defaults {
		t.Fatalf("defaults: %+v", s.DiskSpace())
	}
	want := diskspace.Thresholds{WarnPercent: 20, WarnMB: 10240, CriticalPercent: 8, CriticalMB: 2048}
	if err := s.SetDiskSpace("admin", want); err != nil {
		t.Fatal(err)
	}
	if err := s.SetDiskSpace("admin", want); err != nil || len(records) != 1 {
		t.Fatalf("unchanged thresholds are not journaled again: %v %d", err, len(records))
	}
	if err := s.SetDiskSpace("admin", diskspace.Thresholds{WarnPercent: 5, CriticalPercent: 15}); !errors.Is(err, ErrInvalid) {
		t.Errorf("critical above warning: %v", err)
	}
	r, err := New(records, j)
	if err != nil || r.DiskSpace() != want {
		t.Fatalf("replayed: %v %+v", err, r.DiskSpace())
	}
	if _, err := New([][]byte{[]byte(`{"v":1,"kind":"DiskSpaceSet"}`)}, j); err == nil {
		t.Error("a disk space event without thresholds is read")
	}
}
