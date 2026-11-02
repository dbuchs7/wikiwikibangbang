package item

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

// accessFixture: an installation with the ready-made page type only. anna
// edits PLAT and protects a sentence of a page that olga owns; ben only
// views PLAT; vic edits it; pat administers it; hanna views it and has
// nothing to do with the request.
func accessFixture(t *testing.T, owner string) (*Store, Item, Secret) {
	t.Helper()
	s := newTestStore(t)
	noErr(t, s.DefineType("admin", TypeTemplates()[0]))
	noErr(t, s.CreateSpace("admin", "PLAT", "Platform"))
	noErr(t, s.CreateSpace("admin", "OPS", "Operations"))
	for who, role := range map[string]Role{"anna": Editor, "olga": Editor, "vic": Editor, "ben": Viewer, "hanna": Viewer, "pat": SpaceAdmin} {
		noErr(t, s.GrantRole("root", "PLAT", who, role))
	}
	var props Props
	if owner != "" {
		props = Props{"owner": owner}
	}
	page := mustItem(t)(s.As("anna").Create(NewItem{Space: "PLAT", Type: "page", Title: "Salaries", Properties: props, Body: &Doc{Blocks: []Block{
		{Kind: Paragraph, Text: "Paul Muster earns 9000."},
		{Kind: Paragraph, Text: "Open to all."},
	}}}))
	sec, err := s.As("anna").ProtectBlocks(page.ID, 0, 0, nil)
	noErr(t, err)
	return s, page, sec
}

// taskOf returns the task the waiting request of who was made for, as
// pat, who administers the room, reads it.
func taskOf(t *testing.T, s *Store, page Item, sec Secret, who string) Item {
	t.Helper()
	views, err := s.As("pat").Secrets(page.ID)
	noErr(t, err)
	for _, v := range views {
		for _, a := range v.Asks {
			if v.ID == sec.ID && a.Principal == who && a.Task != "" {
				return mustItem(t)(s.As("pat").Get(a.Task))
			}
		}
	}
	t.Fatalf("no task for the request of %s: %+v", who, views)
	return Item{}
}

