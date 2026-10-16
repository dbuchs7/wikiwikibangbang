// Package dco checks the Developer Certificate of Origin sign-offs of commits.
//
// The project rule (AGENTS.md, "DCO sign-off: only humans sign") is that
// every commit carries at least one Signed-off-by trailer from a human, and
// none from an AI agent or bot. An agent cannot certify the DCO; it credits
// itself with a Co-Authored-By trailer instead, which this package ignores.
package dco

import (
	"fmt"
	"regexp"
	"strings"
)

// signOffRE splits a trailer value into name and address. It is deliberately
// looser than RFC 5322: git accepts identities such as bot accounts with
// brackets in the name, and those must be recognised as bots, not rejected
// as malformed.
var signOffRE = regexp.MustCompile(`^(.*\S)\s*<([^<>\s]+@[^<>\s]+)>$`)

// agentNameRE matches sign-off names that are an agent's product name on its
// own. A person whose first name happens to be one of these words ("Claude
// Dupont") is not matched, because a full name has more than one word.
var agentNameRE = regexp.MustCompile(`(?i)^(claude|copilot|codex|chatgpt|gemini|devin)$`)

// agentEmailDomains are mail domains whose addresses identify an agent or a
// vendor's service account rather than a person.
var agentEmailDomains = []string{
	"anthropic.com",
	"openai.com",
}

// Check returns the problems with one commit's Signed-off-by trailer values
// (each in the form "Name <email>"). An empty result means the commit is
// acceptable.
func Check(signOffs []string) []string {
	var problems []string
	human := 0
	for _, s := range signOffs {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		m := signOffRE.FindStringSubmatch(s)
		if m == nil {
			problems = append(problems, fmt.Sprintf("malformed sign-off %q: want \"Full Name <email>\"", s))
			continue
		}
		if reason := agentReason(m[1], m[2]); reason != "" {
			problems = append(problems, fmt.Sprintf("sign-off %q is %s; only the responsible human may sign off", s, reason))
			continue
		}
		human++
	}
	if human == 0 {
		problems = append(problems, "no Signed-off-by from a human; the responsible person signs off before merge (git rebase --signoff <base>)")
	}
	return problems
}

func agentReason(name, email string) string {
	if agentNameRE.MatchString(strings.TrimSpace(name)) {
		return "an AI agent name"
	}
	if strings.Contains(name, "[bot]") || strings.Contains(email, "[bot]") {
		return "a bot account"
	}
	email = strings.ToLower(email)
	for _, d := range agentEmailDomains {
		if strings.HasSuffix(email, "@"+d) || strings.HasSuffix(email, "."+d) {
			return "an AI vendor address (" + d + ")"
		}
	}
	return ""
}
