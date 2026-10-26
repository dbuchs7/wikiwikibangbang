package item

import "testing"

// TestStateSince: an item knows when it reached its state: when it was
// made or by its last transition (ADR-0133); TestImportSetsStateSince
// covers imports. A change that leaves the state keeps the time; an item
// without a workflow has none.
func TestStateSince(t *testing.T) {
	s := setup(t)
	it := mustItem(t)(s.Create("anna", NewItem{Space: "PLAT", Type: "task", Title: "T", Properties: Props{"priority": "low", "assignee": "anna"}}))
	if it.StateSince.IsZero() {
		t.Fatal("a new item has reached its first state")
	}
	made := it.StateSince
	noErr(t, s.SetProperty("anna", it.ID, "priority", "high"))
	got := mustItem(t)(s.Get(it.ID))
	if !got.StateSince.Equal(made) {
		t.Fatalf("a property keeps the time: %v, made %v", got.StateSince, made)
	}
	noErr(t, s.Transition("anna", it.ID, "start"))
	started := mustItem(t)(s.Get(it.ID))
	if !started.StateSince.After(made) {
		t.Fatalf("a transition sets the time: %v, made %v", started.StateSince, made)
	}
	if page := mustItem(t)(s.Create("anna", NewItem{Space: "PLAT", Type: "page", Title: "P"})); !page.StateSince.IsZero() {
		t.Fatalf("a page has no state, so no time: %v", page.StateSince)
	}
	if listed := s.Find(Query{Space: "PLAT", Type: "task"}); len(listed) != 1 || !listed[0].StateSince.Equal(started.StateSince) {
		t.Fatalf("a listing carries the time: %+v", listed)
	}
}
