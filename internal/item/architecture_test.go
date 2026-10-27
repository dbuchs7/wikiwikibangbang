package item

import (
	"errors"
	"math"
	"testing"
)

// TestArchitectureSketches: an architecture block names an engine, an
// application there, a release (0 for the newest with a sketch) and the
// layers it shows, in the engine's order, or none for the engine's
// default; and nothing else. No other block carries these fields
// (ADR-0145).
func TestArchitectureSketches(t *testing.T) {
	s := permFixture(t)
	anna := s.As("anna")
	page := mustItem(t)(anna.Create(NewItem{Space: "PLAT", Type: "page", Title: "Ordering", Body: &Doc{Blocks: []Block{
		{Kind: Architecture, Engine: "atlas-prod", Application: "c46e0d6f-1b2a", Text: "Bestellwesen"},
		{Kind: Architecture, Engine: "atlas-prod", Application: "c46e0d6f-1b2a", Release: 3, Layers: []string{"business", "application", "technology"}, Text: "As released"},
	}}}))
	got := mustItem(t)(s.Get(page.ID)).Body.Blocks
	if got[0].Kind != Architecture || got[0].Engine != "atlas-prod" || got[0].Application != "c46e0d6f-1b2a" || got[0].Release != 0 || got[0].Layers != nil ||
		got[1].Release != 3 || len(got[1].Layers) != 3 || got[1].Layers[0] != "business" || got[1].Text != "As released" {
		t.Fatalf("body %+v", got)
	}
	// A read hands out a copy: changing its layers changes nothing stored.
	got[1].Layers[0] = "strategy"
	if again := mustItem(t)(s.Get(page.ID)).Body.Blocks[1].Layers[0]; again != "business" {
		t.Fatalf("a read shares the stored layers: %q", again)
	}

	ok := Block{Kind: Architecture, Engine: "atlas", Application: "app"}
	beyond := int64(math.MaxInt32) + 1 // a variable, so that the test builds where int has 32 bits
	with := func(change func(*Block)) Block {
		b := ok
		change(&b)
		return b
	}
	for name, b := range map[string]Block{
		"without engine":             with(func(b *Block) { b.Engine = "" }),
		"engine in capitals":         with(func(b *Block) { b.Engine = "Atlas" }),
		"without application":        with(func(b *Block) { b.Application = "" }),
		"application with a space":   with(func(b *Block) { b.Application = " app" }),
		"application with a break":   with(func(b *Block) { b.Application = "a\nb" }),
		"a negative release":         with(func(b *Block) { b.Release = -1 }),
		"a release beyond int32":     with(func(b *Block) { b.Release = int(beyond) }),
		"an unknown layer":           with(func(b *Block) { b.Layers = []string{"physics"} }),
		"a layer twice":              with(func(b *Block) { b.Layers = []string{"data", "data"} }),
		"layers out of order":        with(func(b *Block) { b.Layers = []string{"technology", "application"} }),
		"layers in capitals":         with(func(b *Block) { b.Layers = []string{"Data"} }),
		"an empty list of layers":    with(func(b *Block) { b.Layers = []string{} }),
		"with a process":             with(func(b *Block) { b.Process = "p" }),
		"with a form":                with(func(b *Block) { b.Form = "f" }),
		"with a decision":            with(func(b *Block) { b.Decision = "d" }),
		"with a file":                with(func(b *Block) { b.Ref = fileA }),
		"with a mention":             with(func(b *Block) { b.Mentions = []ID{"x"} }),
		"with a formatted caption":   with(func(b *Block) { b.Text = "x"; b.Marks = []Mark{{Type: MarkBold, Start: 0, End: 1}} }),
		"aligned":                    with(func(b *Block) { b.Align = "center" }),
		"with a width":               with(func(b *Block) { b.Width = 50 }),
		"with a view":                with(func(b *Block) { b.View = ViewBoth }),
		"process with application":   {Kind: Process, Engine: "atlas", Process: "p", Application: "app"},
		"form with a release":        {Kind: Form, Engine: "atlas", Form: "f", Release: 2},
		"decision with layers":       {Kind: Decision, Engine: "atlas", Decision: "d", Layers: []string{"data"}},
		"paragraph with application": {Kind: Paragraph, Text: "x", Application: "app"},
		"paragraph with a release":   {Kind: Paragraph, Text: "x", Release: 1},
		"paragraph with layers":      {Kind: Paragraph, Text: "x", Layers: []string{"data"}},
		"image with a release":       {Kind: Image, Ref: fileA, Release: 1},
	} {
		if err := CheckBlock(b, "self"); !errors.Is(err, ErrValidation) {
			t.Errorf("%s: %v", name, err)
		}
	}
	for name, b := range map[string]Block{
		"the newest release":   ok,
		"a fixed release":      with(func(b *Block) { b.Release = 7 }),
		"the highest release":  with(func(b *Block) { b.Release = math.MaxInt32 }),
		"one layer":            with(func(b *Block) { b.Layers = []string{"data"} }),
		"every layer":          with(func(b *Block) { b.Layers = []string{"strategy", "business", "application", "data", "technology"} }),
		"an ID with umlauts":   with(func(b *Block) { b.Application = "Bestellwesen-ä" }),
		"a plain caption":      with(func(b *Block) { b.Text = "Ordering, as released" }),
		"an engine with a dot": with(func(b *Block) { b.Engine = "atlas.prod-1" }),
	} {
		if err := CheckBlock(b, "self"); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}
