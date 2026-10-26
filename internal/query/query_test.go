package query

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/dbuchs7/wikiwikibangbang/internal/budget"
)

// show prints an expression in a fixed form, so a test states the tree a
// query parses to in one line.
func show(e Expr) string {
	switch x := e.(type) {
	case nil:
		return ""
	case *And:
		return "(" + show(x.Left) + " AND " + show(x.Right) + ")"
	case *Or:
		return "(" + show(x.Left) + " OR " + show(x.Right) + ")"
	case *Not:
		return "NOT " + show(x.X)
	case *Clause:
		name := x.Field.Text
		if x.Field.Quoted {
			name = `"` + name + `"`
		}
		var vals []string
		for _, v := range x.Values {
			vals = append(vals, showValue(v))
		}
		switch x.Op {
		case In, NotIn:
			return name + " " + string(x.Op) + " [" + strings.Join(vals, ",") + "]"
		case Empty, NotEmpty:
			return name + " " + string(x.Op)
		}
		return name + " " + string(x.Op) + " " + vals[0]
	}
	return "?"
}

func showValue(v Value) string {
	if !v.Func {
		if v.Quoted {
			return `"` + v.Text + `"`
		}
		return v.Text
	}
	var args []string
	for _, a := range v.Args {
		args = append(args, showValue(a))
	}
	return v.Text + "(" + strings.Join(args, ",") + ")"
}

func TestParse(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", ""},
		{"type = task", "type = task"},
		{"Type=task", "Type = task"},
		{"state != done", "state != done"},
		{"due<-7d", "due < -7d"},
		{"due <= 2026-10-01 and due >= 2026-01-01", "(due <= 2026-10-01 AND due >= 2026-01-01)"},
		{"a = 1 OR b = 2 AND c = 3", "(a = 1 OR (b = 2 AND c = 3))"},
		{"(a = 1 OR b = 2) AND c = 3", "((a = 1 OR b = 2) AND c = 3)"},
		{"not a = 1", "NOT a = 1"},
		{"NOT (a = 1 or b = 2)", "NOT (a = 1 OR b = 2)"},
		{"title ~ 'release notes'", `title ~ "release notes"`},
		{`text !~ "draft"`, `text !~ "draft"`},
		{"state in (open, 'in progress')", `state in [open,"in progress"]`},
		{"state NOT IN (done)", "state not in [done]"},
		{"assignee is empty", "assignee is empty"},
		{"assignee IS NOT NULL", "assignee is not empty"},
		{"assignee = currentUser()", "assignee = currentUser()"},
		{"assignee in (currentUser(), alice)", "assignee in [currentUser(),alice]"},
		{"created >= startOfWeek(-1)", "created >= startOfWeek(-1)"},
		{"created > startofday( -1d )", "created > startOfDay(-1d)"},
		{`"due date" < now()`, `"due date" < now()`},
		{`"created" = 2026-10-01`, `"created" = 2026-10-01`},
		{`title = "say \"hi\" \\ there"`, `title = "say "hi" \ there"`},
		{"key = PLAT-12", "key = PLAT-12"},
		{"source = https://data.example/a/b", "source = https://data.example/a/b"},
		{"label = empty-ish", "label = empty-ish"},
		{`label = "empty"`, `label = "empty"`},
		{"ORDER BY created", ""},
		{"Ä = ü", "Ä = ü"},
	}
	for _, c := range cases {
		q, err := Parse(c.in)
		if err != nil {
			t.Errorf("Parse(%q): %v", c.in, err)
			continue
		}
		if got := show(q.Where); got != c.want {
			t.Errorf("Parse(%q) = %s, want %s", c.in, got, c.want)
		}
	}
}

func TestParseOrder(t *testing.T) {
	q, err := Parse(`type = task order by due DESC, "due date" asc, key`)
	if err != nil {
		t.Fatal(err)
	}
	want := []Sort{{Field: Name{Text: "due", Pos: 21}, Desc: true}, {Field: Name{Text: "due date", Quoted: true, Pos: 31}}, {Field: Name{Text: "key", Pos: 47}}}
	if len(q.Order) != len(want) {
		t.Fatalf("order = %+v", q.Order)
	}
	for i := range want {
		if q.Order[i] != want[i] {
			t.Errorf("order[%d] = %+v, want %+v", i, q.Order[i], want[i])
		}
	}
	if show(q.Where) != "type = task" {
		t.Errorf("where = %s", show(q.Where))
	}
}

