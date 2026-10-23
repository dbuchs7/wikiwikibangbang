// Command wikiwikibangbang is the product's single binary (ADR-0024): the
// server, the API and the embedded web UI in one executable, for every
// supported platform and as a container image.
//
//	wikiwikibangbang serve [-addr :8080] [-data-dir data]
//	wikiwikibangbang check [-data-dir data]
//	wikiwikibangbang import -from SOURCE -site URL [-spaces KEY,...] [-projects KEY,...] [-data-dir data]
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
	"net/netip"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/dbuchs7/wikiwikibangbang/internal/autobackup"
	"github.com/dbuchs7/wikiwikibangbang/internal/backup"
	"github.com/dbuchs7/wikiwikibangbang/internal/backup/dest"
	"github.com/dbuchs7/wikiwikibangbang/internal/budget"
	"github.com/dbuchs7/wikiwikibangbang/internal/buildinfo"
	"github.com/dbuchs7/wikiwikibangbang/internal/collab"
	"github.com/dbuchs7/wikiwikibangbang/internal/connector"
	"github.com/dbuchs7/wikiwikibangbang/internal/item"
	"github.com/dbuchs7/wikiwikibangbang/internal/linkcheck"
	"github.com/dbuchs7/wikiwikibangbang/internal/mcp"
	"github.com/dbuchs7/wikiwikibangbang/internal/notify"
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
  import       import spaces from another product (ADR-0023)
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
	case "import":
		return importCmd(ctx, args, stdout, stderr)
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
	go cleanAlbum(ctx, db, log, dailyAt(ctx, albumCleanHour, time.Now))

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

	registration := false
	switch v := os.Getenv("WWBB_OAUTH_DYNAMIC_REGISTRATION"); v {
	case "", "off":
	case "on":
		registration = true
		log.Info("dynamic OAuth client registration is open; applications that register themselves are marked so on the consent page")
	default:
		log.Error("WWBB_OAUTH_DYNAMIC_REGISTRATION is unusable", "value", v, "want", "on or off")
		return 1
	}

	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Error("cannot listen", "addr", *addr, "err", err)
		return 1
	}
	providers, err := ssoFromEnv(os.Getenv)
	if err != nil {
		log.Error("single sign-on is misconfigured", "err", err)
		return 1
	}
	for _, p := range providers {
		log.Info("single sign-on enabled", "provider", p.ID(), "issuer", p.Issuer(), "domains", strings.Join(p.Domains(), ","))
		if p.Kind() == "google" && len(p.Domains()) == 0 {
			log.Warn("any Google account may sign in and gets an account without roles; set WWBB_GOOGLE_DOMAINS to your domains")
		}
	}
	live := server.NewCollab(db)
	liveCtx, stopLive := context.WithCancel(ctx)
	liveDone := make(chan struct{})
	go func() { runCollab(liveCtx, live, time.NewTicker(collabTick).C); close(liveDone) }()
	// Runs before the data directory closes: open drafts are committed.
	defer func() { stopLive(); <-liveDone }()
	notifier, every, err := notifierFromEnv(os.Getenv)
	if err != nil {
		log.Error("message delivery is misconfigured", "err", err)
		return 1
	}
	st := notifier.Status()
	log.Info("message delivery", "mail", st.Mail, "mail-host", st.MailHost, "public-url", st.PublicURL, "digest", every)
	messages := server.NewMessenger(notifier, db, log, every)
	if every > 0 {
		digests := time.NewTicker(every)
		defer digests.Stop()
		digestCtx, stopDigest := context.WithCancel(ctx)
		digestDone := make(chan struct{})
		go func() { messages.Run(digestCtx, digests.C); close(digestDone) }()
		// Runs before the data directory closes.
		defer func() { stopDigest(); <-digestDone; messages.Wait() }()
	}
	// Automatic backups (ADR-0073) follow the schedule set under
	// Administration; a backup running at shutdown is abandoned before the
	// data directory closes, and the destination keeps no half of it.
	backups := autobackup.New(db, os.Getenv, buildinfo.Get().String(), log)
	backupCtx, stopBackups := context.WithCancel(ctx)
	backupDone := make(chan struct{})
	go func() { backups.Loop(backupCtx); close(backupDone) }()
	defer func() { stopBackups(); <-backupDone; backups.Close() }()
	// The scan for sensitive data is done ahead, in the background, so the
	// first page after a start does not wait for it (ADR-0081).
	go db.WarmUp(ctx)
	// Connectors keep catalog spaces in step with their sources, such as
	// opendata.swiss, on the schedules set under Administration (ADR-0077).
	catalog := connector.New(db, log)
	catalogCtx, stopCatalog := context.WithCancel(ctx)
	catalogDone := make(chan struct{})
	go func() { catalog.Loop(catalogCtx); close(catalogDone) }()
	defer func() { stopCatalog(); <-catalogDone; catalog.Close() }()
	routes := server.Routes(server.Deps{UI: web.FS(), DB: db, Log: log, SSO: providers, Collab: live, LinkCheck: links, Messages: messages, OAuthRegistration: registration, Backups: backups, Connectors: catalog})
	if err := server.Serve(ctx, ln, server.BehindProxies(proxies, server.WithPublicURL(notifier.PublicURL(), server.Handler(routes, db.Accounts))), log); err != nil {
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
			// A commit in a live session may have restricted its item
			// (ADR-0040); participants who may no longer read it leave.
			hub.Revalidate()
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

// albumCleanHour is the hour of the server's local time (TZ, UTC in the
// container unless set) at which the automatic clean-up of older versions
// of pictures runs (ADR-0105): a fixed hour, so restarts do not move or
// skip it.
const albumCleanHour = 3

// nextAt returns the first time at hour:00 in now's location after now.
func nextAt(now time.Time, hour int) time.Time {
	at := time.Date(now.Year(), now.Month(), now.Day(), hour, 0, 0, 0, now.Location())
	if !at.After(now) {
		at = time.Date(now.Year(), now.Month(), now.Day()+1, hour, 0, 0, 0, now.Location())
	}
	return at
}

// dailyAt sends the time once a day at hour:00 local time, until ctx ends.
func dailyAt(ctx context.Context, hour int, now func() time.Time) <-chan time.Time {
	ch := make(chan time.Time)
	go func() {
		for {
			t := time.NewTimer(nextAt(now(), hour).Sub(now()))
			select {
			case <-ctx.Done():
				t.Stop()
				return
			case at := <-t.C:
				select {
				case ch <- at:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return ch
}

// cleanAlbum removes, once a day at albumCleanHour while the album's
// default says automatic, the older versions of pictures their rules do
// not keep (ADR-0104, ADR-0105).
func cleanAlbum(ctx context.Context, db *storage.DB, log *slog.Logger, tick <-chan time.Time) {
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-tick:
			if !db.Site.AlbumRetention().Automatic {
				continue
			}
			done, err := server.CleanAlbum(db, item.RetentionActor, now)
			switch {
			case err != nil:
				log.Error("older versions of pictures not cleaned up", "err", err)
			case len(done) > 0:
				log.Info("cleaned up older versions of pictures", "pictures", len(done))
			}
		}
	}
}

// notifierFromEnv configures message delivery (ADR-0064). Mail is sent
// when WWBB_SMTP_HOST is set; links in messages and the password reset by
// mail need WWBB_PUBLIC_URL. WWBB_DIGEST_MINUTES sets how often the digest
// of notifications goes out, 15 by default, 0 for never.
// WWBB_WEBHOOKS_PRIVATE=on lets webhooks reach addresses in private
// networks, for a chat server in the intranet.
func notifierFromEnv(getenv func(string) string) (*notify.Notifier, time.Duration, error) {
	cfg := notify.Config{PublicURL: getenv("WWBB_PUBLIC_URL")}
	switch v := getenv("WWBB_WEBHOOKS_PRIVATE"); v {
	case "", "off":
	case "on":
		// Private networks, never loopback or link-local (where cloud
		// metadata services answer).
		cfg.AllowedHook = func(a netip.Addr) bool { return a.Unmap().IsGlobalUnicast() }
	default:
		return nil, 0, fmt.Errorf("WWBB_WEBHOOKS_PRIVATE is %q, want on or off", v)
	}
	if host := getenv("WWBB_SMTP_HOST"); host != "" {
		security := getenv("WWBB_SMTP_SECURITY")
		if security == "" {
			security = notify.SecurityStartTLS
		}
		port := 587
		if security == notify.SecurityTLS {
			port = 465
		}
		if v := getenv("WWBB_SMTP_PORT"); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil {
				return nil, 0, fmt.Errorf("WWBB_SMTP_PORT is %q, want a number", v)
			}
			port = n
		}
		cfg.Mail = &notify.MailConfig{Host: host, Port: port, Security: security, Username: getenv("WWBB_SMTP_USERNAME"),
			Password: getenv("WWBB_SMTP_PASSWORD"), From: getenv("WWBB_SMTP_FROM")}
	}
	every := 15 * time.Minute
	if v := getenv("WWBB_DIGEST_MINUTES"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			return nil, 0, fmt.Errorf("WWBB_DIGEST_MINUTES is %q, want minutes from 0", v)
		}
		every = time.Duration(n) * time.Minute
	}
	n, err := notify.New(cfg)
	return n, every, err
}

// ssoFromEnv configures single sign-on (ADR-0030, ADR-0066): any of a
// generic OpenID Connect provider (WWBB_OIDC_*), Microsoft Entra ID
// (WWBB_ENTRA_*) and Google (WWBB_GOOGLE_*), offered in that order. The
// callback address is WWBB_OIDC_REDIRECT_URL, else WWBB_PUBLIC_URL with
// /api/oidc/callback; it is the same for every provider. Client secrets
// come from the environment only, never a flag.
func ssoFromEnv(getenv func(string) string) ([]*sso.Provider, error) {
	redirect := getenv("WWBB_OIDC_REDIRECT_URL")
	if redirect == "" && getenv("WWBB_PUBLIC_URL") != "" {
		redirect = strings.TrimRight(getenv("WWBB_PUBLIC_URL"), "/") + "/api/oidc/callback"
	}
	domains := func(v string) []string {
		var out []string
		for _, d := range strings.Split(v, ",") {
			if d = strings.TrimSpace(d); d != "" {
				out = append(out, d)
			}
		}
		return out
	}
	var cfgs []sso.Config
	if issuer := getenv("WWBB_OIDC_ISSUER"); issuer != "" {
		cfgs = append(cfgs, sso.Config{ID: "sso", Issuer: issuer, ClientID: getenv("WWBB_OIDC_CLIENT_ID"), ClientSecret: getenv("WWBB_OIDC_CLIENT_SECRET"),
			Name: getenv("WWBB_OIDC_NAME"), Domains: domains(getenv("WWBB_OIDC_DOMAINS"))})
	}
	if tenant := getenv("WWBB_ENTRA_TENANT_ID"); tenant != "" || getenv("WWBB_ENTRA_CLIENT_ID") != "" {
		// The issuer of a tenant's v2.0 endpoint names its ID; a domain
		// name or "common" would not match the issuer in its tokens.
		if !entraTenant.MatchString(tenant) {
			return nil, fmt.Errorf("WWBB_ENTRA_TENANT_ID must be the tenant's ID (a GUID such as 72f988bf-86f1-41af-91ab-2d7cd011db47), not %q", tenant)
		}
		cfgs = append(cfgs, sso.Config{ID: "entra", Kind: "microsoft", Name: "Microsoft",
			Issuer:   "https://login.microsoftonline.com/" + strings.ToLower(tenant) + "/v2.0",
			ClientID: getenv("WWBB_ENTRA_CLIENT_ID"), ClientSecret: getenv("WWBB_ENTRA_CLIENT_SECRET"),
			Domains: domains(getenv("WWBB_ENTRA_DOMAINS")), UsernameIsVerified: true})
	}
	if id := getenv("WWBB_GOOGLE_CLIENT_ID"); id != "" {
		cfgs = append(cfgs, sso.Config{ID: "google", Kind: "google", Name: "Google", Issuer: "https://accounts.google.com",
			ClientID: id, ClientSecret: getenv("WWBB_GOOGLE_CLIENT_SECRET"), Domains: domains(getenv("WWBB_GOOGLE_DOMAINS"))})
	}
	var out []*sso.Provider
	for _, c := range cfgs {
		if c.ID != "sso" && c.ClientSecret == "" {
			return nil, fmt.Errorf("%s: the client secret is required", c.Name)
		}
		c.RedirectURL = redirect
		p, err := sso.New(c, nil)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", c.ID, err)
		}
		out = append(out, p)
	}
	return out, nil
}

