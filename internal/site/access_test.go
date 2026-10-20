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
