// Package server is the HTTP side of the single binary (ADR-0024): one route
// table (ADR-0011), the embedded UI (ADR-0009) and a server with timeouts and
// graceful shutdown.
//
// Every request is resolved to a principal, or none, before routing
// (ADR-0013, ADR-0027). A route that is not declared public refuses requests
// without a principal, so nothing is served unprotected by forgetting a
// declaration.
package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"html"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/dbuchs7/wikiwikibangbang/internal/auth"
	"github.com/dbuchs7/wikiwikibangbang/internal/autobackup"
	"github.com/dbuchs7/wikiwikibangbang/internal/buildinfo"
	"github.com/dbuchs7/wikiwikibangbang/internal/collab"
	"github.com/dbuchs7/wikiwikibangbang/internal/connector"
	"github.com/dbuchs7/wikiwikibangbang/internal/engine"
	"github.com/dbuchs7/wikiwikibangbang/internal/item"
	"github.com/dbuchs7/wikiwikibangbang/internal/metrics"
	"github.com/dbuchs7/wikiwikibangbang/internal/sso"
	"github.com/dbuchs7/wikiwikibangbang/internal/storage"
)

// Access is a route's access class (ADR-0011). The zero value is
// Authenticated, so a route without a declaration is gated, not open.
type Access int

const (
	Authenticated Access = iota
	Public
)

func (a Access) String() string {
	if a == Public {
		return "public"
	}
	return "authenticated"
}

// Route is one entry of the route table.
type Route struct {
	Method  string
	Pattern string // net/http pattern without the method
	Summary string
	Access  Access
	// ReadScope lets a token of scope read call a route that is not a
	// GET or HEAD. It is the deliberate exception for watching, which
	// changes no item and no item's history (ADR-0046, ADR-0121); every
	// other change stays refused to such a token.
	ReadScope bool
	Handler   http.Handler
}

// Deps are what the routes serve from.
type Deps struct {
	UI fs.FS
	DB *storage.DB
	// Log receives operational errors; security events go to DB.Audit.
	Log *slog.Logger
	// SSO are the OpenID Connect providers, none when none is configured;
	// the login page offers them in this order (ADR-0030, ADR-0066).
	SSO []*sso.Provider
	// Collab holds the live editing sessions (ADR-0010); Routes creates
	// one over DB when it is nil. NewCollab makes one to share, for a
	// caller that runs its Tick.
	Collab *collab.Hub
	// Metrics is the metric registry (ADR-0016); Routes creates one when
	// it is nil.
	Metrics *metrics.Metrics
	// LinkCheck runs link checks (ADR-0039); nil means link checking is
	// switched off.
	LinkCheck *LinkChecks
	// Messages sends messages to people (ADR-0064); Routes creates one
	// that sends nothing when it is nil.
	Messages *Messenger
	// OAuthRegistration opens dynamic client registration (ADR-0068).
	OAuthRegistration bool
	// Backups takes automatic backups (ADR-0073); Routes creates one that
	// reads the process environment when it is nil.
	Backups *autobackup.Runner
	// Connectors keep catalog spaces in step with external sources
	// (ADR-0077); Routes creates one when it is nil.
	Connectors *connector.Runner
	// Engines reads processes from process engines (ADR-0128); Routes
	// creates one that reads credentials from the process environment
	// when it is nil.
	Engines *engine.Reader
}

// MetricsPath serves the operational metrics. It is the only path a token
// of scope metrics may reach.
const MetricsPath = "/metrics"

// NewMetrics returns a metric registry that reads the data directory and
// the live sessions.
func NewMetrics(db *storage.DB, hub *collab.Hub) *metrics.Metrics {
	src := metrics.Sources{Build: buildinfo.Get().String()}
	if db != nil {
		src.Storage = db.Sizes
		src.Items = func() map[string]int {
			out := map[string]int{}
			db.Read(func(s *item.Store) {
				for p, n := range s.Counts() {
					out[string(p)] = n
				}
			})
			return out
		}
	}
	if hub != nil {
		src.Live = hub.Stats
	}
	return metrics.New(src)
}

// NewCollab returns a live-session hub over the data directory.
func NewCollab(db *storage.DB) *collab.Hub { return collab.NewHub(storeBackend{db}) }

