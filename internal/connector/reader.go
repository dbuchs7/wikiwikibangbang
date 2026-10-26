package connector

import (
	"context"
	"fmt"
	"net/http"

	"github.com/dbuchs7/wikiwikibangbang/internal/site"
)

// A Reader reads one kind of source into entries (ADR-0127). Scheduling,
// the dry run, applying entries to the space, withdrawing what the source
// dropped, the report and the audit trail are the same for every kind;
// a new kind of source is a new Reader and its settings.
type Reader interface {
	Read(ctx context.Context, c site.Connector, env Env) (reading, error)
}

// Env is what a reader is given besides the connector's settings: its
// credential from the environment, and the HTTP client to reach the
// source with, which keeps to public addresses unless allowed (guard.go).
type Env struct {
	Token string
	HTTP  *http.Client
	// CKAN makes the client of a CKAN API; tests replace it.
	CKAN func(base, token string) Client
}

// readers are the kinds of source this server reads, by kind.
var readers = map[string]Reader{
	site.KindCKAN: ckanReader{},
	site.KindJSON: jsonReader{},
}

// reader returns the reader of a connector's kind.
func reader(kind string) (Reader, error) {
	if r, ok := readers[kind]; ok {
		return r, nil
	}
	return nil, fmt.Errorf("connector: %q is not a kind this server reads", kind)
}

// ckanReader reads a CKAN source (ADR-0075, ADR-0077).
type ckanReader struct{}

func (ckanReader) Read(ctx context.Context, c site.Connector, env Env) (reading, error) {
	return readCKAN(ctx, c, env.CKAN(c.CKAN.BaseURL, env.Token))
}
