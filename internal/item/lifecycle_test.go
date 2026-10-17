package item

import (
	"errors"
	"strings"
	"testing"
)

// --- ADR-0020: embedding vs containment -------------------------------------

func TestCreateEmbeddedIsOneBatch(t *testing.T) {
	s := setup(t)
	page := mustItem(t)(s.Create("anna", NewItem{Space: "PLAT", Type: "page", Title: "Notes"}))
	noErr(t, s.CommitBody("anna", page.ID, Doc{Blocks: []Block{{Kind: Heading, Text: "Actions"}}}))

	task := mustItem(t)(s.CreateEmbedded("anna", page.ID, NewItem{Type: "task", Title: "Patch", Properties: Props{"priority": "low"}}))
	if task.Parent != page.ID || task.Space != "PLAT" {
		t.Fatalf("inline item must live in its host: parent=%s space=%s", task.Parent, task.Space)
	}
	host := mustItem(t)(s.Get(page.ID))
	if n := len(host.Body.Blocks); n != 2 || host.Body.Blocks[1].Ref != task.ID {
		t.Fatalf("host body = %+v, want the embed appended", host.Body.Blocks)
	}
	log := s.Log()
	a, b := log[len(log)-2], log[len(log)-1]
	if a.Kind != ItemCreated || b.Kind != BodyCommitted || a.Batch == 0 || a.Batch != b.Batch {
		t.Fatalf("want ItemCreated and BodyCommitted in one batch, got %s/%d and %s/%d", a.Kind, a.Batch, b.Kind, b.Batch)
	}
	if single := log[len(log)-3]; single.Batch != 0 {
		t.Fatalf("a single-event command must not carry a batch, got %d", single.Batch)
	}
}

func TestCreateEmbeddedValidatesBeforeWriting(t *testing.T) {
	s := setup(t)
	noErr(t, s.DefineType("admin", TypeDef{Name: "label"}))
	page := mustItem(t)(s.Create("anna", NewItem{Space: "PLAT", Type: "page", Title: "Notes"}))
	label := mustItem(t)(s.Create("anna", NewItem{Space: "PLAT", Type: "label", Title: "l"}))
	before := len(s.Log())
	cases := map[string]struct {
		host ID
		n    NewItem
	}{
		"invalid item":          {page.ID, NewItem{Type: "task", Title: "x"}}, // priority missing
		"host without body":     {label.ID, NewItem{Type: "task", Title: "x", Properties: Props{"priority": "low"}}},
		"space given and wrong": {page.ID, NewItem{Space: "OPS", Type: "task", Title: "x", Properties: Props{"priority": "low"}}},
		"parent given":          {page.ID, NewItem{Parent: label.ID, Type: "task", Title: "x", Properties: Props{"priority": "low"}}},
	}
	for name, c := range cases {
		if _, err := s.CreateEmbedded("anna", c.host, c.n); !errors.Is(err, ErrValidation) {
			t.Errorf("%s: err = %v, want ErrValidation", name, err)
		}
	}
	if after := len(s.Log()); after != before {
		t.Fatalf("a refused CreateEmbedded wrote %d events; the batch must be all or nothing", after-before)
	}
}

func TestEmbedViewDistinguishesChildFromReference(t *testing.T) {
	s := setup(t)
	page := mustItem(t)(s.Create("anna", NewItem{Space: "PLAT", Type: "page", Title: "Status"}))
	child := mustItem(t)(s.CreateEmbedded("anna", page.ID, NewItem{Type: "task", Title: "Own task", Properties: Props{"priority": "low"}}))
	foreign := mustItem(t)(s.Create("ben", NewItem{Space: "OPS", Type: "task", Title: "Ops task", Properties: Props{"priority": "high"}}))
	host := mustItem(t)(s.Get(page.ID))
	blocks := append(host.Body.Blocks, Block{Kind: Embed, Ref: foreign.ID})
	noErr(t, s.CommitBody("anna", page.ID, Doc{Blocks: blocks}))

	views := mustViews(t)(s.Embeds(page.ID))
	if len(views) != 2 || !views[0].Child || views[1].Child {
		t.Fatalf("views = %+v: want the inline task as child and the OPS task as reference", views)
	}
	if views[1].Key != foreign.Key || views[1].Presence != Live {
		t.Fatalf("reference view = %+v", views[1])
	}

	// Moving the child elsewhere turns it into a reference without touching the body.
	noErr(t, s.Move("anna", child.ID, "PLAT", ""))
	views = mustViews(t)(s.Embeds(page.ID))
	if views[0].Child {
		t.Fatal("after the move the embed must render as a reference")
	}
	if got := mustItem(t)(s.Get(page.ID)); len(got.Body.Blocks) != 2 {
		t.Fatal("moving an embedded item must not edit the host body")
	}
	if _, err := s.Embeds("missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Embeds unknown: err = %v", err)
	}
}

