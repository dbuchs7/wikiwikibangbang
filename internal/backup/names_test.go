package backup

import (
	"slices"
	"testing"
	"time"
)

func TestNamesAndVersions(t *testing.T) {
	at := time.Date(2026, 10, 1, 2, 3, 4, 0, time.FixedZone("CEST", 2*3600))
	full := FileName(at, "0123456789abcdef", 0)
	if full != "wikiwikibangbang-20261001T000304Z-0123456789abcdef-000-full.wwbb" {
		t.Fatalf("name %s", full)
	}
	n, ok := ParseName(full)
	if !ok || !n.At.Equal(at) || n.Chain != "0123456789abcdef" || n.Seq != 0 {
		t.Fatalf("parsed %+v %v", n, ok)
	}
	for _, bad := range []string{"backup.wwbb", "wikiwikibangbang-20261001T000304Z-0123456789abcdef-001-full.wwbb",
		"wikiwikibangbang-20261001T000304Z-0123456789abcdef-000-inc.wwbb", "wikiwikibangbang-20261399T000304Z-0123456789abcdef-000-full.wwbb"} {
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
