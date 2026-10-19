package settingsdrift

import (
	"encoding/json"
	"strings"
	"testing"
)

func decode(t *testing.T, s string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		t.Fatalf("invalid test JSON: %v", err)
	}
	return m
}

// summary renders findings one per line, so a test states the expected
// result as text.
func summary(fs []Finding) string {
	var b strings.Builder
	for _, f := range fs {
		b.WriteString(f.String())
		b.WriteString("\n")
	}
	return b.String()
}

func TestCompareMatchingObjectsHasNoFindings(t *testing.T) {
	want := decode(t, `{"allow_squash_merge": true, "default_branch": "main"}`)
	live := decode(t, `{"allow_squash_merge": true, "default_branch": "main", "id": 1, "name": "repo"}`)
	if fs := Compare("settings", want, live); len(fs) != 0 {
		t.Fatalf("want no findings, got:\n%s", summary(fs))
	}
}

func TestCompareReportsChangedValue(t *testing.T) {
	want := decode(t, `{"allow_merge_commit": false, "squash_merge_commit_message": "PR_BODY"}`)
	live := decode(t, `{"allow_merge_commit": true, "squash_merge_commit_message": "COMMIT_MESSAGES"}`)
	got := summary(Compare("settings", want, live))
	wantText := "drift: settings.allow_merge_commit: want false, got true\n" +
		"drift: settings.squash_merge_commit_message: want \"PR_BODY\", got \"COMMIT_MESSAGES\"\n"
	if got != wantText {
		t.Fatalf("got:\n%s\nwant:\n%s", got, wantText)
	}
}

// A field the token may not read is absent from the response. That is not
// drift, but the check cannot vouch for it either.
func TestCompareReportsAbsentFieldAsUnverifiable(t *testing.T) {
	want := decode(t, `{"allow_merge_commit": false}`)
	live := decode(t, `{"default_branch": "main"}`)
	fs := Compare("settings", want, live)
	if len(fs) != 1 || fs[0].Kind != Unverifiable {
		t.Fatalf("want one unverifiable finding, got:\n%s", summary(fs))
	}
	if got, want := fs[0].String(), "unverifiable: settings.allow_merge_commit: not in the response (want false)"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestCompareRulesMatchedByTypeNotPosition(t *testing.T) {
	want := decode(t, `{"rules": [{"type": "deletion"}, {"type": "non_fast_forward"}]}`)
	live := decode(t, `{"rules": [{"type": "non_fast_forward"}, {"type": "deletion"}]}`)
	if fs := Compare("ruleset", want, live); len(fs) != 0 {
		t.Fatalf("want no findings, got:\n%s", summary(fs))
	}
}

func TestCompareReportsMissingAndExtraRules(t *testing.T) {
	want := decode(t, `{"rules": [{"type": "deletion"}, {"type": "non_fast_forward"}]}`)
	live := decode(t, `{"rules": [{"type": "deletion"}, {"type": "creation"}]}`)
	got := summary(Compare("ruleset", want, live))
	wantText := "drift: ruleset.rules[type=non_fast_forward]: missing\n" +
		"drift: ruleset.rules[type=creation]: not in the file\n"
	if got != wantText {
		t.Fatalf("got:\n%s\nwant:\n%s", got, wantText)
	}
}

// GitHub adds parameters of its own (and integration_id to a status check);
// the file states only what it decides, so extra keys are not drift.
func TestCompareIgnoresKeysTheFileDoesNotSet(t *testing.T) {
	want := decode(t, `{"rules": [{"type": "required_status_checks", "parameters": {
		"strict_required_status_checks_policy": true,
		"required_status_checks": [{"context": "a"}, {"context": "b"}]}}]}`)
	live := decode(t, `{"rules": [{"type": "required_status_checks", "parameters": {
		"strict_required_status_checks_policy": true,
		"do_not_enforce_on_create": false,
		"required_status_checks": [{"context": "b", "integration_id": 15368}, {"context": "a"}]}}]}`)
	if fs := Compare("ruleset", want, live); len(fs) != 0 {
		t.Fatalf("want no findings, got:\n%s", summary(fs))
	}
}

func TestCompareReportsRequiredCheckRemovedOrAdded(t *testing.T) {
	want := decode(t, `{"checks": [{"context": "a"}, {"context": "b"}]}`)
	live := decode(t, `{"checks": [{"context": "a"}, {"context": "c"}]}`)
	got := summary(Compare("ruleset", want, live))
	wantText := "drift: ruleset.checks[context=b]: missing\n" +
		"drift: ruleset.checks[context=c]: not in the file\n"
	if got != wantText {
		t.Fatalf("got:\n%s\nwant:\n%s", got, wantText)
	}
}

func TestCompareScalarListsAsSets(t *testing.T) {
	want := decode(t, `{"include": ["~DEFAULT_BRANCH"], "methods": ["squash"]}`)
	live := decode(t, `{"include": ["~DEFAULT_BRANCH"], "methods": ["squash", "merge"]}`)
	got := summary(Compare("ruleset", want, live))
	wantText := "drift: ruleset.methods: want [\"squash\"], got [\"squash\",\"merge\"]\n"
	if got != wantText {
		t.Fatalf("got:\n%s\nwant:\n%s", got, wantText)
	}
	same := decode(t, `{"methods": ["merge", "squash"]}`)
	reordered := decode(t, `{"methods": ["squash", "merge"]}`)
	if fs := Compare("ruleset", same, reordered); len(fs) != 0 {
		t.Fatalf("order of a scalar list is not drift, got:\n%s", summary(fs))
	}
}

// An empty bypass list is the decision; a list that gains an actor is drift.
func TestCompareEmptyListAgainstFilledList(t *testing.T) {
	want := decode(t, `{"bypass_actors": []}`)
	live := decode(t, `{"bypass_actors": [{"actor_id": 5, "actor_type": "RepositoryRole", "bypass_mode": "always"}]}`)
	fs := Compare("ruleset", want, live)
	if len(fs) != 1 || fs[0].Kind != Drift || fs[0].Path != "ruleset.bypass_actors" {
		t.Fatalf("want drift on bypass_actors, got:\n%s", summary(fs))
	}
}

func TestCompareTypeMismatch(t *testing.T) {
	want := decode(t, `{"conditions": {"ref_name": {"include": ["main"]}}}`)
	live := decode(t, `{"conditions": "none"}`)
	fs := Compare("ruleset", want, live)
	if len(fs) != 1 || fs[0].Kind != Drift || fs[0].Path != "ruleset.conditions" {
		t.Fatalf("want drift on conditions, got:\n%s", summary(fs))
	}
}

func TestHasDrift(t *testing.T) {
	if HasDrift([]Finding{{Kind: Unverifiable}}) {
		t.Fatal("an unverifiable finding alone is not drift")
	}
	if !HasDrift([]Finding{{Kind: Unverifiable}, {Kind: Drift}}) {
		t.Fatal("a drift finding is drift")
	}
}
