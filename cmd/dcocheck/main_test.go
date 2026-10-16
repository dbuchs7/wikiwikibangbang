package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// gitRepo creates a repository with a base commit and returns a function
// that runs git in it.
func gitRepo(t *testing.T) (dir string, gitc func(args ...string) string) {
	t.Helper()
	dir = t.TempDir()
	gitc = func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
			"GIT_AUTHOR_NAME=Claude", "GIT_AUTHOR_EMAIL=noreply@anthropic.com",
			"GIT_COMMITTER_NAME=Claude", "GIT_COMMITTER_EMAIL=noreply@anthropic.com",
		)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	gitc("init", "-q")
	gitc("commit", "-q", "--allow-empty", "-m", "base")
	return dir, gitc
}

// inDir runs f with the working directory set to dir, because run calls git
// in the current directory.
func inDir(t *testing.T, dir string, f func()) {
	t.Helper()
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Chdir(old); err != nil {
			t.Fatal(err)
		}
	}()
	f()
}

func writeBody(t *testing.T, text string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "body.txt")
	if err := os.WriteFile(p, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestRun(t *testing.T) {
	dir, gitc := gitRepo(t)
	base := gitc("rev-parse", "HEAD")
	gitc("commit", "-q", "--allow-empty", "-m", "agent commit")

	signed := writeBody(t, "Summary\n\nSigned-off-by: Jane Doe <jane@example.org>\n")
	unsigned := writeBody(t, "Summary\n")

	inDir(t, dir, func() {
		if ok, err := run(base, "HEAD", ""); err != nil || ok {
			t.Errorf("unsigned commit, no body: ok=%v err=%v, want false", ok, err)
		}
		if ok, err := run(base, "HEAD", unsigned); err != nil || ok {
			t.Errorf("unsigned commit, body without sign-off: ok=%v err=%v, want false", ok, err)
		}
		if ok, err := run(base, "HEAD", signed); err != nil || !ok {
			t.Errorf("body sign-off: ok=%v err=%v, want true", ok, err)
		}
		if ok, err := run("HEAD", "HEAD", ""); err != nil || !ok {
			t.Errorf("empty range: ok=%v err=%v, want true", ok, err)
		}
		if _, err := run(base, "HEAD", filepath.Join(dir, "missing.txt")); err == nil {
			t.Error("missing body file: want an error")
		}
		if _, err := run("no-such-ref", "HEAD", ""); err == nil {
			t.Error("unknown ref: want an error")
		}
	})

	// A commit signed off by an agent fails even with a human body sign-off.
	gitc("commit", "-q", "--allow-empty", "-m", "agent signed", "-m", "Signed-off-by: Claude <noreply@anthropic.com>")
	inDir(t, dir, func() {
		if ok, err := run(base, "HEAD", signed); err != nil || ok {
			t.Errorf("agent-signed commit: ok=%v err=%v, want false", ok, err)
		}
	})
}