func TestRemovingAnEmbedKeepsTheItem(t *testing.T) {
	s := setup(t)
	page := mustItem(t)(s.Create("anna", NewItem{Space: "PLAT", Type: "page", Title: "Notes"}))
	task := mustItem(t)(s.CreateEmbedded("anna", page.ID, NewItem{Type: "task", Title: "Patch", Properties: Props{"priority": "low"}}))
	noErr(t, s.CommitBody("anna", page.ID, Doc{}))

	got := mustItem(t)(s.Get(task.ID))
	if got.Presence != Live || got.Parent != page.ID {
		t.Fatalf("task after its embed was removed: %+v", got)
	}
	if children := s.Find(Query{Parent: page.ID}); len(children) != 1 {
		t.Fatal("the task must still be listed under its parent")
	}
}

// --- ADR-0021: no deletion while referenced, then trash, then purge ---------

// trashFixture builds a PLAT page with a child and a grandchild, and an OPS
// page that references the PLAT items from outside: it embeds the child,
// mentions the page and relates to the page by hand.
func trashFixture(t *testing.T) (s *Store, page, child, grandchild, foreign Item) {
	t.Helper()
	s = setup(t)
	page = mustItem(t)(s.Create("anna", NewItem{Space: "PLAT", Type: "page", Title: "Runbook"}))
	child = mustItem(t)(s.CreateEmbedded("anna", page.ID, NewItem{Type: "task", Title: "Patch", Properties: Props{"priority": "low"}}))
	grandchild = mustItem(t)(s.Create("anna", NewItem{Space: "PLAT", Type: "task", Parent: child.ID, Title: "Sub", Properties: Props{"priority": "low"}}))
	foreign = mustItem(t)(s.Create("ben", NewItem{Space: "OPS", Type: "page", Title: "Ops overview"}))
	noErr(t, s.CommitBody("ben", foreign.ID, Doc{Blocks: []Block{
		{Kind: Embed, Ref: child.ID},
		{Kind: Paragraph, Mentions: []ID{page.ID}},
	}}))
	noErr(t, s.AddRelation("ben", "relates", foreign.ID, page.ID))
	return
}

// unreference removes every reference from the OPS page into the PLAT subtree.
func unreference(t *testing.T, s *Store, foreign Item) {
	t.Helper()
	noErr(t, s.CommitBody("ben", foreign.ID, Doc{Blocks: []Block{{Kind: Paragraph, Text: "moved on"}}}))
	for _, r := range s.Relations(foreign.ID, Outgoing) {
		noErr(t, s.RemoveRelation("ben", r.ID))
	}
}

func TestTrashIsRefusedWhileReferencedFromOutside(t *testing.T) {
	s, page, child, grandchild, foreign := trashFixture(t)
	before := len(s.Log())

	err := s.Trash("anna", page.ID)
	if !errors.Is(err, ErrReferenced) || !errors.Is(err, ErrValidation) {
		t.Fatalf("err = %v, want ErrReferenced (an ErrValidation)", err)
	}
	if !strings.Contains(err.Error(), foreign.Key) {
		t.Fatalf("the error must name the referring item %s: %v", foreign.Key, err)
	}
	// An embed of a descendant blocks too: the subtree would go with the page.
	if err := s.Trash("anna", child.ID); !errors.Is(err, ErrReferenced) {
		t.Fatalf("trash child embedded elsewhere: err = %v", err)
	}
	// The grandchild is referenced only from inside the subtree.
	noErr(t, s.Trash("anna", grandchild.ID))
	if len(s.Log()) != before+1 {
		t.Fatal("refused trash commands must not write events")
	}

	// After the references are removed, trashing is allowed.
	unreference(t, s, foreign)
	noErr(t, s.Trash("anna", page.ID))
}

