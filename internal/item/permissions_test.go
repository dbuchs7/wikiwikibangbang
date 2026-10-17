package item

import (
	"errors"
	"testing"
)

// permFixture: anna edits PLAT, ben views PLAT, carl has no role, the group
// @ops (member dora) edits OPS, pat administers PLAT, erin holds the system
// role "erasure".
func permFixture(t *testing.T) *Store {
	t.Helper()
	s := setup(t)
	noErr(t, s.GrantRole("root", "PLAT", "anna", Editor))
	noErr(t, s.GrantRole("root", "PLAT", "ben", Viewer))
	noErr(t, s.GrantRole("root", "PLAT", "pat", SpaceAdmin))
	noErr(t, s.AddToGroup("root", "ops", "dora"))
	noErr(t, s.GrantRole("root", "OPS", "@ops", Editor))
	noErr(t, s.GrantSystemRole("root", "erin", RoleErasure))
	return s
}

func TestRolesGateReadAndEdit(t *testing.T) {
	s := permFixture(t)
	anna, ben, carl, dora := s.As("anna"), s.As("ben"), s.As("carl"), s.As("dora")

	page := mustItem(t)(anna.Create(NewItem{Space: "PLAT", Type: "page", Title: "Runbook"}))
	if _, err := ben.Get(page.ID); err != nil {
		t.Fatalf("viewer cannot read: %v", err)
	}
	if err := ben.SetTitle(page.ID, "x"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("viewer edits: err = %v, want ErrForbidden", err)
	}
	if _, err := ben.Create(NewItem{Space: "PLAT", Type: "page", Title: "x"}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("viewer creates: err = %v, want ErrForbidden", err)
	}
	// An outsider must not learn that the item exists.
	if _, err := carl.Get(page.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("outsider reads: err = %v, want ErrNotFound", err)
	}
	if err := carl.SetTitle(page.ID, "x"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("outsider edits: err = %v, want ErrNotFound", err)
	}
	if got := carl.Find(Query{}); len(got) != 0 {
		t.Fatalf("outsider finds %v", ids(got))
	}
	if _, err := carl.History(page.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("outsider history: err = %v", err)
	}
	// Roles through a group.
	if _, err := dora.Create(NewItem{Space: "OPS", Type: "page", Title: "Ops"}); err != nil {
		t.Fatalf("group member cannot create: %v", err)
	}
	if _, err := dora.Get(page.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("OPS editor reads PLAT: err = %v", err)
	}
}

func TestRestrictionsNarrowAndInherit(t *testing.T) {
	s := permFixture(t)
	anna, ben, pat := s.As("anna"), s.As("ben"), s.As("pat")
	noErr(t, s.GrantRole("root", "PLAT", "dora", Editor))
	dora := s.As("dora")

	page := mustItem(t)(anna.Create(NewItem{Space: "PLAT", Type: "page", Title: "HR"}))
	child := mustItem(t)(anna.CreateEmbedded(page.ID, NewItem{Type: "task", Title: "Salary review", Properties: Props{"priority": "high"}}))

	// Read restricted to anna; edit is implied by read restriction for others.
	noErr(t, anna.Restrict(page.ID, Restriction{Read: []string{"anna"}}))
	for _, id := range []ID{page.ID, child.ID} {
		if _, err := ben.Get(id); !errors.Is(err, ErrNotFound) {
			t.Fatalf("restriction not inherited to %s: err = %v", id, err)
		}
		if _, err := dora.Get(id); !errors.Is(err, ErrNotFound) {
			t.Fatalf("editor outside the restriction reads %s: err = %v", id, err)
		}
	}
	// A space admin is not locked out by restrictions.
	if _, err := pat.Get(child.ID); err != nil {
		t.Fatalf("space admin locked out: %v", err)
	}
	// Restrictions only narrow: an entry cannot grant a role the space did not.
	noErr(t, anna.Restrict(page.ID, Restriction{Read: []string{"anna", "carl"}}))
	if _, err := s.As("carl").Get(page.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("a restriction entry must not grant access without a space role")
	}
}

