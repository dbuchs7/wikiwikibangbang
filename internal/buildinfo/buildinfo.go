// Package buildinfo reports which build of the binary is running (ADR-0024).
package buildinfo

import (
	"fmt"
	"runtime"
	"runtime/debug"
)

// Version is the release version, stamped at build time:
//
//	go build -ldflags "-X github.com/dbuchs7/wikiwikibangbang/internal/buildinfo.Version=1.2.3"
//
// An unstamped build reports "dev".
var Version = "dev"

// Info describes the running binary.
type Info struct {
	Version  string
	Revision string // VCS revision, empty when the build carries none
	Modified bool   // the working tree had uncommitted changes
	Go       string
	OS       string
	Arch     string
}

// Get returns the build information of the running binary.
func Get() Info {
	bi, _ := debug.ReadBuildInfo()
	return from(bi)
}

func from(bi *debug.BuildInfo) Info {
	in := Info{Version: Version, Go: runtime.Version(), OS: runtime.GOOS, Arch: runtime.GOARCH}
	if bi == nil {
		return in
	}
	for _, s := range bi.Settings {
		switch s.Key {
		case "vcs.revision":
			in.Revision = s.Value
		case "vcs.modified":
			in.Modified = s.Value == "true"
		}
	}
	return in
}

// String renders the information on one line, e.g.
// "wikiwikibangbang 1.2.3 (abc123def456) go1.26.0 linux/amd64".
func (in Info) String() string {
	rev := ""
	if in.Revision != "" {
		r := in.Revision
		if len(r) > 12 {
			r = r[:12]
		}
		if in.Modified {
			r += "+dirty"
		}
		rev = " (" + r + ")"
	}
	return fmt.Sprintf("wikiwikibangbang %s%s %s %s/%s", in.Version, rev, in.Go, in.OS, in.Arch)
}