// TestInlineItemIsBlockedByItsOwnHost: an item created inline is embedded in
// its parent's body, and the parent is outside the item's subtree, so the
// embed has to go before the item can be trashed.
func TestInlineItemIsBlockedByItsOwnHost(t *testing.T) {
	s, page, child, _, foreign := trashFixture(t)
	unreference(t, s, foreign)
	err := s.Trash("anna", child.ID)
	if !errors.Is(err, ErrReferenced) || !strings.Contains(err.Error(), page.Key) {
		t.Fatalf("err = %v, want ErrReferenced naming the host %s", err, page.Key)
	}
	noErr(t, s.CommitBody("anna", page.ID, Doc{}))
	noErr(t, s.Trash("anna", child.ID))
}

func TestReferencesThatDoNotBlock(t *testing.T) {
	s, page, child, _, foreign := trashFixture(t)
	other := mustItem(t)(s.Create("ben", NewItem{Space: "OPS", Type: "page", Title: "Other"}))
	// References from items in the trash do not block: the OPS page, which
	// references the PLAT subtree, goes to the trash first.
	noErr(t, s.Trash("ben", foreign.ID))
	// References from inside the subtree do not block.
	noErr(t, s.AddRelation("anna", "blocks", child.ID, page.ID))
	// The page's own outgoing references do not block either.
	noErr(t, s.AddRelation("anna", "relates", page.ID, other.ID))
	noErr(t, s.Trash("anna", page.ID))
}

func TestTrashMovesTheSubtreeOutOfSight(t *testing.T) {
	s, page, child, grandchild, foreign := trashFixture(t)
	unreference(t, s, foreign)
	noErr(t, s.Trash("anna", page.ID))

	for _, id := range []ID{page.ID, child.ID, grandchild.ID} {
		if got := mustItem(t)(s.Get(id)); got.Presence != Trashed {
			t.Fatalf("%s presence = %s, want trashed", id, got.Presence)
		}
	}
	if got := s.Find(Query{Space: "PLAT"}); len(got) != 0 {
		t.Fatalf("trashed items must not appear in queries: %v", ids(got))
	}
	if got := s.Find(Query{Space: "PLAT", IncludeDeleted: true}); len(got) != 3 {
		t.Fatalf("IncludeDeleted must list the trash: %v", ids(got))
	}
	if got := mustItem(t)(s.ByKey(child.Key)); got.ID != child.ID {
		t.Fatal("key of a trashed item must resolve")
	}
}

// TestRestoredReferrerKeepsItsReferences covers the one way a live body can
// point at a trashed item: the referrer was itself in the trash and comes back.
func TestRestoredReferrerKeepsItsReferences(t *testing.T) {
	s, page, child, _, foreign := trashFixture(t)
	other := mustItem(t)(s.Create("ben", NewItem{Space: "OPS", Type: "page", Title: "Other"}))
	noErr(t, s.Trash("ben", foreign.ID))
	noErr(t, s.Trash("anna", page.ID))
	noErr(t, s.Restore("ben", foreign.ID))

	// The restored body is not edited; its embed renders the item as trashed (D4).
	if v := mustViews(t)(s.Embeds(foreign.ID)); len(v) != 1 || v[0].Presence != Trashed {
		t.Fatalf("embed view = %+v, want one trashed embed", v)
	}
	// Relations to trashed items are hidden, not removed.
	if n := len(s.Relations(foreign.ID, Outgoing)); n != 0 {
		t.Fatalf("relations to trashed items must be hidden, got %d", n)
	}
	// Editing the restored page while it still embeds the trashed task works.
	kept := append(mustItem(t)(s.Get(foreign.ID)).Body.Blocks, Block{Kind: Paragraph, Text: "new text"})
	noErr(t, s.CommitBody("ben", foreign.ID, Doc{Blocks: kept}))
	// A new reference to a trashed item is refused.
	if err := s.CommitBody("ben", other.ID, Doc{Blocks: []Block{{Kind: Embed, Ref: child.ID}}}); !errors.Is(err, ErrValidation) {
		t.Fatalf("new embed of a trashed item: err = %v, want ErrValidation", err)
	}
	// Restoring the page brings the relations back.
	noErr(t, s.Restore("anna", page.ID))
	if n := len(s.Relations(foreign.ID, Outgoing)); n != 3 {
		t.Fatalf("relations after restore = %d, want 3", n)
	}
}

