// Command wikiwikibangbang is the product's single binary (ADR-0024): the
// server, the API and the embedded web UI in one executable, for every
// supported platform and as a container image.
//
//	wikiwikibangbang serve [-addr :8080]
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
	if err := fs.Parse(args); err != nil {
		return parseFailed(err)
	}
	log := slog.New(slog.NewTextHandler(stderr, nil))
	log.Info("starting", "build", buildinfo.Get().String())

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
