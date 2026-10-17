package web

import (
	"io/fs"
	"regexp"
	"strings"
	"testing"
)

func TestEmbeddedUIHasEntryPointAndAssets(t *testing.T) {
	for _, name := range []string{"index.html", "assets/app.js", "assets/api.js", "assets/dom.js", "assets/style.css"} {
		if _, err := fs.Stat(FS(), name); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// TestNoMarkupFromStrings enforces ADR-0014 on the UI's source: elements are
// created by dom.js, text is inserted as text, and nothing turns a string
// into markup or code. The content-security policy would block inline code
// as well; this keeps the first line of defence from eroding.
func TestNoMarkupFromStrings(t *testing.T) {
	forbidden := regexp.MustCompile(`innerHTML|outerHTML|insertAdjacentHTML|document\.write|\beval\(|new Function|setAttribute\(\s*["']on|\bon[a-z]+=["']`)
	err := fs.WalkDir(FS(), ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !(strings.HasSuffix(path, ".js") || strings.HasSuffix(path, ".html")) {
			return err
		}
		data, err := fs.ReadFile(FS(), path)
		if err != nil {
			return err
		}
		for i, line := range strings.Split(string(data), "\n") {
			// Comments may name the forbidden APIs to forbid them.
			if trimmed := strings.TrimSpace(line); strings.HasPrefix(trimmed, "//") {
				continue
			}
			if m := forbidden.FindString(line); m != "" {
				t.Errorf("%s:%d uses %q", path, i+1, m)
			}
		}
		if strings.HasSuffix(path, ".html") && regexp.MustCompile(`<script(?:\s[^>]*)?>[^<]`).Match(data) {
			t.Errorf("%s has an inline script; the content-security policy forbids it", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
