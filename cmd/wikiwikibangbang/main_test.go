package main

import (
	"bytes"
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
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
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan int, 1)
	go func() {
		code, _, _ := runCmd(t, ctx, "serve", "-addr", addr)
		done <- code
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
	case <-time.After(5 * time.Second):
		t.Fatal("serve did not stop after cancellation")
	}
}

func TestServeFailsOnUnusableAddress(t *testing.T) {
	code, _, errOut := runCmd(t, context.Background(), "serve", "-addr", "256.0.0.1:1")
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
