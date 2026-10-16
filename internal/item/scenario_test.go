package item

import (
	"errors"
	"fmt"
	"testing"
	"time"
)

// newTestStore returns a store with a deterministic clock and ID generator,
// so that tests and replays produce identical results.
func newTestStore(t *testing.T) *Store {
	t.Helper()
	n := 0
	clock := time.Date(2026, 10, 16, 9, 0, 0, 0, time.UTC)
	s := NewStore(
		WithClock(func() time.Time { clock = clock.Add(time.Second); return clock }),
		WithIDs(func() ID { n++; return ID(fmt.Sprintf("id%03d", n)) }),
	)
	return s
}

// builtinTypes are the reference types of the scenario: a page (document
// heavy, optional review workflow), a task (field and workflow heavy) and a
// decision (both). They are ordinary type definitions, not special code (D1, D6).
func builtinTypes() []TypeDef {
	return []TypeDef{
		{
			Name: "page", Body: true,
			Fields: []FieldDef{{Name: "owner", Kind: KindUser}},
		},
		{
			Name: "task", Body: true,
			Fields: []FieldDef{
				{Name: "assignee", Kind: KindUser},
				{Name: "estimate", Kind: KindNumber},
				{Name: "priority", Kind: KindEnum, Values: []string{"low", "normal", "high"}, Required: true},
			},
			Workflow: &Workflow{
				Initial: "open",
				States:  []string{"open", "in progress", "done"},
				Transitions: []Transition{
					{Name: "start", From: []string{"open"}, To: "in progress", Requires: []string{"assignee"}},
					{Name: "finish", From: []string{"open", "in progress"}, To: "done"},
					{Name: "reopen", From: []string{"done"}, To: "open"},
				},
			},
		},
		{
			Name: "decision", Body: true,
			Fields: []FieldDef{{Name: "deciders", Kind: KindText, Required: true}},
			Workflow: &Workflow{
				Initial: "proposed",
				States:  []string{"proposed", "accepted", "superseded"},
				Transitions: []Transition{
					{Name: "accept", From: []string{"proposed"}, To: "accepted"},
					{Name: "supersede", From: []string{"accepted"}, To: "superseded"},
				},
			},
		},
	}
}

func setup(t *testing.T) *Store {
	t.Helper()
	s := newTestStore(t)
	for _, td := range builtinTypes() {
		if err := s.DefineType("admin", td); err != nil {
			t.Fatalf("DefineType(%s): %v", td.Name, err)
		}
	}
	for _, sp := range []string{"PLAT", "OPS"} {
		if err := s.CreateSpace("admin", sp, sp+" space"); err != nil {
			t.Fatalf("CreateSpace(%s): %v", sp, err)
		}
	}
	return s
}

// mustItem wraps a call returning (Item, error) and fails the test on error.
func mustItem(t *testing.T) func(Item, error) Item {
	return func(it Item, err error) Item {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return it
	}
}

