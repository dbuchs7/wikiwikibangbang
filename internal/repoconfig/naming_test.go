package repoconfig

import (
	"bufio"
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// otherProducts are the products the import engine reads from (ADR-0023).
var otherProducts = regexp.MustCompile(`(?i)confluence|jira`)

// namingAllowed reports whether a file or a line of it may name another
// product: only where the subject is import (AGENTS.md, "Other products are
// named only for import").
func namingAllowed(path, line string) bool {
	switch {
	// The import engine, the item model's import, the golden logs, which
	// record an imported item, and the import command.
	case strings.HasPrefix(path, "internal/migrate/"),
		path == "internal/item/import.go", path == "internal/item/import_test.go",
		strings.HasPrefix(path, "internal/item/testdata/"),
		path == "cmd/wikiwikibangbang/import.go", path == "cmd/wikiwikibangbang/import_test.go":
		return true
	// The import record itself, and ADRs accepted before the rule
	// (2026-10-19), which keep their wording.
	case strings.HasPrefix(path, "docs/adr/0023-"):
		return true
	case strings.HasPrefix(path, "docs/adr/00") && path < "docs/adr/0035":
		return true
	}
	// A line about import: it refers to the import record, or builds an
	// imported item's origin.
	return strings.Contains(line, "0023") || strings.Contains(line, "Origin{")
}

// binary files are not searched.
var binary = map[string]bool{".png": true, ".pdf": true, ".ico": true}

// TestOtherProductsAreNamedOnlyForImport enforces the maintainer's rule of
// 2026-10-19 on every text file of the repository.
func TestOtherProductsAreNamedOnlyForImport(t *testing.T) {
	root := "../.."
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" || d.Name() == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		if binary[filepath.Ext(p)] {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		if rel == "internal/repoconfig/naming_test.go" {
			return nil // this test names them to find them
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		sc := bufio.NewScanner(bytes.NewReader(data))
		sc.Buffer(nil, 1<<24)
		for n := 1; sc.Scan(); n++ {
			if line := sc.Text(); otherProducts.MatchString(line) && !namingAllowed(rel, line) {
				t.Errorf("%s:%d names another product outside import; describe the product in its own terms (AGENTS.md)", rel, n)
			}
		}
		return sc.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
}

// The check itself finds what it must and lets through what it may.
func TestNamingAllowed(t *testing.T) {
	for _, c := range []struct {
		path, line string
		want       bool
	}{
		{"internal/migrate/source.go", "a Jira reader", true},
		{"docs/adr/0001-unified-item-data-model.md", "like Confluence", true},
		{"docs/adr/0035-comments-are-part-of-their-item.md", "like Confluence", false},
		{"docs/adr/0040-something.md", "like Jira", false},
		{"AGENTS.md", "the import engine (ADR-0023)", true},
		{"AGENTS.md", "the Jira domain", false},
		{"internal/web/static/assets/app.js", `h("p", { text: "Like Jira" })`, false},
		{"README.md", "a Confluence alternative", false},
	} {
		if got := namingAllowed(c.path, c.line); got != c.want {
			t.Errorf("%s %q: allowed %v, want %v", c.path, c.line, got, c.want)
		}
	}
}