func TestRestoreBringsBackExactlyWhatWasTrashedTogether(t *testing.T) {
	s, page, child, grandchild, foreign := trashFixture(t)
	unreference(t, s, foreign)
	noErr(t, s.Trash("anna", grandchild.ID)) // trashed on its own first
	noErr(t, s.Trash("anna", page.ID))

	if err := s.Restore("anna", child.ID); !errors.Is(err, ErrValidation) {
		t.Fatalf("restoring a non-root of a trash batch: err = %v, want ErrValidation", err)
	}
	noErr(t, s.Restore("anna", page.ID))
	if got := mustItem(t)(s.Get(child.ID)); got.Presence != Live {
		t.Fatal("child trashed with the page must come back")
	}
	if got := mustItem(t)(s.Get(grandchild.ID)); got.Presence != Trashed {
		t.Fatal("an item trashed earlier on its own must stay in the trash")
	}
	noErr(t, s.Restore("anna", grandchild.ID))
	if err := s.Restore("anna", grandchild.ID); !errors.Is(err, ErrValidation) {
		t.Fatalf("restoring a live item: err = %v", err)
	}
}

func TestRestoreRequiresALiveParent(t *testing.T) {
	s, page, child, _, foreign := trashFixture(t)
	unreference(t, s, foreign)
	noErr(t, s.CommitBody("anna", page.ID, Doc{})) // the page itself embeds the child
	noErr(t, s.Trash("anna", child.ID))
	noErr(t, s.Trash("anna", page.ID))
	if err := s.Restore("anna", child.ID); !errors.Is(err, ErrValidation) {
		t.Fatalf("restoring below a trashed parent: err = %v, want ErrValidation", err)
	}
}

func TestTrashedItemsAreReadOnly(t *testing.T) {
	s, page, child, _, foreign := trashFixture(t)
	var relates ID
	for _, r := range s.Relations(foreign.ID, Outgoing) {
		if r.Type == "relates" {
			relates = r.ID
		}
	}
	noErr(t, s.Trash("ben", foreign.ID))
	noErr(t, s.Trash("anna", page.ID))
	noErr(t, s.Restore("ben", foreign.ID))
	checks := map[string]error{
		"set title":                s.SetTitle("anna", child.ID, "x"),
		"set property":             s.SetProperty("anna", child.ID, "estimate", 1.0),
		"transition":               s.Transition("anna", child.ID, "finish"),
		"commit body":              s.CommitBody("anna", page.ID, Doc{}),
		"move":                     s.Move("anna", child.ID, "OPS", ""),
		"add relation":             s.AddRelation("anna", "blocks", foreign.ID, child.ID),
		"trash again":              s.Trash("anna", page.ID),
		"remove relation to trash": s.RemoveRelation("ben", relates),
		"child of trash": func() error {
			_, err := s.Create("anna", NewItem{Space: "PLAT", Type: "page", Title: "x", Parent: page.ID})
			return err
		}(),
		"embed into trash": func() error {
			_, err := s.CreateEmbedded("anna", page.ID, NewItem{Type: "page", Title: "x"})
			return err
		}(),
	}
	for name, err := range checks {
		if !errors.Is(err, ErrValidation) {
			t.Errorf("%s on a trashed item: err = %v, want ErrValidation", name, err)
		}
	}
}

