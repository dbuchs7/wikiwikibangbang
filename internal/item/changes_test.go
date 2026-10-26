package item

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
)

func changeItems(cs []Change) []ID {
	out := []ID{}
	for _, c := range cs {
		out = append(out, c.Item)
	}
	return out
}

func TestChangeFeedListsEveryEventOnWatchedItemsAndTheirAncestors(t *testing.T) {
	s := permFixture(t)
	anna, ben := s.As("anna"), s.As("ben")
	parent := mustItem(t)(anna.Create(NewItem{Space: "PLAT", Type: "page", Title: "Services"}))
	page := mustItem(t)(anna.Create(NewItem{Space: "PLAT", Type: "page", Title: "Backup", Parent: parent.ID}))
	other := mustItem(t)(anna.Create(NewItem{Space: "PLAT", Type: "page", Title: "Other"}))
	noErr(t, anna.SetTitle(page.ID, "Before the watch"))
	start := s.Seq()

	noErr(t, ben.Watch(page.ID, true)) // watching is no change
	noErr(t, anna.SetTitle(page.ID, "Backup v2"))
	noErr(t, anna.CommitBody(page.ID, Doc{Blocks: []Block{{Kind: Paragraph, Text: "steps"}}}))
	noErr(t, anna.SetLabels(page.ID, []string{"service"}))
	mustComment(t)(anna.AddComment(page.ID, "looks good"))
	noErr(t, anna.SetTitle(other.ID, "Not watched"))
	noErr(t, anna.SetTitle(parent.ID, "Services v2")) // an ancestor
	noErr(t, anna.Restrict(page.ID, Restriction{Edit: []string{"anna"}}))
	noErr(t, anna.Watch(other.ID, true)) // someone else's watch is no change of ben's

	got, last := ben.Changes(start, 100)
	want := []ID{page.ID, page.ID, page.ID, page.ID, parent.ID, page.ID}
	if !slices.Equal(changeItems(got), want) || last != s.Seq() {
		t.Fatalf("changes %v, last %d; want %v, %d", changeItems(got), last, want, s.Seq())
	}
	for i := 1; i < len(got); i++ {
		if got[i].Seq <= got[i-1].Seq || got[i].At.IsZero() {
			t.Fatalf("changes out of order or without time: %+v", got)
		}
	}

	// Paging: the cursor moves on by last, also past events that are not
	// listed.
	first, last := ben.Changes(start, 2)
	if len(first) != 2 || last != first[1].Seq {
		t.Fatalf("first page %+v, last %d", first, last)
	}
	rest, last := ben.Changes(last, 100)
	if len(rest) != len(want)-2 || last != s.Seq() {
		t.Fatalf("rest %+v, last %d", rest, last)
	}
	if none, last := ben.Changes(s.Seq(), 100); len(none) != 0 || last != s.Seq() {
		t.Fatalf("after the head: %v, %d", none, last)
	}
	if none, last := ben.Changes(s.Seq()+5, 100); none != nil || last != s.Seq() {
		t.Fatalf("beyond the head: %v, %d", none, last)
	}
	// From the start, the grant of ben's role comes first.
	if all, _ := ben.Changes(-1, 100); len(all) != len(want)+1 || all[0].Item != "" {
		t.Fatalf("from the start: %v", changeItems(all))
	}
}

func TestChangeFeedCountsTheWatchesOfTheirTime(t *testing.T) {
	s := permFixture(t)
	anna, ben := s.As("anna"), s.As("ben")
	page := mustItem(t)(anna.Create(NewItem{Space: "PLAT", Type: "page", Title: "Runbook"}))
	start := s.Seq()
	noErr(t, anna.SetTitle(page.ID, "Not watched yet"))
	noErr(t, ben.Watch(page.ID, true))
	noErr(t, anna.SetTitle(page.ID, "Watched"))
	noErr(t, ben.Watch(page.ID, false))
	noErr(t, anna.SetTitle(page.ID, "Not watched any more"))
	noErr(t, ben.WatchSpace("PLAT", true))
	noErr(t, anna.SetTitle(page.ID, "Watched through the space"))
	got, _ := ben.Changes(start, 100)
	if len(got) != 2 || got[0].Item != page.ID || got[1].Item != page.ID {
		t.Fatalf("changes %+v", got)
	}
	// Ended watches still count for the events of their time.
	noErr(t, ben.WatchSpace("PLAT", false))
	if again, _ := ben.Changes(start, 100); len(again) != 2 {
		t.Fatalf("after unwatching: %+v", again)
	}
}

