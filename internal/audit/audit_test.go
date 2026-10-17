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
	for _, recs := range [][][]byte{{[]byte("{")}, {[]byte(`{"seq":2}`)}} {
		if _, err := New(recs, nil, nil, nil); err == nil {
			t.Errorf("%s loaded", recs)
		}
	}
}
