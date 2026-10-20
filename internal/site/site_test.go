package site

import (
	"errors"
	"strings"
	"testing"
)

func TestTheDesignIsKeptAndReplayed(t *testing.T) {
	var records [][]byte
	j := func(r []byte) error { records = append(records, r); return nil }
	s, err := New(nil, j)
	if err != nil {
		t.Fatal(err)
	}
	if css := s.Stylesheet(); strings.Contains(css, ":root") {
		t.Fatalf("an empty design changes the look: %s", css)
	}
	d := Design{Name: "ACME", Accent: "#d40000", Background: "#fafafa", HeaderBackground: "#202020", HeaderText: "#ffffff",
		Font: "upload", FontFamily: "Frutiger", CSS: ".brand { letter-spacing: 1px; }"}
	if err := s.SetDesign("admin", d); err != nil {
		t.Fatal(err)
	}
	if err := s.SetAsset("admin", AssetFont, "font/woff2", []byte("wOF2 font")); err != nil {
		t.Fatal(err)
	}
	if err := s.SetAsset("admin", AssetLogo, "image/png", []byte("\x89PNG logo")); err != nil {
		t.Fatal(err)
	}
	if err := s.RemoveAsset("admin", AssetLogo); err != nil {
		t.Fatal(err)
	}
	again, err := New(records, j)
	if err != nil {
		t.Fatal(err)
	}
	if again.Design() != d {
		t.Fatalf("after a restart: %+v", again.Design())
	}
	if _, ok := again.Asset(AssetLogo); ok {
		t.Fatal("a removed logo came back")
	}
	css := again.Stylesheet()
	for _, want := range []string{"--accent: #d40000", `"Frutiger", "Corporate"`, "@font-face", ".top { background: #202020; }", "letter-spacing: 1px"} {
		if !strings.Contains(css, want) {
			t.Errorf("the stylesheet lacks %q:\n%s", want, css)
		}
	}
}

// TestAnOwnStylesheetWithoutADarkSchemeTurnsItOff: an own stylesheet that
// does not say how it looks in the dark turns the dark scheme off; then the
// light rules apply always (ADR-0070).
func TestAnOwnStylesheetWithoutADarkSchemeTurnsItOff(t *testing.T) {
	s, _ := New(nil, func([]byte) error { return nil })
	if !s.Design().DarkMode() {
		t.Fatal("no own stylesheet, yet the dark scheme is off")
	}
	d := Design{Accent: "#0b2f6b", Background: "#fafafa", CSS: "body { background: white; }"}
	if err := s.SetDesign("admin", d); err != nil {
		t.Fatal(err)
	}
	css := s.Stylesheet()
	if s.Design().DarkMode() || !strings.Contains(css, ":root { color-scheme: light; }") || strings.Contains(css, "prefers-color-scheme") {
		t.Fatalf("light only expected:\n%s", css)
	}
	if !strings.Contains(css, "\n:root { --bg: #fafafa; }") || !strings.Contains(css, "--accent-soft: color-mix(in srgb, #0b2f6b 12%, white)") {
		t.Fatalf("the light rules must apply always:\n%s", css)
	}
	d.CSS += "\n@media (prefers-color-scheme:dark) { body { background: black; } }"
	if err := s.SetDesign("admin", d); err != nil {
		t.Fatal(err)
	}
	css = s.Stylesheet()
	if !s.Design().DarkMode() || strings.Contains(css, "color-scheme: light;") || !strings.Contains(css, "@media (prefers-color-scheme: light) {\n  :root { --bg: #fafafa; }") ||
		!strings.Contains(css, "@media (prefers-color-scheme: dark) { :root { --accent:") {
		t.Fatalf("with a dark scheme of its own:\n%s", css)
	}
}

