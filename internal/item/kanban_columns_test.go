package item

import (
	"slices"
	"testing"
)

// The task workflow of the tests: open → in progress (start, needs an
// assignee), open|in progress → done (finish), done → open (reopen).
func columnsBoard(t *testing.T) (*Store, *Session, Kanban, []Item) {
	t.Helper()
	s := setup(t)
	noErr(t, s.GrantRole("admin", "PLAT", "anna", Editor))
	noErr(t, s.GrantRole("admin", "PLAT", "vera", Viewer))
	anna := s.As("anna")
	k, err := anna.SetKanban("PLAT", Kanban{Name: "Web", Type: "task"})
	noErr(t, err)
	var cards []Item
	for _, title := range []string{"A", "B", "C"} {
		it := mustItem(t)(s.Create("anna", NewItem{Space: "PLAT", Type: "task", Title: title, Properties: Props{"priority": "low", "assignee": "anna"}}))
		noErr(t, s.SetLabels("anna", it.ID, []string{k.Label}))
		cards = append(cards, it)
	}
	return s, anna, k, cards
}

func columnCards(t *testing.T, x *Session, k Kanban) map[string][]ID {
	t.Helper()
	_, board, err := x.Board("PLAT", k.ID)
	noErr(t, err)
	out := map[string][]ID{}
	for _, c := range board {
		out[c.Name] = c.Cards
	}
	return out
}

func TestKanbanColumnsAndPlaces(t *testing.T) {
	s, anna, k, cards := columnsBoard(t)
	a, b, c := cards[0].ID, cards[1].ID, cards[2].ID
	if got := columnCards(t, anna, k); !slices.Equal(got["open"], []ID{a, b, c}) || len(got) != 3 {
		t.Fatalf("the workflow's columns %v", got)
	}
	k.Columns = []KanbanColumn{{Name: "Backlog"}, {Name: "To do", State: "open"}, {Name: "Doing", State: "in progress"}, {Name: "Review"}}
	k.CardFields = []string{"assignee", "priority", "labels", "state"}
	k, err := anna.SetKanban("PLAT", k)
	noErr(t, err)
	backlog, todo, doing, review := k.Columns[0].ID, k.Columns[1].ID, k.Columns[2].ID, k.Columns[3].ID
	if todo != "state:open" || backlog == "" || backlog == review {
		t.Fatalf("column IDs %+v", k.Columns)
	}
	got := columnCards(t, anna, k)
	if !slices.Equal(got["To do"], []ID{a, b, c}) || len(got["done"]) != 0 || len(got) != 5 {
		t.Fatalf("a missing state gets a column: %v", got)
	}

	// Into a column of its own: the state stays.
	noErr(t, anna.PlaceCard("PLAT", k.ID, b, backlog, ""))
	noErr(t, anna.PlaceCard("PLAT", k.ID, c, backlog, b))
	if got := columnCards(t, anna, k); !slices.Equal(got["Backlog"], []ID{c, b}) || !slices.Equal(got["To do"], []ID{a}) {
		t.Fatalf("after placing %v", got)
	}
	if it, _ := s.Get(b); it.State != "open" {
		t.Errorf("a column of its own changed the state: %s", it.State)
	}
	// Into a column of a state: the transition is performed; back too, as
	// long as the workflow has the move.
	noErr(t, anna.PlaceCard("PLAT", k.ID, b, doing, ""))
	if it, _ := s.Get(b); it.State != "in progress" {
		t.Fatalf("state %s", it.State)
	}
	isInvalid(t, "a move the workflow lacks", anna.PlaceCard("PLAT", k.ID, b, todo, ""), "no transition from in progress to open")
	noErr(t, anna.PlaceCard("PLAT", k.ID, b, review, ""))
	// A card whose state changes elsewhere leaves its own column.
	noErr(t, anna.Transition(b, "finish"))
	if got := columnCards(t, anna, k); !slices.Contains(got["done"], b) || slices.Contains(got["Review"], b) {
		t.Fatalf("after finishing elsewhere %v", got)
	}

	// A move the workflow lacks is added by an administrator.
	if _, err := anna.AddTransition("task", "in progress", "open"); err == nil {
		t.Error("an editor changed a type")
	}
	admin := s.As("admin")
	noErr(t, s.GrantSystemRole("admin", "admin", RoleAdmin))
	td, err := admin.AddTransition("task", "in progress", "open")
	noErr(t, err)
	td, err = admin.AddTransition("task", "done", "open")
	noErr(t, err)
	if i := slices.IndexFunc(td.Workflow.Transitions, func(tr Transition) bool { return tr.Name == "to open" }); i < 0 || !slices.Equal(td.Workflow.Transitions[i].From, []string{"in progress", "done"}) {
		t.Fatalf("transitions %+v", td.Workflow.Transitions)
	}
	if again, err := admin.AddTransition("task", "done", "open"); err != nil || again.Version != td.Version {
		t.Errorf("adding it again: %v %v", again.Version, err)
	}
	for _, bad := range [][2]string{{"open", "open"}, {"open", "nowhere"}} {
		if _, err := admin.AddTransition("task", bad[0], bad[1]); err == nil {
			t.Errorf("%v was added", bad)
		}
	}
	if _, err := admin.AddTransition("page", "a", "b"); err == nil {
		t.Error("a type without a workflow")
	}
	// done → open now has two transitions: reopen and "to open".
	isInvalid(t, "two transitions", anna.PlaceCard("PLAT", k.ID, b, todo, ""), "several transitions")
	noErr(t, anna.PlaceCard("PLAT", k.ID, a, doing, ""))
	noErr(t, anna.PlaceCard("PLAT", k.ID, a, todo, ""))

	// Who may not edit, and what is not a card, is refused.
	if err := s.As("vera").PlaceCard("PLAT", k.ID, a, backlog, ""); err == nil {
		t.Error("a viewer moved a card")
	}
	page := mustItem(t)(s.Create("anna", NewItem{Space: "PLAT", Type: "page", Title: "P"}))
	isInvalid(t, "not a card", anna.PlaceCard("PLAT", k.ID, page.ID, backlog, ""), "not a card")
	if err := anna.PlaceCard("PLAT", k.ID, a, "nowhere", ""); err == nil {
		t.Error("an unknown column")
	}
	if err := anna.PlaceCard("PLAT", "nokanban", a, backlog, ""); err == nil {
		t.Error("an unknown kanban")
	}
	if err := anna.PlaceCard("PLAT", k.ID, "missing", backlog, ""); err == nil {
		t.Error("a missing card")
	}
	if _, _, err := s.As("nobody").Board("PLAT", k.ID); err == nil {
		t.Error("someone without a role saw a board")
	}

	// Renaming keeps columns and fields.
	renamed, err := anna.SetKanban("PLAT", Kanban{ID: k.ID, Name: "Website"})
	noErr(t, err)
	if len(renamed.Columns) != 4 || len(renamed.CardFields) != 4 {
		t.Errorf("renaming lost the columns or fields: %+v", renamed)
	}
}