func TestChangeFeedNeverRevealsAnUnreadableItem(t *testing.T) {
	s := permFixture(t)
	anna, ben := s.As("anna"), s.As("ben")
	parent := mustItem(t)(anna.Create(NewItem{Space: "PLAT", Type: "page", Title: "Secret services"}))
	page := mustItem(t)(anna.Create(NewItem{Space: "PLAT", Type: "page", Title: "Copied page", Parent: parent.ID}))
	hidden := mustItem(t)(anna.Create(NewItem{Space: "PLAT", Type: "page", Title: "Hidden from the start"}))
	noErr(t, anna.Restrict(hidden.ID, Restriction{Read: []string{"anna"}}))
	noErr(t, ben.Watch(page.ID, true))
	noErr(t, ben.WatchSpace("PLAT", true))
	start := s.Seq()

	// The restriction of an ancestor hides the watched page: ben is told
	// that something he watches changed, not what.
	noErr(t, anna.Restrict(parent.ID, Restriction{Read: []string{"anna"}}))
	noErr(t, anna.SetTitle(page.ID, "Classified title"))
	noErr(t, anna.CommitBody(page.ID, Doc{Blocks: []Block{{Kind: Paragraph, Text: "classified content"}}}))
	// An item ben never read, seen only through the space watch, is not
	// listed at all: unreadable is non-existent (D10).
	noErr(t, anna.SetTitle(hidden.ID, "Still hidden"))
	noErr(t, anna.CommitBody(hidden.ID, Doc{Blocks: []Block{{Kind: Paragraph, Text: "hidden content"}}}))

	got, _ := ben.Changes(start, 100)
	if !slices.Equal(changeItems(got), []ID{"", "", ""}) {
		t.Fatalf("changes %+v", got)
	}
	dump := fmt.Sprintf("%+v", got)
	for _, leak := range []string{string(parent.ID), string(page.ID), string(hidden.ID), parent.Key, page.Key, hidden.Key,
		"Secret", "Classified", "classified", "Hidden", "hidden", "Copied"} {
		if strings.Contains(dump, leak) {
			t.Fatalf("the feed reveals %q: %s", leak, dump)
		}
	}

	// A held draft above a readable watched page: its changes are not told,
	// as ben may not read it and it hides nothing he reads (ADR-0058).
	noErr(t, anna.Restrict(parent.ID, Restriction{}))
	s.SetAssessor(testAssessor)
	mark := s.Seq()
	noErr(t, anna.CommitBody(parent.ID, Doc{Blocks: []Block{{Kind: Paragraph, Text: "Diagnose: flu"}}}))
	if _, err := ben.Get(parent.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("the parent is not held: %v", err)
	}
	if _, err := ben.Get(page.ID); err != nil {
		t.Fatalf("the watched page is hidden: %v", err)
	}
	if got, _ := ben.Changes(mark, 100); len(got) != 0 {
		t.Fatalf("changes of a held ancestor: %+v", got)
	}
	s.SetAssessor(nil)

	// One stops watching an item one may no longer read by its ID; one
	// who never watched it learns nothing.
	noErr(t, anna.Restrict(page.ID, Restriction{Read: []string{"anna"}}))
	if err := ben.Watch(page.ID, true); !errors.Is(err, ErrNotFound) {
		t.Fatalf("watching an unreadable item: %v", err)
	}
	noErr(t, ben.Watch(page.ID, false))
	if ben.Watching(page.ID) {
		t.Fatal("the watch did not end")
	}
	if err := s.As("carl").Watch(page.ID, false); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unwatching without a watch: %v", err)
	}
	if err := s.SetWatch("root", "ben", "no-such-item", "", false); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unwatching an unknown item: %v", err)
	}
}

