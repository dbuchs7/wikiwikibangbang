package main

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dbuchs7/wikiwikibangbang/internal/item"
	"github.com/dbuchs7/wikiwikibangbang/internal/storage"
	"github.com/dbuchs7/wikiwikibangbang/internal/wal"
)

func runCmd(t *testing.T, ctx context.Context, args ...string) (int, string, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := run(ctx, args, &out, &errOut)
	return code, out.String(), errOut.String()
}

func TestUsage(t *testing.T) {
	cases := []struct {
		args []string
		code int
	}{
		{nil, 2},
		{[]string{"bogus"}, 2},
		{[]string{"help"}, 0},
		{[]string{"serve", "-h"}, 0},
		{[]string{"serve", "-nope"}, 2},
		{[]string{"healthcheck", "-nope"}, 2},
		{[]string{"check", "-nope"}, 2},
	}
	for _, c := range cases {
		code, _, errOut := runCmd(t, context.Background(), c.args...)
		if code != c.code {
			t.Errorf("%v: exit %d, want %d (stderr %q)", c.args, code, c.code, errOut)
		}
		if c.code == 2 && !strings.Contains(errOut, "Usage") && !strings.Contains(errOut, "flag provided but not defined") {
			t.Errorf("%v: stderr %q does not explain the usage", c.args, errOut)
		}
	}
}

func TestVersion(t *testing.T) {
	code, out, _ := runCmd(t, context.Background(), "version")
	if code != 0 || !strings.HasPrefix(out, "wikiwikibangbang ") {
		t.Errorf("exit %d, output %q", code, out)
	}
}

func freeAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()
	return addr
}

// TestServeAndHealthcheck runs the binary's two halves against each other:
// serve starts, healthcheck sees it, and serve stops cleanly on cancellation.
func TestServeAndHealthcheck(t *testing.T) {
	addr := freeAddr(t)
	dataDir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan int, 1)
	var errOut bytes.Buffer
	go func() {
		done <- run(ctx, []string{"serve", "-addr", addr, "-data-dir", dataDir}, io.Discard, &errOut)
	}()

	url := "http://" + addr + "/healthz"
	deadline := time.Now().Add(5 * time.Second)
	for {
		code, _, _ := runCmd(t, context.Background(), "healthcheck", "-url", url, "-timeout", "500ms")
		if code == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("healthcheck never succeeded against the running server")
		}
		time.Sleep(20 * time.Millisecond)
	}

	cancel()
	select {
	case code := <-done:
		if code != 0 {
			t.Errorf("serve exited %d after cancellation, want 0", code)
		}
		if out := errOut.String(); !strings.Contains(out, "created the first administrator with a generated password") {
			t.Errorf("no administrator was set up:\n%s", out)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("serve did not stop after cancellation")
	}
}

func TestServeFailsOnUnusableAddress(t *testing.T) {
	code, _, errOut := runCmd(t, context.Background(), "serve", "-addr", "256.0.0.1:1", "-data-dir", t.TempDir())
	if code != 1 || !strings.Contains(errOut, "cannot listen") {
		t.Errorf("exit %d, stderr %q", code, errOut)
	}
}

func TestHealthcheckFailures(t *testing.T) {
	unhealthy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer unhealthy.Close()

	cases := []struct {
		url  string
		code int
	}{
		{unhealthy.URL, 1},
		{"http://" + freeAddr(t) + "/healthz", 1},
		{"://bad", 2},
	}
	for _, c := range cases {
		if code, _, _ := runCmd(t, context.Background(), "healthcheck", "-url", c.url, "-timeout", "1s"); code != c.code {
			t.Errorf("%s: exit %d, want %d", c.url, code, c.code)
		}
	}
}

func TestServeRefusesADataDirectoryInUse(t *testing.T) {
	dir := t.TempDir()
	db, _, err := storage.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	code, _, errOut := runCmd(t, context.Background(), "serve", "-addr", freeAddr(t), "-data-dir", dir)
	if code != 1 || !strings.Contains(errOut, "in use by another process") {
		t.Errorf("exit %d, stderr %q", code, errOut)
	}
}

func TestCheck(t *testing.T) {
	dir := t.TempDir()
	db, _, err := storage.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Store.CreateSpace("admin", "PLAT", "Platform"); err != nil {
		t.Fatal(err)
	}
	db.Close()

	code, out, _ := runCmd(t, context.Background(), "check", "-data-dir", dir)
	if code != 0 || !strings.Contains(out, "ok: 1 events in 1 batches") || strings.Contains(out, "erased") {
		t.Errorf("exit %d, output %q", code, out)
	}

	path := filepath.Join(dir, wal.FileName)
	data, _ := os.ReadFile(path)
	if err := os.WriteFile(path, data[:len(data)-3], 0o640); err != nil {
		t.Fatal(err)
	}
	if code, out, _ := runCmd(t, context.Background(), "check", "-data-dir", dir); code != 0 || !strings.Contains(out, "interrupted write") {
		t.Errorf("torn tail: exit %d, output %q", code, out)
	}

	if err := os.WriteFile(path, []byte("not a log file"), 0o640); err != nil {
		t.Fatal(err)
	}
	if code, _, errOut := runCmd(t, context.Background(), "check", "-data-dir", dir); code != 1 || !strings.Contains(errOut, "corrupt") {
		t.Errorf("corrupt log: exit %d, stderr %q", code, errOut)
	}
}

func TestCheckReportsErasedContent(t *testing.T) {
	dir := t.TempDir()
	db, _, err := storage.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	s := db.Store
	steps := []error{
		s.DefineType("admin", item.TypeDef{Name: "page"}),
		s.CreateSpace("admin", "PLAT", "Platform"),
	}
	it, err := s.Create("anna", item.NewItem{Space: "PLAT", Type: "page", Title: "secret"})
	steps = append(steps, err, s.Trash("anna", it.ID), s.Purge("anna", it.ID), db.Close())
	for _, err := range steps {
		if err != nil {
			t.Fatal(err)
		}
	}
	if code, out, _ := runCmd(t, context.Background(), "check", "-data-dir", dir); code != 0 || !strings.Contains(out, "1 events have erased content") {
		t.Errorf("exit %d, output %q", code, out)
	}
}

func TestServeUsesTheGivenAdministrator(t *testing.T) {
	t.Setenv("WWBB_ADMIN_USERNAME", "root")
	t.Setenv("WWBB_ADMIN_PASSWORD", "short")
	code, _, errOut := runCmd(t, context.Background(), "serve", "-addr", freeAddr(t), "-data-dir", t.TempDir())
	if code != 1 || !strings.Contains(errOut, "cannot set up the administrator") {
		t.Errorf("a too short password: exit %d, stderr %q", code, errOut)
	}

	dir := t.TempDir()
	t.Setenv("WWBB_ADMIN_PASSWORD", "root's own password")
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // stop right after start-up
	code, _, errOut = runCmd(t, ctx, "serve", "-addr", freeAddr(t), "-data-dir", dir)
	if code != 0 || !strings.Contains(errOut, "created the first administrator\" username=root") {
		t.Errorf("exit %d, stderr %q", code, errOut)
	}
	db, _, err := storage.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, _, err := db.Accounts.Login("root", "root's own password", "x"); err != nil {
		t.Errorf("the given password does not log in: %v", err)
	}
}