// entraTenant is a tenant ID.
var entraTenant = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

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

// restore unpacks a backup into a new data directory: one made through
// POST /api/backup (ADR-0034), or an automatic one with the backups it
// builds on (ADR-0073). The passphrase comes from WWBB_BACKUP_PASSPHRASE or
// a file, never from a flag, which would show in the process list and the
// shell history.
func restore(args []string, stdout, stderr io.Writer) int {
	fs := newFlags("restore", stderr)
	var from fileFlags
	fs.Var(&from, "from", "a backup file, repeated for a full backup and the incremental ones after it, or a directory or remote destination of automatic backups (https://, s3://, ftps://, sftp://; credentials from the environment as for the server)")
	at := fs.String("at", "", "with -from a directory or destination: the backup to restore, by file name (default: the newest)")
	dataDir := fs.String("data-dir", defaultDataDir, "the new data directory; must not exist or be empty")
	passFile := fs.String("passphrase-file", "", "file whose first line is the passphrase (default: $WWBB_BACKUP_PASSPHRASE)")
	if err := fs.Parse(args); err != nil {
		return parseFailed(err)
	}
	if len(from) == 0 {
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
	var paths []string
	var chain []backup.Source
	if _, isDir := dest.Directory(from[0]); len(from) == 1 && !isDir && dest.Check(from[0]) == nil {
		// A remote destination of automatic backups (ADR-0073), with its
		// credentials from the environment as for the server.
		tg, err := dest.Open(from[0], os.Getenv)
		var names []string
		if err == nil {
			names, err = tg.List(context.Background())
		}
		if err == nil {
			paths, err = pickChain(names, *at, from[0])
		}
		if err != nil {
			fmt.Fprintln(stderr, "restore:", err)
			return 1
		}
		for _, name := range paths {
			chain = append(chain, func() (io.ReadCloser, error) { return tg.Open(context.Background(), name) })
		}
	} else {
		var err error
		if paths, err = chainOf(from, *at); err != nil {
			fmt.Fprintln(stderr, "restore:", err)
			return 1
		}
		for _, p := range paths {
			chain = append(chain, func() (io.ReadCloser, error) { return os.Open(p) })
		}
	}
	m, err := backup.RestoreChain(chain, pass, *dataDir)
	if err != nil {
		fmt.Fprintln(stderr, "restore:", err)
		return 1
	}
	if len(paths) > 1 {
		fmt.Fprintf(stdout, "applied %d backups: %s\n", len(paths), strings.Join(paths, ", "))
	}
	fmt.Fprintf(stdout, "restored a backup of %s (%s) into %s\n", m.Created.Format(time.RFC3339), m.Build, *dataDir)
	return 0
}

// fileFlags collects repeated -from values.
type fileFlags []string

func (f *fileFlags) String() string { return strings.Join(*f, ",") }

func (f *fileFlags) Set(v string) error {
	*f = append(*f, v)
	return nil
}

// chainOf names the files to restore, in order. Several files are taken
// as given. A directory gives the newest automatic backup in it, or the
// one at names, with the backups it builds on; so does an incremental
// backup given alone, from its own directory.
func chainOf(from []string, at string) ([]string, error) {
	if len(from) > 1 {
		if at != "" {
			return nil, errors.New("-at goes with -from a directory")
		}
		return from, nil
	}
	dir, want := from[0], at
	fi, err := os.Stat(dir)
	if err != nil {
		return nil, err
	}
	if !fi.IsDir() {
		if at != "" {
			return nil, errors.New("-at goes with -from a directory")
		}
		n, ok := backup.ParseName(filepath.Base(dir))
		if !ok || n.Seq == 0 {
			return from, nil
		}
		dir, want = filepath.Dir(dir), n.File
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	picked, err := pickChain(names, want, dir)
	if err != nil {
		return nil, err
	}
	var paths []string
	for _, name := range picked {
		paths = append(paths, filepath.Join(dir, name))
	}
	return paths, nil
}

// pickChain names the backups to restore among the files of a
// destination, in order: the newest backup, or the one want names, with
// the backups it builds on.
func pickChain(names []string, want, dir string) ([]string, error) {
	versions := backup.Versions(names)
	if len(versions) == 0 {
		return nil, fmt.Errorf("%s holds no automatic backups", dir)
	}
	v, upTo := versions[len(versions)-1], -1
	if want != "" {
		for _, c := range versions {
			for i, b := range c.Backups {
				if b.File == want {
					v, upTo = c, i
				}
			}
		}
		if upTo < 0 {
			return nil, fmt.Errorf("%s is not an automatic backup in %s", want, dir)
		}
	} else {
		upTo = len(v.Backups) - 1
	}
	var out []string
	for i, b := range v.Backups[:upTo+1] {
		if b.Seq != i {
			return nil, fmt.Errorf("the backups before %s are not all in %s; backup %d of its chain is missing", v.Backups[upTo].File, dir, i)
		}
		out = append(out, b.File)
	}
	return out, nil
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
