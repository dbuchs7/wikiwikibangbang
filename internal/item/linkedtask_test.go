package item

import (
	"errors"
	"slices"
	"testing"
	"time"
)

// day is midnight UTC of a date, as a date field holds it.
func day(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }

// engineTaskType is a type for the user tasks of an engine: a due date the
// engine sets, a note people keep, and a workflow whose open states each
// reach done and cancelled by one transition.
func engineTaskType() TypeDef {
	return TypeDef{
		Name: "engine-task", Body: true,
		Fields: []FieldDef{{Name: "due", Kind: KindDate}, {Name: "note", Kind: KindText}},
		Workflow: &Workflow{
			Initial: "open",
			States:  []string{"open", "in progress", "done", "cancelled"},
			Transitions: []Transition{
				{Name: "start", From: []string{"open"}, To: "in progress"},
				{Name: "finish", From: []string{"open", "in progress"}, To: "done"},
				{Name: "cancel", From: []string{"open", "in progress"}, To: "cancelled"},
			},
		},
	}
}

// linkSetup is setup with the engine-task type and anna, an editor of PLAT.
func linkSetup(t *testing.T) *Store {
	t.Helper()
	s := setup(t)
	noErr(t, s.DefineType("admin", engineTaskType()))
	noErr(t, s.GrantRole("admin", "PLAT", "anna", Editor))
	return s
}

func approval(key string) LinkedTask {
	return LinkedTask{Engine: "atlas-prod", Key: key, URL: "https://atlas.example/#/tasks/t/" + key,
		Space: "PLAT", Type: "engine-task", Title: "Approve holidays",
		Fields: Props{"due": day(2026, 11, 2)}, Labels: []string{"atlas", "Holidays"}}
}

// TestALinkedTaskIsMadeHiddenAndKeepsWhatPeopleAdd (ADR-0151): the item of a
// task is made, in one batch, restricted to the synchronisation and with
// its labels, so nobody reads it before the engine has said who may; a
// later run updates the title and the fields the engine owns and leaves
// the labels, the other fields, the state and the restriction as they are.
func TestALinkedTaskIsMadeHiddenAndKeepsWhatPeopleAdd(t *testing.T) {
	s := linkSetup(t)
	it, changed, err := s.LinkTask("engine:atlas-prod", approval("4711"))
	if err != nil || !changed {
		t.Fatalf("LinkTask = %v %v", changed, err)
	}
	if it.Linked != "atlas-prod" || !slices.Equal(it.LinkedFields, []string{"due"}) || it.State != "open" ||
		it.Origin == nil || it.Origin.System != LinkedSystem || it.Origin.ExternalID != "task/4711" || it.Origin.ExternalKey != "4711" {
		t.Fatalf("linked item = %+v (%+v)", it, it.Origin)
	}
	if !slices.Equal(it.Labels, []string{"atlas", "holidays"}) {
		t.Errorf("labels = %v", it.Labels)
	}
	if !slices.Equal(it.Restriction.Read, []string{"engine:atlas-prod"}) {
		t.Errorf("restriction = %+v, want the synchronisation alone", it.Restriction)
	}
	log := s.Log()
	if last := log[len(log)-1]; last.Batch == 0 || log[len(log)-3].Batch != last.Batch {
		t.Fatalf("the item, its restriction and its labels are not one batch: %+v", log[len(log)-3:])
	}
	if _, err := s.As("anna").Get(it.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("anna reads a linked task the engine has not shown her: %v", err)
	}

	// People keep what is theirs.
	noErr(t, s.Restrict("admin", it.ID, Restriction{}))
	anna := s.As("anna")
	noErr(t, anna.SetProperty(it.ID, "note", "call Ben"))
	noErr(t, anna.SetLabels(it.ID, []string{"atlas", "holidays", "q4"}))
	noErr(t, anna.Transition(it.ID, "start"))
	noErr(t, s.Restrict("admin", it.ID, Restriction{Read: []string{"anna"}}))

	again := approval("4711")
	again.Title, again.Fields = "Approve holidays (Ben)", Props{"due": day(2026, 11, 5)}
	again.Labels = []string{"other"}
	it2, changed, err := s.LinkTask("engine:atlas-prod", again)
	if err != nil || !changed {
		t.Fatalf("LinkTask again = %v %v", changed, err)
	}
	if it2.ID != it.ID || it2.Title != "Approve holidays (Ben)" || it2.Properties["due"] != day(2026, 11, 5) ||
		it2.Properties["note"] != "call Ben" || it2.State != "in progress" ||
		!slices.Equal(it2.Labels, []string{"atlas", "holidays", "q4"}) || !slices.Equal(it2.Restriction.Read, []string{"anna"}) {
		t.Fatalf("after a second run = %+v", it2)
	}
	if _, changed, err := s.LinkTask("engine:atlas-prod", again); err != nil || changed {
		t.Errorf("an unchanged task wrote %v %v", changed, err)
	}
	// A due date the engine no longer sets is cleared.
	again.Fields = Props{"due": nil}
	it3, _, err := s.LinkTask("engine:atlas-prod", again)
	if err != nil || it3.Properties["due"] != nil || it3.Properties["note"] != "call Ben" {
		t.Fatalf("clearing the due date = %+v %v", it3.Properties, err)
	}
	if got := s.LinkedTasks("atlas-prod"); len(got) != 1 || got[0].ID != it.ID {
		t.Errorf("LinkedTasks = %v", got)
	}
	if got := s.LinkedTasks("other"); len(got) != 0 {
		t.Errorf("another engine's tasks = %v", got)
	}
}

