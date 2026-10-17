// Package server is the HTTP side of the single binary (ADR-0024): one route
// table (ADR-0011), the embedded UI (ADR-0009) and a server with timeouts and
// graceful shutdown.
//
// Authentication (ADR-0013) is not built yet. Until it is, every route that is
// not declared public is refused, so nothing can be served unprotected by
// forgetting a declaration.
package server

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"
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
	Handler http.Handler
}

// Routes is the product's route table.
func Routes(ui fs.FS) []Route {
	files := http.FileServerFS(ui)
	return []Route{
		{Method: http.MethodGet, Pattern: "/healthz", Summary: "Liveness probe; reveals nothing but that the process serves", Access: Public, Handler: http.HandlerFunc(healthz)},
		{Method: http.MethodGet, Pattern: "/{$}", Summary: "Web UI entry point", Access: Public, Handler: index(ui)},
		{Method: http.MethodGet, Pattern: "/assets/", Summary: "Web UI assets", Access: Public, Handler: noListing(files)},
	}
}

func healthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte("ok\n"))
}

func index(ui fs.FS) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeFileFS(w, r, ui, "index.html")
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

// Handler registers the route table on one mux. Every route passes the access
// gate, and every response carries the security headers.
func Handler(routes []Route) http.Handler {
	mux := http.NewServeMux()
	for _, rt := range routes {
		mux.Handle(rt.Method+" "+rt.Pattern, gate(rt.Access, rt.Handler))
	}
	return securityHeaders(mux)
}

func gate(a Access, h http.Handler) http.Handler {
	if a == Public {
		return h
	}
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// No principal can be resolved before ADR-0013 is built.
		http.Error(w, "authentication required", http.StatusUnauthorized)
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