func noErr(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// TestScenarioPageWithEmbeddedTask is the acceptance scenario of the
// prototype: meeting notes on a page produce a task that lives on the page
// and on the board at the same time, without a copy.
func TestScenarioPageWithEmbeddedTask(t *testing.T) {
	s := setup(t)

	page := mustItem(t)(s.Create("anna", NewItem{
		Space: "PLAT", Type: "page", Title: "Sprint planning 42",
		Properties: Props{"owner": "anna"},
	}))
	if page.Key != "PLAT-1" {
		t.Fatalf("page key = %q, want PLAT-1", page.Key)
	}

	// The task is created inside the page: the page is its containment parent.
	task := mustItem(t)(s.Create("anna", NewItem{
		Space: "PLAT", Type: "task", Parent: page.ID, Title: "Upgrade the database",
		Properties: Props{"priority": "high"},
	}))
	if task.State != "open" {
		t.Fatalf("new task state = %q, want the workflow's initial state", task.State)
	}

	// The page body embeds the task and mentions a person's decision record.
	dec := mustItem(t)(s.Create("ben", NewItem{
		Space: "OPS", Type: "decision", Title: "Use PostgreSQL 18",
		Properties: Props{"deciders": "ops team"},
	}))
	if err := s.CommitBody("anna", page.ID, Doc{Blocks: []Block{
		{Kind: Heading, Text: "Actions"},
		{Kind: Embed, Ref: task.ID},
		{Kind: Paragraph, Text: "Follows the decision", Mentions: []ID{dec.ID}},
	}}); err != nil {
		t.Fatal(err)
	}

	// The board is a query over items: the embedded task is on it, no copy.
	board := s.Find(Query{Space: "PLAT", Type: "task", State: "open"})
	if len(board) != 1 || board[0].ID != task.ID {
		t.Fatalf("board = %v, want exactly the embedded task", ids(board))
	}

	// Working the task on the board is visible from the page: same item.
	if err := s.Transition("ben", task.ID, "start"); !errors.Is(err, ErrValidation) {
		t.Fatalf("start without assignee: err = %v, want ErrValidation", err)
	}
	noErr(t, s.SetProperty("anna", task.ID, "assignee", "ben"))
	noErr(t, s.Transition("ben", task.ID, "start"))
	if got := mustItem(t)(s.Get(task.ID)); got.State != "in progress" {
		t.Fatalf("task state = %q", got.State)
	}

	// Derived relations come from the body: page embeds task, page mentions decision,
	// across space boundaries.
	out := s.Relations(page.ID, Outgoing)
	if !hasRelation(out, "embeds", task.ID) || !hasRelation(out, "mentions", dec.ID) {
		t.Fatalf("derived relations = %+v", out)
	}
	if in := s.Relations(dec.ID, Incoming); !hasRelation(in, "mentions", page.ID) {
		t.Fatalf("decision does not see the mention from the page: %+v", in)
	}

	// A decision has its own lifecycle, through the same mechanism.
	noErr(t, s.Transition("ben", dec.ID, "accept"))

	// One history for every item type (D6).
	h := s.History(task.ID)
	kinds := []EventKind{}
	for _, e := range h {
		kinds = append(kinds, e.Kind)
	}
	want := []EventKind{ItemCreated, BodyCommitted, PropertySet, Transitioned}
	if fmt.Sprint(kinds) != fmt.Sprint(want) {
		t.Fatalf("task history = %v, want %v", kinds, want)
	}
	if h[3].Actor != "ben" || h[3].From != "open" || h[3].To != "in progress" {
		t.Fatalf("transition event = %+v", h[3])
	}
}

// TestReplayRebuildsIdenticalState checks that the state is a pure fold of
// the event log (ADR-0008): replaying the log into an empty store yields the
// same items, keys and relations.
func TestReplayRebuildsIdenticalState(t *testing.T) {
	s := setup(t)
	page := mustItem(t)(s.Create("anna", NewItem{Space: "PLAT", Type: "page", Title: "Runbook"}))
	task := mustItem(t)(s.Create("anna", NewItem{Space: "PLAT", Type: "task", Parent: page.ID, Title: "Patch", Properties: Props{"priority": "low"}}))
	noErr(t, s.CommitBody("anna", page.ID, Doc{Blocks: []Block{{Kind: Embed, Ref: task.ID}}}))
	noErr(t, s.AddRelation("anna", "blocks", task.ID, page.ID))
	noErr(t, s.Move("anna", page.ID, "OPS", ""))

	r, err := Replay(s.Log())
	if err != nil {
		t.Fatalf("Replay: %v", err)
	}
	if got, want := snapshot(r), snapshot(s); got != want {
		t.Fatalf("replayed state differs\n got: %s\nwant: %s", got, want)
	}
}

func ids(items []Item) []ID {
	out := make([]ID, len(items))
	for i, it := range items {
		out[i] = it.ID
	}
	return out
}

func hasRelation(rs []Relation, typ string, other ID) bool {
	for _, r := range rs {
		if r.Type == typ && (r.To == other || r.From == other) {
			return true
		}
	}
	return false
}

// snapshot renders the observable state for comparison.
func snapshot(s *Store) string {
	out := ""
	for _, it := range s.Find(Query{}) {
		out += fmt.Sprintf("%s %s %s %s parent=%s state=%s props=%v body=%d v%d\n",
			it.ID, it.Key, it.Type, it.Title, it.Parent, it.State, it.Properties, len(it.Body.Blocks), it.Version)
		for _, r := range s.Relations(it.ID, Outgoing) {
			out += fmt.Sprintf("  %s -> %s (%s, derived=%v)\n", r.Type, r.To, r.ID, r.Derived)
		}
	}
	return out
}
