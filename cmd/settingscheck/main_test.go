package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	settingsFile = `{"allow_merge_commit": false, "allow_squash_merge": true, "default_branch": "main"}`
	rulesetFile  = `{"name": "protect-main", "enforcement": "active", "bypass_actors": [],
		"rules": [{"type": "deletion"}, {"type": "pull_request", "parameters": {"allowed_merge_methods": ["squash"]}}]}`
	liveRepo    = `{"id": 1, "allow_merge_commit": false, "allow_squash_merge": true, "default_branch": "main"}`
	liveList    = `[{"id": 7, "name": "protect-main", "source_type": "Repository"}]`
	liveRuleset = `{"id": 7, "name": "protect-main", "enforcement": "active", "bypass_actors": [],
		"rules": [{"type": "pull_request", "parameters": {"allowed_merge_methods": ["squash"], "required_reviewers": []}}, {"type": "deletion"}]}`
)

// fixture writes the recorded files and returns a config pointing at them.
func fixture(t *testing.T, settings string, rulesets ...string) config {
	t.Helper()
	dir := t.TempDir()
	write := func(name, content string) string {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	cfg := config{Repo: "owner/repo", SettingsFile: write("settings.json", settings), RulesetDir: filepath.Join(dir, "rulesets")}
	if err := os.MkdirAll(cfg.RulesetDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for i, rs := range rulesets {
		write(filepath.Join("rulesets", string(rune('a'+i))+".json"), rs)
	}
	return cfg
}

// fakeGitHub serves the given bodies by request path and records the
// authorization header it saw.
func fakeGitHub(t *testing.T, routes map[string]string) (*httptest.Server, *string) {
	t.Helper()
	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		body, ok := routes[r.URL.Path]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"message": "Not Found"}`))
			return
		}
		w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv, &auth
}

func defaultRoutes() map[string]string {
	return map[string]string{
		"/repos/owner/repo":            liveRepo,
		"/repos/owner/repo/rulesets":   liveList,
		"/repos/owner/repo/rulesets/7": liveRuleset,
	}
}

func runCheck(t *testing.T, cfg config, routes map[string]string) (int, string, string) {
	t.Helper()
	srv, auth := fakeGitHub(t, routes)
	cfg.API = srv.URL
	var out bytes.Buffer
	code := run(cfg, srv.Client(), &out)
	return code, out.String(), *auth
}

func TestNoDrift(t *testing.T) {
	cfg := fixture(t, settingsFile, rulesetFile)
	cfg.Token = "secret"
	code, out, auth := runCheck(t, cfg, defaultRoutes())
	if code != 0 {
		t.Fatalf("exit %d, output:\n%s", code, out)
	}
	if !strings.Contains(out, "no drift in owner/repo") {
		t.Errorf("output lacks the summary:\n%s", out)
	}
	if auth != "Bearer secret" {
		t.Errorf("Authorization header = %q, want the token", auth)
	}
}

func TestSettingDriftFailsWithAnnotation(t *testing.T) {
	cfg := fixture(t, settingsFile, rulesetFile)
	cfg.Actions = true
	routes := defaultRoutes()
	routes["/repos/owner/repo"] = `{"allow_merge_commit": true, "allow_squash_merge": false, "default_branch": "main"}`
	code, out, _ := runCheck(t, cfg, routes)
	if code != 1 {
		t.Fatalf("exit %d, want 1; output:\n%s", code, out)
	}
	for _, want := range []string{
		"drift: settings.allow_merge_commit: want false, got true",
		"::error title=Repository settings drift (ADR-0003)::settings.allow_squash_merge: want true, got false",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
}

func TestUnreadableSettingWarnsButPasses(t *testing.T) {
	cfg := fixture(t, settingsFile, rulesetFile)
	cfg.Actions = true
	routes := defaultRoutes()
	routes["/repos/owner/repo"] = `{"default_branch": "main"}`
	code, out, _ := runCheck(t, cfg, routes)
	if code != 0 {
		t.Fatalf("exit %d, want 0; output:\n%s", code, out)
	}
	if !strings.Contains(out, "::warning title=Repository setting not readable::settings.allow_merge_commit") {
		t.Errorf("output lacks the warning:\n%s", out)
	}
}

func TestRulesetMissingOnGitHub(t *testing.T) {
	cfg := fixture(t, settingsFile, rulesetFile)
	routes := defaultRoutes()
	routes["/repos/owner/repo/rulesets"] = `[]`
	code, out, _ := runCheck(t, cfg, routes)
	if code != 1 || !strings.Contains(out, "drift: ruleset protect-main: not on GitHub") {
		t.Fatalf("exit %d, output:\n%s", code, out)
	}
}

func TestRulesetOnlyOnGitHub(t *testing.T) {
	cfg := fixture(t, settingsFile, rulesetFile)
	routes := defaultRoutes()
	routes["/repos/owner/repo/rulesets"] = `[{"id": 7, "name": "protect-main"}, {"id": 8, "name": "loosen-main"}]`
	code, out, _ := runCheck(t, cfg, routes)
	if code != 1 || !strings.Contains(out, "drift: ruleset loosen-main: on GitHub but in no file") {
		t.Fatalf("exit %d, output:\n%s", code, out)
	}
}

func TestRulesetRuleChanged(t *testing.T) {
	cfg := fixture(t, settingsFile, rulesetFile)
	routes := defaultRoutes()
	routes["/repos/owner/repo/rulesets/7"] = `{"name": "protect-main", "enforcement": "disabled", "bypass_actors": [],
		"rules": [{"type": "pull_request", "parameters": {"allowed_merge_methods": ["squash", "merge"]}}]}`
	code, out, _ := runCheck(t, cfg, routes)
	if code != 1 {
		t.Fatalf("exit %d, want 1; output:\n%s", code, out)
	}
	for _, want := range []string{
		`drift: ruleset protect-main.enforcement: want "active", got "disabled"`,
		"drift: ruleset protect-main.rules[type=deletion]: missing",
		`drift: ruleset protect-main.rules[type=pull_request].parameters.allowed_merge_methods: want ["squash"], got ["squash","merge"]`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
}

func TestAPIErrorStopsTheCheck(t *testing.T) {
	cfg := fixture(t, settingsFile, rulesetFile)
	cfg.Actions = true
	routes := defaultRoutes()
	delete(routes, "/repos/owner/repo/rulesets")
	code, out, _ := runCheck(t, cfg, routes)
	if code != 2 {
		t.Fatalf("exit %d, want 2; output:\n%s", code, out)
	}
	if !strings.Contains(out, "404 Not Found Not Found") || !strings.Contains(out, "::error title=Settings check failed::") {
		t.Errorf("output does not name the failed request:\n%s", out)
	}
}

func TestBrokenFilesStopTheCheck(t *testing.T) {
	for name, cfg := range map[string]config{
		"invalid settings JSON": fixture(t, `{`, rulesetFile),
		"no rulesets":           fixture(t, settingsFile),
		"unnamed ruleset":       fixture(t, settingsFile, `{"rules": []}`),
		"invalid ruleset JSON":  fixture(t, settingsFile, `[`),
	} {
		t.Run(name, func(t *testing.T) {
			if code, out, _ := runCheck(t, cfg, defaultRoutes()); code != 2 {
				t.Fatalf("exit %d, want 2; output:\n%s", code, out)
			}
		})
	}
}

func TestMissingSettingsFileStopsTheCheck(t *testing.T) {
	cfg := fixture(t, settingsFile, rulesetFile)
	cfg.SettingsFile = filepath.Join(t.TempDir(), "absent.json")
	if code, out, _ := runCheck(t, cfg, defaultRoutes()); code != 2 {
		t.Fatalf("exit %d, want 2; output:\n%s", code, out)
	}
}

func TestInvalidResponseStopsTheCheck(t *testing.T) {
	cfg := fixture(t, settingsFile, rulesetFile)
	routes := defaultRoutes()
	routes["/repos/owner/repo"] = `not json`
	if code, out, _ := runCheck(t, cfg, routes); code != 2 {
		t.Fatalf("exit %d, want 2; output:\n%s", code, out)
	}
}

// The files in the repository are the ones the workflow checks; they must
// at least be readable and name their rulesets.
func TestRecordedFilesAreReadable(t *testing.T) {
	if _, err := readObject("../../.github/repository-settings.json"); err != nil {
		t.Fatal(err)
	}
	files, err := filepath.Glob("../../.github/rulesets/*.json")
	if err != nil || len(files) == 0 {
		t.Fatalf("no rulesets: %v", err)
	}
	for _, f := range files {
		m, err := readObject(f)
		if err != nil {
			t.Fatal(err)
		}
		if name, _ := m["name"].(string); name == "" {
			t.Errorf("%s: ruleset has no name", f)
		}
	}
}

// With -strict an unreadable value fails the run: once the workflow has a
// token that may read everything, a value it cannot read means the token
// lost a permission or expired in part, and that must not pass silently.
func TestStrictFailsOnUnreadableSetting(t *testing.T) {
	cfg := fixture(t, settingsFile, rulesetFile)
	cfg.Actions = true
	cfg.Strict = true
	routes := defaultRoutes()
	routes["/repos/owner/repo"] = `{"default_branch": "main"}`
	code, out, _ := runCheck(t, cfg, routes)
	if code != 1 {
		t.Fatalf("exit %d, want 1; output:\n%s", code, out)
	}
	for _, want := range []string{
		"::error title=Repository setting not readable::settings.allow_merge_commit",
		"the token cannot read every recorded value",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
}

func TestStrictPassesWhenEverythingIsReadable(t *testing.T) {
	cfg := fixture(t, settingsFile, rulesetFile)
	cfg.Strict = true
	if code, out, _ := runCheck(t, cfg, defaultRoutes()); code != 0 {
		t.Fatalf("exit %d, want 0; output:\n%s", code, out)
	}
}