// TestALinkedTaskRefusesBadInput: an engine and a key are needed, a label
// must be one, and the type's own checks hold.
func TestALinkedTaskRefusesBadInput(t *testing.T) {
	s := linkSetup(t)
	for _, bad := range []LinkedTask{
		{Engine: "Atlas Prod", Key: "1", Space: "PLAT", Type: "engine-task", Title: "x"},
		{Engine: "atlas-prod", Key: " ", Space: "PLAT", Type: "engine-task", Title: "x"},
		{Engine: "atlas-prod", Key: "1", Space: "PLAT", Type: "engine-task", Title: "x", Labels: []string{"no!"}},
		{Engine: "atlas-prod", Key: "1", Space: "NOPE", Type: "engine-task", Title: "x"},
		{Engine: "atlas-prod", Key: "1", Space: "PLAT", Type: "engine-task", Title: "x", Fields: Props{"due": 7}},
	} {
		if _, _, err := s.LinkTask("engine:atlas-prod", bad); err == nil {
			t.Errorf("LinkTask(%+v) was accepted", bad)
		}
	}
}

// TestPeopleMayNotDoWhatOnlyTheEngineDecides: on a linked task a person
// may not rename it, set a field the engine owns, finish it, lift or set
// its restriction, move it to another space, or remove it while it is open
// — and the same person may comment, label, start it and set other fields.
func TestPeopleMayNotDoWhatOnlyTheEngineDecides(t *testing.T) {
	s := linkSetup(t)
	it := mustItem(t)(func() (Item, error) { it, _, err := s.LinkTask("engine:atlas-prod", approval("1")); return it, err }())
	noErr(t, s.Restrict("admin", it.ID, Restriction{}))
	// anna may make items in OPS, so a refused move is the task's doing.
	noErr(t, s.GrantRole("admin", "OPS", "anna", Editor))
	anna := s.As("anna")
	for what, err := range map[string]error{
		"rename":           anna.SetTitle(it.ID, "mine"),
		"set due":          anna.SetProperty(it.ID, "due", day(2027, 1, 1)),
		"finish":           anna.Transition(it.ID, "finish"),
		"cancel":           anna.Transition(it.ID, "cancel"),
		"restrict":         anna.Restrict(it.ID, Restriction{Read: []string{"anna"}}),
		"move to OPS":      anna.Move(it.ID, "OPS", ""),
		"place in OPS":     anna.Place(it.ID, "OPS", "", ""),
		"trash while open": anna.Trash(it.ID),
	} {
		if !errors.Is(err, ErrForbidden) {
			t.Errorf("%s: %v, want forbidden", what, err)
		}
	}
	noErr(t, anna.SetProperty(it.ID, "note", "fine"))
	noErr(t, anna.SetLabels(it.ID, []string{"mine"}))
	noErr(t, anna.Transition(it.ID, "start"))

	// A page holding a linked task does not leave the space either.
	page := mustItem(t)(anna.Create(NewItem{Space: "PLAT", Type: "page", Title: "Holidays"}))
	noErr(t, s.Move("admin", it.ID, "PLAT", page.ID))
	if err := anna.Move(page.ID, "OPS", ""); !errors.Is(err, ErrForbidden) {
		t.Errorf("moving the page holding a linked task to OPS: %v", err)
	}

	// Once the engine has ended it, it may go.
	noErr(t, s.EndTask("engine:atlas-prod", it.ID, "done"))
	noErr(t, anna.Trash(it.ID))
}