func TestEditRestriction(t *testing.T) {
	s := permFixture(t)
	noErr(t, s.GrantRole("root", "PLAT", "dora", Editor))
	anna, dora := s.As("anna"), s.As("dora")
	page := mustItem(t)(anna.Create(NewItem{Space: "PLAT", Type: "page", Title: "Policy"}))
	noErr(t, anna.Restrict(page.ID, Restriction{Edit: []string{"anna"}}))
	if _, err := dora.Get(page.ID); err != nil {
		t.Fatalf("edit restriction must not hide the item: %v", err)
	}
	if err := dora.SetTitle(page.ID, "x"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("editor outside the edit restriction: err = %v, want ErrForbidden", err)
	}
	if err := dora.Restrict(page.ID, Restriction{}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("lifting a restriction one cannot edit under: err = %v", err)
	}
}

func TestRestrictingCannotLockOutOneself(t *testing.T) {
	s := permFixture(t)
	anna, pat := s.As("anna"), s.As("pat")
	page := mustItem(t)(anna.Create(NewItem{Space: "PLAT", Type: "page", Title: "p"}))
	if err := anna.Restrict(page.ID, Restriction{Read: []string{"ben"}}); !errors.Is(err, ErrValidation) {
		t.Fatalf("self lock-out: err = %v, want ErrValidation", err)
	}
	if err := anna.Restrict(page.ID, Restriction{Edit: []string{"ben"}}); !errors.Is(err, ErrValidation) {
		t.Fatalf("self lock-out on edit: err = %v, want ErrValidation", err)
	}
	// A space admin may restrict to others; it is not locked out anyway.
	noErr(t, pat.Restrict(page.ID, Restriction{Read: []string{"ben"}}))
}

func TestReferencesRespectReadAccess(t *testing.T) {
	s := permFixture(t)
	noErr(t, s.GrantRole("root", "PLAT", "dora", Editor))
	anna, dora := s.As("anna"), s.As("dora")
	secret := mustItem(t)(anna.Create(NewItem{Space: "PLAT", Type: "page", Title: "Secret"}))
	public := mustItem(t)(anna.Create(NewItem{Space: "PLAT", Type: "page", Title: "Public"}))
	noErr(t, anna.CommitBody(public.ID, Doc{Blocks: []Block{{Kind: Embed, Ref: secret.ID}}}))
	noErr(t, anna.AddRelation("relates", public.ID, secret.ID))
	noErr(t, anna.Restrict(secret.ID, Restriction{Read: []string{"anna"}}))

	// dora sees the public page; the embed renders as restricted, without key or title.
	views := mustViews(t)(dora.Embeds(public.ID))
	if len(views) != 1 || !views[0].Restricted || views[0].Key != "" || views[0].Title != "" || views[0].Ref != "" {
		t.Fatalf("restricted embed leaks: %+v", views)
	}
	if rels := mustRels(t)(dora.Relations(public.ID, Outgoing)); len(rels) != 0 {
		t.Fatalf("relations to unreadable items must be hidden: %+v", rels)
	}
	// dora can keep the embed while editing, but cannot add references to what she cannot see.
	blocks := mustItem(t)(dora.Get(public.ID)).Body.Blocks
	noErr(t, dora.CommitBody(public.ID, Doc{Blocks: append(blocks, Block{Kind: Paragraph, Text: "edit"})}))
	other := mustItem(t)(dora.Create(NewItem{Space: "PLAT", Type: "page", Title: "Mine"}))
	if err := dora.CommitBody(other.ID, Doc{Blocks: []Block{{Kind: Paragraph, Mentions: []ID{secret.ID}}}}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("mention of an unreadable item: err = %v, want ErrNotFound", err)
	}
	if err := dora.AddRelation("relates", other.ID, secret.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("relation to an unreadable item: err = %v, want ErrNotFound", err)
	}
	// anna sees the embed normally.
	if v := mustViews(t)(anna.Embeds(public.ID)); v[0].Restricted || v[0].Title != "Secret" {
		t.Fatalf("authorised view = %+v", v[0])
	}
}

