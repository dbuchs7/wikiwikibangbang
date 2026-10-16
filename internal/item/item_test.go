package item

import (
	"errors"
	"testing"
	"time"
)

func TestDefineTypeValidation(t *testing.T) {
	wf := func() *Workflow {
		return &Workflow{Initial: "a", States: []string{"a", "b"}, Transitions: []Transition{{Name: "go", From: []string{"a"}, To: "b"}}}
	}
	tests := []struct {
		name string
		td   TypeDef
	}{
		{"empty name", TypeDef{}},
		{"duplicate field", TypeDef{Name: "x", Fields: []FieldDef{{Name: "f", Kind: KindText}, {Name: "f", Kind: KindText}}}},
		{"reserved field", TypeDef{Name: "x", Fields: []FieldDef{{Name: "state", Kind: KindText}}}},
		{"unknown kind", TypeDef{Name: "x", Fields: []FieldDef{{Name: "f", Kind: "colour"}}}},
		{"enum without values", TypeDef{Name: "x", Fields: []FieldDef{{Name: "f", Kind: KindEnum}}}},
		{"unknown initial state", TypeDef{Name: "x", Workflow: &Workflow{Initial: "z", States: []string{"a"}}}},
		{"transition to unknown state", func() TypeDef {
			w := wf()
			w.Transitions[0].To = "z"
			return TypeDef{Name: "x", Workflow: w}
		}()},
		{"transition from unknown state", func() TypeDef {
			w := wf()
			w.Transitions[0].From = []string{"z"}
			return TypeDef{Name: "x", Workflow: w}
		}()},
		{"transition requires unknown field", func() TypeDef {
			w := wf()
			w.Transitions[0].Requires = []string{"nope"}
			return TypeDef{Name: "x", Workflow: w}
		}()},
		{"duplicate transition", func() TypeDef {
			w := wf()
			w.Transitions = append(w.Transitions, w.Transitions[0])
			return TypeDef{Name: "x", Workflow: w}
		}()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestStore(t)
			if err := s.DefineType("admin", tt.td); !errors.Is(err, ErrValidation) {
				t.Fatalf("err = %v, want ErrValidation", err)
			}
		})
	}
}

func TestRedefiningATypeBumpsItsVersion(t *testing.T) {
	s := setup(t)
	td := builtinTypes()[0]
	td.Fields = append(td.Fields, FieldDef{Name: "audience", Kind: KindText})
	if err := s.DefineType("admin", td); err != nil {
		t.Fatal(err)
	}
	if got := s.Type("page"); got.Version != 2 {
		t.Fatalf("page version = %d, want 2", got.Version)
	}
	it := mustItem(t)(s.Create("anna", NewItem{Space: "PLAT", Type: "page", Title: "p", Properties: Props{"audience": "all"}}))
	if it.TypeVersion != 2 {
		t.Fatalf("item type version = %d, want 2", it.TypeVersion)
	}
}

