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
