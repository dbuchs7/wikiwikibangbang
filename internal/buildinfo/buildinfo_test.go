package buildinfo

import (
	"runtime"
	"runtime/debug"
	"strings"
	"testing"
)

func TestStringNamesVersionRevisionAndPlatform(t *testing.T) {
	in := from(&debug.BuildInfo{Settings: []debug.BuildSetting{
		{Key: "vcs.revision", Value: "0123456789abcdef0123"},
		{Key: "vcs.modified", Value: "true"},
	}})
	got := in.String()
	for _, want := range []string{"wikiwikibangbang dev", "(0123456789ab+dirty)", runtime.Version(), runtime.GOOS + "/" + runtime.GOARCH} {
		if !strings.Contains(got, want) {
			t.Errorf("String() = %q, want it to contain %q", got, want)
		}
	}
}

func TestStringWithoutRevision(t *testing.T) {
	got := from(nil).String()
	if strings.Contains(got, "(") {
		t.Errorf("String() = %q, want no revision part", got)
	}
}

func TestStampedVersionIsReported(t *testing.T) {
	old := Version
	t.Cleanup(func() { Version = old })
	Version = "1.2.3"
	if got := Get().Version; got != "1.2.3" {
		t.Errorf("Version = %q, want 1.2.3", got)
	}
}