func TestCreateValidation(t *testing.T) {
	s := setup(t)
	page := mustItem(t)(s.Create("anna", NewItem{Space: "PLAT", Type: "page", Title: "p"}))
	tests := []struct {
		name string
		n    NewItem
		want error
	}{
		{"unknown space", NewItem{Space: "NOPE", Type: "page", Title: "x"}, ErrNotFound},
		{"unknown type", NewItem{Space: "PLAT", Type: "nope", Title: "x"}, ErrNotFound},
		{"empty title", NewItem{Space: "PLAT", Type: "page"}, ErrValidation},
		{"unknown parent", NewItem{Space: "PLAT", Type: "page", Title: "x", Parent: "missing"}, ErrNotFound},
		{"parent in another space", NewItem{Space: "OPS", Type: "page", Title: "x", Parent: page.ID}, ErrValidation},
		{"unknown field", NewItem{Space: "PLAT", Type: "page", Title: "x", Properties: Props{"colour": "red"}}, ErrValidation},
		{"missing required field", NewItem{Space: "PLAT", Type: "task", Title: "x"}, ErrValidation},
		{"enum value not allowed", NewItem{Space: "PLAT", Type: "task", Title: "x", Properties: Props{"priority": "urgent"}}, ErrValidation},
		{"wrong value kind", NewItem{Space: "PLAT", Type: "task", Title: "x", Properties: Props{"priority": "low", "estimate": "three"}}, ErrValidation},
		{"state is not a property (D3)", NewItem{Space: "PLAT", Type: "task", Title: "x", Properties: Props{"priority": "low", "state": "done"}}, ErrValidation},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := s.Create("anna", tt.n); !errors.Is(err, tt.want) {
				t.Fatalf("err = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestValueKinds(t *testing.T) {
	s := setup(t)
	td := TypeDef{Name: "record", Fields: []FieldDef{
		{Name: "t", Kind: KindText}, {Name: "n", Kind: KindNumber}, {Name: "b", Kind: KindBool},
		{Name: "d", Kind: KindDate}, {Name: "u", Kind: KindUser},
	}}
	noErr(t, s.DefineType("admin", td))
	ok := Props{"t": "x", "n": 1.5, "b": true, "d": time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC), "u": "anna"}
	if _, err := s.Create("anna", NewItem{Space: "PLAT", Type: "record", Title: "r", Properties: ok}); err != nil {
		t.Fatalf("valid values rejected: %v", err)
	}
	for field, bad := range map[string]any{"t": 1, "n": "1", "b": "yes", "d": "2026-01-02", "u": ""} {
		if _, err := s.Create("anna", NewItem{Space: "PLAT", Type: "record", Title: "r", Properties: Props{field: bad}}); !errors.Is(err, ErrValidation) {
			t.Errorf("field %s = %#v: err = %v, want ErrValidation", field, bad, err)
		}
	}
}

func TestSetPropertyAndTitle(t *testing.T) {
	s := setup(t)
	task := mustItem(t)(s.Create("anna", NewItem{Space: "PLAT", Type: "task", Title: "t", Properties: Props{"priority": "low"}}))
	if err := s.SetProperty("anna", task.ID, "priority", nil); !errors.Is(err, ErrValidation) {
		t.Fatalf("clearing a required field: err = %v", err)
	}
	noErr(t, s.SetProperty("anna", task.ID, "estimate", 3.0))
	noErr(t, s.SetProperty("anna", task.ID, "estimate", nil))
	if got := mustItem(t)(s.Get(task.ID)); got.Properties["estimate"] != nil {
		t.Fatalf("estimate not cleared: %v", got.Properties)
	}
	if err := s.SetProperty("anna", "missing", "estimate", 1.0); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown item: err = %v", err)
	}
	if err := s.SetTitle("anna", task.ID, " "); !errors.Is(err, ErrValidation) {
		t.Fatalf("blank title: err = %v", err)
	}
	noErr(t, s.SetTitle("anna", task.ID, "renamed"))
	if got := mustItem(t)(s.Get(task.ID)); got.Title != "renamed" || got.Version != 4 {
		t.Fatalf("after rename: %+v", got)
	}
}

func TestTransitionRules(t *testing.T) {
	s := setup(t)
	page := mustItem(t)(s.Create("anna", NewItem{Space: "PLAT", Type: "page", Title: "p"}))
	task := mustItem(t)(s.Create("anna", NewItem{Space: "PLAT", Type: "task", Title: "t", Properties: Props{"priority": "low"}}))
	if err := s.Transition("anna", page.ID, "start"); !errors.Is(err, ErrValidation) {
		t.Fatalf("type without workflow: err = %v", err)
	}
	if err := s.Transition("anna", task.ID, "nope"); !errors.Is(err, ErrValidation) {
		t.Fatalf("unknown transition: err = %v", err)
	}
	if err := s.Transition("anna", task.ID, "reopen"); !errors.Is(err, ErrValidation) {
		t.Fatalf("transition from wrong state: err = %v", err)
	}
	if err := s.Transition("anna", "missing", "finish"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown item: err = %v", err)
	}
	noErr(t, s.Transition("anna", task.ID, "finish"))
	noErr(t, s.Transition("anna", task.ID, "reopen"))
}

func TestMoveAcrossSpacesKeepsIdentityAndRedirectsKeys(t *testing.T) {
	s := setup(t)
	page := mustItem(t)(s.Create("anna", NewItem{Space: "PLAT", Type: "page", Title: "Runbook"}))
	child := mustItem(t)(s.Create("anna", NewItem{Space: "PLAT", Type: "task", Parent: page.ID, Title: "Patch", Properties: Props{"priority": "low"}}))
	noErr(t, s.AddRelation("anna", "blocks", child.ID, page.ID))

	noErr(t, s.Move("anna", page.ID, "OPS", ""))

	moved := mustItem(t)(s.Get(page.ID))
	movedChild := mustItem(t)(s.Get(child.ID))
	if moved.Space != "OPS" || movedChild.Space != "OPS" {
		t.Fatalf("subtree not moved: %s %s", moved.Space, movedChild.Space)
	}
	if moved.Key != "OPS-1" || movedChild.Key != "OPS-2" {
		t.Fatalf("new keys = %s, %s", moved.Key, movedChild.Key)
	}
	// D2: old keys resolve to the same identity; relations are untouched.
	if got := mustItem(t)(s.ByKey("PLAT-2")); got.ID != child.ID {
		t.Fatalf("old key resolves to %s", got.ID)
	}
	if !hasRelation(s.Relations(child.ID, Outgoing), "blocks", page.ID) {
		t.Fatal("relation lost on move")
	}
	if _, err := s.ByKey("PLAT-99"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown key: err = %v", err)
	}
}

func TestMoveRules(t *testing.T) {
	s := setup(t)
	a := mustItem(t)(s.Create("anna", NewItem{Space: "PLAT", Type: "page", Title: "a"}))
	b := mustItem(t)(s.Create("anna", NewItem{Space: "PLAT", Type: "page", Title: "b", Parent: a.ID}))
	o := mustItem(t)(s.Create("anna", NewItem{Space: "OPS", Type: "page", Title: "o"}))
	if err := s.Move("anna", a.ID, "PLAT", b.ID); !errors.Is(err, ErrValidation) {
		t.Fatalf("cycle: err = %v", err)
	}
	if err := s.Move("anna", a.ID, "PLAT", a.ID); !errors.Is(err, ErrValidation) {
		t.Fatalf("own parent: err = %v", err)
	}
	if err := s.Move("anna", b.ID, "PLAT", o.ID); !errors.Is(err, ErrValidation) {
		t.Fatalf("parent in other space: err = %v", err)
	}
	if err := s.Move("anna", b.ID, "NOPE", ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown space: err = %v", err)
	}
	if err := s.Move("anna", b.ID, "PLAT", "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown parent: err = %v", err)
	}
	if err := s.Move("anna", "missing", "PLAT", ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown item: err = %v", err)
	}
	// Re-parenting within a space keeps the key.
	noErr(t, s.Move("anna", b.ID, "PLAT", ""))
	if got := mustItem(t)(s.Get(b.ID)); got.Parent != "" || got.Key != "PLAT-2" {
		t.Fatalf("after re-parent: %+v", got)
	}
}

