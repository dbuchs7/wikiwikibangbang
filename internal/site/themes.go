package site

import (
	"embed"
	"slices"
	"strings"
)

// themeFiles are the ready-made themes (ADR-0085): one stylesheet each,
// applied under the design's own colours, font and stylesheet.
//
//go:embed themes/*.css
var themeFiles embed.FS

// Themes are the IDs of the ready-made themes, in the order offered.
var Themes = []string{"ocean", "forest", "sunset", "berry", "swiss", "night", "comic", "sketch"}

// ThemeCSS returns a theme's stylesheet, or "" for none or an unknown one.
func ThemeCSS(id string) string {
	if !slices.Contains(Themes, id) {
		return ""
	}
	data, err := themeFiles.ReadFile("themes/" + id + ".css")
	if err != nil {
		return ""
	}
	return string(data)
}

// ThemeDark reports whether a theme has a dark scheme: a theme without
// one keeps its look whatever the reader's system prefers (ADR-0070).
func ThemeDark(id string) bool {
	css := ThemeCSS(id)
	return strings.TrimSpace(css) == "" || darkRules.MatchString(css)
}