func TestMoveAndDeletionRights(t *testing.T) {
	s := permFixture(t)
	noErr(t, s.GrantRole("root", "PLAT", "dora", Editor))
	anna, dora, pat := s.As("anna"), s.As("dora"), s.As("pat")
	page := mustItem(t)(anna.Create(NewItem{Space: "PLAT", Type: "page", Title: "Team"}))
	child := mustItem(t)(anna.Create(NewItem{Space: "PLAT", Type: "page", Parent: page.ID, Title: "Private notes"}))
	noErr(t, anna.Restrict(child.ID, Restriction{Edit: []string{"anna"}}))

	// dora may edit the page but not the restricted child. Any move changes
	// the restrictions the child inherits, so dora may not move the page,
	// not even within the space; nor trash the subtree.
	if err := dora.Move(page.ID, "PLAT", ""); !errors.Is(err, ErrForbidden) {
		t.Fatalf("moving a subtree with an item one cannot edit: err = %v, want ErrForbidden", err)
	}
	if err := anna.Move(page.ID, "OPS", ""); !errors.Is(err, ErrForbidden) {
		t.Fatalf("move into a space without a role: err = %v, want ErrForbidden", err)
	}
	noErr(t, anna.Move(child.ID, "PLAT", ""))
	noErr(t, anna.Move(child.ID, "PLAT", page.ID))
	if err := dora.Trash(page.ID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("trash a subtree with an item one cannot edit: err = %v, want ErrForbidden", err)
	}
	noErr(t, anna.Trash(page.ID))
	if err := anna.Purge(page.ID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("editor purges: err = %v, want ErrForbidden", err)
	}
	noErr(t, anna.Restore(page.ID))
	noErr(t, anna.Trash(page.ID))
	noErr(t, pat.Purge(page.ID))
}

func TestGrantingRequiresSpaceAdmin(t *testing.T) {
	s := permFixture(t)
	if err := s.As("anna").Grant("PLAT", "carl", Viewer); !errors.Is(err, ErrForbidden) {
		t.Fatalf("editor grants: err = %v, want ErrForbidden", err)
	}
	page := mustItem(t)(s.As("anna").Create(NewItem{Space: "PLAT", Type: "page", Title: "p"}))
	noErr(t, s.As("pat").Grant("PLAT", "carl", Viewer))
	if _, err := s.As("carl").Get(page.ID); err != nil {
		t.Fatalf("granted viewer cannot read: %v", err)
	}
	if err := s.GrantRole("root", "NOPE", "carl", Viewer); !errors.Is(err, ErrNotFound) {
		t.Fatalf("grant in unknown space: err = %v", err)
	}
	if err := s.GrantRole("root", "PLAT", "carl", Role(9)); !errors.Is(err, ErrValidation) {
		t.Fatalf("unknown role: err = %v", err)
	}
	if err := s.GrantSystemRole("root", "carl", "superuser"); !errors.Is(err, ErrValidation) {
		t.Fatalf("unknown system role: err = %v", err)
	}
	if err := s.AddToGroup("root", "", "carl"); !errors.Is(err, ErrValidation) {
		t.Fatalf("empty group: err = %v", err)
	}
}

func TestSystemAdminHasEveryRole(t *testing.T) {
	s := permFixture(t)
	noErr(t, s.GrantSystemRole("root", "root", RoleAdmin))
	page := mustItem(t)(s.As("anna").Create(NewItem{Space: "PLAT", Type: "page", Title: "p"}))
	noErr(t, s.As("anna").Restrict(page.ID, Restriction{Read: []string{"anna"}}))
	if _, err := s.As("root").Get(page.ID); err != nil {
		t.Fatalf("system admin locked out: %v", err)
	}
	noErr(t, s.As("root").Grant("OPS", "anna", Viewer))
}