func TestBodyRules(t *testing.T) {
	s := setup(t)
	noErr(t, s.DefineType("admin", TypeDef{Name: "label"}))
	label := mustItem(t)(s.Create("anna", NewItem{Space: "PLAT", Type: "label", Title: "l"}))
	page := mustItem(t)(s.Create("anna", NewItem{Space: "PLAT", Type: "page", Title: "p"}))
	other := mustItem(t)(s.Create("anna", NewItem{Space: "PLAT", Type: "page", Title: "o"}))

	if err := s.CommitBody("anna", label.ID, Doc{}); !errors.Is(err, ErrValidation) {
		t.Fatalf("type without body: err = %v", err)
	}
	if err := s.CommitBody("anna", page.ID, Doc{Blocks: []Block{{Kind: Embed}}}); !errors.Is(err, ErrValidation) {
		t.Fatalf("embed without ref: err = %v", err)
	}
	if err := s.CommitBody("anna", page.ID, Doc{Blocks: []Block{{Kind: Embed, Ref: page.ID}}}); !errors.Is(err, ErrValidation) {
		t.Fatalf("self embed: err = %v", err)
	}
	if err := s.CommitBody("anna", page.ID, Doc{Blocks: []Block{{Kind: Paragraph, Mentions: []ID{"missing"}}}}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("dangling mention: err = %v", err)
	}
	if err := s.CommitBody("anna", page.ID, Doc{Blocks: []Block{{Kind: "table"}}}); !errors.Is(err, ErrValidation) {
		t.Fatalf("unknown block kind: err = %v", err)
	}
	if err := s.CommitBody("anna", "missing", Doc{}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown item: err = %v", err)
	}

	// D4: derived relations follow the latest body version, and only it.
	noErr(t, s.CommitBody("anna", page.ID, Doc{Blocks: []Block{
		{Kind: Paragraph, Mentions: []ID{other.ID, other.ID}},
	}}))
	if n := len(s.Relations(page.ID, Outgoing)); n != 1 {
		t.Fatalf("duplicate mentions must yield one relation, got %d", n)
	}
	noErr(t, s.CommitBody("anna", page.ID, Doc{Blocks: []Block{{Kind: Paragraph, Text: "no links"}}}))
	if n := len(s.Relations(page.ID, Outgoing)); n != 0 {
		t.Fatalf("stale derived relations after new version: %d", n)
	}
}

