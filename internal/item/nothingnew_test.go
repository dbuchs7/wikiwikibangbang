package item

import (
	"errors"
	"testing"
)

var errFull = errors.New("full")

// TestNothingNewIsMadeWhileTheCheckSaysSo: while the check refuses, every
// command that makes an item, a space or a kanban fails with its error and
// leaves the log as it was, an access request with the task it brings
// included; changing, commenting, labelling, moving to the trash and
// purging what exists, and keeping an imported item in step, go on; with
// the check lifted, everything is made again; replay never consults it
// (ADR-0163).
func TestNothingNewIsMadeWhileTheCheckSaysSo(t *testing.T) {
	s, page, sec := accessFixture(t, "olga")
	todo := builtinTypes()[1]
	todo.Name = "todo"
	noErr(t, s.DefineType("admin", todo))
	anna := s.As("anna")
	task := mustItem(t)(anna.Create(NewItem{Space: "PLAT", Type: "todo", Title: "Budget", Properties: Props{"priority": "low"}}))
	board, err := anna.SetKanban("PLAT", Kanban{Name: "Web", Type: "todo"})
	noErr(t, err)
	origin := Origin{System: "sync", Instance: "x", ExternalID: "1"}
	imported, _, err := s.Upsert("sync", Upsert{Origin: origin, Space: "PLAT", Type: "page", Title: "Imported"})
	noErr(t, err)

	full := true
	s.SetNothingNew(func() error {
		if full {
			return errFull
		}
		return nil
	})
	before := len(s.Log())
	refused := map[string]error{}
	_, refused["item"] = anna.Create(NewItem{Space: "PLAT", Type: "page", Title: "New"})
	_, refused["child"] = anna.Create(NewItem{Space: "PLAT", Type: "page", Parent: page.ID, Title: "Child"})
	_, refused["folder"] = anna.CreateFolder("PLAT", "", "Pictures")
	refused["space"] = s.CreateSpace("admin", "NEW", "New")
	_, refused["kanban"] = anna.SetKanban("PLAT", Kanban{Name: "Other", Type: "todo"})
	_, _, refused["import"] = s.Upsert("sync", Upsert{Origin: Origin{System: "sync", Instance: "x", ExternalID: "2"}, Space: "PLAT", Type: "page", Title: "New import"})
	refused["request"] = s.As("ben").RequestSecret(page.ID, sec.ID)
	for what, err := range refused {
		if !errors.Is(err, errFull) {
			t.Errorf("%s while nothing new is made: %v", what, err)
		}
	}
	if n := len(s.Log()); n != before {
		t.Fatalf("%d events recorded while nothing new is made", n-before)
	}
	if views, _ := s.As("anna").Secrets(page.ID); len(views) != 1 || len(views[0].Asks) != 0 {
		t.Errorf("a request stands without its task: %+v", views)
	}

	// What exists still changes.
	noErr(t, anna.SetTitle(task.ID, "Budget 2027"))
	noErr(t, anna.SetProperty(task.ID, "priority", "high"))
	noErr(t, anna.Transition(task.ID, "finish"))
	noErr(t, anna.SetLabels(task.ID, []string{"money"}))
	_, err = anna.AddComment(task.ID, "Done for this year.")
	noErr(t, err)
	board.Name = "Website"
	_, err = anna.SetKanban("PLAT", board)
	noErr(t, err)
	if _, changed, err := s.Upsert("sync", Upsert{Origin: origin, Space: "PLAT", Type: "page", Title: "Imported, changed"}); err != nil || !changed {
		t.Errorf("an imported item kept in step: %v %v", changed, err)
	}
	noErr(t, anna.Trash(task.ID))
	noErr(t, s.Purge("admin", task.ID))
	noErr(t, anna.Trash(imported.ID))
	noErr(t, anna.Restore(imported.ID))

	// With room again, everything is made.
	full = false
	mustItem(t)(anna.Create(NewItem{Space: "PLAT", Type: "page", Title: "New"}))
	noErr(t, s.CreateSpace("admin", "NEW", "New"))
	noErr(t, s.As("ben").RequestSecret(page.ID, sec.ID))
	taskOf(t, s, page, sec, "ben")

	// Replay rebuilds the same state, whatever the check would say.
	full = true
	r, err := Replay(s.Log(), WithIDs(s.newID))
	noErr(t, err)
	if !r.HasSpace("NEW") || len(r.Log()) != len(s.Log()) {
		t.Error("replay is not the same")
	}
}

// TestWhatCreatesIsTold: the classification itself, event by event.
func TestWhatCreatesIsTold(t *testing.T) {
	s := permFixture(t)
	k, err := s.As("anna").SetKanban("PLAT", Kanban{Name: "Web", Type: "task"})
	noErr(t, err)
	it := mustItem(t)(s.As("anna").Create(NewItem{Space: "PLAT", Type: "page", Title: "x"}))
	for _, c := range []struct {
		e    Event
		want bool
	}{
		{Event{Kind: ItemCreated}, true},
		{Event{Kind: SpaceCreated}, true},
		{Event{Kind: ItemImported, Item: "new"}, true},
		{Event{Kind: ItemImported, Item: it.ID}, false},
		{Event{Kind: KanbanSet, Space: "PLAT", Kanban: &Kanban{ID: "new"}}, true},
		{Event{Kind: KanbanSet, Space: "PLAT", Kanban: &k}, false},
		{Event{Kind: TitleSet, Item: it.ID}, false},
		{Event{Kind: CommentAdded, Item: it.ID}, false},
		{Event{Kind: TypeDefined}, false},
	} {
		if got := s.creates([]Event{c.e}); got != c.want {
			t.Errorf("%s creates: %v", c.e.Kind, got)
		}
	}
}
