package item

import "testing"

// TestRelationSummary: a card counts its hand-made relations to items the
// reader may read, and the unfinished items it waits on: those it depends
// on and those that block it (ADR-0137). Relations derived from a body and
// items the reader may not read are left out.
func TestRelationSummary(t *testing.T) {
	s := setup(t)
	noErr(t, s.GrantRole("admin", "PLAT", "anna", Editor))
	noErr(t, s.GrantRole("admin", "OPS", "anna", Viewer))
	noErr(t, s.GrantRole("admin", "PLAT", "vera", Viewer))
	task := func(space, title string) Item {
		return mustItem(t)(s.Create("anna", NewItem{Space: space, Type: "task", Title: title, Properties: Props{"priority": "low", "assignee": "anna"}}))
	}
	card, dep, blocker, done, other := task("PLAT", "Card"), task("PLAT", "Dependency"), task("PLAT", "Blocker"), task("PLAT", "Done"), task("OPS", "Elsewhere")
	noErr(t, s.Transition("anna", done.ID, "finish"))
	noErr(t, s.AddRelation("anna", RelDependsOn, card.ID, dep.ID))
	noErr(t, s.AddRelation("anna", RelBlocks, blocker.ID, card.ID))
	noErr(t, s.AddRelation("anna", RelDependsOn, card.ID, done.ID))
	noErr(t, s.AddRelation("anna", "relates-to", card.ID, other.ID))

	related, waiting, err := s.As("anna").RelationSummary(card.ID)
	noErr(t, err)
	if related != 4 || waiting != 2 {
		t.Fatalf("anna: %d related, %d waiting; want 4 and 2", related, waiting)
	}
	// vera may not read OPS: the relation to it does not count for her.
	related, waiting, err = s.As("vera").RelationSummary(card.ID)
	noErr(t, err)
	if related != 3 || waiting != 2 {
		t.Fatalf("vera: %d related, %d waiting; want 3 and 2", related, waiting)
	}
	// The dependency done, the card waits on its blocker only.
	noErr(t, s.Transition("anna", dep.ID, "finish"))
	if _, waiting, _ = s.As("anna").RelationSummary(card.ID); waiting != 1 {
		t.Fatalf("waiting %d, want 1", waiting)
	}
	if _, _, err := s.As("carla").RelationSummary(card.ID); err == nil {
		t.Fatal("an outsider learns of the card")
	}
}
