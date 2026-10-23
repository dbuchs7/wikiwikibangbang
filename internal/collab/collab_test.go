package collab

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/dbuchs7/wikiwikibangbang/internal/budget"
	"github.com/dbuchs7/wikiwikibangbang/internal/item"
)

// fakeStore is a body store with per-principal access and a commit log.
type fakeStore struct {
	body    item.Doc
	gone    bool
	readers map[string]bool
	editors map[string]bool
	commits []commit
	fail    error
}

type commit struct {
	by   string
	text []string
}

func (f *fakeStore) Access(_ item.ID, p string) (bool, bool) {
	if f.gone {
		return false, false
	}
	return f.readers[p] || f.editors[p], f.editors[p]
}

func (f *fakeStore) Body(item.ID) (item.Doc, error) {
	if f.gone {
		return item.Doc{}, item.ErrNotFound
	}
	return cloneDoc(f.body), nil
}

func (f *fakeStore) Commit(_ item.ID, by string, d item.Doc) error {
	if f.fail != nil {
		return f.fail
	}
	f.body = cloneDoc(d)
	f.commits = append(f.commits, commit{by, texts(d)})
	return nil
}

func texts(d item.Doc) []string {
	var out []string
	for _, b := range d.Blocks {
		out = append(out, b.Text)
	}
	return out
}

func para(text string) item.Block { return item.Block{Kind: item.Paragraph, Text: text} }

const page item.ID = "page"

type fixture struct {
	t     *testing.T
	store *fakeStore
	hub   *Hub
	now   time.Time
}

func newFixture(t *testing.T, blocks ...string) *fixture {
	f := &fixture{t: t, now: time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)}
	f.store = &fakeStore{readers: map[string]bool{"vera": true}, editors: map[string]bool{"anna": true, "bert": true}}
	for _, b := range blocks {
		f.store.body.Blocks = append(f.store.body.Blocks, para(b))
	}
	f.hub = NewHub(f.store, WithClock(func() time.Time { return f.now }))
	return f
}

// conn is a joined client that collects its events.
type conn struct {
	f      *fixture
	sub    *Sub
	who    string
	events []Event
	closed bool
}

func (f *fixture) join(who string) *conn {
	f.t.Helper()
	sub, err := f.hub.Join(page, who, who)
	if err != nil {
		f.t.Fatalf("join %s: %v", who, err)
	}
	c := &conn{f: f, sub: sub, who: who}
	c.drain()
	return c
}

func (c *conn) drain() {
	for {
		select {
		case e, ok := <-c.sub.C:
			if !ok {
				c.closed = true
				return
			}
			c.events = append(c.events, e)
		default:
			return
		}
	}
}

func (c *conn) apply(ops ...Op) error {
	err := c.f.hub.Apply(page, c.sub.Client, c.who, ops)
	c.drain()
	return err
}

// ids are the session's block IDs in order, from the snapshot.
func (c *conn) ids() []string { return c.events[0].Order }

func (c *conn) last(kind string) Event {
	c.drain()
	for i := len(c.events) - 1; i >= 0; i-- {
		if c.events[i].Kind == kind {
			return c.events[i]
		}
	}
	c.f.t.Fatalf("%s saw no %s event", c.who, kind)
	return Event{}
}