func TestPurgeLeavesATombstone(t *testing.T) {
	s, page, child, grandchild, foreign := trashFixture(t)
	noErr(t, s.Trash("ben", foreign.ID))
	if err := s.Purge("anna", page.ID); !errors.Is(err, ErrValidation) {
		t.Fatalf("purging a live item: err = %v, want ErrValidation", err)
	}
	noErr(t, s.Trash("anna", page.ID))
	if err := s.Purge("anna", child.ID); !errors.Is(err, ErrValidation) {
		t.Fatalf("purging a non-root of a trash batch: err = %v", err)
	}
	noErr(t, s.Purge("anna", page.ID))

	for _, id := range []ID{page.ID, child.ID, grandchild.ID} {
		got := mustItem(t)(s.Get(id))
		if got.Presence != Purged || got.Title != "" || len(got.Properties) != 0 || len(got.Body.Blocks) != 0 {
			t.Fatalf("tombstone %s still carries content: %+v", id, got)
		}
		if got.Type == "" || got.Key == "" || got.Space == "" {
			t.Fatalf("tombstone %s lost its identity data: %+v", id, got)
		}
	}
	// D7: keys and IDs resolve to the tombstone.
	if got := mustItem(t)(s.ByKey(page.Key)); got.Presence != Purged {
		t.Fatal("key of a purged item must resolve to its tombstone")
	}
	// Hand-made relations to purged items are gone; derived ones from the
	// OPS body remain (they are a function of that body).
	for _, r := range s.rels {
		if !r.Derived && (r.To == page.ID || r.From == page.ID) {
			t.Fatal("hand-made relation to a purged item survived")
		}
	}
	// A referrer restored later renders the purged item as deleted.
	noErr(t, s.Restore("ben", foreign.ID))
	if v := mustViews(t)(s.Embeds(foreign.ID)); v[0].Presence != Purged || v[0].Title != "" {
		t.Fatalf("embed of a purged item = %+v", v[0])
	}
	if err := s.Restore("anna", page.ID); !errors.Is(err, ErrValidation) {
		t.Fatalf("restoring a purged item: err = %v", err)
	}
	if err := s.Purge("anna", page.ID); !errors.Is(err, ErrValidation) {
		t.Fatalf("purging twice: err = %v", err)
	}
}

// TestPurgeLeavesContentInTheLog documents a known gap of ADR-0021: purged
// content survives in past events until crypto-shredding is designed with
// ADR-0008. When that lands, this test must be inverted.
func TestPurgeLeavesContentInTheLog(t *testing.T) {
	s, page, _, _, foreign := trashFixture(t)
	unreference(t, s, foreign)
	noErr(t, s.Trash("anna", page.ID))
	noErr(t, s.Purge("anna", page.ID))
	found := false
	for _, e := range s.Log() {
		if e.Kind == ItemCreated && e.Item == page.ID && strings.Contains(e.Title, "Runbook") {
			found = true
		}
	}
	if !found {
		t.Fatal("expected the purged title to still be in the log (known gap)")
	}
}

func TestLifecycleCommandsOnUnknownItems(t *testing.T) {
	s := setup(t)
	for name, err := range map[string]error{
		"trash":   s.Trash("anna", "missing"),
		"restore": s.Restore("anna", "missing"),
		"purge":   s.Purge("anna", "missing"),
	} {
		if !errors.Is(err, ErrNotFound) {
			t.Errorf("%s: err = %v, want ErrNotFound", name, err)
		}
	}
	if _, err := s.CreateEmbedded("anna", "missing", NewItem{Type: "task", Title: "x"}); !errors.Is(err, ErrNotFound) {
		t.Errorf("CreateEmbedded unknown host: err = %v", err)
	}
}

func TestReplayWithLifecycle(t *testing.T) {
	s, page, _, grandchild, foreign := trashFixture(t)
	noErr(t, s.Trash("anna", grandchild.ID))
	noErr(t, s.Restore("anna", grandchild.ID))
	noErr(t, s.Trash("ben", foreign.ID))
	noErr(t, s.Trash("anna", page.ID))
	noErr(t, s.Purge("anna", page.ID))
	noErr(t, s.Restore("ben", foreign.ID))
	r, err := Replay(s.Log())
	if err != nil {
		t.Fatal(err)
	}
	if got, want := snapshot(r), snapshot(s); got != want {
		t.Fatalf("replayed state differs\n got: %s\nwant: %s", got, want)
	}
}

func mustViews(t *testing.T) func([]EmbedView, error) []EmbedView {
	return func(v []EmbedView, err error) []EmbedView {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
}
