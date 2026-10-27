package item

import (
	"errors"
	"strings"
	"testing"
)

// TestFormsAndDecisions: a form block names an engine and a form there, a
// decision block an engine and a decision there, and neither holds
// anything else. The IDs are taken as the engine gives them: atlas lists a
// decision by its name, spaces and umlauts included (ADR-0144).
func TestFormsAndDecisions(t *testing.T) {
	s := permFixture(t)
	anna := s.As("anna")
	page := mustItem(t)(anna.Create(NewItem{Space: "PLAT", Type: "page", Title: "Travel", Body: &Doc{Blocks: []Block{
		{Kind: Form, Engine: "atlas-prod", Form: "reise-antrag", Text: "What a traveller fills in"},
		{Kind: Decision, Engine: "atlas-prod", Decision: "Decision 1", Text: "Who needs a visa"},
	}}}))
	got := mustItem(t)(s.Get(page.ID)).Body.Blocks
	if got[0].Kind != Form || got[0].Engine != "atlas-prod" || got[0].Form != "reise-antrag" || got[1].Kind != Decision || got[1].Decision != "Decision 1" || got[1].Text != "Who needs a visa" {
		t.Fatalf("body %+v", got)
	}

	for name, b := range map[string]Block{
		"form without engine":         {Kind: Form, Form: "f"},
		"form without form":           {Kind: Form, Engine: "atlas"},
		"form with a bad process":     {Kind: Form, Engine: "atlas", Form: "f", Process: "1p"},
		"form with a link":            {Kind: Form, Engine: "atlas", Form: "f", Link: strings.Repeat("a", 64)},
		"form with a decision":        {Kind: Form, Engine: "atlas", Form: "f", Decision: "d"},
		"form engine in capitals":     {Kind: Form, Engine: "Atlas", Form: "f"},
		"form with a file":            {Kind: Form, Engine: "atlas", Form: "f", Ref: fileA},
		"form with a mention":         {Kind: Form, Engine: "atlas", Form: "f", Mentions: []ID{"x"}},
		"form with a formatted text":  {Kind: Form, Engine: "atlas", Form: "f", Text: "x", Marks: []Mark{{Type: MarkBold, Start: 0, End: 1}}},
		"form aligned":                {Kind: Form, Engine: "atlas", Form: "f", Align: "center"},
		"form with a leading space":   {Kind: Form, Engine: "atlas", Form: " f"},
		"form with a line break":      {Kind: Form, Engine: "atlas", Form: "a\nb"},
		"form too long":               {Kind: Form, Engine: "atlas", Form: strings.Repeat("f", 256)},
		"decision without engine":     {Kind: Decision, Decision: "d"},
		"decision without decision":   {Kind: Decision, Engine: "atlas"},
		"decision with a form":        {Kind: Decision, Engine: "atlas", Decision: "d", Form: "f"},
		"decision with a process":     {Kind: Decision, Engine: "atlas", Decision: "d", Process: "p"},
		"decision with a tab":         {Kind: Decision, Engine: "atlas", Decision: "a\tb"},
		"decision with a width":       {Kind: Decision, Engine: "atlas", Decision: "d", Width: 50},
		"form with an unknown view":   {Kind: Form, Engine: "atlas", Form: "f", View: "table"},
		"decision with a view":        {Kind: Decision, Engine: "atlas", Decision: "d", View: ViewText},
		"process with a form":         {Kind: Process, Engine: "atlas", Process: "p", Form: "f"},
		"process with a decision":     {Kind: Process, Engine: "atlas", Process: "p", Decision: "d"},
		"paragraph with a form":       {Kind: Paragraph, Text: "x", Form: "f"},
		"paragraph with a decision":   {Kind: Paragraph, Text: "x", Decision: "d"},
		"paragraph engine and a form": {Kind: Paragraph, Text: "x", Engine: "atlas", Form: "f"},
	} {
		if err := CheckBlock(b, "self"); !errors.Is(err, ErrValidation) {
			t.Errorf("%s: %v", name, err)
		}
	}
	for _, ok := range []string{"f", "Bonität", "Decision 1", "a/b", "form-mvmnyud66ipw", "Termin planen & bestätigen", strings.Repeat("ä", 255)} {
		if err := CheckBlock(Block{Kind: Form, Engine: "atlas.prod-1", Form: ok}, "self"); err != nil {
			t.Errorf("form %q: %v", ok, err)
		}
		if err := CheckBlock(Block{Kind: Decision, Engine: "atlas.prod-1", Decision: ok}, "self"); err != nil {
			t.Errorf("decision %q: %v", ok, err)
		}
	}
}