func TestARequestBecomesATaskForTheOwner(t *testing.T) {
	s, page, sec := accessFixture(t, "olga")
	// Ben only views the room, and may not create anything in it.
	if _, err := s.As("ben").Create(NewItem{Space: "PLAT", Type: "page", Title: "x"}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("ben creates: %v", err)
	}
	noErr(t, s.As("ben").RequestSecret(page.ID, sec.ID))
	task := taskOf(t, s, page, sec, "ben")
	// The ready-made task type was defined for it, by the mechanism.
	td, ok := s.latest(task.Type)
	if !ok || task.Type != "task" || !holdsRequests(td) {
		t.Fatalf("task of type %q: %+v", task.Type, td)
	}
	if task.Space != "PLAT" || task.Parent != page.ID || task.State != "open" || task.Properties["assignee"] != "olga" ||
		!slices.Equal(task.Labels, []string{AccessLabel}) {
		t.Fatalf("task %+v", task)
	}
	if !slices.Equal(task.Restriction.Read, []string{"olga", "ben", "anna"}) || !slices.Equal(task.Restriction.Edit, task.Restriction.Read) {
		t.Fatalf("restriction %+v", task.Restriction)
	}
	var actors []string
	for _, e := range s.Log() {
		if e.Item == task.ID || e.Kind == TypeDefined && e.TypeDef.Name == "task" {
			actors = append(actors, e.Actor)
		}
	}
	for _, a := range actors {
		if a != AccessActor("ben") {
			t.Fatalf("the task is made by %q, not the mechanism: %v", a, actors)
		}
	}
	// Title and text say what is asked for, never the protected content.
	text := task.Title
	for _, b := range task.Body.Blocks {
		text += "\n" + b.Text
	}
	if !strings.Contains(task.Title, "ben") || !strings.Contains(task.Title, "Salaries") || strings.Contains(text, "Paul") || strings.Contains(text, "9000") {
		t.Fatalf("task text %q", text)
	}
	if !slices.ContainsFunc(task.Body.Blocks, func(b Block) bool {
		return slices.ContainsFunc(b.Marks, func(m Mark) bool { return m.Type == MarkMention && m.Ref == page.ID })
	}) {
		t.Fatalf("the text does not mention the page: %+v", task.Body)
	}
	// The owner answers it: the card and the task tell olga, who also
	// heard of the request.
	views, _ := s.As("olga").Secrets(page.ID)
	if !views[0].Answers || views[0].Decides || len(views[0].Asks) != 1 || views[0].Asks[0].TaskKey != task.Key || views[0].Asks[0].Assignee != "olga" {
		t.Fatalf("olga sees %+v", views[0])
	}
	if n, _ := s.As("olga").Notifications(0, 10); len(n) == 0 || n[0].Kind != SecretRequested {
		t.Fatalf("olga is not told: %+v", n)
	}
	v, ok := s.As("olga").AccessRequestOf(task.ID)
	if !ok || !v.Waiting || !v.Answers || v.Item != page.ID || v.Principal != "ben" || v.Number != 1 || !v.Blocks {
		t.Fatalf("olga reads the request as %+v", v)
	}
	// Ben reads his task, and whom it is assigned to.
	mine, _ := s.As("ben").Secrets(page.ID)
	if mine[0].Own == nil || mine[0].Own.TaskKey != task.Key || mine[0].Own.Assignee != "olga" || mine[0].Answers {
		t.Fatalf("ben sees %+v", mine[0])
	}
	if v, ok := s.As("ben").AccessRequestOf(task.ID); !ok || v.Answers || !v.Waiting {
		t.Fatalf("ben reads the request as %+v", v)
	}
	// Asked again while it waits, nothing new is made.
	noErr(t, s.As("ben").RequestSecret(page.ID, sec.ID))
	if n := len(s.Find(Query{Label: AccessLabel})); n != 1 {
		t.Fatalf("%d tasks", n)
	}
	// The owner grants it: the task records it and is done.
	noErr(t, s.As("olga").DecideSecret(page.ID, sec.ID, "ben", true))
	got := mustItem(t)(s.As("pat").Get(task.ID))
	if got.State != "done" || len(got.Comments) != 1 || got.Comments[0].Author != "olga" || !strings.Contains(got.Comments[0].Text, "Granted by olga") {
		t.Fatalf("granted task %+v", got)
	}
	if b := mustItem(t)(s.As("ben").Get(page.ID)); !strings.Contains(b.Body.Blocks[0].Text, "Paul") {
		t.Fatalf("granted, ben reads %q", b.Body.Blocks[0].Text)
	}
	if v, _ := s.As("olga").AccessRequestOf(task.ID); v.Waiting || v.Answers {
		t.Fatalf("an answered request still waits: %+v", v)
	}
}

func TestTheTaskOfARequestIsSeenOnlyByThoseItConcerns(t *testing.T) {
	s, page, sec := accessFixture(t, "olga")
	noErr(t, s.As("ben").RequestSecret(page.ID, sec.ID))
	task := taskOf(t, s, page, sec, "ben")
	for _, who := range []string{"olga", "ben", "anna", "pat"} {
		if _, err := s.As(who).Get(task.ID); err != nil {
			t.Errorf("%s does not read the task: %v", who, err)
		}
	}
	// Hanna and vic read the room and the page, not the task (D10).
	for _, who := range []string{"hanna", "vic"} {
		x := s.As(who)
		if _, err := x.Get(task.ID); !errors.Is(err, ErrNotFound) {
			t.Errorf("%s reads the task: %v", who, err)
		}
		if list, _, _, _ := x.FindPage(Query{Space: "PLAT"}, "", 50); slices.ContainsFunc(list, func(it Item) bool { return it.ID == task.ID }) {
			t.Errorf("%s lists the task", who)
		}
		if hits, _ := x.Search("Access", Query{}); len(hits) != 0 {
			t.Errorf("%s finds the task: %+v", who, hits)
		}
		if _, ok := x.AccessRequestOf(task.ID); ok {
			t.Errorf("%s reads the request of the task", who)
		}
		if n, _ := x.Notifications(0, 10); len(n) != 0 {
			t.Errorf("%s is told: %+v", who, n)
		}
	}
	// Vic, who edits the room, may not decide the request either.
	if err := s.As("vic").DecideSecret(page.ID, sec.ID, "ben", true); !errors.Is(err, ErrForbidden) {
		t.Fatalf("vic decides: %v", err)
	}
}

