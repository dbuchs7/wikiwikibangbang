package audit

import (
	"bytes"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"
)

type mem struct {
	records [][]byte
	fail    bool
}

func (m *mem) append(r []byte) error {
	if m.fail {
		return errors.New("disk full")
	}
	m.records = append(m.records, r)
	return nil
}

func clock() func() time.Time {
	t := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	return func() time.Time { t = t.Add(time.Second); return t }
}

func TestEntriesSurviveARestart(t *testing.T) {
	m := &mem{}
	var logs bytes.Buffer
	tr, err := New(nil, m.append, slog.New(slog.NewTextHandler(&logs, nil)), clock())
	if err != nil {
		t.Fatal(err)
	}
	tr.Record(Entry{Event: Login, Actor: "u1", Addr: "192.0.2.1"})
	tr.Record(Entry{Event: TokenCreated, Actor: "u1", Subject: "t1", Detail: map[string]string{"scope": "read"}})
	if !strings.Contains(logs.String(), "event=token.create") || !strings.Contains(logs.String(), "scope=read") {
		t.Fatalf("log lines: %s", logs.String())
	}

	again, err := New(m.records, m.append, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	got := again.List(0, 10)
	if len(got) != 2 || got[0].Event != TokenCreated || got[1].Seq != 1 || got[0].Detail["scope"] != "read" {
		t.Fatalf("after restart: %+v", got)
	}
	again.Record(Entry{Event: Logout, Actor: "u1"})
	if got := again.List(0, 1); got[0].Seq != 3 {
		t.Fatalf("sequence continues at %d", got[0].Seq)
	}
}

func TestListPagesNewestFirst(t *testing.T) {
	tr, _ := New(nil, (&mem{}).append, nil, clock())
	for range 5 {
		tr.Record(Entry{Event: LoginFailed})
	}
	page := tr.List(0, 2)
	if len(page) != 2 || page[0].Seq != 5 || page[1].Seq != 4 {
		t.Fatalf("first page %+v", page)
	}
	page = tr.List(page[1].Seq, 10)
	if len(page) != 3 || page[0].Seq != 3 || page[2].Seq != 1 {
		t.Fatalf("second page %+v", page)
	}
}

func TestAFailedWriteIsCountedAndLoggedNotDropped(t *testing.T) {
	m := &mem{fail: true}
	var logs bytes.Buffer
	tr, _ := New(nil, m.append, slog.New(slog.NewTextHandler(&logs, nil)), clock())
	tr.Record(Entry{Event: ItemPurged, Actor: "u1", Subject: "i1"})
	if tr.Failures() != 1 || !strings.Contains(logs.String(), "audit entry not persisted") || !strings.Contains(logs.String(), "subject=i1") {
		t.Fatalf("failures %d, log %s", tr.Failures(), logs.String())
	}
	m.fail = false
	tr.Record(Entry{Event: ItemPurged})
	if got := tr.List(0, 10); len(got) != 1 || got[0].Seq != 1 {
		t.Fatalf("a failed entry used up a sequence number: %+v", got)
	}
}

func TestMemoryIsBounded(t *testing.T) {
	tr, _ := New(nil, func([]byte) error { return nil }, nil, clock())
	for range Retained + 5 {
		tr.Record(Entry{Event: LoginFailed})
	}
	if got := tr.List(0, Retained+10); len(got) != Retained || got[len(got)-1].Seq != 6 {
		t.Fatalf("%d entries retained, oldest %d", len(got), got[len(got)-1].Seq)
	}
}

func TestDamagedJournalsAreRefused(t *testing.T) {
	for _, recs := range [][][]byte{{[]byte("{")}, {[]byte(`{"seq":0}`)}, {[]byte(`{"seq":2}`), []byte(`{"seq":4}`)}} {
		if _, err := New(recs, nil, nil, nil); err == nil {
			t.Errorf("%s loaded", recs)
		}
	}
}

func TestExpiry(t *testing.T) {
	now := time.Date(2036, 10, 18, 0, 0, 0, 0, time.UTC)
	cutoff := Cutoff(now)
	if cutoff != time.Date(2026, 10, 18, 0, 0, 0, 0, time.UTC) {
		t.Fatalf("cutoff %v, want ten years before", cutoff)
	}
	m := &mem{}
	at := []time.Time{cutoff.Add(-time.Hour), cutoff.Add(-time.Second), cutoff, cutoff.Add(time.Hour)}
	i := 0
	tr, _ := New(nil, m.append, nil, func() time.Time { v := at[i]; i++; return v })
	for range at {
		tr.Record(Entry{Event: Login})
	}
	kept, dropped, err := Unexpired(m.records, cutoff)
	if err != nil || dropped != 2 || len(kept) != 3 {
		t.Fatalf("kept %d, dropped %d, %v", len(kept), dropped, err)
	}
	// The trail rebuilt from what is kept starts with the marker, which
	// takes the last removed entry's number and time.
	again, err := New(kept, m.append, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	got := again.List(0, 10)
	if len(got) != 3 || got[2].Event != Expired || got[2].Seq != 2 || got[2].Detail["removed"] != "2" || !got[2].At.Equal(at[1]) || got[1].Seq != 3 {
		t.Fatalf("after expiry %+v", got)
	}
	if k, d, _ := Unexpired(m.records[2:], cutoff); d != 0 || len(k) != 2 {
		t.Fatal("nothing expired, yet records changed")
	}
	if k, d, _ := Unexpired(nil, cutoff); d != 0 || k != nil {
		t.Fatal("an empty journal changed")
	}
	if err := tr.Exclusive(func() ([][]byte, bool, error) { return kept, true, nil }); err != nil {
		t.Fatal(err)
	}
	if got := tr.List(0, 10); len(got) != 3 || got[2].Event != Expired {
		t.Fatalf("memory after expiry %+v", got)
	}
	if err := tr.Exclusive(func() ([][]byte, bool, error) { return nil, false, errors.New("no") }); err == nil {
		t.Fatal("the error of fn was dropped")
	}
	if err := tr.Exclusive(func() ([][]byte, bool, error) { return [][]byte{[]byte("{")}, true, nil }); err == nil {
		t.Fatal("a damaged record was accepted")
	}
	if k, dropped, _ := Unexpired(m.records, now.AddDate(1, 0, 0)); dropped != 4 || len(k) != 1 {
		t.Fatalf("everything expired: dropped %d, kept %d", dropped, len(k))
	}
	if _, _, err := Unexpired([][]byte{[]byte("{")}, cutoff); err == nil {
		t.Fatal("a damaged record was accepted")
	}
}

// TestListAllReachesTheJournal covers ADR-0122: an entry pushed out of the
// in-memory window by many later ones is still listed, from the journal.
func TestListAllReachesTheJournal(t *testing.T) {
	m := &mem{}
	tr, _ := New(nil, m.append, nil, clock())
	read := func(each func([]byte) error) error {
		for _, r := range m.records {
			if err := each(r); err != nil {
				return err
			}
		}
		return nil
	}
	design := func(e Entry) bool { return e.Event == DesignChanged }
	tr.Record(Entry{Event: DesignChanged, Detail: map[string]string{"theme": "swiss"}})
	tr.Record(Entry{Event: DesignChanged})
	tr.Record(Entry{Event: DesignChanged})
	if got, err := tr.ListAll(0, 10, design); err != nil || len(got) != 3 {
		t.Fatalf("within the window: %d, %v", len(got), err)
	}
	for range Retained {
		tr.Record(Entry{Event: LoginFailed})
	}
	if got := tr.ListWhere(0, 10, design); len(got) != 0 {
		t.Fatalf("the window still holds %d design entries", len(got))
	}
	if got, err := tr.ListAll(0, 10, design); err != nil || len(got) != 0 {
		t.Fatalf("without a journal reader: %d, %v", len(got), err)
	}
	tr.ReadJournal(read)
	got, err := tr.ListAll(0, 10, design)
	if err != nil || len(got) != 3 || got[0].Seq != 3 || got[2].Seq != 1 || got[2].Detail["theme"] != "swiss" {
		t.Fatalf("from the journal: %+v, %v", got, err)
	}
	page, _ := tr.ListAll(0, 2, design)
	rest, _ := tr.ListAll(page[1].Seq, 2, design)
	if len(page) != 2 || page[0].Seq != 3 || page[1].Seq != 2 || len(rest) != 1 || rest[0].Seq != 1 {
		t.Fatalf("paged: %+v then %+v", page, rest)
	}
	// Unfiltered, the window's entries come first and the journal adds the rest.
	all, _ := tr.ListAll(0, Retained+10, nil)
	if len(all) != Retained+3 || all[0].Seq != Retained+3 || all[len(all)-1].Seq != 1 {
		t.Fatalf("%d entries, newest %d, oldest %d", len(all), all[0].Seq, all[len(all)-1].Seq)
	}
	if got, _ := tr.ListAll(0, 5, nil); len(got) != 5 || got[0].Seq != Retained+3 {
		t.Fatal("a full page from the window read the journal")
	}
	m.records[0] = []byte("{")
	if _, err := tr.ListAll(0, 10, design); err == nil {
		t.Fatal("a damaged journal listed")
	}
	tr.ReadJournal(func(func([]byte) error) error { return errors.New("disk gone") })
	if _, err := tr.ListAll(0, 10, design); err == nil || !strings.Contains(err.Error(), "disk gone") {
		t.Fatalf("a failed read: %v", err)
	}
	// After a rewrite that fits the window, nothing older is left to read.
	if err := tr.Exclusive(func() ([][]byte, bool, error) { return m.records[len(m.records)-3:], true, nil }); err != nil {
		t.Fatal(err)
	}
	if got, err := tr.ListAll(0, 10, nil); err != nil || len(got) != 3 {
		t.Fatalf("after the rewrite: %d, %v", len(got), err)
	}
}