// TestADetachedTaskIsTheWikisAgain (ADR-0152): a task the synchronisation
// can no longer follow is detached, not ended — the item keeps its origin,
// state, fields, labels and readers, and people may finish, move and
// remove it as any item; detaching an engine's tasks is one batch; a task
// that comes back is linked again to the same item.
func TestADetachedTaskIsTheWikisAgain(t *testing.T) {
	s := linkSetup(t)
	link := func(key string) Item {
		return mustItem(t)(func() (Item, error) { it, _, err := s.LinkTask("engine:atlas-prod", approval(key)); return it, err }())
	}
	one, two, three := link("1"), link("2"), link("3")
	noErr(t, s.Restrict("admin", one.ID, Restriction{}))
	noErr(t, s.SetLabels("admin", one.ID, []string{"atlas", "q4"}))
	noErr(t, s.EndTask("engine:atlas-prod", three.ID, "done"))

	detached, err := s.DetachTask("engine:atlas-prod", one.ID)
	if err != nil || !detached {
		t.Fatalf("DetachTask = %v %v", detached, err)
	}
	got, _ := s.Get(one.ID)
	if got.Linked != "" || got.LinkedFields != nil || got.DetachedFrom() != "atlas-prod" {
		t.Fatalf("detached item = linked %q %v, detached from %q", got.Linked, got.LinkedFields, got.DetachedFrom())
	}
	if got.State != "open" || got.Title != one.Title || got.Properties["due"] != day(2026, 11, 2) ||
		!slices.Equal(got.Labels, []string{"atlas", "q4"}) || got.Origin == nil || got.Origin.ExternalKey != "1" {
		t.Errorf("detaching changed the item: %+v", got)
	}
	if again, err := s.DetachTask("engine:atlas-prod", one.ID); err != nil || again {
		t.Errorf("detaching a detached item = %v %v, want nothing to do", again, err)
	}
	if plain := mustItem(t)(s.Create("anna", NewItem{Space: "PLAT", Type: "engine-task", Title: "Own"})); plain.DetachedFrom() != "" {
		t.Error("an item that never was a linked task counts as detached")
	}

	// Nothing the engine decided binds it any longer.
	noErr(t, s.GrantRole("admin", "OPS", "anna", Editor))
	anna := s.As("anna")
	noErr(t, anna.SetTitle(one.ID, "Approve holidays, by hand"))
	noErr(t, anna.SetProperty(one.ID, "due", day(2027, 1, 1)))
	noErr(t, anna.Transition(one.ID, "finish"))
	noErr(t, anna.Move(one.ID, "OPS", ""))
	noErr(t, anna.Trash(one.ID))

	// An engine's tasks are detached in one batch; ended ones too, so they
	// may leave their space; others' tasks are left.
	other := mustItem(t)(func() (Item, error) {
		t := approval("9")
		t.Engine = "atlas-test"
		it, _, err := s.LinkTask("engine:atlas-test", t)
		return it, err
	}())
	n, err := s.DetachTasks("engine:atlas-prod", "atlas-prod")
	if err != nil || n != 2 {
		t.Fatalf("DetachTasks = %d %v, want the open and the ended one", n, err)
	}
	log := s.Log()
	if last := log[len(log)-1]; last.Batch == 0 || log[len(log)-2].Batch != last.Batch {
		t.Errorf("an engine's tasks were not detached in one batch: %+v", log[len(log)-2:])
	}
	if got, _ := s.Get(other.ID); got.Linked != "atlas-test" {
		t.Errorf("another engine's task was detached: %+v", got)
	}
	if n, err := s.DetachTasks("engine:atlas-prod", "atlas-prod"); err != nil || n != 0 {
		t.Errorf("detaching again = %d %v", n, err)
	}

	// A task that comes back is the same item, linked again.
	back, changed, err := s.LinkTask("engine:atlas-prod", approval("2"))
	if err != nil || !changed || back.ID != two.ID || back.Linked != "atlas-prod" || back.DetachedFrom() != "" {
		t.Errorf("a returning task = %+v %v %v, want item %s linked again", back, changed, err, two.ID)
	}
}

