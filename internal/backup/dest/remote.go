package dest

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)

// maxListing bounds what the server reads of a destination's listing or
// error answer: a listing of backups is small, and a destination that
// sends more is not trusted to fill memory.
const maxListing = 32 << 20

// tlsConfig is the TLS configuration for a server: the system's roots,
// or the certificates in the file CAFileEnv names, e.g. the certificate
// of a NAS that signs its own.
func tlsConfig(env Env, serverName string) (*tls.Config, error) {
	cfg := &tls.Config{ServerName: serverName, MinVersion: tls.VersionTLS12}
	if file := strings.TrimSpace(env(CAFileEnv)); file != "" {
		data, err := os.ReadFile(file)
		if err != nil {
			return nil, fmt.Errorf("dest: %s: %w", CAFileEnv, err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(data) {
			return nil, fmt.Errorf("dest: %s holds no certificate in PEM form", CAFileEnv)
		}
		cfg.RootCAs = pool
	}
	return cfg, nil
}

// httpClient talks to a WebDAV or S3 server. It has no overall timeout:
// an upload takes as long as the backup; each call's context ends it.
func httpClient(cfg *tls.Config) *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			DialContext:           (&net.Dialer{Timeout: dialTimeout}).DialContext,
			TLSClientConfig:       cfg,
			TLSHandshakeTimeout:   dialTimeout,
			ResponseHeaderTimeout: 5 * time.Minute,
			ForceAttemptHTTP2:     true,
		},
		// A redirect would carry the credentials elsewhere.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

// statusError describes an answer a destination should not have given,
// with what the server said and, for the common causes, what to check.
func statusError(kind, op, name string, resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
	msg := fmt.Sprintf("%s: %s %s: %s", kind, op, name, resp.Status)
	switch resp.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		msg += " — check the credentials in the server's environment and the rights at the destination"
	case http.StatusNotFound, http.StatusConflict:
		msg += " — check that the folder exists"
	case http.StatusLengthRequired:
		msg += " — the server does not accept uploads of a length not known in advance"
	}
	if text := strings.TrimSpace(string(body)); text != "" && len(text) < 500 && !strings.ContainsAny(text, "<>") {
		msg += ": " + text
	}
	return errors.New(msg)
}

// ctxConn closes a connection when its context ends, so a blocked read
// or write returns.
func ctxConn(ctx context.Context, c net.Conn) (stop func() bool) {
	return context.AfterFunc(ctx, func() { c.Close() })
}
