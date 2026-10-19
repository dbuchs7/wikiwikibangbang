package item

import (
	"errors"
	"testing"
)

func TestBlockKindsOfADR0042(t *testing.T) {
	s := setup(t)
	page := mustItem(t)(s.Create("anna", NewItem{Space: "PLAT", Type: "page", Title: "Runbook"}))
	good := []Block{
		{Kind: TOC},
		{Kind: TaskItem, Text: "Back up", Checked: true},
		{Kind: TaskItem, Text: ""},
		{Kind: Panel, Tone: "info", Text: "Read me", Marks: []Mark{{Type: MarkItalic, Start: 0, End: 4}}},
		{Kind: Table, Text: "a\tb\n1\t2\n\t"},
		{Kind: Table, Text: "single"},
		{Kind: Children},
	}
	noErr(t, s.CommitBody("anna", page.ID, Doc{Blocks: good}))
	bad := map[string]Block{
		"panel without tone":     {Kind: Panel, Text: "x"},
		"panel with a bad tone":  {Kind: Panel, Tone: "pink", Text: "x"},
		"tone on a paragraph":    {Kind: Paragraph, Tone: "info", Text: "x"},
		"checked paragraph":      {Kind: Paragraph, Checked: true, Text: "x"},
		"empty table":            {Kind: Table},
		"ragged table":           {Kind: Table, Text: "a\tb\n1"},
		"marks in a table":       {Kind: Table, Text: "abc", Marks: []Mark{{Type: MarkBold, Start: 0, End: 1}}},
		"text in a toc":          {Kind: TOC, Text: "x"},
		"text in children":       {Kind: Children, Text: "x"},
		"reference on a task":    {Kind: TaskItem, Text: "x", Ref: page.ID},
		"level on a panel":       {Kind: Panel, Tone: "note", Text: "x", Level: 2},
		"mentions in a children": {Kind: Children, Mentions: []ID{page.ID}},
	}
	for name, b := range bad {
		if err := s.CommitBody("anna", page.ID, Doc{Blocks: []Block{b}}); !errors.Is(err, ErrValidation) {
			t.Errorf("%s: err = %v, want ErrValidation", name, err)
		}
	}
	// Table cells are text: search finds them.
	noErr(t, s.GrantSystemRole("root", "root", RoleAdmin))
	hits, err := s.As("root").Search("primary", Query{})
	noErr(t, err)
	if len(hits) != 0 {
		t.Fatalf("found %d hits for a word not in the body", len(hits))
	}
	noErr(t, s.CommitBody("anna", page.ID, Doc{Blocks: []Block{{Kind: Table, Text: "Host\tRole\ndb1\tprimary"}}}))
	if hits, err = s.As("root").Search("primary", Query{}); err != nil || len(hits) != 1 {
		t.Fatalf("table cell not found: %v, %v", hits, err)
	}
}