func TestTheTaskGoesToTheOneWhoProtectedWithoutAnOwnerWhoReads(t *testing.T) {
	cases := map[string]func(*Store, Item){
		"no owner":      nil,
		"owner outside": func(s *Store, _ Item) { noErr(t, s.RevokeRole("root", "PLAT", "olga")) },
		"owner shut out": func(s *Store, page Item) {
			noErr(t, s.Restrict("root", page.ID, Restriction{Read: []string{"anna", "ben"}}))
		},
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			owner := "olga"
			if change == nil {
				owner = ""
			}
			s, page, sec := accessFixture(t, owner)
			if change != nil {
				change(s, page)
			}
			noErr(t, s.As("ben").RequestSecret(page.ID, sec.ID))
			task := taskOf(t, s, page, sec, "ben")
			if task.Properties["assignee"] != "anna" || slices.Contains(task.Restriction.Read, "olga") {
				t.Fatalf("task %+v", task)
			}
			// One who owns the item but cannot read it answers nothing.
			if err := s.As("olga").DecideSecret(page.ID, sec.ID, "ben", true); err == nil {
				t.Fatal("an owner who cannot read the page decides")
			}
		})
	}
	// The owner who asks is not assigned their own request.
	s, page, sec := accessFixture(t, "olga")
	noErr(t, s.As("olga").RequestSecret(page.ID, sec.ID))
	task := taskOf(t, s, page, sec, "olga")
	if task.Properties["assignee"] != "anna" {
		t.Fatalf("the owner's own request goes to %v", task.Properties["assignee"])
	}
	if err := s.As("olga").DecideSecret(page.ID, sec.ID, "olga", true); !errors.Is(err, ErrForbidden) {
		t.Fatalf("the owner grants themself: %v", err)
	}
}

func TestTheTaskTypeIsChosenByItsDefinition(t *testing.T) {
	// A type of another name that can hold the task is used as it is.
	s, page, sec := accessFixture(t, "")
	noErr(t, s.DefineType("admin", TypeDef{Name: "ticket", Body: true, Fields: []FieldDef{{Name: "assignee", Kind: KindUser}, {Name: "due", Kind: KindDate}},
		Workflow: &Workflow{Initial: "new", States: []string{"new", "closed"}, Transitions: []Transition{{Name: "close", From: []string{"new"}, To: "closed"}}}}))
	noErr(t, s.DefineType("admin", TypeDef{Name: "decision", Body: true, Fields: []FieldDef{{Name: "assignee", Kind: KindUser}},
		Workflow: &Workflow{Initial: "proposed", States: []string{"proposed", "accepted"}, Transitions: []Transition{{Name: "accept", From: []string{"proposed"}, To: "accepted"}}}}))
	noErr(t, s.As("ben").RequestSecret(page.ID, sec.ID))
	if task := taskOf(t, s, page, sec, "ben"); task.Type != "ticket" {
		t.Fatalf("the task is a %s", task.Type)
	}
	if _, ok := s.latest("task"); ok {
		t.Fatal("the ready-made type is defined although a type can hold the task")
	}
	// A type of the ready-made name that cannot hold a task, and no other:
	// the request is made without a task, answered from the page.
	s, page, sec = accessFixture(t, "")
	noErr(t, s.DefineType("admin", TypeDef{Name: "task", Fields: []FieldDef{{Name: "priority", Kind: KindEnum, Values: []string{"low"}, Required: true}}}))
	noErr(t, s.As("ben").RequestSecret(page.ID, sec.ID))
	if n := len(s.Find(Query{Label: AccessLabel})); n != 0 {
		t.Fatalf("%d tasks", n)
	}
	views, _ := s.As("anna").Secrets(page.ID)
	if len(views[0].Asks) != 1 || views[0].Asks[0].Task != "" || !slices.Equal(views[0].Requests, []string{"ben"}) {
		t.Fatalf("anna sees %+v", views[0])
	}
	noErr(t, s.As("anna").DecideSecret(page.ID, sec.ID, "ben", false))
	// Removed, the ready-made type is defined again, as its next version.
	s, page, sec = accessFixture(t, "")
	noErr(t, s.DefineType("admin", TypeTemplates()[1]))
	noErr(t, s.RemoveType("admin", "task"))
	noErr(t, s.As("ben").RequestSecret(page.ID, sec.ID))
	if task := taskOf(t, s, page, sec, "ben"); task.Type != "task" || task.TypeVersion != 2 {
		t.Fatalf("task %s v%d", task.Type, task.TypeVersion)
	}
	if holdsRequests(TypeDef{Name: "x", Body: true, Fields: []FieldDef{{Name: "assignee", Kind: KindText}}, Workflow: TypeTemplates()[1].Workflow}) {
		t.Fatal("an assignee that is no user field holds requests")
	}
}