// TestRedactIsTheLoggedExceptionToD8: an erasure officer removes content at
// once, even while it is referenced, with a mandatory reason.
func TestRedactIsTheLoggedExceptionToD8(t *testing.T) {
	s := permFixture(t)
	anna, erin := s.As("anna"), s.As("erin")
	page := mustItem(t)(anna.Create(NewItem{Space: "PLAT", Type: "page", Title: "Contains a password"}))
	child := mustItem(t)(anna.Create(NewItem{Space: "PLAT", Type: "page", Parent: page.ID, Title: "Child"}))
	ref := mustItem(t)(anna.Create(NewItem{Space: "PLAT", Type: "page", Title: "Links to it"}))
	noErr(t, anna.CommitBody(ref.ID, Doc{Blocks: []Block{{Kind: Embed, Ref: page.ID}}}))

	if err := anna.Redact(page.ID, "password pasted"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("editor redacts: err = %v, want ErrForbidden", err)
	}
	if err := erin.Redact(page.ID, " "); !errors.Is(err, ErrValidation) {
		t.Fatalf("redact without reason: err = %v, want ErrValidation", err)
	}
	noErr(t, erin.Redact(page.ID, "password pasted"))

	got := mustItem(t)(s.Get(page.ID))
	if got.Presence != Purged || got.Title != "" {
		t.Fatalf("redacted item still has content: %+v", got)
	}
	if c := mustItem(t)(s.Get(child.ID)); c.Presence != Live {
		t.Fatal("redaction targets one item; its children stay")
	}
	last := s.Log()[len(s.Log())-1]
	if last.Kind != ItemRedacted || last.Reason != "password pasted" || last.Actor != "erin" {
		t.Fatalf("redaction event = %+v", last)
	}
	if v := mustViews(t)(anna.Embeds(ref.ID)); v[0].Presence != Purged {
		t.Fatalf("embed of a redacted item = %+v", v[0])
	}
	if err := erin.Redact(page.ID, "again"); !errors.Is(err, ErrValidation) {
		t.Fatalf("redacting a tombstone: err = %v", err)
	}
	if err := erin.Redact("missing", "x"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("redacting unknown: err = %v", err)
	}
}

func TestReplayWithPermissions(t *testing.T) {
	s := permFixture(t)
	anna := s.As("anna")
	page := mustItem(t)(anna.Create(NewItem{Space: "PLAT", Type: "page", Title: "p"}))
	noErr(t, anna.Restrict(page.ID, Restriction{Read: []string{"anna", "@ops"}}))
	noErr(t, s.As("erin").Redact(page.ID, "test"))
	r, err := Replay(s.Log())
	if err != nil {
		t.Fatal(err)
	}
	if got, want := snapshot(r), snapshot(s); got != want {
		t.Fatalf("replayed state differs\n got: %s\nwant: %s", got, want)
	}
	for _, who := range []string{"anna", "ben", "dora", "carl"} {
		if a, b := len(r.As(who).Find(Query{IncludeDeleted: true})), len(s.As(who).Find(Query{IncludeDeleted: true})); a != b {
			t.Fatalf("%s sees %d items after replay, %d before", who, a, b)
		}
	}
}

