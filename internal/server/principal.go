package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"

	"github.com/dbuchs7/wikiwikibangbang/internal/auth"
	"github.com/dbuchs7/wikiwikibangbang/internal/budget"
)

// SessionCookie carries a login session (ADR-0027).
const SessionCookie = "wwbb_session"

type principalKey struct{}

// PrincipalFrom returns the request's principal, if it has one.
func PrincipalFrom(ctx context.Context) (auth.Principal, bool) {
	p, ok := ctx.Value(principalKey{}).(auth.Principal)
	return p, ok
}

// principals resolves every request to a principal: an API token in the
// Authorization header, or a session cookie. A request carrying a token that
// does not resolve gets no principal, not the cookie's: a client that sends
// a credential means that one.
func principals(a *auth.Accounts, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if a != nil {
			if p, ok := resolve(a, r); ok {
				r = r.WithContext(context.WithValue(r.Context(), principalKey{}, p))
			}
		}
		next.ServeHTTP(w, r)
	})
}

func resolve(a *auth.Accounts, r *http.Request) (auth.Principal, bool) {
	if h := r.Header.Get("Authorization"); h != "" {
		secret, ok := strings.CutPrefix(h, "Bearer ")
		if !ok {
			return auth.Principal{}, false
		}
		secret = strings.TrimSpace(secret)
		if strings.HasPrefix(secret, auth.OAuthAccessPrefix) {
			return oauthPrincipal(a, r, secret)
		}
		return a.TokenPrincipal(secret)
	}
	if c, err := r.Cookie(SessionCookie); err == nil {
		return a.Session(c.Value)
	}
	return auth.Principal{}, false
}

// oauthPrincipal resolves an OAuth access token (ADR-0068). It acts as its
// person on the MCP endpoint, for which it must have been issued, and on
// the requests the MCP adapter makes in process for its tool calls;
// anywhere else it is no credential at all.
func oauthPrincipal(a *auth.Accounts, r *http.Request, secret string) (auth.Principal, bool) {
	p, target, ok := a.OAuthPrincipal(secret)
	if !ok {
		return auth.Principal{}, false
	}
	if r.Context().Value(inProcessKey{}) == true {
		return p, true
	}
	if r.Method != http.MethodPost || r.URL.Path != MCPPath || (target != "" && target != resource(r)) {
		return auth.Principal{}, false
	}
	return p, true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// errUnsupportedMedia makes readJSON's caller answer 415.
var errUnsupportedMedia = errors.New("the request body must be application/json")

// readJSON decodes a bounded JSON body into v, refusing unknown fields.
// Requiring application/json is also the CSRF defence of cookie sessions:
// a cross-site form cannot send it without a CORS preflight, which this
// server never grants (ADR-0027).
func readJSON(w http.ResponseWriter, r *http.Request, v any) error {
	mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mt != "application/json" {
		return errUnsupportedMedia
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, budget.Get().RequestBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return err
	}
	if dec.More() {
		return errors.New("the body holds more than one JSON value")
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return errors.New("trailing data after the JSON value")
	}
	return nil
}

// badRequest answers a readJSON failure.
func badRequest(w http.ResponseWriter, err error) {
	if errors.Is(err, errUnsupportedMedia) {
		writeError(w, http.StatusUnsupportedMediaType, err.Error())
		return
	}
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		writeError(w, http.StatusRequestEntityTooLarge, "request body too large")
		return
	}
	writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
}