func noErr(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func TestJoinSendsASnapshotAndPresence(t *testing.T) {
	f := newFixture(t, "one", "two")
	anna := f.join("anna")
	snap := anna.events[0]
	if snap.Kind != Snapshot || snap.Client != anna.sub.Client || len(snap.Blocks) != 2 || snap.Blocks[1].Block.Text != "two" {
		t.Fatalf("snapshot %+v", snap)
	}
	vera := f.join("vera")
	if !snap.Editable || vera.events[0].Editable {
		t.Fatal("the snapshot misstates who may edit")
	}
	if p := anna.last(Presence).Participants; len(p) != 2 || p[0].Name != "anna" || p[1].Name != "vera" {
		t.Fatalf("presence %+v", p)
	}
	if _, err := f.hub.Join(page, "carl", "carl"); !errors.Is(err, item.ErrNotFound) {
		t.Fatalf("an outsider joins: %v", err)
	}
}

func TestEditingABlockLocksItForOthers(t *testing.T) {
	f := newFixture(t, "one", "two")
	anna, bert := f.join("anna"), f.join("bert")
	ids := anna.ids()

	noErr(t, anna.apply(Op{Op: "set", ID: ids[0], Block: para("one, edited")}))
	if e := bert.last(Set); e.Block.Text != "one, edited" || e.Holder.Name != "anna" {
		t.Fatalf("bert sees %+v", e)
	}
	if err := bert.apply(Op{Op: "set", ID: ids[0], Block: para("mine")}); !errors.Is(err, ErrLocked) {
		t.Fatalf("bert writes a locked block: %v", err)
	}
	if err := bert.apply(Op{Op: "delete", ID: ids[0]}); !errors.Is(err, ErrLocked) {
		t.Fatalf("bert deletes a locked block: %v", err)
	}
	// A different block is free.
	noErr(t, bert.apply(Op{Op: "set", ID: ids[1], Block: para("two, by bert")}))
	if len(f.store.commits) != 0 {
		t.Fatalf("committed before any release: %v", f.store.commits)
	}
	// A viewer may watch but not edit.
	vera := f.join("vera")
	if err := vera.apply(Op{Op: "set", ID: ids[1], Block: para("x")}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("viewer edits: %v", err)
	}
}

func TestReleaseCommitsOnlyStableTextAsTheReleaser(t *testing.T) {
	f := newFixture(t, "one", "two")
	anna, bert := f.join("anna"), f.join("bert")
	ids := anna.ids()
	noErr(t, anna.apply(Op{Op: "set", ID: ids[0], Block: para("one by anna")}))
	noErr(t, bert.apply(Op{Op: "set", ID: ids[1], Block: para("two by bert, unfinished")}))

	noErr(t, anna.apply(Op{Op: "release", ID: ids[0]}))
	want := []commit{{"anna", []string{"one by anna", "two"}}}
	if !slices.EqualFunc(f.store.commits, want, func(a, b commit) bool { return a.by == b.by && slices.Equal(a.text, b.text) }) {
		t.Fatalf("commits %v, want %v", f.store.commits, want)
	}
	if e := bert.last(Lock); e.ID != ids[0] || e.Holder != nil {
		t.Fatalf("release not broadcast: %+v", e)
	}
	if e := bert.last(Committed); e.By != "anna" {
		t.Fatalf("commit not broadcast: %+v", e)
	}
	// The block is free again for bert.
	noErr(t, bert.apply(Op{Op: "set", ID: ids[0], Block: para("one by bert")}, Op{Op: "release", ID: ids[0]}, Op{Op: "release", ID: ids[1]}))
	if got := f.store.commits[1]; got.by != "bert" || !slices.Equal(got.text, []string{"one by bert", "two by bert, unfinished"}) {
		t.Fatalf("second commit %v", got)
	}
}

func TestInsertAndDelete(t *testing.T) {
	f := newFixture(t, "one", "two")
	anna, bert := f.join("anna"), f.join("bert")
	ids := anna.ids()
	noErr(t, anna.apply(Op{Op: "insert", ID: "new1", After: ids[0], Block: para("between")}))
	if e := bert.last(Insert); !slices.Equal(e.Order, []string{ids[0], "new1", ids[1]}) || e.Holder.Name != "anna" {
		t.Fatalf("insert %+v", e)
	}
	// An inserted block is not committed until it is released.
	noErr(t, bert.apply(Op{Op: "delete", ID: ids[1]}))
	if got := f.store.commits; len(got) != 1 || got[0].by != "bert" || !slices.Equal(got[0].text, []string{"one"}) {
		t.Fatalf("delete commit %v", got)
	}
	noErr(t, anna.apply(Op{Op: "release", ID: "new1"}))
	if got := f.store.commits[1]; !slices.Equal(got.text, []string{"one", "between"}) {
		t.Fatalf("insert commit %v", got)
	}
	for _, bad := range []Op{
		{Op: "insert", ID: "new1", Block: para("taken")},
		{Op: "insert", ID: "has space", Block: para("x")},
		{Op: "insert", ID: "n2", After: "nope", Block: para("x")},
		{Op: "set", ID: "nope", Block: para("x")},
		{Op: "frobnicate", ID: ids[0]},
	} {
		if err := anna.apply(bad); !errors.Is(err, ErrInvalid) {
			t.Errorf("%+v: %v", bad, err)
		}
	}
}

func TestABatchAppliesWhollyOrNotAtAll(t *testing.T) {
	f := newFixture(t, "one", "two")
	anna, bert := f.join("anna"), f.join("bert")
	ids := anna.ids()
	noErr(t, bert.apply(Op{Op: "lock", ID: ids[1]}))
	// Merging block two into block one needs both locks.
	err := anna.apply(Op{Op: "set", ID: ids[0], Block: para("onetwo")}, Op{Op: "delete", ID: ids[1]})
	if !errors.Is(err, ErrLocked) {
		t.Fatalf("err %v", err)
	}
	if snap := f.join("vera").events[0]; snap.Blocks[0].Block.Text != "one" || snap.Blocks[0].Holder != nil || len(snap.Blocks) != 2 {
		t.Fatalf("a failed batch changed the session: %+v", snap.Blocks)
	}
}

func TestBadBlocksAreRefusedWhenTyped(t *testing.T) {
	f := newFixture(t, "one")
	anna := f.join("anna")
	id := anna.ids()[0]
	for _, b := range []item.Block{
		{Kind: "nope"},
		{Kind: item.Paragraph, Text: "x", Level: 2},
		{Kind: item.Paragraph, Text: "x", Marks: []item.Mark{{Type: item.MarkLink, Start: 0, End: 1, Href: "javascript:alert(1)"}}},
		{Kind: item.Unsupported, Text: "<macro/>"},
		{Kind: item.Embed, Ref: page},
	} {
		if err := anna.apply(Op{Op: "set", ID: id, Block: b}); err == nil {
			t.Errorf("%+v accepted", b)
		}
	}
}

func TestLeavingCommitsTheDraftsOfTheLeaver(t *testing.T) {
	f := newFixture(t, "one")
	anna, bert := f.join("anna"), f.join("bert")
	id := anna.ids()[0]
	noErr(t, anna.apply(Op{Op: "set", ID: id, Block: para("typed, then the tab closed")}))
	anna.sub.Leave()
	if got := f.store.commits; len(got) != 1 || got[0].by != "anna" {
		t.Fatalf("commits %v", got)
	}
	if e := bert.last(Lock); e.Holder != nil {
		t.Fatalf("the lock was not freed: %+v", e)
	}
	if p := bert.last(Presence).Participants; len(p) != 1 || p[0].Name != "bert" {
		t.Fatalf("presence %+v", p)
	}
	bert.sub.Leave()
	if len(f.hub.Sessions()) != 0 {
		t.Fatal("an empty session stays open")
	}
}

func TestIdleLocksLapseAndCommit(t *testing.T) {
	f := newFixture(t, "one")
	anna, bert := f.join("anna"), f.join("bert")
	id := anna.ids()[0]
	noErr(t, anna.apply(Op{Op: "set", ID: id, Block: para("left at the desk")}))
	f.now = f.now.Add(LockTTL / 2)
	f.hub.Tick()
	if err := bert.apply(Op{Op: "set", ID: id, Block: para("x")}); !errors.Is(err, ErrLocked) {
		t.Fatalf("lock lapsed early: %v", err)
	}
	f.now = f.now.Add(LockTTL)
	f.hub.Tick()
	if got := f.store.commits; len(got) != 1 || got[0].by != "anna" || got[0].text[0] != "left at the desk" {
		t.Fatalf("commits %v", got)
	}
	noErr(t, bert.apply(Op{Op: "set", ID: id, Block: para("now bert")}))
}

func TestAFailedReleaseKeepsTheBlockForItsHolder(t *testing.T) {
	f := newFixture(t, "one")
	anna := f.join("anna")
	id := anna.ids()[0]
	noErr(t, anna.apply(Op{Op: "set", ID: id, Block: para("rejected by the store")}))
	f.store.fail = errors.New("validation failed")
	if err := anna.apply(Op{Op: "release", ID: id}); err == nil {
		t.Fatal("the failure was hidden")
	}
	f.store.fail = nil
	noErr(t, anna.apply(Op{Op: "set", ID: id, Block: para("fixed")}, Op{Op: "release", ID: id}))
	if got := f.store.commits; len(got) != 1 || got[0].text[0] != "fixed" {
		t.Fatalf("commits %v", got)
	}
}

func TestAChangeOutsideTheSessionResetsIt(t *testing.T) {
	f := newFixture(t, "one")
	anna := f.join("anna")
	id := anna.ids()[0]
	noErr(t, anna.apply(Op{Op: "set", ID: id, Block: para("in the session")}))
	if !f.hub.Busy(page) {
		t.Fatal("a held lock is not busy")
	}
	f.store.body = item.Doc{Blocks: []item.Block{para("written elsewhere")}}
	if err := anna.apply(Op{Op: "release", ID: id}); !errors.Is(err, ErrDiverged) {
		t.Fatalf("err %v", err)
	}
	if snap := anna.last(Snapshot); snap.Blocks[0].Block.Text != "written elsewhere" {
		t.Fatalf("not reset: %+v", snap)
	}
	if len(f.store.commits) != 0 {
		t.Fatalf("overwrote the outside change: %v", f.store.commits)
	}
}

// A body written outside an idle session, by a commit or a restored
// version, reaches the session when someone joins: the joiner and those
// already in it see the stored body.
func TestJoiningAnIdleSessionTakesTheStoredBody(t *testing.T) {
	f := newFixture(t, "one")
	vera := f.join("vera")
	f.store.body = item.Doc{Blocks: []item.Block{para("restored")}}
	anna := f.join("anna")
	if snap := anna.last(Snapshot); len(snap.Blocks) != 1 || snap.Blocks[0].Block.Text != "restored" {
		t.Fatalf("joiner sees %+v", snap)
	}
	if snap := vera.last(Snapshot); snap.Blocks[0].Block.Text != "restored" {
		t.Fatalf("the one already in sees %+v", snap)
	}
	// A busy session keeps its work; its next commit resets it.
	id := anna.ids()[0]
	noErr(t, anna.apply(Op{Op: "set", ID: id, Block: para("typing")}))
	f.store.body = item.Doc{Blocks: []item.Block{para("elsewhere")}}
	bert := f.join("bert")
	if snap := bert.last(Snapshot); snap.Blocks[0].Block.Text != "typing" {
		t.Fatalf("a busy session was reset on join: %+v", snap)
	}
}

func TestErasureAndLostAccessEndTheSession(t *testing.T) {
	f := newFixture(t, "secret")
	anna, vera := f.join("anna"), f.join("vera")
	id := anna.ids()[0]
	noErr(t, anna.apply(Op{Op: "set", ID: id, Block: para("draft")}))

	// vera loses read access: only her stream ends.
	delete(f.store.readers, "vera")
	f.hub.Revalidate()
	vera.drain()
	anna.drain()
	if !vera.closed || anna.closed {
		t.Fatalf("vera closed %v, anna closed %v", vera.closed, anna.closed)
	}
	// anna loses edit access: her lock and draft are discarded.
	delete(f.store.editors, "anna")
	f.store.readers["anna"] = true
	f.hub.Revalidate()
	if e := anna.last(Set); e.Block.Text != "secret" {
		t.Fatalf("draft kept: %+v", e)
	}
	// The item is purged: the session closes and forgets its text.
	f.store.gone = true
	f.hub.Revalidate()
	anna.drain()
	if !anna.closed || len(f.hub.Sessions()) != 0 {
		t.Fatalf("closed %v, sessions %v", anna.closed, f.hub.Sessions())
	}
	if e := anna.last(Closed); e.Reason == "" {
		t.Fatal("no reason")
	}
}

func TestCloseDiscardsAtOnce(t *testing.T) {
	f := newFixture(t, "one")
	anna := f.join("anna")
	noErr(t, anna.apply(Op{Op: "set", ID: anna.ids()[0], Block: para("draft")}))
	f.hub.Close(page, "redacted")
	anna.drain()
	if !anna.closed || len(f.store.commits) != 0 || len(f.hub.Sessions()) != 0 {
		t.Fatalf("closed %v, commits %v", anna.closed, f.store.commits)
	}
	if err := anna.apply(Op{Op: "lock", ID: "x"}); !errors.Is(err, ErrNotJoined) {
		t.Fatalf("apply after close: %v", err)
	}
}

func TestASlowClientIsDropped(t *testing.T) {
	f := newFixture(t, "one")
	anna := f.join("anna")
	slow, err := f.hub.Join(page, "bert", "bert") // never reads
	noErr(t, err)
	id := anna.ids()[0]
	for i := range buffer + 10 {
		noErr(t, anna.apply(Op{Op: "set", ID: id, Block: para(string(rune('a' + i%26)))}))
	}
	n := 0
	for range slow.C {
		n++
	}
	if n > buffer {
		t.Fatalf("slow client got %d events", n)
	}
}

func TestALapsedInsertThatCannotBeStoredDisappears(t *testing.T) {
	f := newFixture(t, "one")
	anna, bert := f.join("anna"), f.join("bert")
	noErr(t, anna.apply(Op{Op: "insert", ID: "n1", After: anna.ids()[0], Block: para("new")}))
	f.store.fail = errors.New("store refuses")
	f.now = f.now.Add(2 * LockTTL)
	f.hub.Tick()
	if e := bert.last(Delete); e.ID != "n1" || !slices.Equal(e.Order, anna.ids()) {
		t.Fatalf("delete %+v", e)
	}
}

func TestShutdownCommitsAndEndsEveryStream(t *testing.T) {
	f := newFixture(t, "one")
	anna, bert := f.join("anna"), f.join("bert")
	noErr(t, anna.apply(Op{Op: "set", ID: anna.ids()[0], Block: para("saved on shutdown")}))
	f.hub.Shutdown()
	anna.drain()
	bert.drain()
	if !anna.closed || !bert.closed || len(f.hub.Sessions()) != 0 {
		t.Fatal("streams or sessions left open")
	}
	if got := f.store.commits; len(got) != 1 || got[0].by != "anna" {
		t.Fatalf("commits %v", got)
	}
}

// TestWatchersFollowWithoutEditing: a watcher sees the text as it changes
// and who edits, edits nothing even when it may, is no participant, and
// takes no room from editors (ADR-0115).
func TestWatchersFollowWithoutEditing(t *testing.T) {
	f := newFixture(t, "one", "two")
	anna := f.join("anna")
	sub, err := f.hub.Watch(page, "bert", "bert")
	noErr(t, err)
	bert := &conn{f: f, sub: sub, who: "bert"}
	bert.drain()
	if bert.events[0].Kind != Snapshot || bert.events[0].Editable {
		t.Fatalf("a watcher's snapshot: %+v", bert.events[0])
	}
	if p := bert.events[0].Participants; len(p) != 1 || p[0].Name != "anna" {
		t.Fatalf("the watcher is told who edits: %+v", p)
	}
	if p := anna.last(Presence).Participants; len(p) != 1 || p[0].Name != "anna" {
		t.Fatalf("a watcher counts as a participant: %+v", p)
	}
	ids := anna.ids()
	noErr(t, anna.apply(Op{Op: "set", ID: ids[0], Block: para("one, edited")}))
	if e := bert.last(Set); e.Block.Text != "one, edited" || e.Holder.Name != "anna" {
		t.Fatalf("the watcher sees %+v", e)
	}
	if err := bert.apply(Op{Op: "set", ID: ids[1], Block: para("x")}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("a watcher who may edit edits: %v", err)
	}
	if _, err := f.hub.Watch(page, "carl", "carl"); !errors.Is(err, item.ErrNotFound) {
		t.Fatalf("an outsider watches: %v", err)
	}

	// Watchers fill their own room, not the editors'.
	b := budget.Get()
	b.LiveClients = 3
	budget.Set(b)
	t.Cleanup(func() { budget.Set(budget.Default()) })
	_, err = f.hub.Watch(page, "vera", "vera")
	noErr(t, err)
	_, err = f.hub.Watch(page, "vera", "vera")
	noErr(t, err)
	if _, err := f.hub.Watch(page, "vera", "vera"); !errors.Is(err, ErrBusy) {
		t.Fatalf("a fourth watcher: %v", err)
	}
	f.join("bert")
	f.join("bert")
	bert.sub.Leave()
	if p := anna.last(Presence).Participants; len(p) != 2 {
		t.Fatalf("leaving watchers and joining editors: %+v", p)
	}
}

// TestReleasingABlockNoLongerHeldIsAnswered: a client that releases a
// block it no longer holds, e.g. after its lock lapsed or was taken over,
// is told who holds it, so it does not wait for its release forever.
func TestReleasingABlockNoLongerHeldIsAnswered(t *testing.T) {
	f := newFixture(t, "one", "two")
	anna, bert := f.join("anna"), f.join("bert")
	ids := anna.ids()
	noErr(t, bert.apply(Op{Op: "set", ID: ids[0], Block: para("bert's")}))
	n := len(anna.events)
	noErr(t, anna.apply(Op{Op: "release", ID: ids[1]}))
	if len(anna.events) == n {
		t.Fatal("a release of a block nobody holds goes unanswered")
	}
	if e := anna.last(Lock); e.ID != ids[1] || e.Holder != nil {
		t.Fatalf("answer %+v", e)
	}
	noErr(t, anna.apply(Op{Op: "release", ID: ids[0]}))
	if e := anna.last(Lock); e.ID != ids[0] || e.Holder == nil || e.Holder.Name != "bert" {
		t.Fatalf("answer for a block bert holds %+v", e)
	}
	if len(f.store.commits) != 0 {
		t.Fatalf("releasing what one does not hold commits: %v", f.store.commits)
	}
}
