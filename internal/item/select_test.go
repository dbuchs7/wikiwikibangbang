package item

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/dbuchs7/wikiwikibangbang/internal/query"
)

func keysOf(items []Item) []string {
	out := []string{}
	for _, it := range items {
		out = append(out, it.Key)
	}
	return out
}

// selectFixture: anna edits PLAT, ben views it, dora edits OPS, carl
// holds no role. A risk type shares the field priority with the task, as
// a number, and has a field named created.
//
//	PLAT-1 task "Fix login"     high,   anna, 3,   labels security web
//	PLAT-2 task "Write docs"    low,    ben,  1.5, done
//	PLAT-3 task "Plan release"  normal, -,    8,   label web
//	PLAT-4 page "Runbook"       body with a passage only anna reads, a comment
//	PLAT-5 page "Salaries"      readable by anna only
//	PLAT-6 risk "Outage"        below PLAT-4, due 2026-10-20, critical, priority 2
//	OPS-1  page "Ops handbook"  by dora
func selectFixture(t *testing.T) (*Store, map[string]Item) {
	t.Helper()
	s := permFixture(t)
	noErr(t, s.DefineType("admin", TypeDef{Name: "risk", Fields: []FieldDef{
		{Name: "due", Kind: KindDate}, {Name: "critical", Kind: KindBool}, {Name: "priority", Kind: KindNumber},
		{Name: "created", Kind: KindText}, {Name: "Severity", Kind: KindText},
	}}))
	anna := s.As("anna")
	items := map[string]Item{}
	add := func(name string, n NewItem) Item {
		it := mustItem(t)(anna.Create(n))
		items[name] = it
		return it
	}
	login := add("login", NewItem{Space: "PLAT", Type: "task", Title: "Fix login", Properties: Props{"priority": "high", "assignee": "anna", "estimate": 3}})
	noErr(t, anna.SetLabels(login.ID, []string{"security", "web"}))
	docs := add("docs", NewItem{Space: "PLAT", Type: "task", Title: "Write docs", Properties: Props{"priority": "low", "assignee": "ben", "estimate": 1.5}})
	noErr(t, anna.Transition(docs.ID, "finish"))
	release := add("release", NewItem{Space: "PLAT", Type: "task", Title: "Plan release", Properties: Props{"priority": "normal", "estimate": 8}})
	noErr(t, anna.SetLabels(release.ID, []string{"web"}))
	runbook := add("runbook", NewItem{Space: "PLAT", Type: "page", Title: "Runbook", Body: &Doc{Blocks: []Block{
		{Kind: Paragraph, Text: "Restart the primary first."},
		{Kind: Paragraph, Text: "Codeword Bluebird opens the vault."},
	}}})
	_, err := anna.Protect(runbook.ID, 1, 0, 17, nil)
	noErr(t, err)
	_, err = anna.AddComment(runbook.ID, "Check the replica lag")
	noErr(t, err)
	salaries := add("salaries", NewItem{Space: "PLAT", Type: "page", Title: "Salaries"})
	noErr(t, anna.Restrict(salaries.ID, Restriction{Read: []string{"anna"}}))
	add("outage", NewItem{Space: "PLAT", Type: "risk", Parent: runbook.ID, Title: "Outage", Properties: Props{
		"due": time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC), "critical": true, "priority": 2, "created": "yesterday", "Severity": "Low impact"}})
	items["ops"] = mustItem(t)(s.As("dora").Create(NewItem{Space: "OPS", Type: "page", Title: "Ops handbook"}))
	return s, items
}

// at is the clock of the queries: two weeks after the fixture's store
// starts counting.
var at = time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)