func TestRelationRules(t *testing.T) {
	s := setup(t)
	a := mustItem(t)(s.Create("anna", NewItem{Space: "PLAT", Type: "page", Title: "a"}))
	b := mustItem(t)(s.Create("anna", NewItem{Space: "OPS", Type: "page", Title: "b"}))
	if err := s.AddRelation("anna", "", a.ID, b.ID); !errors.Is(err, ErrValidation) {
		t.Fatalf("empty type: err = %v", err)
	}
	if err := s.AddRelation("anna", "mentions", a.ID, b.ID); !errors.Is(err, ErrValidation) {
		t.Fatalf("derived type added by hand: err = %v", err)
	}
	if err := s.AddRelation("anna", "relates", a.ID, a.ID); !errors.Is(err, ErrValidation) {
		t.Fatalf("self relation: err = %v", err)
	}
	if err := s.AddRelation("anna", "relates", a.ID, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown target: err = %v", err)
	}
	noErr(t, s.AddRelation("anna", "relates", a.ID, b.ID))
	if err := s.AddRelation("anna", "relates", a.ID, b.ID); !errors.Is(err, ErrValidation) {
		t.Fatalf("duplicate: err = %v", err)
	}
	rel := s.Relations(a.ID, Outgoing)[0]
	if err := s.RemoveRelation("anna", "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown relation: err = %v", err)
	}
	noErr(t, s.RemoveRelation("anna", rel.ID))
	if n := len(s.Relations(b.ID, Incoming)); n != 0 {
		t.Fatalf("relation not removed: %d", n)
	}
	// History of both ends records the relation events.
	if n := len(s.History(b.ID)); n != 3 {
		t.Fatalf("history of target = %d events, want create, add, remove", n)
	}
}

func TestDerivedRelationCannotBeRemovedByHand(t *testing.T) {
	s := setup(t)
	a := mustItem(t)(s.Create("anna", NewItem{Space: "PLAT", Type: "page", Title: "a"}))
	b := mustItem(t)(s.Create("anna", NewItem{Space: "PLAT", Type: "page", Title: "b"}))
	noErr(t, s.CommitBody("anna", a.ID, Doc{Blocks: []Block{{Kind: Embed, Ref: b.ID}}}))
	rel := s.Relations(a.ID, Outgoing)[0]
	if err := s.RemoveRelation("anna", rel.ID); !errors.Is(err, ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation: change the body instead", err)
	}
}

