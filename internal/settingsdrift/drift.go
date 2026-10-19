// Package settingsdrift compares the repository settings recorded in the
// repository with the ones GitHub reports (ADR-0003).
//
// The recorded files state only what the project decides. GitHub answers
// with more: ids, timestamps, links and parameters it fills in with defaults.
// So the comparison is one-sided: every value in the file must be in the
// response with the same value, and keys the file does not set are ignored.
// Two kinds of list are compared by identity instead of position, because
// GitHub does not keep their order: rules, matched by "type", and required
// status checks, matched by "context". Within such a list an element on
// either side without a partner is drift — a rule added on GitHub weakens or
// tightens the protection just as much as one removed.
package settingsdrift

import (
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"sort"
)

// Kind classifies a finding.
type Kind int

const (
	// Drift means GitHub differs from the file.
	Drift Kind = iota
	// Unverifiable means the response lacks a value the file sets, usually
	// because the token may not read it. The check cannot confirm it, but
	// it is not evidence of a change either.
	Unverifiable
)

// Finding is one difference between a file and GitHub.
type Finding struct {
	Kind Kind
	// Path names the value, e.g. "ruleset.rules[type=deletion]".
	Path string
	// Detail says what differs.
	Detail string
}

func (f Finding) String() string {
	kind := "drift"
	if f.Kind == Unverifiable {
		kind = "unverifiable"
	}
	return fmt.Sprintf("%s: %s: %s", kind, f.Path, f.Detail)
}

// HasDrift reports whether any finding is drift.
func HasDrift(fs []Finding) bool {
	return slices.ContainsFunc(fs, func(f Finding) bool { return f.Kind == Drift })
}

// Compare returns the findings for want, as recorded in a file, against
// live, as GitHub reports it. Both are decoded JSON objects; path prefixes
// every finding.
func Compare(path string, want, live map[string]any) []Finding {
	var fs []Finding
	compareObjects(path, want, live, &fs)
	return fs
}

func compareObjects(path string, want, live map[string]any, fs *[]Finding) {
	keys := make([]string, 0, len(want))
	for k := range want {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		p := path + "." + k
		got, ok := live[k]
		if !ok {
			*fs = append(*fs, Finding{Unverifiable, p, "not in the response (want " + render(want[k]) + ")"})
			continue
		}
		compareValues(p, want[k], got, fs)
	}
}

func compareValues(path string, want, got any, fs *[]Finding) {
	switch w := want.(type) {
	case map[string]any:
		g, ok := got.(map[string]any)
		if !ok {
			*fs = append(*fs, mismatch(path, want, got))
			return
		}
		compareObjects(path, w, g, fs)
	case []any:
		g, ok := got.([]any)
		if !ok {
			*fs = append(*fs, mismatch(path, want, got))
			return
		}
		compareLists(path, w, g, fs)
	default:
		if !reflect.DeepEqual(want, got) {
			*fs = append(*fs, mismatch(path, want, got))
		}
	}
}

// identityKeys are the keys by which elements of a list are matched, in
// order of preference.
var identityKeys = []string{"type", "context"}

func compareLists(path string, want, got []any, fs *[]Finding) {
	if key, ok := identityKey(want); ok {
		compareKeyedLists(path, key, want, got, fs)
		return
	}
	// Any other list is a set of values; order carries no meaning.
	if !sameSet(want, got) {
		*fs = append(*fs, mismatch(path, want, got))
	}
}

// identityKey returns the key that identifies every element of list, if
// there is one. An empty list has none: it is then compared as a set, so
// any element on GitHub's side is drift.
func identityKey(list []any) (string, bool) {
	if len(list) == 0 {
		return "", false
	}
	for _, key := range identityKeys {
		if all(list, func(e any) bool {
			m, ok := e.(map[string]any)
			if !ok {
				return false
			}
			_, ok = m[key].(string)
			return ok
		}) {
			return key, true
		}
	}
	return "", false
}

func compareKeyedLists(path, key string, want, got []any, fs *[]Finding) {
	index := make(map[string]map[string]any, len(got))
	for _, e := range got {
		if m, ok := e.(map[string]any); ok {
			if id, ok := m[key].(string); ok {
				index[id] = m
			}
		}
	}
	seen := make(map[string]bool, len(want))
	for _, e := range want {
		m := e.(map[string]any)
		id := m[key].(string)
		seen[id] = true
		p := fmt.Sprintf("%s[%s=%s]", path, key, id)
		g, ok := index[id]
		if !ok {
			*fs = append(*fs, Finding{Drift, p, "missing"})
			continue
		}
		compareObjects(p, m, g, fs)
	}
	for _, e := range got {
		m, ok := e.(map[string]any)
		id, isString := m[key].(string)
		if !ok || !isString {
			*fs = append(*fs, Finding{Drift, path, "element without " + key + ": " + render(e)})
			continue
		}
		if !seen[id] {
			*fs = append(*fs, Finding{Drift, fmt.Sprintf("%s[%s=%s]", path, key, id), "not in the file"})
		}
	}
}

func sameSet(a, b []any) bool {
	if len(a) != len(b) {
		return false
	}
	ra, rb := renderAll(a), renderAll(b)
	sort.Strings(ra)
	sort.Strings(rb)
	return slices.Equal(ra, rb)
}

func renderAll(list []any) []string {
	out := make([]string, len(list))
	for i, e := range list {
		out[i] = render(e)
	}
	return out
}

func all(list []any, f func(any) bool) bool {
	for _, e := range list {
		if !f(e) {
			return false
		}
	}
	return true
}

func mismatch(path string, want, got any) Finding {
	return Finding{Drift, path, "want " + render(want) + ", got " + render(got)}
}

// render writes a value as compact JSON. Maps come out with sorted keys, so
// the text is stable.
func render(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(b)
}
