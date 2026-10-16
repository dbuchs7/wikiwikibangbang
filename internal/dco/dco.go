// Package dco checks the Developer Certificate of Origin sign-offs of commits.
//
// The project rule (AGENTS.md, "DCO sign-off: only humans sign"; ADR-0018) is
// that every commit is covered by a human Signed-off-by — on the commit
// itself, or once for the whole pull request in its description — and that
// no commit carries a sign-off from an AI agent or bot. An agent cannot certify the DCO; it credits
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

// Result is the verdict on one set of Signed-off-by values.
type Result struct {
	// Humans counts the well-formed sign-offs from people.
	Humans int
	// Violations are sign-offs that are never acceptable: malformed values
	// and sign-offs from AI agents or bots. A human sign-off elsewhere does
	// not cure them.
	Violations []string
}

// Analyze classifies Signed-off-by values (each "Name <email>").
func Analyze(signOffs []string) Result {
	var r Result
	for _, s := range signOffs {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		m := signOffRE.FindStringSubmatch(s)
		if m == nil {
			r.Violations = append(r.Violations, fmt.Sprintf("malformed sign-off %q: want \"Full Name <email>\"", s))
			continue
		}
		if reason := agentReason(m[1], m[2]); reason != "" {
			r.Violations = append(r.Violations, fmt.Sprintf("sign-off %q is %s; only the responsible human may sign off", s, reason))
			continue
		}
		r.Humans++
	}
	return r
}

// MissingHuman is the problem reported when a commit has no human sign-off
// and no pull-request sign-off covers it.
const MissingHuman = "no Signed-off-by from a human; sign off the commits (git rebase --signoff <base>) or add a Signed-off-by line to the pull request description (ADR-0018)"

// Check returns the problems with one commit's Signed-off-by trailer values,
// judged on their own. An empty result means the commit is acceptable.
func Check(signOffs []string) []string {
	r := Analyze(signOffs)
	problems := r.Violations
	if r.Humans == 0 {
		problems = append(problems, MissingHuman)
	}
	return problems
}

// signOffLineRE matches a Signed-off-by line in free text such as a pull
// request description. The line must start the (trimmed) line, so a quoted
// or inline mention does not count.
var signOffLineRE = regexp.MustCompile(`(?i)^signed-off-by:\s*(.+?)\s*$`)

// ParseSignOffs extracts the values of Signed-off-by lines from free text.
// Lines inside fenced code blocks are ignored, so an example in the text is
// not mistaken for a sign-off.
func ParseSignOffs(text string) []string {
	var out []string
	inFence := false
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		if m := signOffLineRE.FindStringSubmatch(trimmed); m != nil {
			out = append(out, m[1])
		}
	}
	return out
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

// Commit is one commit to be judged.
type Commit struct {
	ID       string
	Subject  string
	SignOffs []string
}

// Verdict is the outcome for one commit.
type Verdict struct {
	Commit
	// CoveredByPR is true when the commit has no human sign-off of its own
	// and is accepted because of the pull request's sign-off.
	CoveredByPR bool
	Problems    []string
}

// Report is the outcome for a whole pull request.
type Report struct {
	// PRProblems are violations in the pull request description itself,
	// e.g. a sign-off line naming an AI agent.
	PRProblems []string
	// PRSigners are the human sign-offs found in the description.
	PRSigners []string
	Commits   []Verdict
}

// OK reports whether the pull request may be merged.
func (r Report) OK() bool {
	if len(r.PRProblems) > 0 {
		return false
	}
	for _, v := range r.Commits {
		if len(v.Problems) > 0 {
			return false
		}
	}
	return true
}

// Evaluate judges the commits of a pull request together with the
// Signed-off-by values found in its description. A human sign-off in the
// description covers every commit that lacks one of its own; it never covers
// a commit that carries an agent's or a malformed sign-off.
func Evaluate(commits []Commit, prSignOffs []string) Report {
	var rep Report
	pr := Analyze(prSignOffs)
	rep.PRProblems = pr.Violations
	for _, s := range prSignOffs {
		if Analyze([]string{s}).Humans == 1 {
			rep.PRSigners = append(rep.PRSigners, strings.TrimSpace(s))
		}
	}
	covered := pr.Humans > 0 && len(pr.Violations) == 0

	for _, c := range commits {
		r := Analyze(c.SignOffs)
		v := Verdict{Commit: c, Problems: r.Violations}
		if r.Humans == 0 {
			if covered && len(r.Violations) == 0 {
				v.CoveredByPR = true
			} else {
				v.Problems = append(v.Problems, MissingHuman)
			}
		}
		rep.Commits = append(rep.Commits, v)
	}
	return rep
}
