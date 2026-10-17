package repoconfig

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestInvariantChecksExist: every test docs/architecture/invariants.md
// names as a check exists, so a renamed or deleted test cannot leave an
// invariant silently unchecked.
func TestInvariantChecksExist(t *testing.T) {
	doc, err := os.ReadFile("../../docs/architecture/invariants.md")
	if err != nil {
		t.Fatal(err)
	}
	names := regexp.MustCompile("`(Test[A-Za-z0-9_]+)`").FindAllStringSubmatch(string(doc), -1)
	if len(names) == 0 {
		t.Fatal("no checks named")
	}
	var src strings.Builder
	err = filepath.WalkDir("../..", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (d.Name() == "node_modules" || strings.HasPrefix(d.Name(), ".")) && p != "../.." {
			return filepath.SkipDir
		}
		if strings.HasSuffix(p, "_test.go") {
			b, err := os.ReadFile(p)
			src.Write(b)
			return err
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range names {
		if !strings.Contains(src.String(), "func "+n[1]+"(") {
			t.Errorf("invariants.md names %s, which does not exist", n[1])
		}
	}
}