func TestChangeFeedTellsTrashPurgeAndOwnAccess(t *testing.T) {
	s := permFixture(t)
	anna, ben, pat := s.As("anna"), s.As("ben"), s.As("pat")
	parent := mustItem(t)(anna.Create(NewItem{Space: "PLAT", Type: "page", Title: "Parent"}))
	page := mustItem(t)(anna.Create(NewItem{Space: "PLAT", Type: "page", Title: "Child", Parent: parent.ID}))
	noErr(t, ben.Watch(page.ID, true))
	start := s.Seq()

	noErr(t, anna.Trash(parent.ID)) // the child goes with it
	if got, _ := ben.Changes(start, 100); !slices.Equal(changeItems(got), []ID{parent.ID}) {
		t.Fatalf("trash: %+v", got)
	}
	noErr(t, pat.Purge(parent.ID))
	// Purged, the items are named no more; the events are still told.
	got, _ := ben.Changes(start, 100)
	if !slices.Equal(changeItems(got), []ID{"", ""}) {
		t.Fatalf("trash and purge: %+v", got)
	}

	mark := s.Seq()
	noErr(t, s.GrantRole("root", "PLAT", "ben", Editor))    // ben's own role
	noErr(t, s.GrantRole("root", "PLAT", "carl", Viewer))   // someone else's
	noErr(t, s.AddToGroup("root", "ops", "ben"))            // ben joins a group
	noErr(t, s.GrantRole("root", "PLAT", "@ops", Editor))   // a role of his group
	noErr(t, s.GrantRole("root", "PLAT", "@other", Editor)) // a role of another group
	noErr(t, s.GrantSystemRole("root", "ben", RoleErasure))
	noErr(t, s.RevokeRole("root", "PLAT", "ben"))
	noErr(t, s.RenameSpace("root", "OPS", "Operations")) // a space of his group
	noErr(t, s.CreateSpace("root", "HR", "People"))
	noErr(t, s.RenameSpace("root", "HR", "Human resources")) // ben holds no role there
	noErr(t, s.TrashSpace("root", "PLAT"))
	got, _ = ben.Changes(mark, 100)
	if !slices.Equal(changeItems(got), []ID{"", "", "", "", "", "", ""}) {
		t.Fatalf("own access: %+v", got)
	}

	// A space watch, and an item watch in a space, tell what happens to the
	// space.
	s2 := permFixture(t)
	a2, b2 := s2.As("anna"), s2.As("ben")
	p2 := mustItem(t)(a2.Create(NewItem{Space: "PLAT", Type: "page", Title: "Page"}))
	noErr(t, s2.RevokeRole("root", "PLAT", "ben"))
	noErr(t, s2.GrantRole("root", "PLAT", "@readers", Viewer))
	noErr(t, s2.AddToGroup("root", "readers", "ben"))
	noErr(t, b2.Watch(p2.ID, true))
	noErr(t, s2.RevokeRole("root", "PLAT", "@readers"))
	mark = s2.Seq()
	noErr(t, s2.RenameSpace("root", "PLAT", "Platform"))
	if got, _ := b2.Changes(mark, 100); len(got) != 1 || got[0].Item != "" {
		t.Fatalf("space renamed under a watched item: %+v", got)
	}
}

func TestOwnRolesListEveryGrantOfThePrincipal(t *testing.T) {
	s := permFixture(t)
	noErr(t, s.GrantRole("root", "PLAT", "dora", Viewer))
	noErr(t, s.GrantRole("root", "PLAT", "@ops", Editor))
	noErr(t, s.GrantRole("root", "PLAT", "@other", SpaceAdmin))
	noErr(t, s.GrantSystemRole("root", "dora", RoleErasure))
	system, grants := s.As("dora").OwnRoles()
	want := []OwnGrant{{Space: "OPS", Role: Editor, Via: "@ops"}, {Space: "PLAT", Role: Viewer}, {Space: "PLAT", Role: Editor, Via: "@ops"}}
	if !slices.Equal(system, []string{RoleErasure}) || !slices.Equal(grants, want) {
		t.Fatalf("dora: %v %+v", system, grants)
	}
	noErr(t, s.TrashSpace("root", "OPS"))
	if _, grants := s.As("dora").OwnRoles(); len(grants) != 2 || grants[0].Space != "PLAT" {
		t.Fatalf("after trashing OPS: %+v", grants)
	}
	system, grants = s.As("carl").OwnRoles()
	if system == nil || grants == nil || len(system)+len(grants) != 0 {
		t.Fatalf("carl: %#v %#v", system, grants)
	}
}