func TestClosingTheTaskWithoutDecidingDeclines(t *testing.T) {
	s, page, sec := accessFixture(t, "olga")
	// Vic edits the room and asks; he reads his task and may edit it, but
	// may not finish it while it waits.
	noErr(t, s.As("vic").RequestSecret(page.ID, sec.ID))
	task := taskOf(t, s, page, sec, "vic")
	if err := s.As("vic").Transition(task.ID, "finish"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("vic finishes his request's task: %v", err)
	}
	// Other transitions are free.
	noErr(t, s.As("olga").Transition(task.ID, "start"))
	noErr(t, s.As("olga").Transition(task.ID, "finish"))
	got := mustItem(t)(s.As("pat").Get(task.ID))
	if got.State != "done" || len(got.Comments) != 1 || !strings.Contains(got.Comments[0].Text, "declined by olga") {
		t.Fatalf("closed task %+v", got)
	}
	if v, _ := s.As("vic").Secrets(page.ID); v[0].Requested || v[0].Readable {
		t.Fatalf("closed, vic sees %+v", v[0])
	}
	if n, _ := s.As("vic").Notifications(0, 10); !slices.ContainsFunc(n, func(n Notification) bool { return n.Kind == SecretDeclined }) {
		t.Fatalf("vic is not told: %+v", n)
	}
	// Reopened, the task is a task like any other.
	noErr(t, s.As("olga").Transition(task.ID, "reopen"))
	noErr(t, s.As("vic").Transition(task.ID, "finish"))
	// Asked again, a new task is made.
	noErr(t, s.As("vic").RequestSecret(page.ID, sec.ID))
	again := taskOf(t, s, page, sec, "vic")
	if again.ID == task.ID || len(s.Find(Query{Label: AccessLabel})) != 2 {
		t.Fatalf("asked again: %s", again.ID)
	}
	noErr(t, s.As("anna").DecideSecret(page.ID, sec.ID, "vic", false))
	if got := mustItem(t)(s.As("pat").Get(again.ID)); got.State != "done" || !strings.Contains(got.Comments[0].Text, "Declined by anna") {
		t.Fatalf("declined task %+v", got)
	}
}

