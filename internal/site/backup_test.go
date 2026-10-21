package site

import (
	"errors"
	"testing"
)

func TestBackupScheduleIsKeptAndReplayed(t *testing.T) {
	var records [][]byte
	j := func(r []byte) error { records = append(records, r); return nil }
	s, err := New(nil, j)
	if err != nil {
		t.Fatal(err)
	}
	if s.Backup() != DefaultBackup || DefaultBackup.FullDays != 14 || DefaultBackup.Enabled {
		t.Fatalf("default %+v", s.Backup())
	}
	b := Backup{Enabled: true, Destination: "/backups", FullDays: 7, IncrementalDays: 1, Keep: 10}
	if err := s.SetBackup("admin", b); err != nil {
		t.Fatal(err)
	}
	if err := s.SetBackup("admin", b); err != nil || len(records) != 1 {
		t.Fatalf("an unchanged schedule is not journaled again: %v %d", err, len(records))
	}
	r, err := New(records, j)
	if err != nil || r.Backup() != b {
		t.Fatalf("replayed: %v %+v", err, r.Backup())
	}
	if _, err := New([][]byte{[]byte(`{"v":1,"kind":"BackupSet"}`)}, j); err == nil {
		t.Error("a backup event without settings replayed")
	}
	for name, bad := range map[string]Backup{
		"no full backups":        {FullDays: 0, Keep: 1},
		"a full backup too rare": {FullDays: 366, Keep: 1},
		"negative increments":    {FullDays: 7, IncrementalDays: -1, Keep: 1},
		"increments as rare":     {FullDays: 7, IncrementalDays: 7, Keep: 1},
		"nothing kept":           {FullDays: 7, Keep: 0},
		"too much kept":          {FullDays: 7, Keep: MaxBackupVersions + 1},
		"on without a place":     {Enabled: true, FullDays: 7, Keep: 1},
		"a long destination":     {FullDays: 7, Keep: 1, Destination: string(make([]byte, 1025))},
	} {
		if err := s.SetBackup("admin", bad); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if err := s.SetBackup("admin", Backup{FullDays: 14, IncrementalDays: 0, Keep: 1}); err != nil {
		t.Errorf("no incremental backups: %v", err)
	}
}
