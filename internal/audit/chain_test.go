package audit

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func sum(raw []byte) string { s := sha256.Sum256(raw); return hex.EncodeToString(s[:]) }

func chained(t *testing.T, n int) (*mem, *Trail) {
	t.Helper()
	m := &mem{}
	tr, err := New(nil, m.append, nil, clock())
	if err != nil {
		t.Fatal(err)
	}
	for range n {
		tr.Record(Entry{Event: DesignChanged, Detail: map[string]string{"theme": "swiss"}})
	}
	return m, tr
}

func decode(t *testing.T, raw []byte) Entry {
	t.Helper()
	var e Entry
	if err := json.Unmarshal(raw, &e); err != nil {
		t.Fatal(err)
	}
	return e
}

func encode(t *testing.T, e Entry) []byte {
	t.Helper()
	raw, err := json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func verify(t *testing.T, records [][]byte, now time.Time) Check {
	t.Helper()
	c, err := Verify(Each(records), now)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

var later = time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)

// TestEveryEntryNamesTheOneBeforeIt covers ADR-0123: each entry carries
// the hash of the record before it, and the log line its own.
func TestEveryEntryNamesTheOneBeforeIt(t *testing.T) {
	var logs bytes.Buffer
	m := &mem{}
	tr, _ := New(nil, m.append, slog.New(slog.NewTextHandler(&logs, nil)), clock())
	for range 3 {
		tr.Record(Entry{Event: Login})
	}
	if e := decode(t, m.records[0]); e.Prev != Genesis {
		t.Fatalf("the first entry names %q", e.Prev)
	}
	if e := decode(t, m.records[2]); e.Prev != sum(m.records[1]) {
		t.Fatal("the third entry does not name the second")
	}
	if !strings.Contains(logs.String(), "hash="+sum(m.records[2])) || !strings.Contains(logs.String(), "seq=3") {
		t.Fatalf("the log lines do not witness the links: %s", logs.String())
	}
	c := verify(t, m.records, later)
	if c.Broken != 0 || c.Entries != 3 || c.Unchained != 0 || c.Head != sum(m.records[2]) || c.HeadSeq != 3 {
		t.Fatalf("%+v", c)
	}
	// A trail rebuilt from its journal goes on with the same chain.
	again, _ := New(m.records, m.append, nil, clock())
	again.Record(Entry{Event: Logout})
	if e := decode(t, m.records[3]); e.Prev != sum(m.records[2]) {
		t.Fatal("after a restart the chain began again")
	}
	if _, err := tr.Verify(later); err == nil {
		t.Fatal("verified without a journal reader")
	}
	tr.ReadJournal(Each(m.records))
	if c, err := tr.Verify(later); err != nil || c.Broken != 0 || c.Entries != 4 {
		t.Fatalf("%+v, %v", c, err)
	}
}

func TestAChangedEntryBreaksTheChain(t *testing.T) {
	m, _ := chained(t, 4)
	recs := append([][]byte(nil), m.records...)
	e := decode(t, recs[1])
	e.Detail["theme"] = "comic"
	recs[1] = encode(t, e)
	if c := verify(t, recs, later); c.Broken != 3 || c.Reason != BrokenLink || c.Entries != 2 {
		t.Fatalf("a changed entry: %+v", c)
	}
	// Removing an entry leaves a gap; renumbering the rest breaks the link.
	if c := verify(t, [][]byte{m.records[0], m.records[2]}, later); c.Broken != 3 || c.Reason != BrokenGap {
		t.Fatalf("a removed entry: %+v", c)
	}
	moved := decode(t, m.records[2])
	moved.Seq = 2
	if c := verify(t, [][]byte{m.records[0], encode(t, moved)}, later); c.Broken != 2 || c.Reason != BrokenLink {
		t.Fatalf("a renumbered entry: %+v", c)
	}
	// The start cut off without a marker.
	if c := verify(t, m.records[2:], later); c.Broken != 3 || c.Reason != BrokenStart {
		t.Fatalf("a cut start: %+v", c)
	}
	// A link taken away after the chain began.
	bare := decode(t, m.records[3])
	bare.Prev = ""
	if c := verify(t, append(append([][]byte(nil), m.records[:3]...), encode(t, bare)), later); c.Broken != 4 || c.Reason != BrokenUnlinked {
		t.Fatalf("an unlinked entry: %+v", c)
	}
	// An expiry marker in the middle.
	fake := encode(t, Entry{Seq: 2, Event: Expired, Detail: map[string]string{"before": "2000-01-01T00:00:00Z"}})
	if c := verify(t, [][]byte{m.records[0], fake}, later); c.Broken != 2 || c.Reason != BrokenMarker {
		t.Fatalf("a marker in the middle: %+v", c)
	}
	if _, err := Verify(Each([][]byte{[]byte("{")}), later); err == nil {
		t.Fatal("a damaged record verified")
	}
	if c := verify(t, nil, later); c.Broken != 0 || c.Entries != 0 {
		t.Fatalf("an empty journal: %+v", c)
	}
}

// TestTheChainBeginsAfterOlderEntries covers a journal from before the
// chain: its entries count as unchained, and the first new one names the
// last of them.
func TestTheChainBeginsAfterOlderEntries(t *testing.T) {
	at := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	legacy := [][]byte{encode(t, Entry{Seq: 1, At: at, Event: Login}), encode(t, Entry{Seq: 2, At: at, Event: Logout})}
	m := &mem{records: append([][]byte(nil), legacy...)}
	tr, err := New(m.records, m.append, nil, clock())
	if err != nil {
		t.Fatal(err)
	}
	tr.Record(Entry{Event: Login})
	if e := decode(t, m.records[2]); e.Prev != sum(legacy[1]) {
		t.Fatal("the first linked entry does not name the last older one")
	}
	if c := verify(t, m.records, later); c.Broken != 0 || c.Unchained != 2 || c.Entries != 3 {
		t.Fatalf("%+v", c)
	}
	// The older entries are covered only at the last one.
	changed := decode(t, legacy[1])
	changed.Event = Login
	recs := [][]byte{legacy[0], encode(t, changed), m.records[2]}
	if c := verify(t, recs, later); c.Broken != 3 || c.Reason != BrokenLink {
		t.Fatalf("%+v", c)
	}
}

// TestExpiryKeepsTheChain covers expiry (ADR-0029) with the chain: the
// marker carries the link of the last removed entry, also when it removes
// an earlier marker.
func TestExpiryKeepsTheChain(t *testing.T) {
	m := &mem{}
	base := time.Date(2026, 10, 17, 0, 0, 0, 0, time.UTC)
	at := []time.Time{base, base.Add(time.Hour), base.AddDate(1, 0, 0), base.AddDate(2, 0, 0)}
	i := 0
	tr, _ := New(nil, m.append, nil, func() time.Time { v := at[i]; i++; return v })
	for range at {
		tr.Record(Entry{Event: Login})
	}
	now := base.AddDate(10, 6, 0)
	kept, n, err := Unexpired(m.records, Cutoff(now))
	if err != nil || n != 2 {
		t.Fatalf("dropped %d, %v", n, err)
	}
	if marker := decode(t, kept[0]); marker.Detail["hash"] != sum(m.records[1]) {
		t.Fatal("the marker does not carry the last removed entry's link")
	}
	if c := verify(t, kept, now); c.Broken != 0 || c.Entries != 3 || c.Head != sum(m.records[3]) {
		t.Fatalf("after expiry: %+v", c)
	}
	// A year on, the marker and the next entry go; the chain still holds.
	now = now.AddDate(1, 0, 0)
	again, n, _ := Unexpired(kept, Cutoff(now))
	if c := verify(t, again, now); n != 2 || c.Broken != 0 || c.Entries != 2 {
		t.Fatalf("after a second expiry (%d): %+v", n, c)
	}
	// Only the marker expires: the new one carries the same link.
	only, n, _ := Unexpired(kept, decode(t, kept[0]).At.Add(time.Minute))
	if c := verify(t, only, now); n != 1 || c.Broken != 0 || decode(t, only[0]).Detail["hash"] != decode(t, kept[0]).Detail["hash"] {
		t.Fatalf("a marker replaced (%d): %+v", n, c)
	}
	// A marker that removed entries younger than the retention period is
	// not an expiry.
	forged, _, _ := Unexpired(m.records, at[3])
	if c := verify(t, forged, now.AddDate(-10, 0, 0)); c.Broken != 3 || c.Reason != BrokenTooYoung {
		t.Fatalf("a forged marker: %+v", c)
	}
	bad := decode(t, kept[0])
	bad.Detail["before"] = "soon"
	if c := verify(t, [][]byte{encode(t, bad)}, now); c.Reason != BrokenTooYoung {
		t.Fatalf("a marker without its time: %+v", c)
	}
	// An entry after the marker older than the marker's cutoff.
	early := decode(t, kept[1])
	early.At = at[0]
	if c := verify(t, [][]byte{kept[0], encode(t, early)}, now); c.Broken != 3 || c.Reason != BrokenTooYoung {
		t.Fatalf("an entry older than the cutoff after the marker: %+v", c)
	}
}