// Routes is the product's route table.
func Routes(d Deps) []Route {
	if d.Collab == nil && d.DB != nil {
		d.Collab = NewCollab(d.DB)
	}
	if d.Metrics == nil {
		d.Metrics = NewMetrics(d.DB, d.Collab)
	}
	if d.Messages == nil {
		d.Messages = NewMessenger(nil, d.DB, d.Log, 0)
	}
	if d.Backups == nil && d.DB != nil {
		d.Backups = autobackup.New(d.DB, os.Getenv, buildinfo.Get().String(), d.Log)
	}
	if d.Connectors == nil && d.DB != nil {
		d.Connectors = connector.New(d.DB, d.Log)
	}
	if d.Engines == nil {
		d.Engines = engine.NewReader(nil, nil)
	}
	files := http.FileServerFS(d.UI)
	routes := []Route{
		{Method: http.MethodGet, Pattern: "/healthz", Summary: "Liveness probe; reveals nothing but that the process serves", Access: Public, Handler: http.HandlerFunc(healthz)},
		{Method: http.MethodGet, Pattern: "/{$}", Summary: "Web UI entry point", Access: Public, Handler: index(d)},
		{Method: http.MethodGet, Pattern: "/assets/", Summary: "Web UI assets", Access: Public, Handler: noListing(files)},
		{Method: http.MethodGet, Pattern: "/favicon.ico", Summary: "The site icon, where browsers look for it without a link", Access: Public, Handler: files},
		{Method: http.MethodGet, Pattern: "/vendor/", Summary: "Vendored UI libraries and their licenses (ADR-0032)", Access: Public, Handler: noListing(files)},
		{Method: http.MethodGet, Pattern: "/draw", Summary: "The drawing editor, shown in a frame of the UI (ADR-0053)", Access: Public, Handler: drawPage(d.UI)},
	}
	routes = append(routes, authRoutes(d)...)
	routes = append(routes, accountRoutes(d)...)
	routes = append(routes, messageRoutes(d)...)
	routes = append(routes, spaceIconRoutes(d)...)
	routes = append(routes, kanbanIconRoutes(d)...)
	routes = append(routes, avatarRoutes(d)...)
	routes = append(routes, brandingRoutes(d)...)
	routes = append(routes, regulationRoutes(d)...)
	routes = append(routes, oidcRoutes(d)...)
	routes = append(routes, itemRoutes(d)...)
	routes = append(routes, mcpRoutes()...)
	routes = append(routes, oauthRoutes(d)...)
	routes = append(routes, autoBackupRoutes(d)...)
	routes = append(routes, connectorRoutes(d)...)
	routes = append(routes, engineRoutes(d)...)
	routes = append(routes, Route{Method: http.MethodGet, Pattern: MetricsPath, Summary: "Operational metrics in the Prometheus format (administrators and tokens of scope metrics)",
		Handler: metricsHandler(d)})
	doc := Route{Method: http.MethodGet, Pattern: "/api/openapi.json", Summary: "This API's description, generated from the route table"}
	routes = append(routes, doc)
	routes[len(routes)-1].Handler = openAPI(routes)
	for i, rt := range routes {
		// Event streams, downloads and the change feed's long polls run as
		// long as they take; their
		// durations would only blur the latency of everything else.
		long := rt.Pattern == "/api/items/{ref}/live" && rt.Method == http.MethodGet || rt.Pattern == "/api/backup" || rt.Pattern == "/api/changes"
		routes[i].Handler = d.Metrics.Instrument(rt.Pattern, long, rt.Handler)
	}
	return routes
}

// metricsHandler serves the metrics to administrators and to tokens of
// scope metrics; a scraper needs no other right.
func metricsHandler(d Deps) http.Handler {
	h := d.Metrics.Handler()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, _ := PrincipalFrom(r.Context())
		admin := false
		if d.DB != nil && p.Scope != auth.ScopeMetrics {
			d.DB.Read(func(s *item.Store) { admin = s.HasSystemRole(string(p.User), item.RoleAdmin) })
		}
		if p.Scope != auth.ScopeMetrics && !admin {
			writeError(w, http.StatusForbidden, "administrators and metrics tokens only")
			return
		}
		h.ServeHTTP(w, r)
	})
}

func healthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte("ok\n"))
}

// brandRef is a reference of the page to a logo or icon file.
var brandRef = regexp.MustCompile(`"/(assets/brand/[a-z0-9-]+\.(?:svg|png)|favicon\.ico)"`)

// versioned gives the page's logos and icons an address that names their
// content, so a browser that kept an earlier icon (browsers keep icons
// long, by address) asks for a changed one.
func versioned(ui fs.FS, page []byte) []byte {
	return brandRef.ReplaceAllFunc(page, func(ref []byte) []byte {
		name := string(ref[2 : len(ref)-1])
		data, err := fs.ReadFile(ui, name)
		if err != nil {
			return ref
		}
		sum := sha256.Sum256(data)
		return []byte(fmt.Sprintf(`"/%s?v=%s"`, name, hex.EncodeToString(sum[:6])))
	})
}

