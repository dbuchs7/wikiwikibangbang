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