func TestSelectByFields(t *testing.T) {
	s, _ := selectFixture(t)
	users := func(name string) []string {
		if name == "Anna" {
			return []string{"anna"}
		}
		return nil
	}
	env := QueryEnv{Now: at, Users: users}
	cases := []struct {
		as, query string
		want      []string
	}{
		{"ben", "", []string{"PLAT-1", "PLAT-2", "PLAT-3", "PLAT-4", "PLAT-6"}},
		{"anna", "", []string{"PLAT-1", "PLAT-2", "PLAT-3", "PLAT-4", "PLAT-5", "PLAT-6"}},
		{"carl", "", []string{}},
		{"ben", "type = task", []string{"PLAT-1", "PLAT-2", "PLAT-3"}},
		{"ben", "TYPE = Task", []string{"PLAT-1", "PLAT-2", "PLAT-3"}},
		{"ben", "type in (page, risk)", []string{"PLAT-4", "PLAT-6"}},
		{"ben", "type != task", []string{"PLAT-4", "PLAT-6"}},
		{"ben", "state = done", []string{"PLAT-2"}},
		{"ben", "state != done", []string{"PLAT-1", "PLAT-3", "PLAT-4", "PLAT-6"}},
		{"ben", "state is empty", []string{"PLAT-4", "PLAT-6"}},
		{"ben", "state = 'In Progress'", []string{}},
		{"ben", "finished = false", []string{"PLAT-1", "PLAT-3"}},
		{"ben", "finished = true", []string{"PLAT-2"}},
		{"ben", "finished is empty", []string{"PLAT-4", "PLAT-6"}},
		{"ben", "space = plat and type = page", []string{"PLAT-4"}},
		{"ben", "key = PLAT-3", []string{"PLAT-3"}},
		{"ben", "key in (plat-1, PLAT-5, OPS-1)", []string{"PLAT-1"}},
		{"ben", "title = 'fix LOGIN'", []string{"PLAT-1"}},
		{"ben", "title ~ plan", []string{"PLAT-3"}},
		{"ben", "title !~ plan", []string{"PLAT-1", "PLAT-2", "PLAT-4", "PLAT-6"}},
		{"ben", "title < g", []string{"PLAT-1"}},
		{"ben", "label = web", []string{"PLAT-1", "PLAT-3"}},
		{"ben", "label != web", []string{"PLAT-2", "PLAT-4", "PLAT-6"}},
		{"ben", "label in (security)", []string{"PLAT-1"}},
		{"ben", "label is empty", []string{"PLAT-2", "PLAT-4", "PLAT-6"}},
		{"ben", "label is not empty and label not in (security)", []string{"PLAT-3"}},
		{"ben", "assignee = currentUser()", []string{"PLAT-2"}},
		{"anna", "assignee = currentUser()", []string{"PLAT-1"}},
		{"ben", "assignee = Anna", []string{"PLAT-1"}},
		{"ben", "assignee = anna", []string{"PLAT-1"}},
		{"ben", "assignee = nobody", []string{}},
		{"ben", "assignee is empty and type = task", []string{"PLAT-3"}},
		{"ben", "assignee in (currentUser(), Anna)", []string{"PLAT-1", "PLAT-2"}},
		{"ben", "estimate > 2", []string{"PLAT-1", "PLAT-3"}},
		{"ben", "estimate <= 3", []string{"PLAT-1", "PLAT-2"}},
		{"ben", "estimate = 1.5", []string{"PLAT-2"}},
		{"ben", "estimate in (8, 3)", []string{"PLAT-1", "PLAT-3"}},
		{"ben", "priority = HIGH", []string{"PLAT-1"}},
		{"ben", "priority >= normal", []string{"PLAT-1", "PLAT-3"}},
		{"ben", "priority < high", []string{"PLAT-2", "PLAT-3"}},
		{"ben", "priority = 2", []string{"PLAT-6"}},
		{"ben", "priority > 1", []string{"PLAT-6"}},
		{"ben", "priority is not empty", []string{"PLAT-1", "PLAT-2", "PLAT-3", "PLAT-6"}},
		{"ben", "critical = true", []string{"PLAT-6"}},
		{"ben", "critical != true", []string{"PLAT-1", "PLAT-2", "PLAT-3", "PLAT-4"}},
		{"ben", "due = 2026-10-20", []string{"PLAT-6"}},
		{"ben", "due < 2026-10-21 and due > '2026-10-19'", []string{"PLAT-6"}},
		{"ben", "due < now()", []string{"PLAT-6"}},
		{"ben", "due >= -7d", []string{}},
		{"ben", "due >= startOfMonth()", []string{"PLAT-6"}},
		{"ben", `"created" = yesterday`, []string{"PLAT-6"}},
		{"ben", "parent = PLAT-4", []string{"PLAT-6"}},
		{"ben", "parent is empty and type != task", []string{"PLAT-4"}},
		{"ben", "ancestor = PLAT-4", []string{"PLAT-6"}},
		{"ben", "text ~ replica", []string{"PLAT-4"}},
		{"ben", "text ~ 'restart PRIMARY'", []string{"PLAT-4"}},
		{"ben", "text ~ 'web security'", []string{"PLAT-1"}},
		{"ben", "text ~ plat-6", []string{"PLAT-6"}},
		{"ben", "body ~ restart", []string{"PLAT-4"}},
		{"ben", "body is empty", []string{"PLAT-1", "PLAT-2", "PLAT-3", "PLAT-6"}},
		{"ben", "comment ~ replica", []string{"PLAT-4"}},
		{"ben", "comment is not empty", []string{"PLAT-4"}},
		{"ben", "creator = anna", []string{"PLAT-1", "PLAT-2", "PLAT-3", "PLAT-4", "PLAT-6"}},
		{"ben", "creator = currentUser()", []string{}},
		{"ben", "created >= 2026-10-16 and created < startOfDay()", []string{"PLAT-1", "PLAT-2", "PLAT-3", "PLAT-4", "PLAT-6"}},
		{"ben", "created = 2026-10-16", []string{"PLAT-1", "PLAT-2", "PLAT-3", "PLAT-4", "PLAT-6"}},
		{"ben", "created > 2026-10-16", []string{}},
		{"ben", "updated < -1d", []string{"PLAT-1", "PLAT-2", "PLAT-3", "PLAT-4", "PLAT-6"}},
		{"ben", "not (type = task or type = page)", []string{"PLAT-6"}},
		{"ben", "type = task and (assignee = anna or label = web) and not state = done", []string{"PLAT-1", "PLAT-3"}},
		{"ben", "id = nothing", []string{}},
		{"dora", "", []string{"OPS-1"}},
	}
	for _, c := range cases {
		got, err := s.As(c.as).Select(Query{}, c.query, env)
		if err != nil {
			t.Errorf("%s: %q: %v", c.as, c.query, err)
			continue
		}
		if keys := keysOf(got); !slices.Equal(keys, c.want) {
			t.Errorf("%s: %q = %v, want %v", c.as, c.query, keys, c.want)
		}
	}
}

