package repoconfig

import (
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