// index sends the UI's page, titled with the installation's name, so the
// browser's tab names the instance from the start (ADR-0097).
func index(d Deps) http.Handler {
	var once sync.Once
	var page []byte
	var err error
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		once.Do(func() {
			if page, err = fs.ReadFile(d.UI, "index.html"); err == nil {
				page = versioned(d.UI, page)
			}
		})
		if err != nil {
			http.Error(w, "the UI is missing", http.StatusInternalServerError)
			return
		}
		out := page
		if name := d.DB.Site.Design().Name; name != "" {
			out = bytes.Replace(page, []byte("<title>wikiwikibangbang</title>"), []byte("<title>"+html.EscapeString(name)+"</title>"), 1)
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		_, _ = w.Write(out)
	})
}

// DrawPolicy is the content security policy of the drawing page (ADR-0053).
// Excalidraw sets inline styles and shows pictures from data: and blob:
// addresses; scripts still come from this server only, and the page may be
// framed by the UI alone.
const DrawPolicy = "default-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; font-src 'self' data:; " +
	"worker-src 'self' blob:; object-src 'none'; base-uri 'none'; form-action 'none'; frame-ancestors 'self'"

func drawPage(ui fs.FS) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", DrawPolicy)
		http.ServeFileFS(w, r, ui, "draw.html")
	})
}

// noListing refuses directory paths, so the asset tree is not browsable.
func noListing(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/") {
			http.NotFound(w, r)
			return
		}
		h.ServeHTTP(w, r)
	})
}

// Handler registers the route table on one mux. Every request is resolved
// to a principal through accounts (nil: never), every route passes the
// access gate, and every response carries the security headers.
func Handler(routes []Route, accounts *auth.Accounts) http.Handler {
	mux := http.NewServeMux()
	for _, rt := range routes {
		mux.Handle(rt.Method+" "+rt.Pattern, gate(rt))
	}
	var h http.Handler
	h = securityHeaders(principals(accounts, withSelf(&h, mux)))
	return h
}

func gate(rt Route) http.Handler {
	h := rt.Handler
	if rt.Access == Public {
		return h
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, ok := PrincipalFrom(r.Context())
		if !ok {
			// The challenge leads an MCP client to the OAuth metadata
			// (RFC 9728, ADR-0068).
			w.Header().Set("WWW-Authenticate", challenge(r))
			writeError(w, http.StatusUnauthorized, "authentication required")
			return
		}
		// The MCP endpoint is posted to even by a reading agent; each tool
		// call it makes passes this gate on its own (ADR-0012).
		mcpCall := r.Method == http.MethodPost && r.URL.Path == MCPPath
		// A read token reads; it may also watch for its own principal
		// (rt.ReadScope, ADR-0121), which changes nothing anyone reads.
		if p.Scope == auth.ScopeRead && r.Method != http.MethodGet && r.Method != http.MethodHead && !mcpCall && !rt.ReadScope {
			writeError(w, http.StatusForbidden, "this token may only read")
			return
		}
		if p.Scope == auth.ScopeMetrics && (r.URL.Path != MetricsPath || r.Method != http.MethodGet) {
			writeError(w, http.StatusForbidden, "this token may only read the metrics")
			return
		}
		h.ServeHTTP(w, r)
	})
}

func securityHeaders(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hd := w.Header()
		hd.Set("Content-Security-Policy", "default-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'")
		hd.Set("X-Content-Type-Options", "nosniff")
		hd.Set("Referrer-Policy", "no-referrer")
		h.ServeHTTP(w, r)
	})
}

// ShutdownTimeout bounds how long Serve waits for running requests after its
// context ends.
const ShutdownTimeout = 10 * time.Second

// Serve answers requests on ln until ctx ends, then shuts down gracefully.
func Serve(ctx context.Context, ln net.Listener, h http.Handler, log *slog.Logger) error {
	srv := &http.Server{
		Handler:           h,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
		ErrorLog:          slog.NewLogLogger(log.Handler(), slog.LevelWarn),
	}
	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()
	log.Info("serving", "addr", ln.Addr().String())

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	log.Info("shutting down")
	sctx, cancel := context.WithTimeout(context.Background(), ShutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(sctx); err != nil {
		return err
	}
	if err := <-errc; !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
