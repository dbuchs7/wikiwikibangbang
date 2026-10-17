package repoconfig

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

const (
	workflowPath = "../../.github/workflows/ci.yml"
	rulesetGlob  = "../../.github/rulesets/*.json"
)

// jobNameRE matches a job's display name in ci.yml: a `name:` key indented
// by exactly four spaces, i.e. directly under a job id. Step names are
// indented deeper and do not match.
var jobNameRE = regexp.MustCompile(`^    name: (.+?)\s*$`)

type ruleset struct {
	Name  string `json:"name"`
	Rules []struct {
		Type       string `json:"type"`
		Parameters struct {
			RequiredStatusChecks []struct {
				Context string `json:"context"`
			} `json:"required_status_checks"`
		} `json:"parameters"`
	} `json:"rules"`
}

func ciJobNames(t *testing.T) map[string]bool {
	t.Helper()
	f, err := os.Open(workflowPath)
	if err != nil {
		t.Fatalf("reading workflow: %v", err)
	}
	defer f.Close()
	names := make(map[string]bool)
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if m := jobNameRE.FindStringSubmatch(sc.Text()); m != nil {
			names[m[1]] = true
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("reading workflow: %v", err)
	}
	if len(names) == 0 {
		t.Fatalf("no job names found in %s", workflowPath)
	}
	return names
}

// TestRequiredChecksNameExistingJobs guards against a renamed CI job: the
// ruleset would then wait for a check that is never reported and block
// every merge.
func TestRequiredChecksNameExistingJobs(t *testing.T) {
	jobs := ciJobNames(t)
	files, err := filepath.Glob(rulesetGlob)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatalf("no rulesets found at %s", rulesetGlob)
	}
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		var rs ruleset
		if err := json.Unmarshal(data, &rs); err != nil {
			t.Fatalf("%s: invalid JSON: %v", file, err)
		}
		checks := 0
		for _, r := range rs.Rules {
			if r.Type != "required_status_checks" {
				continue
			}
			for _, c := range r.Parameters.RequiredStatusChecks {
				checks++
				if !jobs[c.Context] {
					t.Errorf("%s: required check %q is not a job name in ci.yml", filepath.Base(file), c.Context)
				}
			}
		}
		if checks == 0 {
			t.Errorf("%s: requires no status checks", filepath.Base(file))
		}
	}
}

// TestEveryJobIsRequired is the converse: a CI job the ruleset does not
// require only reports, and a pull request could be merged with it red.
func TestEveryJobIsRequired(t *testing.T) {
	data, err := os.ReadFile("../../.github/rulesets/protect-main.json")
	if err != nil {
		t.Fatal(err)
	}
	var rs ruleset
	if err := json.Unmarshal(data, &rs); err != nil {
		t.Fatal(err)
	}
	required := make(map[string]bool)
	for _, r := range rs.Rules {
		for _, c := range r.Parameters.RequiredStatusChecks {
			required[c.Context] = true
		}
	}
	for job := range ciJobNames(t) {
		if !required[job] {
			t.Errorf("CI job %q is not a required check in protect-main.json", job)
		}
	}
}