func TestOtherAnswersEndTheTaskToo(t *testing.T) {
	s, page, sec := accessFixture(t, "olga")
	noErr(t, s.As("ben").RequestSecret(page.ID, sec.ID))
	noErr(t, s.As("vic").RequestSecret(page.ID, sec.ID))
	ben, vic := taskOf(t, s, page, sec, "ben"), taskOf(t, s, page, sec, "vic")
	// Made a reader by hand, ben is granted; the owner may not do that.
	if err := s.As("olga").SetSecretReaders(page.ID, sec.ID, []string{"ben"}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("the owner changes the readers: %v", err)
	}
	noErr(t, s.As("anna").SetSecretReaders(page.ID, sec.ID, []string{"ben"}))
	if got := mustItem(t)(s.As("pat").Get(ben.ID)); got.State != "done" || !strings.Contains(got.Comments[0].Text, "Granted by anna") {
		t.Fatalf("ben's task %+v", got)
	}
	// Taken off, there is nothing left to decide.
	if err := s.As("olga").Unprotect(page.ID, sec.ID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("the owner takes the protection off: %v", err)
	}
	noErr(t, s.As("anna").Unprotect(page.ID, sec.ID))
	if got := mustItem(t)(s.As("pat").Get(vic.ID)); got.State != "done" || !strings.Contains(got.Comments[0].Text, "taken off by anna") {
		t.Fatalf("vic's task %+v", got)
	}
	// Erasing ends them as well; a task in the trash is left alone, and
	// the request is answered from the page.
	sec2, err := s.As("anna").ProtectBlocks(page.ID, 1, 1, nil)
	noErr(t, err)
	noErr(t, s.As("ben").RequestSecret(page.ID, sec2.ID))
	noErr(t, s.As("vic").RequestSecret(page.ID, sec2.ID))
	trashed := taskOf(t, s, page, sec2, "vic")
	noErr(t, s.As("pat").Trash(trashed.ID))
	views, _ := s.As("anna").Secrets(page.ID)
	for _, a := range views[0].Asks {
		if (a.Principal == "vic") != (a.Task == "") {
			t.Fatalf("anna sees %+v", views[0].Asks)
		}
	}
	open := taskOf(t, s, page, sec2, "ben")
	noErr(t, s.As("anna").EraseSecret(page.ID, sec2.ID))
	if got := mustItem(t)(s.As("pat").Get(open.ID)); got.State != "done" {
		t.Fatalf("erased, the task is %s", got.State)
	}
	if got, _ := s.item(trashed.ID); got.State != "open" || len(got.Comments) != 0 {
		t.Fatalf("the trashed task changed: %+v", got)
	}
}

func TestTheOwnerOfProtectedContentIsSetByThoseWhoReadIt(t *testing.T) {
	s, page, _ := accessFixture(t, "olga")
	// Vic edits the page but does not read its protection: making himself
	// its owner would let him answer requests to read it.
	if err := s.As("vic").SetProperty(page.ID, "owner", "vic"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("vic sets the owner: %v", err)
	}
	noErr(t, s.As("vic").SetTitle(page.ID, "Salaries 2026"))
	noErr(t, s.As("anna").SetProperty(page.ID, "owner", "vic"))
	noErr(t, s.As("pat").SetProperty(page.ID, "owner", nil))
	// Without protections, anyone who edits sets it.
	other := mustItem(t)(s.As("vic").Create(NewItem{Space: "PLAT", Type: "page", Title: "Open"}))
	noErr(t, s.As("vic").SetProperty(other.ID, "owner", "olga"))
}

func TestARequestsTaskSurvivesReplay(t *testing.T) {
	s, page, sec := accessFixture(t, "olga")
	noErr(t, s.As("ben").RequestSecret(page.ID, sec.ID))
	task := taskOf(t, s, page, sec, "ben")
	r, err := Replay(s.Log())
	noErr(t, err)
	live, _ := s.As("olga").AccessRequestOf(task.ID)
	again, ok := r.As("olga").AccessRequestOf(task.ID)
	if !ok || live != again {
		t.Fatalf("replayed %+v, live %+v", again, live)
	}
	noErr(t, r.As("olga").DecideSecret(page.ID, sec.ID, "ben", false))
	if got := mustItem(t)(r.As("pat").Get(task.ID)); got.State != "done" {
		t.Fatalf("replayed, the task is %s", got.State)
	}
}

func TestPlainAccessTexts(t *testing.T) {
	for _, o := range []AccessOutcome{AccessGranted, AccessDeclined, AccessClosed, AccessRemoved} {
		if (plainAccessTexts{}).Outcome(o, "anna", AccessTask{}) == "" {
			t.Errorf("no text for %d", o)
		}
	}
	title, body := (plainAccessTexts{}).Task(AccessTask{Requester: "ben", Title: "Salaries", Number: 2})
	if title != "Access request: ben for «Salaries»" || !strings.Contains(body.Blocks[0].Text, "protected passage 2 of Salaries") {
		t.Fatalf("%q %+v", title, body)
	}
}
