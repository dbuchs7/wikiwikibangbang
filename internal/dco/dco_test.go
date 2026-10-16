package dco

import (
	"strings"
	"testing"
)

func TestCheck(t *testing.T) {
	tests := []struct {
		name     string
		signOffs []string
		wantOK   bool
		wantText string
	}{
		{
			name:     "human sign-off",
			signOffs: []string{"Jane Doe <jane@example.org>"},
			wantOK:   true,
		},
		{
			name:     "no sign-off",
			signOffs: nil,
			wantText: "no Signed-off-by from a human",
		},
		{
			name:     "only blank values",
			signOffs: []string{"", "  "},
			wantText: "no Signed-off-by from a human",
		},
		{
			name:     "agent by vendor address",
			signOffs: []string{"Claude <noreply@anthropic.com>"},
			wantText: "only the responsible human",
		},
		{
			name:     "agent by name alone",
			signOffs: []string{"Copilot <someone@example.org>"},
			wantText: "an AI agent name",
		},
		{
			name:     "bot account",
			signOffs: []string{"github-actions[bot] <41898282+github-actions[bot]@users.noreply.github.com>"},
			wantText: "a bot account",
		},
		{
			name:     "vendor subdomain",
			signOffs: []string{"Build Agent <ci@agents.openai.com>"},
			wantText: "openai.com",
		},
		{
			name:     "human and agent together is still rejected",
			signOffs: []string{"Jane Doe <jane@example.org>", "Claude <noreply@anthropic.com>"},
			wantText: "only the responsible human",
		},
		{
			name:     "person whose first name is an agent name",
			signOffs: []string{"Claude Dupont <claude.dupont@example.org>"},
			wantOK:   true,
		},
		{
			name:     "malformed value",
			signOffs: []string{"jane@example.org"},
			wantText: "malformed sign-off",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			problems := Check(tt.signOffs)
			if tt.wantOK {
				if len(problems) != 0 {
					t.Fatalf("want no problems, got %q", problems)
				}
				return
			}
			joined := strings.Join(problems, "\n")
			if !strings.Contains(joined, tt.wantText) {
				t.Fatalf("want a problem containing %q, got %q", tt.wantText, problems)
			}
		})
	}
}

func TestParseSignOffs(t *testing.T) {
	body := strings.Join([]string{
		"## Summary",
		"Some text mentioning Signed-off-by: inline, which does not count.",
		"",
		"```bash",
		"Signed-off-by: Example Only <example@example.org>",
		"```",
		"",
		"  Signed-off-by: Jane Doe <jane@example.org>  ",
		"signed-off-by: John Roe <john@example.org>",
	}, "\r\n")
	got := ParseSignOffs(body)
	want := []string{"Jane Doe <jane@example.org>", "John Roe <john@example.org>"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("ParseSignOffs = %q, want %q", got, want)
	}
}

func TestEvaluate(t *testing.T) {
	human := "Jane Doe <jane@example.org>"
	agent := "Claude <noreply@anthropic.com>"
	unsigned := Commit{ID: "a1", Subject: "agent commit"}
	signed := Commit{ID: "b2", Subject: "human commit", SignOffs: []string{human}}
	agentSigned := Commit{ID: "c3", Subject: "agent signed", SignOffs: []string{agent}}

	tests := []struct {
		name        string
		commits     []Commit
		pr          []string
		wantOK      bool
		wantCovered int
	}{
		{"all commits signed, no PR sign-off", []Commit{signed}, nil, true, 0},
		{"unsigned commit, no PR sign-off", []Commit{unsigned}, nil, false, 0},
		{"unsigned commits covered by PR sign-off", []Commit{unsigned, signed}, []string{human}, true, 1},
		{"agent sign-off in PR description covers nothing", []Commit{unsigned}, []string{agent}, false, 0},
		{"human and agent in PR description is rejected", []Commit{unsigned}, []string{human, agent}, false, 0},
		{"agent-signed commit is not cured by PR sign-off", []Commit{agentSigned}, []string{human}, false, 0},
		{"malformed PR sign-off covers nothing", []Commit{unsigned}, []string{"jane@example.org"}, false, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rep := Evaluate(tt.commits, tt.pr)
			if rep.OK() != tt.wantOK {
				t.Fatalf("OK() = %v, want %v; report %+v", rep.OK(), tt.wantOK, rep)
			}
			covered := 0
			for _, v := range rep.Commits {
				if v.CoveredByPR {
					covered++
				}
			}
			if covered != tt.wantCovered {
				t.Fatalf("covered = %d, want %d", covered, tt.wantCovered)
			}
		})
	}
}
