// Package diskspace measures the file system a data directory lives on and
// tells how close it is to full (ADR-0150).
package diskspace

import (
	"errors"
	"fmt"
)

// Usage is the space of a file system: Free is what an unprivileged
// process may still write, Total its size.
type Usage struct {
	Free  uint64 `json:"free"`
	Total uint64 `json:"total"`
}

// ErrUnsupported is returned where the platform cannot be asked.
var ErrUnsupported = errors.New("free space cannot be measured on this platform")

// Of measures the file system that holds path.
func Of(path string) (Usage, error) {
	u, err := of(path)
	if err != nil {
		return Usage{}, fmt.Errorf("free space of %s: %w", path, err)
	}
	return u, nil
}

// Level says how close a file system is to full.
type Level int

const (
	// OK leaves room above both thresholds.
	OK Level = iota
	// Warning is below the warning threshold: administrators are told.
	Warning
	// Critical is below the critical threshold: uploads are refused.
	Critical
	// Full is below the floor: the wiki only reads, apart from what frees
	// space.
	Full
)

// Floor is the free space below which the wiki stops writing: enough for
// the log to record the commands that free space, and for the operating
// system.
const Floor = 200 << 20

// String names a level as the API and the metrics do.
func (l Level) String() string {
	switch l {
	case Warning:
		return "warning"
	case Critical:
		return "critical"
	case Full:
		return "full"
	}
	return "ok"
}

// Thresholds are the free space below which a file system is in warning or
// critical: a share of its size in percent or a number of megabytes,
// whichever is reached first.
type Thresholds struct {
	WarnPercent     int `json:"warnPercent"`
	WarnMB          int `json:"warnMB"`
	CriticalPercent int `json:"criticalPercent"`
	CriticalMB      int `json:"criticalMB"`
}

// Defaults are the thresholds until an administrator sets others: warning
// below 15 % or 5 GB free, critical below 5 % or 1 GB.
var Defaults = Thresholds{WarnPercent: 15, WarnMB: 5 << 10, CriticalPercent: 5, CriticalMB: 1 << 10}

// Validate checks that percentages lie between 1 and 50, megabytes between
// 0 and 1 TB, and that critical is no higher than warning.
func (t Thresholds) Validate() error {
	for _, p := range []int{t.WarnPercent, t.CriticalPercent} {
		if p < 1 || p > 50 {
			return fmt.Errorf("a share of %d %% is not between 1 and 50", p)
		}
	}
	for _, mb := range []int{t.WarnMB, t.CriticalMB} {
		if mb < 0 || mb > 1<<20 {
			return fmt.Errorf("%d MB is not between 0 and 1048576", mb)
		}
	}
	if t.CriticalPercent > t.WarnPercent || t.CriticalMB > t.WarnMB {
		return errors.New("the critical threshold is above the warning threshold")
	}
	return nil
}

// LevelOf is the level of a file system with u under t.
func LevelOf(u Usage, t Thresholds) Level {
	below := func(percent, mb int) bool {
		return u.Free < uint64(mb)<<20 || (u.Total > 0 && u.Free*100 < u.Total*uint64(percent))
	}
	switch {
	case u.Free < Floor:
		return Full
	case below(t.CriticalPercent, t.CriticalMB):
		return Critical
	case below(t.WarnPercent, t.WarnMB):
		return Warning
	}
	return OK
}
