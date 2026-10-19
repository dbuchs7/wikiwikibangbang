// Package budget is the one place that names every resource budget of the
// server, and the one way to change them (ADR-0015).
//
// A budget bounds how much a caller can make the server hold or do at once:
// the size of a request body, the items of one page, the live editing
// sessions. Ceilings that are not budgets stay where they are, each for a
// reason: a security policy (failed logins before a throttle, the lifetime
// of an API token) is not an operator's memory setting; a limit that is
// part of a file format (the largest record of the event log, the work
// factor a backup header may demand) must not change between the server
// that wrote a file and the one that reads it. The test in this package
// fails when a new input ceiling appears without being one or the other.
//
// [FromEnv] reads one variable per field, WWBB_BUDGET_ plus the field name
// in upper snake case. A variable that is unset or empty leaves the
// default; one that is not a positive number in range is reported and
// leaves the default too, so a typo cannot remove a ceiling.
package budget

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"sync/atomic"
)

// Budgets is a complete set of resource budgets. Every field is a ceiling;
// build one with [Default] or [FromEnv].
type Budgets struct {
	// RequestBody is the largest JSON request body the API reads, in bytes.
	RequestBody int64
	// PageItems is the largest page a listing returns.
	PageItems int
	// SearchQueryBytes and SearchQueryTerms bound one full-text search,
	// which scans the live state (ADR-0033).
	SearchQueryBytes int
	SearchQueryTerms int
	// LiveSessions, LiveClients and LiveBlocks bound live editing
	// (ADR-0010): sessions on the server, participants per session, and
	// blocks one session may hold.
	LiveSessions int
	LiveClients  int
	LiveBlocks   int
	// LoginTracked bounds the addresses and usernames the login throttle
	// remembers, so failed logins cannot exhaust memory.
	LoginTracked int
	// SSOPending bounds the single sign-on logins begun and not finished.
	SSOPending int
}

// Default returns the budgets the server uses unless configured otherwise.
func Default() Budgets {
	return Budgets{
		RequestBody:      64 << 10,
		PageItems:        500,
		SearchQueryBytes: 256,
		SearchQueryTerms: 10,
		LiveSessions:     1000,
		LiveClients:      50,
		LiveBlocks:       5000,
		LoginTracked:     100_000,
		SSOPending:       10_000,
	}
}

// EnvPrefix starts the name of every budget's environment variable.
const EnvPrefix = "WWBB_BUDGET_"

// Names returns the field names, in declaration order.
func Names() []string {
	t := reflect.TypeOf(Budgets{})
	out := make([]string, 0, t.NumField())
	for i := range t.NumField() {
		out = append(out, t.Field(i).Name)
	}
	return out
}

// EnvVar returns the environment variable that sets a field.
func EnvVar(field string) string { return EnvPrefix + upperSnake(field) }

// FromEnv returns the defaults with every valid variable applied, and one
// message per variable that was set but not used.
func FromEnv(getenv func(string) string) (Budgets, []string) {
	b := Default()
	v := reflect.ValueOf(&b).Elem()
	var problems []string
	for i := range v.NumField() {
		key := EnvVar(v.Type().Field(i).Name)
		raw := strings.TrimSpace(getenv(key))
		if raw == "" {
			continue
		}
		n, err := strconv.ParseInt(raw, 10, 64)
		switch {
		case err != nil || n <= 0:
			problems = append(problems, fmt.Sprintf("%s=%q is not a positive number; keeping the default", key, raw))
		case v.Field(i).OverflowInt(n):
			problems = append(problems, fmt.Sprintf("%s=%q is too large; keeping the default", key, raw))
		default:
			v.Field(i).SetInt(n)
		}
	}
	return b, problems
}

// Changed returns "VARIABLE=value" for every budget that differs from its
// default, for the startup log.
func (b Budgets) Changed() []string {
	got, def := reflect.ValueOf(b), reflect.ValueOf(Default())
	var out []string
	for i := range got.NumField() {
		if got.Field(i).Int() != def.Field(i).Int() {
			out = append(out, fmt.Sprintf("%s=%d", EnvVar(got.Type().Field(i).Name), got.Field(i).Int()))
		}
	}
	return out
}

var current atomic.Pointer[Budgets]

// Get returns the budgets in force: the ones last passed to [Set], or the
// defaults.
func Get() Budgets {
	if b := current.Load(); b != nil {
		return *b
	}
	return Default()
}

// Set puts budgets in force for the whole process. The server calls it once
// at startup, before it serves; tests call it to try a smaller budget.
func Set(b Budgets) { current.Store(&b) }

// upperSnake turns a field name into upper snake case. A word starts at an
// upper-case letter after a lower-case one ("PageItems"), and at the last
// letter of an abbreviation followed by a lower-case one ("SSOPending").
func upperSnake(field string) string {
	upper := func(i int) bool { return i >= 0 && i < len(field) && field[i] >= 'A' && field[i] <= 'Z' }
	var b strings.Builder
	for i := range len(field) {
		if i > 0 && upper(i) && (!upper(i-1) || (i+1 < len(field) && !upper(i+1))) {
			b.WriteByte('_')
		}
		b.WriteByte(field[i])
	}
	return strings.ToUpper(b.String())
}