// TestTheEngineEndsATaskByItsTransition: EndTask makes the one transition
// that leads to the state (D3); it leaves an item already there and one no
// longer live, and refuses an item that is no linked task or a state no
// single transition reaches.
func TestTheEngineEndsATaskByItsTransition(t *testing.T) {
	s := linkSetup(t)
	it, _, err := s.LinkTask("engine:atlas-prod", approval("2"))
	noErr(t, err)
	noErr(t, s.EndTask("engine:atlas-prod", it.ID, "cancelled"))
	got, _ := s.Get(it.ID)
	if got.State != "cancelled" {
		t.Fatalf("state = %s, want cancelled", got.State)
	}
	last := s.Log()[len(s.Log())-1]
	if last.Kind != Transitioned || last.Transition != "cancel" || last.Actor != "engine:atlas-prod" {
		t.Errorf("the end was %+v, want the transition cancel by the synchronisation", last)
	}
	before := len(s.Log())
	noErr(t, s.EndTask("engine:atlas-prod", it.ID, "cancelled"))
	noErr(t, s.EndTask("engine:atlas-prod", "nope", "done"))
	if len(s.Log()) != before {
		t.Error("ending an ended task wrote an event")
	}
	if err := s.EndTask("engine:atlas-prod", it.ID, "done"); err == nil {
		t.Error("cancelled → done was made, though no transition leads there")
	}
	plain := mustItem(t)(s.Create("anna", NewItem{Space: "PLAT", Type: "engine-task", Title: "Own"}))
	if err := s.EndTask("engine:atlas-prod", plain.ID, "done"); err == nil {
		t.Error("EndTask ended an item that is no linked task")
	}
}

// TestTaskReachChecksATypeCanHoldLinkedTasks: the rule's type must let the
// synchronisation make and end every task.
func TestTaskReachChecksATypeCanHoldLinkedTasks(t *testing.T) {
	s := linkSetup(t)
	noErr(t, s.TaskReach("PLAT", "engine-task", "done", "cancelled", []string{"due"}))
	noErr(t, s.TaskReach("PLAT", "engine-task", "done", "done", nil))
	for name, err := range map[string]error{
		"no space":             s.TaskReach("NOPE", "engine-task", "done", "cancelled", nil),
		"no type":              s.TaskReach("PLAT", "nope", "done", "cancelled", nil),
		"no workflow":          s.TaskReach("PLAT", "page", "done", "cancelled", nil),
		"no state":             s.TaskReach("PLAT", "engine-task", "done", "gone", nil),
		"open is not finished": s.TaskReach("PLAT", "engine-task", "open", "cancelled", nil),
		"required not owned":   s.TaskReach("PLAT", "task", "done", "done", nil),
		"owned not a field":    s.TaskReach("PLAT", "engine-task", "done", "cancelled", []string{"colour"}),
		"a transition requires": func() error {
			td := engineTaskType()
			td.Workflow.Transitions[1].Requires = []string{"note"}
			noErr(t, s.DefineType("admin", td))
			return s.TaskReach("PLAT", "engine-task", "done", "cancelled", nil)
		}(),
	} {
		if err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	td := engineTaskType()
	td.Workflow.Transitions = td.Workflow.Transitions[:2]
	noErr(t, s.DefineType("admin", td))
	if err := s.TaskReach("PLAT", "engine-task", "done", "cancelled", nil); err == nil {
		t.Error("a state no transition reaches was accepted")
	}
}