func TestQueriesAndCopies(t *testing.T) {
	s := setup(t)
	p := mustItem(t)(s.Create("anna", NewItem{Space: "PLAT", Type: "page", Title: "p"}))
	mustItem(t)(s.Create("anna", NewItem{Space: "PLAT", Type: "task", Parent: p.ID, Title: "t1", Properties: Props{"priority": "high"}}))
	mustItem(t)(s.Create("anna", NewItem{Space: "PLAT", Type: "task", Title: "t2", Properties: Props{"priority": "low"}}))
	if got := s.Find(Query{Where: Props{"priority": "high"}}); len(got) != 1 || got[0].Title != "t1" {
		t.Fatalf("where = %v", ids(got))
	}
	if got := s.Find(Query{Parent: p.ID}); len(got) != 1 || got[0].Title != "t1" {
		t.Fatalf("children = %v", ids(got))
	}
	// Returned items are copies: mutating them does not change the store.
	got := s.Find(Query{Type: "task"})
	got[0].Properties["priority"] = "low"
	if again := s.Find(Query{Where: Props{"priority": "high"}}); len(again) != 1 {
		t.Fatal("store state changed through a returned copy")
	}
	if _, err := s.Get("missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get unknown: err = %v", err)
	}
	if s.Type("nope").Name != "" {
		t.Fatal("unknown type must be zero")
	}
}

func TestSpaceRules(t *testing.T) {
	s := setup(t)
	for _, key := range []string{"", "plat", "PLAT", "A-B"} {
		if err := s.CreateSpace("admin", key, "x"); !errors.Is(err, ErrValidation) {
			t.Errorf("CreateSpace(%q): err = %v, want ErrValidation", key, err)
		}
	}
}

func TestReplayRejectsInvalidLog(t *testing.T) {
	s := setup(t)
	log := s.Log()
	log[0].Seq = 99
	if _, err := Replay(log); err == nil {
		t.Fatal("a log with a gap in sequence numbers must be refused")
	}
}

func TestDefaultIDsAreRandomAndDistinct(t *testing.T) {
	s := NewStore()
	noErr(t, s.DefineType("admin", TypeDef{Name: "page"}))
	noErr(t, s.CreateSpace("admin", "X", "x"))
	a := mustItem(t)(s.Create("anna", NewItem{Space: "X", Type: "page", Title: "a"}))
	b := mustItem(t)(s.Create("anna", NewItem{Space: "X", Type: "page", Title: "b"}))
	if a.ID == b.ID || len(a.ID) != 32 {
		t.Fatalf("ids %q, %q: want two distinct 32-hex-digit ids", a.ID, b.ID)
	}
}

func TestSetTitleUnknownItem(t *testing.T) {
	s := setup(t)
	if err := s.SetTitle("anna", "missing", "x"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestRelationsFromOneBodyAreOrderedStably(t *testing.T) {
	s := setup(t)
	a := mustItem(t)(s.Create("anna", NewItem{Space: "PLAT", Type: "page", Title: "a"}))
	b := mustItem(t)(s.Create("anna", NewItem{Space: "PLAT", Type: "page", Title: "b"}))
	c := mustItem(t)(s.Create("anna", NewItem{Space: "PLAT", Type: "page", Title: "c"}))
	noErr(t, s.CommitBody("anna", a.ID, Doc{Blocks: []Block{{Kind: Paragraph, Mentions: []ID{c.ID, b.ID}}}}))
	first := s.Relations(a.ID, Outgoing)
	for range 20 {
		if again := s.Relations(a.ID, Outgoing); again[0].ID != first[0].ID || again[1].ID != first[1].ID {
			t.Fatal("relations created by one event are not returned in a stable order")
		}
	}
}
