// Command dcocheck verifies the DCO sign-offs of every non-merge commit in a
// git range. It is run by CI on pull requests:
//
//	go run ./cmd/dcocheck <base> <head>
//
// It exits non-zero if any commit lacks a human sign-off or carries one from
// an AI agent or bot. See internal/dco for the rules.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/dbuchs7/wikiwikibangbang/internal/dco"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: dcocheck <base> <head>")
		os.Exit(2)
	}
	failed, err := run(os.Args[1], os.Args[2])
	if err != nil {
		fmt.Fprintln(os.Stderr, "dcocheck:", err)
		os.Exit(2)
	}
	if failed {
		os.Exit(1)
	}
}

func run(base, head string) (failed bool, err error) {
	out, err := git("rev-list", "--no-merges", "--reverse", base+".."+head)
	if err != nil {
		return false, err
	}
	commits := strings.Fields(out)
	if len(commits) == 0 {
		fmt.Println("dcocheck: no commits to check")
		return false, nil
	}
	for _, c := range commits {
		subject, err := git("log", "-1", "--format=%s", c)
		if err != nil {
			return false, err
		}
		trailers, err := git("log", "-1", "--format=%(trailers:key=Signed-off-by,valueonly,unfold)", c)
		if err != nil {
			return false, err
		}
		problems := dco.Check(strings.Split(trailers, "\n"))
		short := c[:min(12, len(c))]
		if len(problems) == 0 {
			fmt.Printf("ok    %s %s\n", short, strings.TrimSpace(subject))
			continue
		}
		failed = true
		fmt.Printf("FAIL  %s %s\n", short, strings.TrimSpace(subject))
		for _, p := range problems {
			fmt.Printf("      - %s\n", p)
		}
	}
	return failed, nil
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
