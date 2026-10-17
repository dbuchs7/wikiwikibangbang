package migrate

import (
	"testing"
	"time"

	"github.com/dbuchs7/wikiwikibangbang/internal/item"
)

func TestConvertValues(t *testing.T) {
	e := &engine{m: Mapping{Users: map[string]string{"acc-1": "anna"}}}
	when := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	ok := []struct {
		kind item.FieldKind
		in   any
		want any
	}{
		{item.KindNumber, 2, 2.0},
		{item.KindNumber, int64(3), 3.0},
		{item.KindNumber, 1.5, 1.5},
		{item.KindDate, when, when},
		{item.KindDate, "2026-01-02T03:04:05Z", when},
		{item.KindUser, "acc-1", "anna"},
		{item.KindUser, "acc-2", "external:acc-2"},
		{item.KindText, "x", "x"},
		{item.KindEnum, "high", "high"},
	}
	for _, c := range ok {
		got, err := e.convert(c.kind, c.in)
		if err != nil {
			t.Errorf("%s %#v: %v", c.kind, c.in, err)
			continue
		}
		if g, isTime := got.(time.Time); isTime {
			if !g.Equal(c.want.(time.Time)) {
				t.Errorf("%s %#v = %v", c.kind, c.in, got)
			}
		} else if got != c.want {
			t.Errorf("%s %#v = %#v, want %#v", c.kind, c.in, got, c.want)
		}
	}
	for _, c := range []struct {
		kind item.FieldKind
		in   any
	}{{item.KindNumber, "3"}, {item.KindDate, "next week"}, {item.KindDate, 7}, {item.KindUser, ""}, {item.KindText, 1}} {
		if _, err := e.convert(c.kind, c.in); err == nil {
			t.Errorf("%s %#v: want an error", c.kind, c.in)
		}
	}
	if fieldDef(item.TypeDef{}, "x").Name != "" {
		t.Error("fieldDef of a missing field")
	}
}

func TestEdgeCases(t *testing.T) {
	s := newStore(t)
	// An existing type with the derived name is used as it is.
	if err := s.DefineType("admin", item.TypeDef{Name: "story", Body: true}); err != nil {
		t.Fatal(err)
	}
	snap := Snapshot{
		Instance: site,
		Users:    []SrcUser{{ID: "acc-1", Name: "Anna", Email: "anna@acme.example"}},
		Projects: []SrcSpace{{Key: "OPS", Name: "Ops"}},
		IssueTypes: []SrcIssueType{
			{Name: "Story"},
			{Name: "Broken", Workflow: SrcWorkflow{Initial: "Nowhere", Statuses: []string{"Open"}}},
		},
		Issues: []SrcIssue{
			{ID: "1", Key: "OPS-1", Project: "OPS", Type: "Story", Summary: "s", Links: []SrcLink{{Type: "Relates", Target: "404"}}, Fields: map[string]any{"unknown": 1}},
			{ID: "2", Key: "OPS-2", Project: "OPS", Type: "Broken", Summary: "b", Status: "Open"},
			{ID: "3", Key: "OPS-3", Project: "OPS", Type: "Epic", Summary: "e"},
			{ID: "4", Key: "OPS-4", Project: "OPS", Type: "Story", Summary: "orphan", Parent: "999"},
		},
		Spaces: []SrcSpace{{Key: "OPS", Name: "Ops"}},
		Pages: []SrcPage{
			{ID: "p1", Space: "OPS", Title: "Notes", Body: `loose text <p>hi <ac:link><ri:user ri:account-id="acc-1"/></ac:link></p>`,
				EditAllowList: []string{"acc-1"}},
			{ID: "p2", Space: "OPS", Title: "Child", Parent: "missing-page"},
		},
	}
	m := Derive(snap)
	rep, err := Run(s, snap, m, Options{Actor: "import"})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"jira type Broken", "OPS-2", "issue type \"Epic\"", "OPS-4", "404", "missing-page"} {
		if !hasProblem(rep, want) {
			t.Errorf("missing problem about %q: %+v", want, rep.Problems)
		}
	}
	notes, ok := s.FindByOrigin("confluence", site, "p1")
	if !ok {
		t.Fatal("page not imported")
	}
	if b := notes.Body.Blocks; len(b) != 2 || b[0].Text != "loose text" || b[1].Text != "hi @anna@acme.example" {
		t.Fatalf("page body = %+v", notes.Body)
	}
	if r := notes.Restriction; len(r.Read) != 0 || len(r.Edit) != 1 || r.Edit[0] != "anna@acme.example" {
		t.Fatalf("edit restriction = %+v", r)
	}
	if _, ok := s.FindByOrigin("jira", site, "1"); !ok {
		t.Fatal("story with an unresolvable link must still be imported")
	}
}
