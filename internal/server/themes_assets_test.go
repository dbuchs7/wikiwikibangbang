package server

import (
	"io/fs"
	"regexp"
	"strings"
	"testing"

	"github.com/dbuchs7/wikiwikibangbang/internal/site"
	"github.com/dbuchs7/wikiwikibangbang/internal/web"
)

// TestThemeFontsShip: every file a ready-made theme loads from the UI's
// assets, such as the fonts of the comic and the Swiss theme, is served by
// it, so no theme reaches outside the installation (ADR-0085, ADR-0120).
func TestThemeFontsShip(t *testing.T) {
	urls := regexp.MustCompile(`url\("?(/[^")]+)"?\)`)
	found := 0
	for _, id := range site.Themes {
		for _, m := range urls.FindAllStringSubmatch(site.ThemeCSS(id), -1) {
			found++
			if _, err := fs.Stat(web.FS(), strings.TrimPrefix(m[1], "/")); err != nil {
				t.Errorf("theme %s loads %s, which the UI does not serve: %v", id, m[1], err)
			}
		}
		if strings.Contains(site.ThemeCSS(id), "https://") {
			t.Errorf("theme %s loads something from outside the installation", id)
		}
	}
	if found == 0 {
		t.Fatal("no theme loads a file; the test checks nothing")
	}
}