func TestSelectNeverRevealsWhatTheCallerMayNotRead(t *testing.T) {
	s, items := selectFixture(t)
	env := QueryEnv{Now: at}
	ben := s.As("ben")
	// A passage only anna reads is not searched for ben, and comes back
	// veiled (ADR-0088).
	for _, q := range []string{"text ~ bluebird", "body ~ bluebird", "body ~ codeword"} {
		if got, _ := ben.Select(Query{}, q, env); len(got) != 0 {
			t.Errorf("ben: %q finds %v", q, keysOf(got))
		}
		if got, _ := s.As("anna").Select(Query{}, q, env); !slices.Equal(keysOf(got), []string{"PLAT-4"}) {
			t.Errorf("anna: %q finds %v", q, keysOf(got))
		}
	}
	got, _ := ben.Select(Query{}, "key = PLAT-4", env)
	if len(got) != 1 || strings.Contains(got[0].Body.Blocks[1].Text, "Bluebird") {
		t.Fatalf("ben reads %+v", got)
	}
	// A key ben may not read names nothing, without an error that tells it
	// exists: as a key, as a parent and as an ancestor.
	salaries := items["salaries"]
	for _, q := range []string{"key = PLAT-5", "parent = PLAT-5", "ancestor = PLAT-5", "parent = OPS-1", "key = NOPE-1", "parent = NOPE-1"} {
		got, err := ben.Select(Query{}, q, env)
		if err != nil || len(got) != 0 {
			t.Errorf("ben: %q = %v, %v", q, keysOf(got), err)
		}
	}
	// Who reads the item finds its children by its key.
	child := mustItem(t)(s.As("anna").Create(NewItem{Space: "PLAT", Type: "page", Title: "Bonus", Parent: salaries.ID}))
	if got, _ := s.As("anna").Select(Query{}, "parent = PLAT-5", env); !slices.Equal(keysOf(got), []string{child.Key}) {
		t.Errorf("anna: children of PLAT-5 = %v", keysOf(got))
	}
	// An outsider gets the same empty answer as for a key that does not exist.
	for _, q := range []string{"key = PLAT-1", "space = PLAT", "text ~ login"} {
		if got, err := s.As("carl").Select(Query{}, q, env); err != nil || len(got) != 0 {
			t.Errorf("carl: %q = %v, %v", q, keysOf(got), err)
		}
	}
}

