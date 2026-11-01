package backup

import (
	"slices"
	"testing"
	"time"
)

func TestNamesAndVersions(t *testing.T) {
	at := time.Date(2026, 10, 1, 2, 3, 4, 0, time.FixedZone("CEST", 2*3600))
	full := FileName(at, "0123456789abcdef", 0)
	if full != "orbithek-20261001T000304Z-0123456789abcdef-000-full.orbithek" {
		t.Fatalf("name %s", full)
	}
	n, ok := ParseName(full)
	if !ok || !n.At.Equal(at) || n.Chain != "0123456789abcdef" || n.Seq != 0 {
		t.Fatalf("parsed %+v %v", n, ok)
	}
	// A backup stored before the rename (ADR-0153) is read alike.
	if old, ok := ParseName("wikiwikibangbang-20261001T000304Z-0123456789abcdef-002-inc.wwbb"); !ok || !old.At.Equal(at) || old.Chain != "0123456789abcdef" || old.Seq != 2 {
		t.Fatalf("old name %+v %v", old, ok)
	}
	for _, bad := range []string{"backup.orbithek", "orbithek-20261001T000304Z-0123456789abcdef-001-full.orbithek",
		"orbithek-20261001T000304Z-0123456789abcdef-000-inc.orbithek", "orbithek-20261399T000304Z-0123456789abcdef-000-full.orbithek",
		"wikiwikibangbang-20261001T000304Z-0123456789abcdef-001-full.wwbb", "orbithek-20261001T000304Z-0123456789abcdef-000-full.wwbb",
		"wikiwikibangbang-20261001T000304Z-0123456789abcdef-000-full.orbithek"} {
		if _, ok := ParseName(bad); ok {
			t.Errorf("parsed %s", bad)
		}
	}
	older, newer := "1111111111111111", "2222222222222222"
	files := []string{
		FileName(at.Add(48*time.Hour), newer, 1), "notes.txt",
		FileName(at, older, 0), FileName(at.Add(24*time.Hour), older, 2),
		FileName(at.Add(47*time.Hour), newer, 0),
	}
	vs := Versions(files)
	if len(vs) != 2 || vs[0].Chain != older || vs[1].Chain != newer {
		t.Fatalf("versions %+v", vs)
	}
	if vs[0].Complete() || !vs[1].Complete() || (Version{}).Complete() {
		t.Error("completeness")
	}
	var seqs []int
	for _, b := range vs[1].Backups {
		seqs = append(seqs, b.Seq)
	}
	if !slices.Equal(seqs, []int{0, 1}) {
		t.Errorf("order %v", seqs)
	}
}
