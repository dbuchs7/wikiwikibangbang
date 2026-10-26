package item

import (
	"errors"
	"strings"
	"testing"
)

// TestBPMNDiagramsAndProcesses: a bpmn block shows a text file of its own
// item, a process block names an engine and a process of it, and neither
// holds anything else (ADR-0129).
func TestBPMNDiagramsAndProcesses(t *testing.T) {
	s := permFixture(t)
	anna := s.As("anna")
	page := mustItem(t)(anna.Create(NewItem{Space: "PLAT", Type: "page", Title: "Order to cash"}))
	model := Attachment{ID: fileA, Name: "order.bpmn", Type: "text/xml; charset=utf-8", Size: 3, SHA256: strings.Repeat("ab", 32)}
	_, err := anna.AddAttachment(page.ID, model)
	noErr(t, err)
	_, err = anna.AddAttachment(page.ID, testFile(fileB, "order.png"))
	noErr(t, err)

	noErr(t, anna.CommitBody(page.ID, Doc{Blocks: []Block{
		{Kind: BPMN, Ref: fileA, Text: "The order process"},
		{Kind: Process, Engine: "atlas-prod", Process: "order-to-cash", Text: "As it runs"},
	}}))
	got := mustItem(t)(s.Get(page.ID)).Body.Blocks
	if got[0].Kind != BPMN || got[0].Ref != fileA || got[1].Engine != "atlas-prod" || got[1].Process != "order-to-cash" {
		t.Fatalf("body %+v", got)
	}

	if err := anna.CommitBody(page.ID, Doc{Blocks: []Block{{Kind: BPMN, Ref: fileB}}}); !errors.Is(err, ErrValidation) {
		t.Errorf("a picture as a BPMN model: %v", err)
	}
	other := mustItem(t)(anna.Create(NewItem{Space: "PLAT", Type: "page", Title: "Other"}))
	if err := anna.CommitBody(other.ID, Doc{Blocks: []Block{{Kind: BPMN, Ref: fileA}}}); !errors.Is(err, ErrNotFound) {
		t.Errorf("a diagram of another item's file: %v", err)
	}
	if _, err := anna.Create(NewItem{Space: "PLAT", Type: "page", Title: "New", Body: &Doc{Blocks: []Block{{Kind: BPMN, Ref: fileA}}}}); !errors.Is(err, ErrValidation) {
		t.Errorf("a new item shows a file it does not have yet: %v", err)
	}
	// A process names no file, so a new item may show one at once.
	_, err = anna.Create(NewItem{Space: "PLAT", Type: "page", Title: "Live", Body: &Doc{Blocks: []Block{{Kind: Process, Engine: "atlas", Process: "onboarding"}}}})
	noErr(t, err)

	for name, b := range map[string]Block{
		"diagram without a file":   {Kind: BPMN},
		"formatted caption":        {Kind: BPMN, Ref: fileA, Text: "x", Marks: []Mark{{Type: MarkBold, Start: 0, End: 1}}},
		"diagram with an engine":   {Kind: BPMN, Ref: fileA, Engine: "atlas", Process: "p"},
		"diagram with a width":     {Kind: BPMN, Ref: fileA, Width: 50},
		"diagram aligned":          {Kind: BPMN, Ref: fileA, Align: "center"},
		"process without engine":   {Kind: Process, Process: "p"},
		"process without process":  {Kind: Process, Engine: "atlas"},
		"engine in capitals":       {Kind: Process, Engine: "Atlas", Process: "p"},
		"engine with a slash":      {Kind: Process, Engine: "a/b", Process: "p"},
		"process with a space":     {Kind: Process, Engine: "atlas", Process: "a b"},
		"process with a slash":     {Kind: Process, Engine: "atlas", Process: "a/b"},
		"process from a digit":     {Kind: Process, Engine: "atlas", Process: "1st"},
		"process too long":         {Kind: Process, Engine: "atlas", Process: strings.Repeat("p", 256)},
		"process with a file":      {Kind: Process, Engine: "atlas", Process: "p", Ref: fileA},
		"process with a mention":   {Kind: Process, Engine: "atlas", Process: "p", Mentions: []ID{"x"}},
		"paragraph with a process": {Kind: Paragraph, Text: "x", Engine: "atlas", Process: "p"},
		"paragraph with an engine": {Kind: Paragraph, Text: "x", Engine: "atlas"},
	} {
		if err := CheckBlock(b, "self"); !errors.Is(err, ErrValidation) {
			t.Errorf("%s: %v", name, err)
		}
	}
	for _, ok := range []string{"p", "Order_To_Cash", "_x", "order-to-cash.v2", "Bestellung_Prüfen", strings.Repeat("p", 255)} {
		if err := CheckBlock(Block{Kind: Process, Engine: "atlas.prod-1", Process: ok}, "self"); err != nil {
			t.Errorf("process %q: %v", ok, err)
		}
	}
}

// TestADiagramShowsItsDescriptionAsChosen: a bpmn or a process block shows
// its diagram, its diagram and its description, or its description only
// (ADR-0143); nothing else carries a view, and no other view is kept.
func TestADiagramShowsItsDescriptionAsChosen(t *testing.T) {
	s := permFixture(t)
	anna := s.As("anna")
	page := mustItem(t)(anna.Create(NewItem{Space: "PLAT", Type: "page", Title: "Order to cash"}))
	_, err := anna.AddAttachment(page.ID, Attachment{ID: fileA, Name: "order.bpmn", Type: "text/xml; charset=utf-8", Size: 3, SHA256: strings.Repeat("ab", 32)})
	noErr(t, err)
	noErr(t, anna.CommitBody(page.ID, Doc{Blocks: []Block{
		{Kind: BPMN, Ref: fileA, View: ViewBoth},
		{Kind: Process, Engine: "atlas", Process: "order", View: ViewText},
		{Kind: Process, Engine: "atlas", Process: "order"},
	}}))
	got := mustItem(t)(s.Get(page.ID)).Body.Blocks
	if got[0].View != ViewBoth || got[1].View != ViewText || got[2].View != "" {
		t.Fatalf("views %q %q %q", got[0].View, got[1].View, got[2].View)
	}
	for name, b := range map[string]Block{
		"unknown view":        {Kind: Process, Engine: "atlas", Process: "p", View: "diagram"},
		"view in capitals":    {Kind: BPMN, Ref: fileA, View: "Both"},
		"paragraph with view": {Kind: Paragraph, Text: "x", View: ViewBoth},
		"picture with view":   {Kind: Image, Ref: fileA, View: ViewText},
	} {
		if err := CheckBlock(b, "self"); !errors.Is(err, ErrValidation) {
			t.Errorf("%s: %v", name, err)
		}
	}
}
