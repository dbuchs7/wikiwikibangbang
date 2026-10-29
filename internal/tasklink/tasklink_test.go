package tasklink

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/dbuchs7/wikiwikibangbang/internal/auth"
	"github.com/dbuchs7/wikiwikibangbang/internal/engine"
	"github.com/dbuchs7/wikiwikibangbang/internal/engineconn"
	"github.com/dbuchs7/wikiwikibangbang/internal/item"
	"github.com/dbuchs7/wikiwikibangbang/internal/site"
	"github.com/dbuchs7/wikiwikibangbang/internal/storage"
)

// fakeLister answers the open tasks it holds; err, when set, instead.
type fakeLister struct {
	tasks    []engine.Task
	complete bool
	err      error
	block    chan struct{}
	// sees are the task keys each person's own list holds; asErr fails
	// a person's list.
	sees  map[string][]uint64
	asErr map[string]error
}

func (f *fakeLister) OpenTasks(ctx context.Context, _ site.Engine) ([]engine.Task, bool, error) {
	if f.block != nil {
		<-f.block
	}
	return slices.Clone(f.tasks), f.complete, f.err
}

// OpenTasksAs answers the tasks of the person whose token it is: the
// token is "token-" and the person's ID.
func (f *fakeLister) OpenTasksAs(_ context.Context, _ site.Engine, bearer string) ([]engine.Task, bool, error) {
	user := strings.TrimPrefix(bearer, "token-")
	if err := f.asErr[user]; err != nil {
		return nil, false, err
	}
	var out []engine.Task
	for _, k := range f.sees[user] {
		out = append(out, holidayTask(k))
	}
	return out, true, nil
}

// fakeGrants gives each connected person the token "token-" and their ID,
// or the error set for them; a person whose grant the engine refused is
// disconnected, as the broker does.
type fakeGrants struct {
	db      *storage.DB
	err     map[string]error
	ended   map[string]bool
	refused []string
}

func (g *fakeGrants) Token(_ context.Context, e site.Engine, user string) (string, error) {
	if g.ended[user] {
		_ = g.db.Connections.End(user, e.ID, "refused by the engine")
		return "", errors.New("the engine refused the grant")
	}
	if err := g.err[user]; err != nil {
		return "", err
	}
	return "token-" + user, nil
}

func (g *fakeGrants) Refused(user, _ string) { g.refused = append(g.refused, user) }

type fixture struct {
	db *storage.DB
	l  *fakeLister
	r  *Runner
}

// holidayTask is an open task of the process urlaub offered to hr.
func holidayTask(key uint64) engine.Task {
	return engine.Task{Key: key, Process: "urlaub", Element: "genehmigen", Name: "Urlaub genehmigen",
		Groups: []string{"hr", "Team Lead"}, Due: time.Date(2026, 11, 2, 15, 30, 0, 0, time.UTC)}
}