func TestSelectAKeyFollowsAMove(t *testing.T) {
	s, items := selectFixture(t)
	noErr(t, s.GrantRole("root", "OPS", "anna", Editor))
	noErr(t, s.As("anna").Move(items["release"].ID, "OPS", ""))
	got, err := s.As("anna").Select(Query{}, "key = PLAT-3", QueryEnv{Now: at})
	if err != nil || len(got) != 1 || got[0].Space != "OPS" {
		t.Fatalf("a former key: %v, %v", keysOf(got), err)
	}
}

func TestSelectOrder(t *testing.T) {
	s, items := selectFixture(t)
	env := QueryEnv{Now: at}
	anna := s.As("anna")
	// The last change of PLAT-1 is newer than every other.
	noErr(t, anna.SetTitle(items["login"].ID, "Fix the login"))
	cases := []struct{ query, want string }{
		{"type = task", "PLAT-1 PLAT-2 PLAT-3"},
		{"type = task order by priority", "PLAT-2 PLAT-3 PLAT-1"},
		{"type = task order by priority desc", "PLAT-1 PLAT-3 PLAT-2"},
		{"type = task order by estimate desc", "PLAT-3 PLAT-1 PLAT-2"},
		{"type = task order by title", "PLAT-1 PLAT-3 PLAT-2"},
		{"type = task order by state, key desc", "PLAT-3 PLAT-1 PLAT-2"},
		{"type = task order by assignee", "PLAT-1 PLAT-2 PLAT-3"},
		{"type = task order by label", "PLAT-1 PLAT-3 PLAT-2"},
		{"type = task order by updated desc", "PLAT-1 PLAT-3 PLAT-2"},
		{"type = task order by created desc", "PLAT-3 PLAT-2 PLAT-1"},
		{"type = task order by finished, key", "PLAT-1 PLAT-3 PLAT-2"},
		{"order by type desc, key", "PLAT-1 PLAT-2 PLAT-3 PLAT-6 PLAT-4 PLAT-5"},
		{"order by space, id", "PLAT-1 PLAT-2 PLAT-3 PLAT-4 PLAT-5 PLAT-6"},
		{"order by creator", "PLAT-1 PLAT-2 PLAT-3 PLAT-4 PLAT-5 PLAT-6"},
		// Items without the value come last either way; mixed kinds sort by kind.
		{"order by due", "PLAT-6 PLAT-1 PLAT-2 PLAT-3 PLAT-4 PLAT-5"},
		{"order by due desc", "PLAT-6 PLAT-1 PLAT-2 PLAT-3 PLAT-4 PLAT-5"},
		{"order by critical", "PLAT-6 PLAT-1 PLAT-2 PLAT-3 PLAT-4 PLAT-5"},
		{"priority is not empty order by priority", "PLAT-2 PLAT-3 PLAT-1 PLAT-6"},
	}
	for _, c := range cases {
		got, err := anna.Select(Query{}, c.query, env)
		if err != nil {
			t.Errorf("%q: %v", c.query, err)
			continue
		}
		if keys := strings.Join(keysOf(got), " "); keys != c.want {
			t.Errorf("%q = %s, want %s", c.query, keys, c.want)
		}
	}
	// Keys sort by their number, not as text.
	for range 9 {
		mustItem(t)(anna.Create(NewItem{Space: "PLAT", Type: "page", Title: "More"}))
	}
	got, _ := anna.Select(Query{}, "order by key desc", env)
	if keys := keysOf(got); keys[0] != "PLAT-15" || keys[len(keys)-1] != "PLAT-1" {
		t.Errorf("keys by number: %v", keys)
	}
}