func mustRels(t *testing.T) func([]Relation, error) []Relation {
	return func(r []Relation, err error) []Relation {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
}

// TestEveryCommandIsChecked runs each session command as an outsider (must
// look like a missing item), as a viewer (forbidden) and as an editor
// (allowed). A command added to Session without a check fails here.
func TestEveryCommandIsChecked(t *testing.T) {
	type fixture struct {
		s                 *Store
		page, task, other Item
		rel               ID
	}
	build := func(t *testing.T) fixture {
		s := permFixture(t)
		anna := s.As("anna")
		page := mustItem(t)(anna.Create(NewItem{Space: "PLAT", Type: "page", Title: "p"}))
		task := mustItem(t)(anna.Create(NewItem{Space: "PLAT", Type: "task", Title: "t", Properties: Props{"priority": "low"}}))
		other := mustItem(t)(anna.Create(NewItem{Space: "PLAT", Type: "page", Title: "o"}))
		noErr(t, anna.AddRelation("relates", page.ID, other.ID))
		rels := mustRels(t)(anna.Relations(page.ID, Outgoing))
		return fixture{s, page, task, other, rels[0].ID}
	}
	commands := map[string]func(x *Session, f fixture) error{
		"SetTitle":       func(x *Session, f fixture) error { return x.SetTitle(f.page.ID, "n") },
		"SetProperty":    func(x *Session, f fixture) error { return x.SetProperty(f.task.ID, "estimate", 2.0) },
		"Transition":     func(x *Session, f fixture) error { return x.Transition(f.task.ID, "finish") },
		"CommitBody":     func(x *Session, f fixture) error { return x.CommitBody(f.page.ID, Doc{}) },
		"AddRelation":    func(x *Session, f fixture) error { return x.AddRelation("blocks", f.task.ID, f.page.ID) },
		"RemoveRelation": func(x *Session, f fixture) error { return x.RemoveRelation(f.rel) },
		"Move":           func(x *Session, f fixture) error { return x.Move(f.task.ID, "PLAT", f.page.ID) },
		"Trash":          func(x *Session, f fixture) error { return x.Trash(f.task.ID) },
		"Restrict":       func(x *Session, f fixture) error { return x.Restrict(f.page.ID, Restriction{}) },
		"CreateEmbedded": func(x *Session, f fixture) error {
			_, err := x.CreateEmbedded(f.page.ID, NewItem{Type: "page", Title: "n"})
			return err
		},
		"Restore": func(x *Session, f fixture) error {
			noErr(t, f.s.Trash("root", f.task.ID)) // trusted setup step
			return x.Restore(f.task.ID)
		},
	}
	for name, cmd := range commands {
		t.Run(name, func(t *testing.T) {
			f := build(t)
			if err := cmd(f.s.As("carl"), f); !errors.Is(err, ErrNotFound) {
				t.Errorf("outsider: err = %v, want ErrNotFound", err)
			}
			f = build(t)
			if err := cmd(f.s.As("ben"), f); !errors.Is(err, ErrForbidden) {
				t.Errorf("viewer: err = %v, want ErrForbidden", err)
			}
			f = build(t)
			if err := cmd(f.s.As("anna"), f); err != nil {
				t.Errorf("editor: err = %v, want nil", err)
			}
		})
	}
	// Reads of unknown or unreadable items look the same.
	f := build(t)
	carl := f.s.As("carl")
	if _, err := carl.Relations(f.page.ID, Outgoing); !errors.Is(err, ErrNotFound) {
		t.Errorf("Relations: err = %v", err)
	}
	if _, err := carl.Embeds(f.page.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("Embeds: err = %v", err)
	}
	if err := carl.RemoveRelation("missing"); !errors.Is(err, ErrNotFound) {
		t.Errorf("RemoveRelation unknown: err = %v", err)
	}
	if err := carl.Purge(f.page.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("Purge unreadable: err = %v", err)
	}
	if err := carl.Redact("missing", "x"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Redact unknown without role: err = %v", err)
	}
	if h := mustEvents(t)(f.s.As("ben").History(f.page.ID)); len(h) == 0 {
		t.Error("viewer must see the history")
	}
	in := mustRels(t)(f.s.As("ben").Relations(f.other.ID, Incoming))
	if len(in) != 1 {
		t.Errorf("incoming relations for a viewer = %d, want 1", len(in))
	}
	noErr(t, f.s.Trash("root", f.task.ID))
	if err := f.s.Restrict("root", f.task.ID, Restriction{}); !errors.Is(err, ErrValidation) {
		t.Errorf("restricting a trashed item: err = %v", err)
	}
}

func mustEvents(t *testing.T) func([]Event, error) []Event {
	return func(e []Event, err error) []Event {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return e
	}
}

// TestHistoryDoesNotLeakUnreadableItems: an item's history includes events
// of other items that touched it (a body that embeds it, a relation). A
// viewer must not see those events when the other item is unreadable.
func TestHistoryDoesNotLeakUnreadableItems(t *testing.T) {
	s := permFixture(t)
	anna, ben := s.As("anna"), s.As("ben")
	task := mustItem(t)(anna.Create(NewItem{Space: "PLAT", Type: "task", Title: "Public task", Properties: Props{"priority": "low"}}))
	secret := mustItem(t)(anna.Create(NewItem{Space: "PLAT", Type: "page", Title: "Secret"}))
	noErr(t, anna.Restrict(secret.ID, Restriction{Read: []string{"anna"}}))
	noErr(t, anna.CommitBody(secret.ID, Doc{Blocks: []Block{
		{Kind: Paragraph, Text: "confidential plan", Mentions: []ID{task.ID}},
	}}))
	noErr(t, anna.AddRelation("relates", secret.ID, task.ID))

	for _, e := range mustEvents(t)(ben.History(task.ID)) {
		if e.Item == secret.ID || (e.Relation != nil && (e.Relation.From == secret.ID || e.Relation.To == secret.ID)) {
			t.Fatalf("viewer sees an event of an unreadable item: %+v", e)
		}
	}
	if n := len(mustEvents(t)(anna.History(task.ID))); n != 3 {
		t.Fatalf("authorised history = %d events, want create, body commit, relation", n)
	}
}
