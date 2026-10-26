package repoconfig

import (
	"encoding/json"
	"os"
	"regexp"
	"strings"
	"testing"
)

var requireRE = regexp.MustCompile(`(?m)^\s*(?:require\s+)?([a-z0-9.\-]+\.[a-z]+/[^\s]+)\s+(v[^\s]+)`)

// TestEveryDependencyIsNoticed: a module that ends up in the binary is
// named, with its version and license, in THIRD_PARTY_NOTICES.md, which the
// release archives ship (AGENTS.md, licenses).
func TestEveryDependencyIsNoticed(t *testing.T) {
	mod, err := os.ReadFile("../../go.mod")
	if err != nil {
		t.Fatal(err)
	}
	notices, err := os.ReadFile("../../THIRD_PARTY_NOTICES.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range requireRE.FindAllStringSubmatch(string(mod), -1) {
		row := "| " + m[1] + " | " + m[2] + " |"
		if !strings.Contains(string(notices), row) {
			t.Errorf("THIRD_PARTY_NOTICES.md has no row for %s %s", m[1], m[2])
		}
	}
}

// TestEveryVendoredPackageIsNoticed: the packages the UI's vendored
// bundles take code from, which their license files list (build.sh reads
// them from esbuild's account of each bundle), are named in
// THIRD_PARTY_NOTICES.md with the version package-lock.json pins and their
// license. Packages installed but not bundled, such as what the drawing
// editor leaves out (ADR-0126), are not shipped and need no row.
func TestEveryVendoredPackageIsNoticed(t *testing.T) {
	lock, err := os.ReadFile("../../internal/web/vendor/package-lock.json")
	if err != nil {
		t.Fatal(err)
	}
	var l struct {
		Packages map[string]struct {
			Version string `json:"version"`
		} `json:"packages"`
	}
	if err := json.Unmarshal(lock, &l); err != nil {
		t.Fatal(err)
	}
	notices, err := os.ReadFile("../../THIRD_PARTY_NOTICES.md")
	if err != nil {
		t.Fatal(err)
	}
	line := regexp.MustCompile(`^(\S+) (\S+) (.+)$`)
	for _, bundle := range []string{"prosemirror", "excalidraw", "bpmn"} {
		data, err := os.ReadFile("../../internal/web/static/vendor/" + bundle + ".LICENSE.txt")
		if err != nil {
			t.Fatal(err)
		}
		// The list is the block of "name version license" lines after the
		// header paragraph.
		blocks := strings.Split(string(data), "\n\n")
		n := 0
		for _, text := range strings.Split(blocks[min(1, len(blocks)-1)], "\n") {
			m := line.FindStringSubmatch(text)
			if m == nil {
				t.Errorf("%s.LICENSE.txt: %q is not \"name version license\"", bundle, text)
				continue
			}
			n++
			// A version may be installed below another package
			// (node_modules/a/node_modules/b), when the one at the top differs.
			pinned := false
			for path, p := range l.Packages {
				if (path == "node_modules/"+m[1] || strings.HasSuffix(path, "/node_modules/"+m[1])) && p.Version == m[2] {
					pinned = true
					break
				}
			}
			if !pinned {
				t.Errorf("%s.LICENSE.txt names %s %s, which package-lock.json does not pin", bundle, m[1], m[2])
			}
			if row := "| " + m[1] + " | " + m[2] + " | " + m[3] + " |"; !strings.Contains(string(notices), row) {
				t.Errorf("THIRD_PARTY_NOTICES.md has no row %q", row)
			}
		}
		if n == 0 {
			t.Errorf("%s.LICENSE.txt lists no packages", bundle)
		}
	}
}
