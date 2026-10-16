// Command dcocheck verifies the DCO sign-offs of every non-merge commit in a
// git range. It is run by CI on pull requests:
//
//	go run ./cmd/dcocheck [-pr-body-file FILE] <base> <head>
//
// A commit passes with a human Signed-off-by of its own, or — when
// -pr-body-file names the pull request description — with a human
// Signed-off-by line in that description, which covers every commit of the
// pull request (ADR-0018). A sign-off from an AI agent or bot fails
// wherever it appears. See internal/dco for the rules.
//
// With -advisory the result is reported but never fails: missing or invalid
// sign-offs become a warning annotation and the command exits 0. CI runs in
// this mode while DCO enforcement is suspended (ADR-0019).
package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/dbuchs7/wikiwikibangbang/internal/dco"
)

func main() {
	prBodyFile := flag.String("pr-body-file", "", "file holding the pull request description; its Signed-off-by lines cover all commits")
	advisory := flag.Bool("advisory", false, "report problems as a warning and exit 0 (ADR-0019)")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: dcocheck [-advisory] [-pr-body-file FILE] <base> <head>")
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() != 2 {
		flag.Usage()
		os.Exit(2)
	}
	ok, err := run(flag.Arg(0), flag.Arg(1), *prBodyFile)
	if err != nil {
		fmt.Fprintln(os.Stderr, "dcocheck:", err)
		os.Exit(2)
	}
	if ok {
		return
	}
	if *advisory {
		// A GitHub Actions workflow command: shown as a warning on the run
		// and the pull request, without failing the job.
		fmt.Println("::warning title=DCO sign-off missing (advisory)::Not every commit is covered by a human sign-off. Enforcement is suspended (ADR-0019); the pull request may be merged.")
		return
	}
	os.Exit(1)
}

func run(base, head, prBodyFile string) (bool, error) {
	var prSignOffs []string
	if prBodyFile != "" {
		body, err := os.ReadFile(prBodyFile)
		if err != nil {
			return false, err
		}
		prSignOffs = dco.ParseSignOffs(string(body))
	}

	out, err := git("rev-list", "--no-merges", "--reverse", base+".."+head)
	if err != nil {
		return false, err
	}
	var commits []dco.Commit
	for _, id := range strings.Fields(out) {
		subject, err := git("log", "-1", "--format=%s", id)
		if err != nil {
			return false, err
		}
		trailers, err := git("log", "-1", "--format=%(trailers:key=Signed-off-by,valueonly,unfold)", id)
		if err != nil {
			return false, err
		}
		commits = append(commits, dco.Commit{
			ID:       id,
			Subject:  strings.TrimSpace(subject),
			SignOffs: strings.Split(trailers, "\n"),
		})
	}
	if len(commits) == 0 {
		fmt.Println("dcocheck: no commits to check")
		return true, nil
	}

	rep := dco.Evaluate(commits, prSignOffs)
	for _, s := range rep.PRSigners {
		fmt.Printf("pull request signed off by %s\n", s)
	}
	for _, p := range rep.PRProblems {
		fmt.Printf("FAIL  pull request description\n      - %s\n", p)
	}
	for _, v := range rep.Commits {
		short := v.ID[:min(12, len(v.ID))]
		switch {
		case len(v.Problems) > 0:
			fmt.Printf("FAIL  %s %s\n", short, v.Subject)
			for _, p := range v.Problems {
				fmt.Printf("      - %s\n", p)
			}
		case v.CoveredByPR:
			fmt.Printf("ok    %s %s (covered by the pull request sign-off)\n", short, v.Subject)
		default:
			fmt.Printf("ok    %s %s\n", short, v.Subject)
		}
	}
	return rep.OK(), nil
}

func git(args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return string(out), nil
}