func TestPositions(t *testing.T) {
	q, err := Parse("a = 1 AND  bé = 'x'")
	if err != nil {
		t.Fatal(err)
	}
	c := q.Where.(*And).Right.(*Clause)
	if c.Field.Pos != 11 || c.Values[0].Pos != 16 {
		t.Errorf("positions in runes: field %d, value %d", c.Field.Pos, c.Values[0].Pos)
	}
}

func TestSyntaxErrors(t *testing.T) {
	cases := []struct{ in, want string }{
		{"type", "at character 5: expected an operator after type"},
		{"type =", "at character 7: expected a value after ="},
		{"= task", "at character 1: expected a field name"},
		{"type = task state = open", "at character 13: expected AND, OR or ORDER BY"},
		{"(type = task", "at character 13: expected ) to close the ( at character 1"},
		{"type = task)", "at character 12: expected AND, OR or ORDER BY"},
		{"state in open", "at character 10: expected ( after IN"},
		{"state in ()", "at character 11: expected a value"},
		{"state in (a b)", "at character 13: expected , or ) in the list"},
		{"state not = a", "at character 11: expected IN after NOT"},
		{"assignee is", "at character 12: expected EMPTY after IS"},
		{"assignee is alice", "at character 13: expected EMPTY after IS"},
		{`title = "open`, "at character 9: the text is not closed with \""},
		{"a = foo()", "at character 5: unknown function foo(); the functions are currentUser, now, startOfDay, endOfDay, startOfWeek, endOfWeek, startOfMonth, endOfMonth, startOfYear, endOfYear"},
		{"a = now(1)", "at character 5: now() takes no argument"},
		{"a = startOfDay(1, 2)", "at character 5: startOfDay() takes at most one argument"},
		{"a = startOfDay(x)", "at character 16: the argument of startOfDay() is a whole number or an offset such as -1d"},
		{"a = startOfDay(now())", "at character 16: the argument of startOfDay() is a whole number or an offset such as -1d"},
		{"a = startOfDay(1", "at character 17: expected , or ) after the argument"},
		{"order created", "at character 7: expected BY after ORDER"},
		{"order by", "at character 9: expected a field name"},
		{"order by due up", "at character 14: expected ASC, DESC or , after due"},
		{"a(b) = 1", "at character 2: expected an operator after a"},
		{"and = 1", "at character 1: expected a field name"},
		{"a = and", "at character 5: expected a value after ="},
		{"a ! b", "at character 3: unexpected !"},
	}
	for _, c := range cases {
		_, err := Parse(c.in)
		if err == nil {
			t.Errorf("Parse(%q) accepted", c.in)
			continue
		}
		var e *Error
		if !errors.As(err, &e) {
			t.Errorf("Parse(%q): %T is not an *Error", c.in, err)
		}
		if err.Error() != c.want {
			t.Errorf("Parse(%q):\n got %s\nwant %s", c.in, err, c.want)
		}
	}
}

func TestBudgets(t *testing.T) {
	limits := budget.Get()
	long := "title ~ '" + strings.Repeat("x", limits.QueryBytes) + "'"
	if _, err := Parse(long); err == nil || !strings.Contains(err.Error(), "at most") {
		t.Errorf("an overlong query: %v", err)
	}
	terms := make([]string, limits.QueryTerms+1)
	for i := range terms {
		terms[i] = "a = 1"
	}
	if _, err := Parse(strings.Join(terms, " or ")); err == nil || !strings.Contains(err.Error(), "conditions") {
		t.Errorf("too many conditions: %v", err)
	}
	vals := make([]string, limits.QueryTerms+1)
	for i := range vals {
		vals[i] = "v"
	}
	if _, err := Parse("a in (" + strings.Join(vals, ",") + ")"); err == nil {
		t.Error("each value of a list counts as a condition")
	}
	if _, err := Parse("a in (" + strings.Join(vals[1:], ",") + ")"); err != nil {
		t.Errorf("a list within the budget: %v", err)
	}
	budget.Set(budget.Budgets{QueryBytes: 8, QueryTerms: 1})
	t.Cleanup(func() { budget.Set(budget.Default()) })
	if _, err := Parse("a = 1 or b = 2"); err == nil {
		t.Error("the budgets in force bound the query")
	}
}