func TestKanbanColumnsAreChecked(t *testing.T) {
	_, anna, k, _ := columnsBoard(t)
	long := ""
	for range 41 {
		long += "x"
	}
	for name, change := range map[string]func(*Kanban){
		"a state twice":    func(k *Kanban) { k.Columns = []KanbanColumn{{Name: "A", State: "open"}, {Name: "B", State: "open"}} },
		"an unknown state": func(k *Kanban) { k.Columns = []KanbanColumn{{Name: "A", State: "nowhere"}} },
		"an empty name":    func(k *Kanban) { k.Columns = []KanbanColumn{{Name: " "}} },
		"a long name":      func(k *Kanban) { k.Columns = []KanbanColumn{{Name: long}} },
		"an ID twice":      func(k *Kanban) { k.Columns = []KanbanColumn{{ID: "x", Name: "A"}, {ID: "x", Name: "B"}} },
		"too many columns": func(k *Kanban) { k.Columns = make([]KanbanColumn, maxColumns+1) },
		"an unknown field": func(k *Kanban) { k.CardFields = []string{"colour"} },
		"a field twice":    func(k *Kanban) { k.CardFields = []string{"priority", "priority"} },
		"too many fields":  func(k *Kanban) { k.CardFields = make([]string, maxCardFields+1) },
	} {
		bad := k
		change(&bad)
		if _, err := anna.SetKanban("PLAT", bad); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	// No columns of its own: back to the workflow's.
	k.Columns = []KanbanColumn{{Name: "Backlog"}}
	k, err := anna.SetKanban("PLAT", k)
	noErr(t, err)
	k.Columns = []KanbanColumn{}
	k, err = anna.SetKanban("PLAT", k)
	noErr(t, err)
	if k.Columns != nil {
		t.Errorf("columns %+v", k.Columns)
	}
}