func TestTheDesignIsValidated(t *testing.T) {
	s, _ := New(nil, func([]byte) error { return nil })
	for name, d := range map[string]Design{
		"a colour that is no colour": {Accent: "red; } body { display:none"},
		"an unknown font":            {Font: "comic"},
		"a font name with quotes":    {FontFamily: `x"; } *{`},
		"a name over two lines":      {Name: "a\nb"},
	} {
		if err := s.SetDesign("admin", d); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if err := s.SetAsset("admin", "favicon", "image/png", []byte("x")); !errors.Is(err, ErrInvalid) {
		t.Errorf("an unknown asset: %v", err)
	}
}

// TestTheAccentGetsReadableText: the text on an accent fill contrasts with
// it in both schemes (ADR-0052): white on dark accents, near black on light.
func TestTheAccentGetsReadableText(t *testing.T) {
	for accent, want := range map[string]string{
		"#d40000": "#ffffff", "#0b5cff": "#ffffff", "#003366": "#ffffff", "#000000": "#ffffff",
		"#ffcc00": "#0b0d12", "#5b95ff": "#0b0d12", "#ffffff": "#0b0d12", "#7fdbff": "#0b0d12",
	} {
		if got := AccentInk(accent); got != want {
			t.Errorf("AccentInk(%s) = %s, want %s", accent, got, want)
		}
	}
	s, err := New(nil, func([]byte) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	for accent, want := range map[string][]string{
		"#d40000": {"--accent-ink: #ffffff;", "82%, black)"},
		"#ffcc00": {"--accent-ink: #0b0d12;", "82%, white)"},
	} {
		if err := s.SetDesign("admin", Design{Accent: accent}); err != nil {
			t.Fatal(err)
		}
		css := s.Stylesheet()
		root := css[strings.Index(css, ":root {"):]
		root = root[:strings.Index(root, "}")]
		for _, w := range want {
			if !strings.Contains(root, w) {
				t.Errorf("%s: the rule outside the schemes lacks %q:\n%s", accent, w, css)
			}
		}
	}
}

// TestContrastWarnsAboutHardToReadPairs: the check names the pairs below
// WCAG AA and nothing else (ADR-0052).
func TestContrastWarnsAboutHardToReadPairs(t *testing.T) {
	if r := ContrastRatio("#000000", "#ffffff"); r < 20.99 || r > 21.01 {
		t.Errorf("black on white: %v", r)
	}
	pairs := func(d Design) (out []string) {
		for _, f := range d.Contrast() {
			if f.Ratio >= MinContrast {
				t.Errorf("%s reported at %v", f.Pair, f.Ratio)
			}
			out = append(out, f.Pair)
		}
		return out
	}
	if got := pairs(Design{}); len(got) != 0 {
		t.Errorf("the product's colours: %v", got)
	}
	// A dark red reads on white; the dark scheme gets it lightened.
	if got := pairs(Design{Accent: "#d40000"}); len(got) != 0 {
		t.Errorf("dark red: %v", got)
	}
	if dark := DarkAccent("#d40000"); dark == "#d40000" || ContrastRatio(dark, productDarkSurface) < MinContrast {
		t.Errorf("dark-scheme red %s", dark)
	}
	if dark := DarkAccent("#5b95ff"); dark != "#5b95ff" {
		t.Errorf("the product's dark-scheme blue reads already, got %s", dark)
	}
	// Yellow carries dark text, but is unreadable as link text on white.
	if got := pairs(Design{Accent: "#ffcc00"}); strings.Join(got, ",") != "accentOnSurface" {
		t.Errorf("yellow: %v", got)
	}
	got := pairs(Design{HeaderBackground: "#336699", Background: "#222222"})
	if strings.Join(got, ",") != "headerText,textOnBackground" {
		t.Errorf("dark header and page: %v", got)
	}
	if got := pairs(Design{HeaderBackground: "#336699", HeaderText: "#ffffff", Accent: "#0b5cff", Background: "#f7f3ea"}); len(got) != 0 {
		t.Errorf("a readable design: %v", got)
	}
}

// TestRegulationsAreKeptAndAssess: regulations replay, only active ones
// assess, indicators match at the start of words, and the templates are
// valid (ADR-0058).
func TestRegulationsAreKeptAndAssess(t *testing.T) {
	for _, tpl := range Templates {
		if err := tpl.Validate(); err != nil {
			t.Errorf("template %s: %v", tpl.ID, err)
		}
	}
	var records [][]byte
	j := func(r []byte) error { records = append(records, r); return nil }
	s, err := New(nil, j)
	if err != nil {
		t.Fatal(err)
	}
	dsg := Templates[0]
	dsg.Active = true
	if err := s.SetRegulation("admin", dsg); err != nil {
		t.Fatal(err)
	}
	inactive := Templates[1]
	if err := s.SetRegulation("admin", inactive); err != nil {
		t.Fatal(err)
	}
	got := s.Assess([]string{"Die Diagnosen liegen vor.", "Eine Prognose-Diagnose"})
	if len(got) != 1 || got[0].Regulation != "dsg" || got[0].Indicator != "Diagnose" {
		t.Fatalf("assess: %+v", got)
	}
	if got := s.Assess([]string{"Agnostiker"}); len(got) != 0 {
		t.Errorf("a word inside another word: %+v", got)
	}
	if err := s.SetRegulation("admin", Regulation{ID: "x", Name: "X", Categories: []Category{{Name: "c", Indicators: []string{"re:("}}}}); err == nil {
		t.Error("a broken pattern is accepted")
	}
	if err := s.SetRegulation("admin", Regulation{ID: "Bad ID", Name: "X", Categories: []Category{{Name: "c"}}}); err == nil {
		t.Error("a bad ID is accepted")
	}
	if err := s.SetAsset("admin", DocumentAsset("dsg"), "application/pdf", []byte("%PDF")); err != nil {
		t.Fatal(err)
	}
	if err := s.SetAsset("admin", DocumentAsset("nope"), "application/pdf", []byte("%PDF")); err == nil {
		t.Error("a document of no regulation is accepted")
	}
	again, err := New(records, j)
	if err != nil {
		t.Fatal(err)
	}
	if len(again.Regulations()) != 2 || len(again.Assess([]string{"Diagnose"})) != 1 {
		t.Fatal("regulations are not replayed")
	}
	if err := again.RemoveRegulation("admin", "dsg"); err != nil {
		t.Fatal(err)
	}
	if _, ok := again.Asset(DocumentAsset("dsg")); ok || len(again.Assess([]string{"Diagnose"})) != 0 {
		t.Error("removing a regulation keeps its document or its indicators")
	}
}
