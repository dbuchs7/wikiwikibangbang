// Command settingscheck compares the repository settings on GitHub with the
// ones recorded in the repository (ADR-0003). It is run by a scheduled
// workflow:
//
//	go run ./cmd/settingscheck [-repo owner/name] [-api URL]
//
// It reads the general settings from .github/repository-settings.json and
// every ruleset from .github/rulesets/*.json, fetches the live values through
// the REST API and reports each difference (see internal/settingsdrift). A
// ruleset on GitHub that no file records is drift too.
//
// GITHUB_TOKEN, if set, authenticates the requests. A value the token may
// not read is reported as unverifiable: a warning, not a failure, so a run
// shows which settings the token can vouch for. The workflow's default token
// cannot read the merge settings or a ruleset's bypass list; a token with
// "Administration: read" can. With -strict an unreadable value fails the
// run, for use with such a token.
//
// Exit status: 0 without drift, 1 with drift (or, with -strict, with an
// unreadable value), 2 when the check could not run.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/dbuchs7/wikiwikibangbang/internal/settingsdrift"
)

func main() {
	cfg := config{
		Token:   os.Getenv("GITHUB_TOKEN"),
		Actions: os.Getenv("GITHUB_ACTIONS") == "true",
	}
	flag.StringVar(&cfg.Repo, "repo", os.Getenv("GITHUB_REPOSITORY"), "repository as owner/name")
	flag.StringVar(&cfg.API, "api", "https://api.github.com", "base URL of the GitHub REST API")
	flag.StringVar(&cfg.SettingsFile, "settings", ".github/repository-settings.json", "file with the general repository settings")
	flag.StringVar(&cfg.RulesetDir, "rulesets", ".github/rulesets", "directory with one JSON file per ruleset")
	flag.BoolVar(&cfg.Strict, "strict", false, "fail when the token cannot read a recorded value")
	flag.Parse()
	if cfg.Repo == "" || flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "usage: settingscheck -repo owner/name [-strict] [-api URL] [-settings FILE] [-rulesets DIR]")
		os.Exit(2)
	}
	os.Exit(run(cfg, http.DefaultClient, os.Stdout))
}

type config struct {
	Repo, API, Token string
	SettingsFile     string
	RulesetDir       string
	Actions          bool // emit GitHub Actions annotations
	Strict           bool // an unverifiable value fails the run
}

// run performs the check and returns the exit status.
func run(cfg config, client *http.Client, out io.Writer) int {
	findings, err := check(cfg, &github{cfg: cfg, client: client})
	if err != nil {
		fmt.Fprintln(out, "settingscheck:", err)
		if cfg.Actions {
			fmt.Fprintf(out, "::error title=Settings check failed::%s\n", err)
		}
		return 2
	}
	for _, f := range findings {
		fmt.Fprintln(out, f)
		if !cfg.Actions {
			continue
		}
		switch {
		case f.Kind == settingsdrift.Drift:
			fmt.Fprintf(out, "::error title=Repository settings drift (ADR-0003)::%s: %s\n", f.Path, f.Detail)
		case cfg.Strict:
			fmt.Fprintf(out, "::error title=Repository setting not readable::%s: %s\n", f.Path, f.Detail)
		default:
			fmt.Fprintf(out, "::warning title=Repository setting not readable::%s: %s\n", f.Path, f.Detail)
		}
	}
	if settingsdrift.HasDrift(findings) {
		fmt.Fprintln(out, "settingscheck: GitHub differs from the files; apply the files or change them by pull request (ADR-0003)")
		return 1
	}
	if cfg.Strict && len(findings) > 0 {
		fmt.Fprintln(out, "settingscheck: the token cannot read every recorded value; check its permissions and expiry")
		return 1
	}
	fmt.Fprintf(out, "settingscheck: no drift in %s\n", cfg.Repo)
	return 0
}

func check(cfg config, gh *github) ([]settingsdrift.Finding, error) {
	var findings []settingsdrift.Finding

	want, err := readObject(cfg.SettingsFile)
	if err != nil {
		return nil, err
	}
	var live map[string]any
	if err := gh.get("/repos/"+cfg.Repo, &live); err != nil {
		return nil, err
	}
	findings = append(findings, settingsdrift.Compare("settings", want, live)...)

	files, err := filepath.Glob(filepath.Join(cfg.RulesetDir, "*.json"))
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no rulesets in %s", cfg.RulesetDir)
	}
	var list []struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	}
	if err := gh.get("/repos/"+cfg.Repo+"/rulesets?includes_parents=false&per_page=100", &list); err != nil {
		return nil, err
	}
	ids := make(map[string]int64, len(list))
	for _, rs := range list {
		ids[rs.Name] = rs.ID
	}
	recorded := make(map[string]bool, len(files))
	for _, file := range files {
		want, err := readObject(file)
		if err != nil {
			return nil, err
		}
		name, _ := want["name"].(string)
		if name == "" {
			return nil, fmt.Errorf("%s: ruleset has no name", file)
		}
		recorded[name] = true
		path := "ruleset " + name
		id, ok := ids[name]
		if !ok {
			findings = append(findings, settingsdrift.Finding{Kind: settingsdrift.Drift, Path: path, Detail: "not on GitHub"})
			continue
		}
		var live map[string]any
		if err := gh.get(fmt.Sprintf("/repos/%s/rulesets/%d", cfg.Repo, id), &live); err != nil {
			return nil, err
		}
		findings = append(findings, settingsdrift.Compare(path, want, live)...)
	}
	var extra []string
	for _, rs := range list {
		if !recorded[rs.Name] {
			extra = append(extra, rs.Name)
		}
	}
	sort.Strings(extra)
	for _, name := range extra {
		findings = append(findings, settingsdrift.Finding{Kind: settingsdrift.Drift, Path: "ruleset " + name, Detail: "on GitHub but in no file"})
	}
	return findings, nil
}

func readObject(file string) (map[string]any, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("%s: %w", file, err)
	}
	return m, nil
}

type github struct {
	cfg    config
	client *http.Client
}

func (g *github) get(path string, v any) error {
	req, err := http.NewRequest(http.MethodGet, strings.TrimSuffix(g.cfg.API, "/")+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if g.cfg.Token != "" {
		req.Header.Set("Authorization", "Bearer "+g.cfg.Token)
	}
	client := *g.client
	if client.Timeout == 0 {
		client.Timeout = 30 * time.Second
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		var e struct {
			Message string `json:"message"`
		}
		_ = json.Unmarshal(body, &e)
		return fmt.Errorf("GET %s: %s %s", path, resp.Status, e.Message)
	}
	if err := json.Unmarshal(body, v); err != nil {
		return fmt.Errorf("GET %s: %w", path, err)
	}
	return nil
}
