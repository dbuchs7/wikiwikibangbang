package site

import "testing"

func TestAccessIsKeptAndReplayed(t *testing.T) {
	var records [][]byte
	j := func(r []byte) error { records = append(records, r); return nil }
	s, err := New(nil, j)
	if err != nil {
		t.Fatal(err)
	}
	if s.Access().AccountRequests {
		t.Fatal("account requests are off by default")
	}
	if err := s.SetAccess("admin", Access{AccountRequests: true}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetAccess("admin", Access{AccountRequests: true}); err != nil || len(records) != 1 {
		t.Fatalf("an unchanged setting is not journaled again: %v %d", err, len(records))
	}
	r, err := New(records, j)
	if err != nil || !r.Access().AccountRequests {
		t.Fatalf("replayed: %v %+v", err, r.Access())
	}
}

func TestAlbumRetentionIsKeptAndReplayed(t *testing.T) {
	var records [][]byte
	j := func(r []byte) error { records = append(records, r); return nil }
	s, err := New(nil, j)
	if err != nil {
		t.Fatal(err)
	}
	if s.AlbumRetention() != (AlbumRetention{}) {
		t.Fatal("by default every version is kept and nothing runs on its own")
	}
	want := AlbumRetention{Automatic: true, Keep: 5, Days: 90}
	if err := s.SetAlbumRetention("admin", want); err != nil {
		t.Fatal(err)
	}
	if err := s.SetAlbumRetention("admin", want); err != nil || len(records) != 1 {
		t.Fatalf("an unchanged rule is not journaled again: %v %d", err, len(records))
	}
	r, err := New(records, j)
	if err != nil || r.AlbumRetention() != want {
		t.Fatalf("replayed: %v %+v", err, r.AlbumRetention())
	}
	if _, err := New([][]byte{[]byte(`{"v":1,"kind":"AlbumRetentionSet"}`)}, j); err == nil {
		t.Error("a rule event without a rule is read")
	}
}
