package item

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func catalogStore(t *testing.T) *Store {
	t.Helper()
	s := setup(t)
	noErr(t, s.DefineType("admin", TypeDef{Name: "interface", Fields: []FieldDef{
		{Name: "source-id", Kind: KindText, Unique: true},
		{Name: "specification", Kind: KindURL},
		{Name: "contact", Kind: KindText, Recommended: true},
		{Name: "owner", Kind: KindUser},
		{Name: "reviewed", Kind: KindDate},
	}, Review: &Review{EveryDays: 365, Field: "reviewed", Owner: "owner"}}))
	noErr(t, s.GrantRole("admin", "PLAT", "anna", Editor))
	noErr(t, s.GrantRole("admin", "OPS", "anna", Editor))
	return s
}

func isInvalid(t *testing.T, what string, err error, want string) {
	t.Helper()
	if !errors.Is(err, ErrValidation) || !strings.Contains(err.Error(), want) {
		t.Errorf("%s: %v, want a validation error with %q", what, err, want)
	}
}

func TestURLFields(t *testing.T) {
	s := catalogStore(t)
	it := mustItem(t)(s.Create("anna", NewItem{Space: "PLAT", Type: "interface", Title: "A", Properties: Props{"specification": "https://example.org/a.yaml"}}))
	for _, bad := range []any{"ftp://example.org/x", "example.org", "https://", "https://exa mple.org", "javascript:alert(1)", 3, "https://x.org/" + strings.Repeat("a", maxURL)} {
		isInvalid(t, "url "+FormatValue(bad), s.SetProperty("anna", it.ID, "specification", bad), "not a valid url")
	}
	noErr(t, s.SetProperty("anna", it.ID, "specification", "http://intranet.local:8080/spec?v=2"))
}

func TestUniqueFields(t *testing.T) {
	s := catalogStore(t)
	a := mustItem(t)(s.Create("anna", NewItem{Space: "PLAT", Type: "interface", Title: "A", Properties: Props{"source-id": "x-1"}}))
	_, err := s.Create("anna", NewItem{Space: "PLAT", Type: "interface", Title: "B", Properties: Props{"source-id": "x-1"}})
	isInvalid(t, "a second item with the value", err, "already used by "+a.Key)
	// Another space, or no value, is fine.
	other := mustItem(t)(s.Create("anna", NewItem{Space: "OPS", Type: "interface", Title: "C", Properties: Props{"source-id": "x-1"}}))
	b := mustItem(t)(s.Create("anna", NewItem{Space: "PLAT", Type: "interface", Title: "B"}))
	mustItem(t)(s.Create("anna", NewItem{Space: "PLAT", Type: "interface", Title: "D"}))
	isInvalid(t, "setting the value", s.SetProperty("anna", b.ID, "source-id", "x-1"), "already used")
	noErr(t, s.SetProperty("anna", a.ID, "source-id", "x-1")) // its own value
	isInvalid(t, "moving into a space that holds the value", s.Move("anna", other.ID, "PLAT", ""), "already used")

	// A trashed item frees its value; restoring it while another holds the
	// value is refused.
	noErr(t, s.Trash("anna", a.ID))
	noErr(t, s.SetProperty("anna", b.ID, "source-id", "x-1"))
	isInvalid(t, "restoring", s.Restore("anna", a.ID), "already used by "+b.Key)

	// Imports are held to it too.
	origin := Origin{System: "test", Instance: "i", ExternalID: "1"}
	_, _, err = s.Upsert("sync", Upsert{Origin: origin, Space: "PLAT", Type: "interface", Title: "E", Properties: Props{"source-id": "x-1"}})
	isInvalid(t, "an import", err, "already used")
	e, _, err := s.Upsert("sync", Upsert{Origin: origin, Space: "PLAT", Type: "interface", Title: "E", Properties: Props{"source-id": "x-2"}})
	noErr(t, err)
	_, _, err = s.Upsert("sync", Upsert{Origin: origin, Space: "PLAT", Type: "interface", Title: "E", Properties: Props{"source-id": "x-1"}})
	isInvalid(t, "an import run changing the value", err, "already used")
	if _, changed, err := s.Upsert("sync", Upsert{Origin: origin, Space: "PLAT", Type: "interface", Title: "E", Properties: Props{"source-id": "x-2"}}); err != nil || changed {
		t.Errorf("an unchanged import: %v %v", changed, err)
	}
	_ = e

	// A type cannot become unique over values held twice.
	noErr(t, s.DefineType("admin", TypeDef{Name: "note", Fields: []FieldDef{{Name: "ref", Kind: KindText}}}))
	mustItem(t)(s.Create("anna", NewItem{Space: "PLAT", Type: "note", Title: "1", Properties: Props{"ref": "r"}}))
	mustItem(t)(s.Create("anna", NewItem{Space: "PLAT", Type: "note", Title: "2", Properties: Props{"ref": "r"}}))
	isInvalid(t, "redefining", s.DefineType("admin", TypeDef{Name: "note", Fields: []FieldDef{{Name: "ref", Kind: KindText, Unique: true}}}), "cannot be unique")
}

