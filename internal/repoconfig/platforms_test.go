package repoconfig

import (
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"
)

const releasePath = "../../.github/workflows/release.yml"

var (
	releaseTargetRE = regexp.MustCompile(`goos: (\w+), goarch: (\w+)`)
	crossBuildRE    = regexp.MustCompile(`for target in ([^;]+);`)
)

// TestCrossBuildCoversReleasePlatforms keeps the CI cross-build and the
// release matrix in step (ADR-0024): a platform that is released is built on
// every push, so a break shows up before a tag does.
func TestCrossBuildCoversReleasePlatforms(t *testing.T) {
	rel, err := os.ReadFile(releasePath)
	if err != nil {
		t.Fatal(err)
	}
	ci, err := os.ReadFile(workflowPath)
	if err != nil {
		t.Fatal(err)
	}

	var released []string
	for _, m := range releaseTargetRE.FindAllStringSubmatch(string(rel), -1) {
		released = append(released, m[1]+"/"+m[2])
	}
	if len(released) == 0 {
		t.Fatalf("no release targets found in %s", releasePath)
	}
	m := crossBuildRE.FindStringSubmatch(string(ci))
	if m == nil {
		t.Fatalf("no cross-build target list found in %s", workflowPath)
	}
	built := strings.Fields(m[1])

	slices.Sort(released)
	slices.Sort(built)
	if !slices.Equal(released, built) {
		t.Errorf("release targets %v differ from CI cross-build targets %v", released, built)
	}
}