func setup(t *testing.T) *fixture {
	t.Helper()
	db, _, err := storage.OpenWith(t.TempDir(), storage.Options{Auth: []auth.Option{auth.WithIterations(1000)}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.Write(func(s *item.Store) error {
		if err := s.CreateSpace("admin", "HR", "People"); err != nil {
			return err
		}
		return s.DefineType("admin", item.TypeDef{
			Name: "engine-task", Body: true,
			Fields: []item.FieldDef{{Name: "due", Kind: item.KindDate}},
			Workflow: &item.Workflow{
				Initial: "open",
				States:  []string{"open", "done", "cancelled"},
				Transitions: []item.Transition{
					{Name: "finish", From: []string{"open"}, To: "done"},
					{Name: "cancel", From: []string{"open"}, To: "cancelled"},
				},
			},
		})
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Site.SetEngine("admin", site.Engine{ID: "atlas-prod", Name: "Atlas", Kind: site.EngineAtlas, BaseURL: "https://atlas.example"}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Site.SetTaskLinks("admin", site.TaskLinks{Engine: "atlas-prod", Enabled: true, Rules: []site.TaskRule{
		{ID: "holidays", Processes: []string{"urlaub"}, Space: "HR", Type: "engine-task", DueField: "due",
			Labels: []string{"atlas", "{process}", "team-{group}", "bad label!"}, Completed: "done", Cancelled: "cancelled"},
	}}); err != nil {
		t.Fatal(err)
	}
	f := &fixture{db: db, l: &fakeLister{complete: true}}
	f.r = New(db, f.l, nil)
	f.r.now = func() time.Time { return time.Date(2026, 10, 29, 8, 0, 0, 0, time.UTC) }
	return f
}

func (f *fixture) linked(t *testing.T) []item.Item {
	t.Helper()
	var out []item.Item
	f.db.Read(func(s *item.Store) { out = s.LinkedTasks("atlas-prod") })
	return out
}

// TestARoundLinksTheTasksARuleMatchesAndEndsTheGoneOnes (ADR-0151): a round
// links each open task the first matching rule takes — hidden, with its
// labels and its due date — leaves the others and the steps of public
// wizards alone, and ends, as completed, the items whose task is gone.
func TestARoundLinksTheTasksARuleMatchesAndEndsTheGoneOnes(t *testing.T) {
	f := setup(t)
	other := holidayTask(3)
	other.Process = "onboarding"
	wizard := holidayTask(4)
	wizard.Wizard = "public"
	f.l.tasks = []engine.Task{holidayTask(1), holidayTask(2), other, wizard}

	rep, err := f.r.Run(context.Background(), "atlas-prod", false)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Open != 4 || rep.Linked != 2 || rep.Unmatched != 1 || rep.Ended != 0 || !rep.Complete {
		t.Fatalf("first round = %+v", rep)
	}
	if len(rep.Problems) != 2 || !strings.Contains(rep.Problems[0], `"bad label!" is no label`) {
		t.Errorf("problems = %v", rep.Problems)
	}
	items := f.linked(t)
	if len(items) != 2 {
		t.Fatalf("linked = %+v", items)
	}
	it := items[0]
	if it.Title != "Urlaub genehmigen" || it.Space != "HR" || it.Origin.ExternalKey != "1" ||
		it.Origin.URL != "https://atlas.example/#/tasks/t/1" ||
		!slices.Equal(it.Labels, []string{"atlas", "team-hr", "team-team-lead", "urlaub"}) ||
		!it.Properties["due"].(time.Time).Equal(time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC)) ||
		!slices.Equal(it.Restriction.Read, []string{"engine:atlas-prod"}) {
		t.Fatalf("linked item = %+v", it)
	}
	if last, ok := f.r.Last("atlas-prod"); !ok || last.Linked != 2 {
		t.Errorf("Last = %+v %v", last, ok)
	}

	// The second round: task 1 renamed, task 2 gone.
	renamed := holidayTask(1)
	renamed.Name = "Urlaub genehmigen (Ben)"
	f.l.tasks = []engine.Task{renamed}
	rep, err = f.r.Run(context.Background(), "atlas-prod", false)
	if err != nil || rep.Updated != 1 || rep.Ended != 1 || rep.Linked != 0 {
		t.Fatalf("second round = %+v %v", rep, err)
	}
	items = f.linked(t)
	if items[0].Title != "Urlaub genehmigen (Ben)" || items[1].State != "done" {
		t.Fatalf("after the second round = %q %q", items[0].Title, items[1].State)
	}
	// Nothing changed: nothing written.
	var seq int
	f.db.Read(func(s *item.Store) { seq = s.Seq() })
	if rep, _ := f.r.Run(context.Background(), "atlas-prod", false); rep.Updated != 0 || rep.Ended != 0 {
		t.Errorf("a round over unchanged tasks = %+v", rep)
	}
	f.db.Read(func(s *item.Store) {
		if s.Seq() != seq {
			t.Errorf("a round over unchanged tasks wrote %d events", s.Seq()-seq)
		}
	})
}

// TestADryRoundChangesNothing: a dry round reports what a round would do.
func TestADryRoundChangesNothing(t *testing.T) {
	f := setup(t)
	f.l.tasks = []engine.Task{holidayTask(1)}
	if _, err := f.r.Run(context.Background(), "atlas-prod", false); err != nil {
		t.Fatal(err)
	}
	f.l.tasks = []engine.Task{holidayTask(2)}
	var seq int
	f.db.Read(func(s *item.Store) { seq = s.Seq() })
	rep, err := f.r.Run(context.Background(), "atlas-prod", true)
	if err != nil || !rep.Dry || rep.Linked != 1 || rep.Ended != 1 {
		t.Fatalf("dry round = %+v %v", rep, err)
	}
	f.db.Read(func(s *item.Store) {
		if s.Seq() != seq {
			t.Error("a dry round wrote")
		}
	})
	if last, _ := f.r.Last("atlas-prod"); last.Dry {
		t.Error("a dry round became the last report")
	}
	f.l.tasks = []engine.Task{holidayTask(1)}
	if rep, _ := f.r.Run(context.Background(), "atlas-prod", true); rep.Updated != 1 || rep.Linked != 0 {
		t.Errorf("a dry round over a linked task = %+v", rep)
	}
}

// TestAnIncompleteListEndsNothing: when the engine's list was cut, a task
// missing from it may still be open, so nothing is ended.
func TestAnIncompleteListEndsNothing(t *testing.T) {
	f := setup(t)
	f.l.tasks = []engine.Task{holidayTask(1), holidayTask(2)}
	if _, err := f.r.Run(context.Background(), "atlas-prod", false); err != nil {
		t.Fatal(err)
	}
	f.l.tasks, f.l.complete = []engine.Task{holidayTask(1)}, false
	rep, err := f.r.Run(context.Background(), "atlas-prod", false)
	if err != nil || rep.Ended != 0 || rep.Complete || len(rep.Problems) == 0 {
		t.Fatalf("round over a cut list = %+v %v", rep, err)
	}
}

// TestAnEndedTaskWithoutItsRuleStaysOpenAndSaysSo: an item whose rule was
// removed is not ended by guess.
func TestAnEndedTaskWithoutItsRuleStaysOpenAndSaysSo(t *testing.T) {
	f := setup(t)
	f.l.tasks = []engine.Task{holidayTask(1)}
	if _, err := f.r.Run(context.Background(), "atlas-prod", false); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Site.SetTaskLinks("admin", site.TaskLinks{Engine: "atlas-prod", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	f.l.tasks = nil
	rep, err := f.r.Run(context.Background(), "atlas-prod", false)
	if err != nil || rep.Ended != 0 || len(rep.Problems) != 1 || !strings.Contains(rep.Problems[0], "no rule names") {
		t.Fatalf("round = %+v %v", rep, err)
	}
	if f.linked(t)[0].State != "open" {
		t.Error("the item was ended without a rule")
	}
}

// TestARoundThatCannotReadTheEngineSaysWhy, and keeps the report.
func TestARoundThatCannotReadTheEngineSaysWhy(t *testing.T) {
	f := setup(t)
	f.l.err = engine.ErrNoCredential
	rep, err := f.r.Run(context.Background(), "atlas-prod", false)
	if !errors.Is(err, engine.ErrNoCredential) || rep.Error == "" {
		t.Fatalf("round = %+v %v", rep, err)
	}
	if last, ok := f.r.Last("atlas-prod"); !ok || last.Error == "" {
		t.Errorf("Last = %+v", last)
	}
	if _, err := f.r.Run(context.Background(), "nope", false); err == nil {
		t.Error("a round of an engine that does not exist ran")
	}
}

// TestOneRoundPerEngineAtATime: a round asked for while one runs is
// refused; Tick runs only engines whose links are switched on.
func TestOneRoundPerEngineAtATime(t *testing.T) {
	f := setup(t)
	f.l.block = make(chan struct{})
	done := make(chan struct{})
	go func() { _, _ = f.r.Run(context.Background(), "atlas-prod", false); close(done) }()
	for {
		f.r.mu.Lock()
		running := f.r.run["atlas-prod"]
		f.r.mu.Unlock()
		if running {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if _, err := f.r.Run(context.Background(), "atlas-prod", false); !errors.Is(err, ErrBusy) {
		t.Errorf("a second round at once: %v", err)
	}
	close(f.l.block)
	<-done
	f.l.block = nil

	f.l.tasks = []engine.Task{holidayTask(1)}
	f.r.Tick(context.Background())
	if len(f.linked(t)) != 1 {
		t.Fatal("Tick did not run the switched-on links")
	}
	if _, err := f.db.Site.SetTaskLinks("admin", site.TaskLinks{Engine: "atlas-prod", Enabled: false, Rules: f.db.Site.TaskLinks("atlas-prod").Rules}); err != nil {
		t.Fatal(err)
	}
	f.l.tasks = []engine.Task{holidayTask(1), holidayTask(2)}
	f.r.Tick(context.Background())
	if len(f.linked(t)) != 1 {
		t.Fatal("Tick ran switched-off links")
	}
}

// TestLoopRunsUntilItsContextEnds.
func TestLoopRunsUntilItsContextEnds(t *testing.T) {
	f := setup(t)
	f.l.tasks = []engine.Task{holidayTask(1)}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { f.r.Loop(ctx); close(done) }()
	for len(f.linked(t)) == 0 {
		time.Sleep(time.Millisecond)
	}
	cancel()
	<-done
}

// TestEachLinkedTaskIsReadByThoseTheEngineShowsItTo covers ADR-0151's
// visibility: each connected person's own task list, read with their
// grant, decides who reads and edits each open linked item; a new task is
// shown in the round that links it; a person whose list cannot be read
// keeps what they saw, a person no longer connected loses it; an ended
// task's readers stay; the lists are read again every VisibilityEvery, or
// sooner when asked.
func TestEachLinkedTaskIsReadByThoseTheEngineShowsItTo(t *testing.T) {
	f := setup(t)
	now := time.Date(2026, 10, 29, 8, 0, 0, 0, time.UTC)
	f.r.now = func() time.Time { return now }
	g := &fakeGrants{db: f.db, err: map[string]error{}, ended: map[string]bool{}}
	f.r.WithGrants(g)
	for _, u := range []string{"bert", "vera", "carla", "dora"} {
		if _, err := f.db.Connections.Connect(u, "atlas-prod", engineconn.Profile{Subject: "x-" + u, Username: u}, "refresh-"+u); err != nil {
			t.Fatal(err)
		}
	}
	f.l.tasks = []engine.Task{holidayTask(1), holidayTask(2)}
	f.l.sees = map[string][]uint64{"bert": {1}, "vera": {1, 2}, "dora": {2}}
	f.l.asErr = map[string]error{}
	g.err["carla"] = errors.New("the engine is busy")
	g.ended["dora"] = true

	rep, err := f.r.Run(context.Background(), "atlas-prod", false)
	if err != nil {
		t.Fatal(err)
	}
	if !rep.Visible || rep.People != 2 || rep.Unread != 1 || rep.Readers != 2 || !strings.Contains(strings.Join(rep.Problems, "\n"), "not read (1), who keep what they saw: the engine is busy") {
		t.Fatalf("first round = %+v", rep)
	}
	readers := func() map[string]string {
		out := map[string]string{}
		for _, it := range f.linked(t) {
			out[it.Origin.ExternalKey] = strings.Join(it.Restriction.Read, ",")
			if !slices.Equal(it.Restriction.Read, it.Restriction.Edit) {
				t.Errorf("%s: read by %v, edited by %v", it.Key, it.Restriction.Read, it.Restriction.Edit)
			}
		}
		return out
	}
	if got := readers(); got["1"] != "bert,engine:atlas-prod,vera" || got["2"] != "engine:atlas-prod,vera" {
		t.Fatalf("readers after the first round = %v", got)
	}
	if _, still := f.db.Connections.Get("dora", "atlas-prod"); still {
		t.Error("dora's refused grant left her connected")
	}

	// Within VisibilityEvery and nothing new linked: the lists are not
	// read again.
	if rep, _ := f.r.Run(context.Background(), "atlas-prod", false); rep.Visible {
		t.Errorf("read again at once: %+v", rep)
	}

	// Bert no longer sees task 1; carla's list is read and holds task 2;
	// vera's token is refused, so she keeps what she saw.
	f.l.sees["bert"], f.l.sees["carla"] = nil, []uint64{2}
	delete(g.err, "carla")
	f.l.asErr["vera"] = engine.ErrRefused
	f.r.Nudge("atlas-prod")
	rep, err = f.r.Run(context.Background(), "atlas-prod", false)
	if err != nil || !rep.Visible || rep.People != 2 || rep.Unread != 1 || rep.Readers != 2 {
		t.Fatalf("second round = %+v %v", rep, err)
	}
	if got := readers(); got["1"] != "engine:atlas-prod,vera" || got["2"] != "carla,engine:atlas-prod,vera" {
		t.Fatalf("readers after the second round = %v", got)
	}
	if !slices.Equal(g.refused, []string{"vera"}) {
		t.Errorf("refused tokens given back: %v", g.refused)
	}

	// Task 1 ends: its item keeps its readers. Vera disconnects: after
	// VisibilityEvery she no longer reads task 2.
	f.l.tasks = []engine.Task{holidayTask(2)}
	delete(f.l.asErr, "vera")
	if err := f.db.Connections.End("vera", "atlas-prod", "disconnected"); err != nil {
		t.Fatal(err)
	}
	now = now.Add(VisibilityEvery)
	rep, err = f.r.Run(context.Background(), "atlas-prod", false)
	if err != nil || rep.Ended != 1 || !rep.Visible || rep.Readers != 1 {
		t.Fatalf("third round = %+v %v", rep, err)
	}
	if got := readers(); got["1"] != "engine:atlas-prod,vera" || got["2"] != "carla,engine:atlas-prod" {
		t.Fatalf("readers after the third round = %v", got)
	}

	// A dry round reads no person's list.
	if rep, _ := f.r.Run(context.Background(), "atlas-prod", true); rep.Visible {
		t.Error("a dry round read the people's lists")
	}
}