func TestTypeRulesAreChecked(t *testing.T) {
	s := setup(t)
	for name, td := range map[string]TypeDef{
		"a unique bool":       {Name: "x", Fields: []FieldDef{{Name: "f", Kind: KindBool, Unique: true}}},
		"a review every 0":    {Name: "x", Fields: []FieldDef{{Name: "d", Kind: KindDate}}, Review: &Review{EveryDays: 0, Field: "d"}},
		"a review of no date": {Name: "x", Fields: []FieldDef{{Name: "d", Kind: KindText}}, Review: &Review{EveryDays: 30, Field: "d"}},
		"a review owner text": {Name: "x", Fields: []FieldDef{{Name: "d", Kind: KindDate}, {Name: "o", Kind: KindText}}, Review: &Review{EveryDays: 30, Field: "d", Owner: "o"}},
		"a review owner none": {Name: "x", Fields: []FieldDef{{Name: "d", Kind: KindDate}}, Review: &Review{EveryDays: 30, Field: "d", Owner: "o"}},
	} {
		if err := s.DefineType("admin", td); !errors.Is(err, ErrValidation) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestWhereText(t *testing.T) {
	s := catalogStore(t)
	mustItem(t)(s.Create("anna", NewItem{Space: "PLAT", Type: "interface", Title: "A", Properties: Props{"source-id": "x-1"}}))
	b := mustItem(t)(s.Create("anna", NewItem{Space: "PLAT", Type: "interface", Title: "B", Properties: Props{"source-id": "x-2", "reviewed": time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)}}))
	if got := s.Find(Query{WhereText: map[string]string{"source-id": "x-2"}}); len(got) != 1 || got[0].ID != b.ID {
		t.Fatalf("by text: %v", got)
	}
	if got := s.Find(Query{WhereText: map[string]string{"reviewed": "2026-01-02"}}); len(got) != 1 {
		t.Fatalf("by date: %v", got)
	}
	if got := s.Find(Query{WhereText: map[string]string{"source-id": "x-2", "contact": ""}}); len(got) != 0 {
		t.Fatalf("an unset field matches nothing: %v", got)
	}
	for v, want := range map[any]string{nil: "", 3: "3", 2.5: "2.5", true: "true", "s": "s"} {
		if got := FormatValue(v); got != want {
			t.Errorf("FormatValue(%v) = %q", v, got)
		}
	}
}

// Items of a type with a review rule are due until reviewed and again
// after EveryDays; the owner answers for their findings; empty
// recommended fields are reported.
func TestReviewsInTheGovernanceReport(t *testing.T) {
	s := catalogStore(t)
	now := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	never := mustItem(t)(s.Create("sync", NewItem{Space: "PLAT", Type: "interface", Title: "Never", Properties: Props{"owner": "anna"}}))
	done := mustItem(t)(s.Create("sync", NewItem{Space: "PLAT", Type: "interface", Title: "Done", Properties: Props{"contact": "x@example.org"}}))
	anna := s.As("anna")
	noErr(t, anna.MarkReviewed(done.ID, now))
	rep, err := anna.Governance("PLAT", now, 1000*24*time.Hour)
	noErr(t, err)
	if len(rep.Reviews) != 1 || rep.Reviews[0].Item != never.ID || !rep.Reviews[0].LastReview.IsZero() || rep.Reviews[0].Responsible != "anna" {
		t.Fatalf("reviews %+v", rep.Reviews)
	}
	found := false
	for _, f := range rep.Implausible {
		if f.Item == never.ID && strings.Contains(f.Problem, `recommended field "contact" is empty`) {
			found = f.Responsible == "anna"
		}
		if f.Item == done.ID && strings.Contains(f.Problem, "contact") {
			t.Error("a set recommended field is reported")
		}
	}
	if !found {
		t.Errorf("the empty recommended field is not reported to the owner: %+v", rep.Implausible)
	}
	later := now.AddDate(1, 0, 1)
	rep, _ = anna.Governance("PLAT", later, 1000*24*time.Hour)
	if len(rep.Reviews) != 2 || rep.Reviews[1].Due.Format(time.DateOnly) != "2027-10-21" {
		t.Fatalf("a year later %+v", rep.Reviews)
	}
	if mine := anna.Findings(now, 1000*24*time.Hour); len(mine.Reviews) != 1 {
		t.Errorf("anna's findings %+v", mine.Reviews)
	}
	page := mustItem(t)(s.Create("anna", NewItem{Space: "PLAT", Type: "page", Title: "P"}))
	isInvalid(t, "a type without a rule", anna.MarkReviewed(page.ID, now), "no review rule")
	if err := s.As("nobody").MarkReviewed(never.ID, now); err == nil {
		t.Error("someone without rights marked an item reviewed")
	}
	if err := anna.MarkReviewed("missing", now); err == nil {
		t.Error("a missing item was marked reviewed")
	}
}

// The link check also covers url fields and an item's place in its system
// of record, with the owner to fix them (ADR-0074).
func TestWebLinksOfFieldsAndOrigins(t *testing.T) {
	s := catalogStore(t)
	mustItem(t)(s.Create("anna", NewItem{Space: "PLAT", Type: "interface", Title: "A",
		Properties: Props{"specification": "https://example.org/a.yaml", "owner": "bert"}}))
	_, _, err := s.Upsert("sync", Upsert{Origin: Origin{System: "opendata.swiss", Instance: "https://opendata.swiss", ExternalID: "x", URL: "https://opendata.swiss/de/dataset/x"},
		Space: "PLAT", Type: "interface", Title: "B"})
	noErr(t, err)
	links, err := s.As("anna").WebLinks("PLAT")
	noErr(t, err)
	if len(links) != 2 || links[0].Field != "specification" || links[0].Block != -1 || links[0].Responsible != "bert" ||
		links[1].Field != "origin" || links[1].Href != "https://opendata.swiss/de/dataset/x" || links[1].Responsible != "sync" {
		t.Fatalf("links %+v", links)
	}
}

// A state is finished when it is the workflow's last or no transition
// leaves it (ADR-0082).
func TestFinishedStates(t *testing.T) {
	decision := &Workflow{Initial: "proposed", States: []string{"proposed", "accepted", "rejected"},
		Transitions: []Transition{{Name: "accept", From: []string{"proposed"}, To: "accepted"}, {Name: "reject", From: []string{"proposed"}, To: "rejected"}}}
	task := &Workflow{Initial: "open", States: []string{"open", "done"},
		Transitions: []Transition{{Name: "finish", From: []string{"open"}, To: "done"}, {Name: "reopen", From: []string{"done"}, To: "open"}}}
	for _, c := range []struct {
		wf    *Workflow
		state string
		want  bool
	}{{decision, "proposed", false}, {decision, "accepted", true}, {decision, "rejected", true}, {decision, "unknown", false},
		{task, "open", false}, {task, "done", true}, {nil, "x", false}, {&Workflow{}, "x", false}} {
		if got := c.wf.Finished(c.state); got != c.want {
			t.Errorf("%s: %v, want %v", c.state, got, c.want)
		}
	}
}