func TestSelectErrors(t *testing.T) {
	s, _ := selectFixture(t)
	ben := s.As("ben")
	cases := []struct{ query, want string }{
		{"type =", "at character 7: expected a value after ="},
		{"priorty = high", "at character 1: no type has a field priorty"},
		{"severity = x", "at character 1: no type has a field severity; the field is called Severity"},
		{"type = tsk", "at character 8: no type is called tsk"},
		{"state = closed", "at character 9: no workflow has a state closed"},
		{"priority = urgent", `at character 12: "urgent" is not a value of priority`},
		{"estimate = many", `at character 12: "many" is not a value of estimate`},
		{"due < tomorrow", `at character 7: "tomorrow" is not a value of due`},
		{"critical = yes", `at character 12: "yes" is not a value of critical`},
		{"estimate ~ 3", "at character 10: estimate does not take ~"},
		{"critical > true", "at character 10: critical does not take >"},
		{"type < task", "at character 6: type does not take <"},
		{"text = login", "at character 6: text does not take ="},
		{"text is empty", "at character 6: text does not take is empty"},
		{"created ~ x", "at character 9: created does not take ~"},
		{"created > soon", `at character 11: "soon" is not a value of created`},
		{"finished = maybe", `at character 12: "maybe" is not a value of finished`},
		{"title ~ '!!'", "at character 9: ~ needs a letter or a digit"},
		{"title = now()", `at character 9: "now()" is not a value of title`},
		{"assignee = now()", `at character 12: "now()" is not a value of assignee`},
		{"due = currentUser()", `at character 7: "currentUser()" is not a value of due`},
		{"order by text", "at character 10: the items cannot be ordered by text"},
		{"order by ancestor", "at character 10: the items cannot be ordered by ancestor"},
		{"order by nothing", "at character 10: no type has a field nothing"},
	}
	for _, c := range cases {
		_, err := ben.Select(Query{}, c.query, QueryEnv{Now: at})
		if !errors.Is(err, ErrValidation) {
			t.Errorf("%q: %v is not a validation error", c.query, err)
			continue
		}
		var qe *query.Error
		if !errors.As(err, &qe) {
			t.Errorf("%q: %v names no position", c.query, err)
		}
		if got := strings.TrimPrefix(err.Error(), ErrValidation.Error()+": "); got != c.want {
			t.Errorf("%q:\n got %s\nwant %s", c.query, got, c.want)
		}
	}
}

func TestSelectPage(t *testing.T) {
	s, _ := selectFixture(t)
	ben := s.As("ben")
	env := QueryEnv{Now: at}
	page, total, next, err := ben.SelectPage(Query{}, "order by key desc", env, "", 2)
	if err != nil || total != 5 || !slices.Equal(keysOf(page), []string{"PLAT-6", "PLAT-4"}) || next != page[1].ID {
		t.Fatalf("first page: %v %d %q %v", keysOf(page), total, next, err)
	}
	page, total, next, err = ben.SelectPage(Query{}, "order by key desc", env, next, 2)
	if err != nil || total != 5 || !slices.Equal(keysOf(page), []string{"PLAT-3", "PLAT-2"}) || next == "" {
		t.Fatalf("second page: %v %d %q %v", keysOf(page), total, next, err)
	}
	page, _, next, _ = ben.SelectPage(Query{}, "order by key desc", env, next, 2)
	if !slices.Equal(keysOf(page), []string{"PLAT-1"}) || next != "" {
		t.Fatalf("last page: %v %q", keysOf(page), next)
	}
	if _, _, _, err := ben.SelectPage(Query{}, "", env, "elsewhere", 2); !errors.Is(err, ErrValidation) {
		t.Errorf("a cursor not in the listing: %v", err)
	}
	// The listing's own filters narrow the query further.
	page, total, _, _ = ben.SelectPage(Query{Type: "task", Label: "web"}, "estimate > 1", env, "", 10)
	if total != 2 || !slices.Equal(keysOf(page), []string{"PLAT-1", "PLAT-3"}) {
		t.Errorf("with the listing's filters: %v %d", keysOf(page), total)
	}
}

