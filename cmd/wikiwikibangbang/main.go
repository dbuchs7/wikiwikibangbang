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
	"strings"
	"syscall"
	"time"

	"github.com/dbuchs7/wikiwikibangbang/internal/backup"
	"github.com/dbuchs7/wikiwikibangbang/internal/budget"
	"github.com/dbuchs7/wikiwikibangbang/internal/buildinfo"
	"github.com/dbuchs7/wikiwikibangbang/internal/collab"
	"github.com/dbuchs7/wikiwikibangbang/internal/linkcheck"
	"github.com/dbuchs7/wikiwikibangbang/internal/mcp"
	"github.com/dbuchs7/wikiwikibangbang/internal/privdrop"
	"github.com/dbuchs7/wikiwikibangbang/internal/server"
	"github.com/dbuchs7/wikiwikibangbang/internal/sso"
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
  restore      restore a backup into a new data directory
  healthcheck  exit 0 if a running server answers its liveness probe
  mcp          serve the MCP tools of a running server on stdin and stdout
  version      print the version

Run "wikiwikibangbang <command> -h" for the flags of a command.
`

// stdin is what the mcp command reads; a variable so its test can feed it.
var stdin io.Reader = os.Stdin

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
	case "restore":
		return restore(args, stdout, stderr)
	case "healthcheck":
		return healthcheck(ctx, args, stderr)
	case "mcp":
		return mcpStdio(ctx, args, stdin, stdout, stderr)
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
	applyBudgets(os.Getenv, log)

	// Started as root with WWBB_RUN_AS set (the container image does,
	// ADR-0038), the server hands the data directory to that account and
	// gives root up before it opens anything.
	if v := os.Getenv("WWBB_RUN_AS"); v != "" {
		to, err := privdrop.Parse(v)
		if err != nil {
			log.Error("WWBB_RUN_AS is unusable", "err", err)
			return 1
		}
		dropped, err := privdrop.Drop(*dataDir, to)
		if err != nil {
			log.Error("cannot give up root", "err", err)
			return 1
		}
		if dropped {
			log.Info("gave up root", "uid", to.UID, "gid", to.GID, "data-dir", *dataDir)
		}
	}

	db, rec, err := storage.OpenWith(*dataDir, storage.Options{Log: log})
	if err != nil {
		log.Error("cannot open the data directory", "dir", *dataDir, "err", err)
		return 1
	}
	defer db.Close()
	if rec.Torn > 0 {
		log.Warn("dropped an interrupted write", "bytes", rec.Torn)
	}
	if rec.Shredded > 0 {
		log.Info("destroyed leftover item keys", "keys", rec.Shredded)
	}
	log.Info("data directory open", "dir", *dataDir, "events", rec.Events)

	if err := ensureAdmin(db, log); err != nil {
		log.Error("cannot set up the administrator", "err", err)
		return 1
	}

	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()
	go expireAudit(ctx, db, log, ticker.C)

	proxies, err := server.ParseTrustedProxies(os.Getenv("WWBB_TRUSTED_PROXIES"))
	if err != nil {
		log.Error("WWBB_TRUSTED_PROXIES is unusable", "err", err)
		return 1
	}
	if len(proxies) > 0 {
		names := make([]string, len(proxies))
		for i, p := range proxies {
			names[i] = p.String()
		}
		log.Info("trusting forwarded headers", "proxies", strings.Join(names, ","))
	}

	var links *server.LinkChecks
	switch v := os.Getenv("WWBB_LINKCHECK"); v {
	case "", "off":
	case "on":
		links = server.NewLinkChecks(linkcheck.New(linkcheck.Options{}))
		log.Info("link checking is on; space administrators may start a check of public web addresses")
	default:
		log.Error("WWBB_LINKCHECK is unusable", "value", v, "want", "on or off")
		return 1
	}

	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Error("cannot listen", "addr", *addr, "err", err)
		return 1
	}
	provider, err := ssoFromEnv()
	if err != nil {
		log.Error("single sign-on is misconfigured", "err", err)
		return 1
	}
	if provider != nil {
		log.Info("single sign-on enabled", "issuer", provider.Issuer())
	}
	live := server.NewCollab(db)
	liveCtx, stopLive := context.WithCancel(ctx)
	liveDone := make(chan struct{})
	go func() { runCollab(liveCtx, live, time.NewTicker(collabTick).C); close(liveDone) }()
	// Runs before the data directory closes: open drafts are committed.
	defer func() { stopLive(); <-liveDone }()
	routes := server.Routes(server.Deps{UI: web.FS(), DB: db, Log: log, SSO: provider, Collab: live, LinkCheck: links})
	if err := server.Serve(ctx, ln, server.BehindProxies(proxies, server.Handler(routes, db.Accounts)), log); err != nil {
		log.Error("server failed", "err", err)
		return 1
	}
	return 0
}

// applyBudgets puts the resource budgets from the environment in force
// (ADR-0015). A value that cannot be used is logged and leaves the default;
// it never stops the server.
func applyBudgets(getenv func(string) string, log *slog.Logger) {
	b, problems := budget.FromEnv(getenv)
	for _, p := range problems {
		log.Warn("ignoring a budget", "problem", p)
	}
	if changed := b.Changed(); len(changed) > 0 {
		log.Info("budgets changed from their defaults", "budgets", strings.Join(changed, " "))
	}
	budget.Set(b)
}

// ensureAdmin creates the first administrator on the first start (ADR-0013):
// WWBB_ADMIN_USERNAME (default "admin") with WWBB_ADMIN_PASSWORD, or with a
// generated password that is logged this once.
func ensureAdmin(db *storage.DB, log *slog.Logger) error {
	username := os.Getenv("WWBB_ADMIN_USERNAME")
	if username == "" {
		username = "admin"
	}
	b, err := db.EnsureAdmin(username, os.Getenv("WWBB_ADMIN_PASSWORD"))
	switch {
	case err != nil:
		return err
	case b.Password != "":
		log.Warn("created the first administrator with a generated password; it is shown only now, change it after the first login",
			"username", b.User.Username, "password", b.Password)
	case b.Created:
		log.Info("created the first administrator", "username", b.User.Username)
	}
	return nil
}

// collabTick is how often idle locks of live sessions are checked.
const collabTick = 5 * time.Second

// runCollab lapses idle locks of live sessions (ADR-0010) on every tick.
// When ctx ends it shuts the sessions down, committing open drafts, so the
// server's graceful shutdown is not held up by event streams.
func runCollab(ctx context.Context, hub *collab.Hub, tick <-chan time.Time) {
	for {
		select {
		case <-ctx.Done():
			hub.Shutdown()
			return
		case <-tick:
			hub.Tick()
		}
	}
}

// expireAudit removes audit entries past their retention (ADR-0029) on
// every tick until ctx ends; Open already did so at the start.
func expireAudit(ctx context.Context, db *storage.DB, log *slog.Logger, tick <-chan time.Time) {
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-tick:
			n, err := db.ExpireAudit(now)
			switch {
			case err != nil:
				log.Error("audit entries not expired", "err", err)
			case n > 0:
				log.Info("expired audit entries", "removed", n)
			}
		}
	}
}

// ssoFromEnv configures OpenID Connect (ADR-0030) from the environment, or
// returns nil when WWBB_OIDC_ISSUER is unset. The client secret is read
// only from the environment, never from a flag, so it does not show in the
// process list.
func ssoFromEnv() (*sso.Provider, error) {
	issuer := os.Getenv("WWBB_OIDC_ISSUER")
	if issuer == "" {
		return nil, nil
	}
	return sso.New(sso.Config{
		Issuer:       issuer,
		ClientID:     os.Getenv("WWBB_OIDC_CLIENT_ID"),
		ClientSecret: os.Getenv("WWBB_OIDC_CLIENT_SECRET"),
		RedirectURL:  os.Getenv("WWBB_OIDC_REDIRECT_URL"),
		Name:         os.Getenv("WWBB_OIDC_NAME"),
	}, nil)
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
	if rec.Erased > 0 {
		fmt.Fprintf(stdout, "%d events have erased content (purged or redacted items)\n", rec.Erased)
	}
	if rec.Torn > 0 {
		fmt.Fprintf(stdout, "an interrupted write of %d bytes at the end will be dropped on the next start\n", rec.Torn)
	}
	return 0
}

// restore unpacks a backup made through POST /api/backup (ADR-0034) into a
// new data directory. The passphrase comes from WWBB_BACKUP_PASSPHRASE or a
// file, never from a flag, which would show in the process list and the
// shell history.
func restore(args []string, stdout, stderr io.Writer) int {
	fs := newFlags("restore", stderr)
	from := fs.String("from", "", "the backup file")
	dataDir := fs.String("data-dir", defaultDataDir, "the new data directory; must not exist or be empty")
	passFile := fs.String("passphrase-file", "", "file whose first line is the passphrase (default: $WWBB_BACKUP_PASSPHRASE)")
	if err := fs.Parse(args); err != nil {
		return parseFailed(err)
	}
	if *from == "" {
		fmt.Fprintln(stderr, "restore: -from is required")
		return 2
	}
	pass := os.Getenv("WWBB_BACKUP_PASSPHRASE")
	if *passFile != "" {
		data, err := os.ReadFile(*passFile)
		if err != nil {
			fmt.Fprintln(stderr, "restore:", err)
			return 1
		}
		line, _, _ := strings.Cut(string(data), "\n")
		pass = strings.TrimRight(line, "\r")
	}
	if pass == "" {
		fmt.Fprintln(stderr, "restore: set WWBB_BACKUP_PASSPHRASE or -passphrase-file")
		return 2
	}
	f, err := os.Open(*from)
	if err != nil {
		fmt.Fprintln(stderr, "restore:", err)
		return 1
	}
	defer f.Close()
	m, err := backup.Restore(f, pass, *dataDir)
	if err != nil {
		fmt.Fprintln(stderr, "restore:", err)
		return 1
	}
	fmt.Fprintf(stdout, "restored a backup of %s (%s) into %s\n", m.Created.Format(time.RFC3339), m.Build, *dataDir)
	return 0
}

// healthcheck lets a container runtime probe the server without a shell or
// curl in the image.
// mcpStdio is the MCP adapter for a local agent process (ADR-0012): one
// JSON-RPC message per line on in, the answers on out. Every tool call is a
// request to the server at -url with the API token in WWBB_TOKEN; the token
// is read from the environment, not a flag, so it does not show in the
// process list.
func mcpStdio(ctx context.Context, args []string, in io.Reader, out, stderr io.Writer) int {
	fs := newFlags("mcp", stderr)
	url := fs.String("url", "http://127.0.0.1:8080", "the server's base URL")
	if err := fs.Parse(args); err != nil {
		return parseFailed(err)
	}
	token := strings.TrimSpace(os.Getenv("WWBB_TOKEN"))
	if token == "" {
		fmt.Fprintln(stderr, "mcp: set WWBB_TOKEN to an API token (POST /api/tokens); the agent acts with its rights")
		return 2
	}
	api := mcp.HTTPAPI{Base: *url, Header: http.Header{"Authorization": {"Bearer " + token}}}
	if err := mcp.Serve(ctx, api, in, out); err != nil {
		fmt.Fprintln(stderr, "mcp:", err)
		return 1
	}
	return 0
}

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
