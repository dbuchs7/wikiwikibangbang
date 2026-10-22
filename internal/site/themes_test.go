package site

import (
	"errors"
	"strings"
	"testing"
)

// Every theme has its stylesheet; those with a dark scheme keep the UI's
// dark scheme, the others turn it off (ADR-0085, ADR-0070).
func TestThemesApplyUnderTheDesign(t *testing.T) {
	light := map[string]bool{"night": true, "comic": true, "sketch": true}
	for _, id := range Themes {
		css := ThemeCSS(id)
		if strings.TrimSpace(css) == "" {
			t.Fatalf("%s has no stylesheet", id)
		}
		if ThemeDark(id) == light[id] {
			t.Errorf("%s: dark scheme %v", id, ThemeDark(id))
		}
	}
	if ThemeCSS("nothing") != "" || ThemeCSS("") != "" || !ThemeDark("") {
		t.Fatal("no theme is the product's look")
	}

	s, _ := New(nil, func([]byte) error { return nil })
	if err := s.SetDesign("admin", Design{Theme: "neon"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("an unknown theme: %v", err)
	}
	if err := s.SetDesign("admin", Design{Theme: "comic", Accent: "#0b2f6b"}); err != nil {
		t.Fatal(err)
	}
	css := s.Stylesheet()
	theme, accent := strings.Index(css, "/* The theme comic"), strings.Index(css, "--accent: #0b2f6b")
	if s.Design().DarkMode() || theme < 0 || accent < theme || !strings.Contains(css, ":root { color-scheme: light; }") {
		t.Fatalf("the theme first, the design's colours over it, light only:\n%s", css[:min(len(css), 400)])
	}
	if err := s.SetDesign("admin", Design{Theme: "ocean"}); err != nil {
		t.Fatal(err)
	}
	if !s.Design().DarkMode() || strings.Contains(s.Stylesheet(), "color-scheme: light;") {
		t.Fatal("a theme with a dark scheme keeps it")
	}
}

// A kanban's picture is an asset of its own, listed with its space so it
// goes with it (ADR-0087).
func TestKanbanIconsAreAssetsOfTheirSpace(t *testing.T) {
	for name, want := range map[string]bool{
		KanbanIcon("PLAT", "0a1b"): true, "kanban-PLAT-": false, "kanban-plat-1": false, "kanban-PLAT-a/b": false, "kanban-PLAT": false, "space-PLAT": false,
	} {
		if IsKanbanIcon(name) != want {
			t.Errorf("%s: %v", name, !want)
		}
	}
	s, _ := New(nil, func([]byte) error { return nil })
	for _, name := range []string{KanbanIcon("PLAT", "a1"), KanbanIcon("PLAT", "b2"), KanbanIcon("OPS", "c3")} {
		if err := s.SetAsset("admin", name, "image/png", []byte("x")); err != nil {
			t.Fatal(err)
		}
	}
	if got := s.KanbanIcons("PLAT"); len(got) != 2 {
		t.Fatalf("PLAT's pictures: %v", got)
	}
}
