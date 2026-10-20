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
	for _, want := range []string{"--accent: #d40000", `"Frutiger", "Corporate"`, "@font-face", ".top { background: #202020; }", "letter-spacing: 1px", "prefers-color-scheme: light"} {
		if !strings.Contains(css, want) {
			t.Errorf("the stylesheet lacks %q:\n%s", want, css)
		}
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
