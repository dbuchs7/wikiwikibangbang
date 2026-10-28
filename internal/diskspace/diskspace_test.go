package diskspace

import (
	"errors"
	"testing"
)

// TestOfMeasuresTheFileSystem: a directory's file system has a size and no
// more free space than that; a path that does not exist is an error.
func TestOfMeasuresTheFileSystem(t *testing.T) {
	u, err := Of(t.TempDir())
	if errors.Is(err, ErrUnsupported) {
		t.Skip(err)
	}
	if err != nil {
		t.Fatal(err)
	}
	if u.Total == 0 || u.Free > u.Total {
		t.Fatalf("usage %+v", u)
	}
	if _, err := Of(t.TempDir() + "/does/not/exist"); err == nil {
		t.Error("a missing path was measured")
	}
}

// TestLevelOf: the levels follow the share and the megabytes, whichever is
// reached first, and the floor below them.
func TestLevelOf(t *testing.T) {
	const gb = 1 << 30
	for _, c := range []struct {
		name        string
		free, total uint64
		want        Level
	}{
		{"plenty", 500 * gb, 1000 * gb, OK},
		{"below 15 %", 140 * gb, 1000 * gb, Warning},
		{"below 5 GB on a small disk", 4 * gb, 20 * gb, Warning},
		{"below 5 %", 40 * gb, 1000 * gb, Critical},
		{"below 1 GB", gb - 1, 10 * gb, Critical},
		{"below the floor", Floor - 1, 1000 * gb, Full},
		{"unknown size", 10 * gb, 0, OK},
	} {
		if got := LevelOf(Usage{Free: c.free, Total: c.total}, Defaults); got != c.want {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
	for l, name := range map[Level]string{OK: "ok", Warning: "warning", Critical: "critical", Full: "full"} {
		if l.String() != name {
			t.Errorf("%d is %q", l, l.String())
		}
	}
}

// TestThresholdsAreChecked: shares between 1 and 50, megabytes up to a
// terabyte, critical not above warning.
func TestThresholdsAreChecked(t *testing.T) {
	if err := Defaults.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []Thresholds{
		{WarnPercent: 0, WarnMB: 1, CriticalPercent: 0, CriticalMB: 0},
		{WarnPercent: 60, WarnMB: 1, CriticalPercent: 5, CriticalMB: 0},
		{WarnPercent: 15, WarnMB: -1, CriticalPercent: 5, CriticalMB: 0},
		{WarnPercent: 15, WarnMB: 2 << 20, CriticalPercent: 5, CriticalMB: 0},
		{WarnPercent: 5, WarnMB: 5000, CriticalPercent: 15, CriticalMB: 1000},
		{WarnPercent: 15, WarnMB: 1000, CriticalPercent: 5, CriticalMB: 5000},
	} {
		if bad.Validate() == nil {
			t.Errorf("%+v was accepted", bad)
		}
	}
}