func TestDeepNesting(t *testing.T) {
	n := budget.Get().QueryBytes/2 - 4
	text := strings.Repeat("(", n) + "a=1" + strings.Repeat(")", n)
	if len(text) > budget.Get().QueryBytes {
		t.Fatal("the test query is too long")
	}
	if _, err := Parse(text); err != nil {
		t.Errorf("nesting within the budget: %v", err)
	}
	if _, err := Parse(strings.Repeat("not ", 200) + "a = 1"); err != nil {
		t.Errorf("repeated NOT: %v", err)
	}
}

func TestTime(t *testing.T) {
	// Wednesday, 2026-10-28, 15:04 UTC.
	now := time.Date(2026, 10, 28, 15, 4, 5, 0, time.UTC)
	day := func(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }
	end := func(y int, m time.Month, d int) time.Time { return day(y, m, d).Add(24*time.Hour - time.Nanosecond) }
	cases := []struct {
		in      string
		want    time.Time
		wantDay bool
	}{
		{"x = 2026-10-01", day(2026, 10, 1), true},
		{"x = '2026-10-01'", day(2026, 10, 1), true},
		{"x = 2026-10-01T08:30:00Z", time.Date(2026, 10, 1, 8, 30, 0, 0, time.UTC), false},
		{"x = 2026-10-01T10:30:00+02:00", time.Date(2026, 10, 1, 8, 30, 0, 0, time.UTC), false},
		{"x = -7d", now.Add(-7 * 24 * time.Hour), false},
		{"x = 2w", now.Add(14 * 24 * time.Hour), false},
		{"x = +12h", now.Add(12 * time.Hour), false},
		{"x = -1y", now.AddDate(-1, 0, 0), false},
		{"x = now()", now, false},
		{"x = startOfDay()", day(2026, 10, 28), false},
		{"x = startOfDay(-1)", day(2026, 10, 27), false},
		{"x = startOfDay(-1d)", day(2026, 10, 27), false},
		{"x = startOfDay(+12h)", day(2026, 10, 28).Add(12 * time.Hour), false},
		{"x = endOfDay()", end(2026, 10, 28), false},
		{"x = startOfWeek()", day(2026, 10, 26), false},
		{"x = startOfWeek(1)", day(2026, 11, 2), false},
		{"x = endOfWeek()", end(2026, 11, 1), false},
		{"x = startOfMonth()", day(2026, 10, 1), false},
		{"x = startOfMonth(-1)", day(2026, 9, 1), false},
		{"x = endOfMonth()", end(2026, 10, 31), false},
		{"x = endOfMonth(-8)", end(2026, 2, 28), false},
		{"x = startOfYear()", day(2026, 1, 1), false},
		{"x = endOfYear(1)", end(2027, 12, 31), false},
	}
	for _, c := range cases {
		q, err := Parse(c.in)
		if err != nil {
			t.Errorf("Parse(%q): %v", c.in, err)
			continue
		}
		got, isDay, ok := q.Where.(*Clause).Values[0].Time(now)
		if !ok || !got.Equal(c.want) || isDay != c.wantDay {
			t.Errorf("%s: %v day=%v ok=%v, want %v day=%v", c.in, got, isDay, ok, c.want, c.wantDay)
		}
	}
	for _, in := range []string{"x = soon", "x = 2026-13-01", "x = -7m", "x = currentUser()", "x = 7", "x = d"} {
		q, err := Parse(in)
		if err != nil {
			t.Errorf("Parse(%q): %v", in, err)
			continue
		}
		if _, _, ok := q.Where.(*Clause).Values[0].Time(now); ok {
			t.Errorf("%s reads as a time", in)
		}
	}
	// A week starts on Monday, also on a Sunday.
	sunday := time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC)
	q, _ := Parse("x = startOfWeek()")
	if got, _, _ := q.Where.(*Clause).Values[0].Time(sunday); !got.Equal(day(2026, 10, 26)) {
		t.Errorf("start of the week on a Sunday: %v", got)
	}
}

func TestCurrentUser(t *testing.T) {
	q, err := Parse("a = CURRENTUSER() and b = currentUser")
	if err != nil {
		t.Fatal(err)
	}
	and := q.Where.(*And)
	if !and.Left.(*Clause).Values[0].IsCurrentUser() {
		t.Error("currentUser() is the caller, in any case")
	}
	if and.Right.(*Clause).Values[0].IsCurrentUser() {
		t.Error("a bare word currentUser is a value, not the function")
	}
}

func TestErrorf(t *testing.T) {
	err := Errorf(4, "no field %q", "x")
	if err.Error() != `at character 5: no field "x"` {
		t.Errorf("Errorf = %v", err)
	}
}
