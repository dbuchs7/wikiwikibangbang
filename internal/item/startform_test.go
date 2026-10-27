package item

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

// TestStartForms: a form block may name the process whose steps name it
// and show the list of its fields; a start form names an engine and a
// public start link there, maybe the process it starts, and the fields it
// fills in beforehand from what this site knows of the reader, and
// nothing else (ADR-0148).
func TestStartForms(t *testing.T) {
	s := permFixture(t)
	anna := s.As("anna")
	link := "8f14e45fceea167a5a36dedd4bea2543a4c1b5e2f0d6c7b8a9e0f1d2c3b4a596"
	page := mustItem(t)(anna.Create(NewItem{Space: "PLAT", Type: "page", Title: "Incidents", Body: &Doc{Blocks: []Block{
		{Kind: Form, Engine: "atlas-prod", Form: "incident-report", Process: "incident", View: ViewText},
		{Kind: StartForm, Engine: "atlas-prod", Link: link, Process: "incident", Prefill: []Prefill{{Field: "reporter", From: "username"}, {Field: "contact.mail", From: "email"}}, Text: "Report"},
		{Kind: StartForm, Engine: "atlas-prod", Link: link},
	}}}))
	got := mustItem(t)(s.Get(page.ID)).Body.Blocks
	if got[0].View != ViewText || got[0].Process != "incident" || got[1].Link != link || len(got[1].Prefill) != 2 || got[1].Prefill[1] != (Prefill{Field: "contact.mail", From: "email"}) ||
		got[2].Process != "" || got[2].Prefill != nil {
		t.Fatalf("body %+v", got)
	}

	tooMany := make([]Prefill, MaxPrefill+1)
	for i := range tooMany {
		tooMany[i] = Prefill{Field: fmt.Sprint("f", i), From: "page"}
	}
	for name, b := range map[string]Block{
		"without engine":          {Kind: StartForm, Link: link},
		"without link":            {Kind: StartForm, Engine: "atlas"},
		"link in capitals":        {Kind: StartForm, Engine: "atlas", Link: strings.ToUpper(link)},
		"link too short":          {Kind: StartForm, Engine: "atlas", Link: link[:31]},
		"link too long":           {Kind: StartForm, Engine: "atlas", Link: strings.Repeat("a", 129)},
		"link as a path":          {Kind: StartForm, Engine: "atlas", Link: "../../api/v1/processes/" + link},
		"bad process":             {Kind: StartForm, Engine: "atlas", Link: link, Process: "a b"},
		"with a form":             {Kind: StartForm, Engine: "atlas", Link: link, Form: "f"},
		"with a view":             {Kind: StartForm, Engine: "atlas", Link: link, View: ViewBoth},
		"with a decision":         {Kind: StartForm, Engine: "atlas", Link: link, Decision: "d"},
		"with a mention":          {Kind: StartForm, Engine: "atlas", Link: link, Mentions: []ID{"x"}},
		"formatted caption":       {Kind: StartForm, Engine: "atlas", Link: link, Text: "x", Marks: []Mark{{Type: MarkBold, Start: 0, End: 1}}},
		"empty prefill":           {Kind: StartForm, Engine: "atlas", Link: link, Prefill: []Prefill{}},
		"prefill without field":   {Kind: StartForm, Engine: "atlas", Link: link, Prefill: []Prefill{{From: "email"}}},
		"prefill from elsewhere":  {Kind: StartForm, Engine: "atlas", Link: link, Prefill: []Prefill{{Field: "x", From: "password"}}},
		"prefill a field twice":   {Kind: StartForm, Engine: "atlas", Link: link, Prefill: []Prefill{{Field: "x", From: "email"}, {Field: "x", From: "page"}}},
		"prefill with a newline":  {Kind: StartForm, Engine: "atlas", Link: link, Prefill: []Prefill{{Field: "a\nb", From: "email"}}},
		"prefill too many":        {Kind: StartForm, Engine: "atlas", Link: link, Prefill: tooMany},
		"paragraph with a link":   {Kind: Paragraph, Text: "x", Link: link},
		"process with a prefill":  {Kind: Process, Engine: "atlas", Process: "p", Prefill: []Prefill{{Field: "x", From: "email"}}},
		"decision with a process": {Kind: Decision, Engine: "atlas", Decision: "d", Process: "p"},
	} {
		if err := CheckBlock(b, "self"); !errors.Is(err, ErrValidation) {
			t.Errorf("%s: %v", name, err)
		}
	}
	for _, from := range PrefillSources {
		if err := CheckBlock(Block{Kind: StartForm, Engine: "atlas", Link: link[:32], Prefill: []Prefill{{Field: "Melder Name", From: from}}}, "self"); err != nil {
			t.Errorf("prefill from %s: %v", from, err)
		}
	}
	if err := CheckBlock(Block{Kind: StartForm, Engine: "atlas", Link: link, Prefill: tooMany[:MaxPrefill]}, "self"); err != nil {
		t.Errorf("%d fields filled in: %v", MaxPrefill, err)
	}
	for _, v := range []string{"", ViewBoth, ViewText} {
		if err := CheckBlock(Block{Kind: Form, Engine: "atlas", Form: "f", View: v}, "self"); err != nil {
			t.Errorf("form view %q: %v", v, err)
		}
	}
	if !ValidStartLink(link) || ValidStartLink(link+"g") || ValidStartLink("") {
		t.Error("ValidStartLink")
	}
}
