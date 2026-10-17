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

// TestEveryVendoredPackageIsNoticed: the packages bundled into the UI's
// vendored editor (internal/web/vendor/package-lock.json, without build
// tools) are named in THIRD_PARTY_NOTICES.md with version and license.
func TestEveryVendoredPackageIsNoticed(t *testing.T) {
	lock, err := os.ReadFile("../../internal/web/vendor/package-lock.json")
	if err != nil {
		t.Fatal(err)
	}
	var l struct {
		Packages map[string]struct {
			Version string `json:"version"`
			License string `json:"license"`
			Dev     bool   `json:"dev"`
		} `json:"packages"`
	}
	if err := json.Unmarshal(lock, &l); err != nil {
		t.Fatal(err)
	}
	notices, err := os.ReadFile("../../THIRD_PARTY_NOTICES.md")
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for path, p := range l.Packages {
		if path == "" || p.Dev {
			continue
		}
		n++
		row := "| " + strings.TrimPrefix(path, "node_modules/") + " | " + p.Version + " | " + p.License + " |"
		if !strings.Contains(string(notices), row) {
			t.Errorf("THIRD_PARTY_NOTICES.md has no row %q", row)
		}
	}
	if n == 0 {
		t.Fatal("no vendored packages found")
	}
}
