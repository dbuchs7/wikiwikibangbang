// Command wikiwikibangbang is the product's single binary (ADR-0024): the
// server, the API and the embedded web UI in one executable, for every
// supported platform and as a container image.
//
//	wikiwikibangbang serve [-addr :8080] [-data-dir data]
//	wikiwikibangbang check [-data-dir data]
//	wikiwikibangbang healthcheck [-url http://127.0.0.1:8080/healthz]
//	wikiwikibangbang version
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/dbuchs7/wikiwikibangbang/internal/buildinfo"
	"github.com/dbuchs7/wikiwikibangbang/internal/server"
	"github.com/dbuchs7/wikiwikibangbang/internal/storage"
	"github.com/dbuchs7/wikiwikibangbang/internal/web"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := run(ctx, os.Args[1:], os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}

const usage = `Usage: wikiwikibangbang <command> [flags]

Commands:
  serve        run the server
  check        verify a data directory without changing it
  healthcheck  exit 0 if a running server answers its liveness probe
  version      print the version

Run "wikiwikibangbang <command> -h" for the flags of a command.
`

// run executes one command and returns the process exit code: 0 on success,
// 1 on failure, 2 on wrong usage.
func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	cmd, args := args[0], args[1:]
	switch cmd {
	case "serve":
		return serve(ctx, args, stderr)
	case "check":
		return check(args, stdout, stderr)
	case "healthcheck":
		return healthcheck(ctx, args, stderr)
	case "version":
		fmt.Fprintln(stdout, buildinfo.Get())
		return 0
	case "-h", "-help", "--help", "help":
		fmt.Fprint(stdout, usage)
		return 0
	default:
		fmt.Fprintf(stderr, "unknown command %q\n\n%s", cmd, usage)
		return 2
	}
}

func newFlags(name string, stderr io.Writer) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	return fs
}

// parseFailed maps a flag parse error to an exit code; -h is not a failure.
func parseFailed(err error) int {
	if errors.Is(err, flag.ErrHelp) {
		return 0
	}
	return 2
}

func serve(ctx context.Context, args []string, stderr io.Writer) int {
	fs := newFlags("serve", stderr)
	addr := fs.String("addr", ":8080", "address to listen on")
	dataDir := fs.String("data-dir", defaultDataDir, "directory holding the event log")
	if err := fs.Parse(args); err != nil {
		return parseFailed(err)
	}
	log := slog.New(slog.NewTextHandler(stderr, nil))
	log.Info("starting", "build", buildinfo.Get().String())

	db, rec, err := storage.Open(*dataDir)
	if err != nil {
		log.Error("cannot open the data directory", "dir", *dataDir, "err", err)
		return 1
	}
	defer db.Close()
	if rec.Torn > 0 {
		log.Warn("dropped an interrupted write", "bytes", rec.Torn)
	}
	log.Info("data directory open", "dir", *dataDir, "events", rec.Events)

	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Error("cannot listen", "addr", *addr, "err", err)
		return 1
	}
	if err := server.Serve(ctx, ln, server.Handler(server.Routes(web.FS())), log); err != nil {
		log.Error("server failed", "err", err)
		return 1
	}
	return 0
}

// defaultDataDir is relative to the working directory, so a binary started
// from a terminal keeps its data next to where it was started.
const defaultDataDir = "data"

// check verifies that a data directory's log is intact and replays, without
// locking or changing it.
func check(args []string, stdout, stderr io.Writer) int {
	fs := newFlags("check", stderr)
	dataDir := fs.String("data-dir", defaultDataDir, "directory holding the event log")
	if err := fs.Parse(args); err != nil {
		return parseFailed(err)
	}
	rec, err := storage.Inspect(*dataDir)
	if err != nil {
		fmt.Fprintln(stderr, "check:", err)
		return 1
	}
	fmt.Fprintf(stdout, "ok: %d events in %d batches\n", rec.Events, rec.Batches)
	if rec.Torn > 0 {
		fmt.Fprintf(stdout, "an interrupted write of %d bytes at the end will be dropped on the next start\n", rec.Torn)
	}
	return 0
}

// healthcheck lets a container runtime probe the server without a shell or
// curl in the image.
func healthcheck(ctx context.Context, args []string, stderr io.Writer) int {
	fs := newFlags("healthcheck", stderr)
	url := fs.String("url", "http://127.0.0.1:8080/healthz", "liveness probe to query")
	timeout := fs.Duration("timeout", 3*time.Second, "how long to wait for an answer")
	if err := fs.Parse(args); err != nil {
		return parseFailed(err)
	}
	ctx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, *url, nil)
	if err != nil {
		fmt.Fprintln(stderr, "healthcheck:", err)
		return 2
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Fprintln(stderr, "healthcheck:", err)
		return 1
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		fmt.Fprintln(stderr, "healthcheck: status", res.Status)
		return 1
	}
	return 0
}