func TestSelectUpdatedCountsChangesOfTheItemOnly(t *testing.T) {
	s, items := selectFixture(t)
	anna := s.As("anna")
	cut := s.now()
	// A relation to the item does not change it.
	noErr(t, anna.AddRelation("blocks", items["login"].ID, items["release"].ID))
	got, _ := anna.Select(Query{}, "key = PLAT-3 and updated > "+cut.Format(time.RFC3339Nano), QueryEnv{Now: at})
	if len(got) != 0 {
		t.Errorf("a relation to PLAT-3 changed it")
	}
	_, err := anna.AddComment(items["release"].ID, "Next week")
	noErr(t, err)
	got, _ = anna.Select(Query{}, "key = PLAT-3 and updated > "+cut.Format(time.RFC3339Nano), QueryEnv{Now: at})
	if len(got) != 1 {
		t.Errorf("a comment is a change of the item")
	}
}

func TestSelectTextFieldsAndMistakesInsideAQuery(t *testing.T) {
	s, _ := selectFixture(t)
	ben := s.As("ben")
	env := QueryEnv{Now: at}
	for q, want := range map[string][]string{
		"Severity ~ IMPACT":                  {"PLAT-6"},
		"Severity < m":                       {"PLAT-6"},
		"Severity !~ impact and type = risk": {},
		"title is empty":                     {},
		"title is not empty and type = page": {"PLAT-4"},
		"title ~ 'run'":                      {"PLAT-4"},
		"id in (nothing, " + string(mustItem(t)(ben.ByKey("PLAT-2")).ID) + ")": {"PLAT-2"},
	} {
		got, err := ben.Select(Query{}, q, env)
		if err != nil || !slices.Equal(keysOf(got), want) {
			t.Errorf("%q = %v, %v; want %v", q, keysOf(got), err, want)
		}
	}
	for _, q := range []string{"type = task and nothing = 1", "nothing = 1 or type = task", "type = task or nothing = 1",
		"not nothing = 1", "creator ~ x", "creator = now()", "Severity ~ '!!'", "key = now()", "parent = now()", "label = now()",
		"body ~ now()", "order by key, nothing"} {
		if _, err := ben.Select(Query{}, q, env); !errors.Is(err, ErrValidation) {
			t.Errorf("%q: %v", q, err)
		}
		if _, _, _, err := ben.SelectPage(Query{}, q, env, "", 10); !errors.Is(err, ErrValidation) {
			t.Errorf("paged %q: %v", q, err)
		}
	}
}

// BenchmarkSelect measures queries over 10,000 items, half of them pages
// with about 1 KB of text, half tasks with properties (ADR-0136).
func BenchmarkSelect(b *testing.B) {
	s := NewStore()
	noErrB := func(err error) {
		if err != nil {
			b.Fatal(err)
		}
	}
	for _, td := range builtinTypes() {
		noErrB(s.DefineType("admin", td))
	}
	noErrB(s.CreateSpace("admin", "PLAT", "Platform"))
	para := strings.Repeat("Lorem ipsum dolor sit amet, consectetur adipiscing elit. ", 20)
	const n = 10000
	priorities := []string{"low", "normal", "high"}
	for i := range n / 2 {
		text := para
		if i%100 == 0 {
			text += " needle"
		}
		_, err := s.Create("admin", NewItem{Space: "PLAT", Type: "page", Title: "Page", Body: &Doc{Blocks: []Block{{Kind: Paragraph, Text: text}}}})
		noErrB(err)
		_, err = s.Create("admin", NewItem{Space: "PLAT", Type: "task", Title: "Task", Properties: Props{"priority": priorities[i%3], "estimate": i % 13}})
		noErrB(err)
	}
	noErrB(s.GrantSystemRole("admin", "admin", RoleAdmin))
	x := s.As("admin")
	env := QueryEnv{Now: time.Now()}
	for _, q := range []string{
		"type = task AND priority = high AND state != done ORDER BY estimate DESC",
		"text ~ needle",
		"updated >= -7d ORDER BY updated DESC",
	} {
		b.Run(q, func(b *testing.B) {
			for b.Loop() {
				if _, err := x.Select(Query{}, q, env); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
