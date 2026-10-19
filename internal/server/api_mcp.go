package server

import (
	"context"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/http/httptest"

	"github.com/dbuchs7/wikiwikibangbang/internal/budget"
	"github.com/dbuchs7/wikiwikibangbang/internal/mcp"
)

// MCPPath is the Model Context Protocol endpoint (ADR-0012). It is an
// authenticated route like any other; a token that may only read may post
// to it, because every tool call is a request of its own that the gate
// judges again.
const MCPPath = "/mcp"

func mcpRoutes() []Route {
	return []Route{{Method: http.MethodPost, Pattern: MCPPath,
		Summary: "Model Context Protocol: JSON-RPC tools over this API, acting with the caller's credential (ADR-0012)",
		Handler: http.HandlerFunc(serveMCP)}}
}

// serveMCP answers one JSON-RPC message. Each tool call becomes a request
// to this server's own handler — through the principal resolution, the gate
// and the route table, exactly like a request from outside — carrying the
// caller's Authorization and Cookie headers and nothing else. The adapter
// has no credential of its own to lend.
func serveMCP(w http.ResponseWriter, r *http.Request) {
	// As for every JSON body: requiring the media type is the CSRF defence
	// of cookie sessions (ADR-0027).
	if mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || mt != "application/json" {
		writeError(w, http.StatusUnsupportedMediaType, errUnsupportedMedia.Error())
		return
	}
	msg, err := io.ReadAll(http.MaxBytesReader(w, r.Body, budget.Get().RequestBody))
	if err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			writeError(w, http.StatusRequestEntityTooLarge, "the message exceeds the request budget")
			return
		}
		writeError(w, http.StatusBadRequest, "the message could not be read")
		return
	}
	self, _ := r.Context().Value(selfKey{}).(http.Handler)
	if self == nil {
		writeError(w, http.StatusInternalServerError, "the MCP endpoint is not mounted in a handler")
		return
	}
	credential := http.Header{}
	for _, k := range []string{"Authorization", "Cookie"} {
		if v := r.Header.Values(k); len(v) > 0 {
			credential[k] = v
		}
	}
	api := mcp.HTTPAPI{
		Base:   "http://in-process",
		Header: credential,
		Client: &http.Client{Transport: inProcess{h: self, remoteAddr: r.RemoteAddr}},
	}
	answer, reply := mcp.Handle(r.Context(), api, msg)
	if !reply {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(answer)
}

// inProcess delivers a request to the server's own handler without a
// socket.
type inProcess struct {
	h          http.Handler
	remoteAddr string
}

func (t inProcess) RoundTrip(req *http.Request) (*http.Response, error) {
	// The inner request starts without the outer request's context values,
	// so its principal comes from the credential headers alone, never from
	// the request that carried the tool call. Cancellation still passes.
	req = req.WithContext(withoutValues{req.Context()})
	req.RemoteAddr = t.remoteAddr
	req.RequestURI = req.URL.RequestURI()
	rec := httptest.NewRecorder()
	t.h.ServeHTTP(rec, req)
	return rec.Result(), nil
}

// withoutValues keeps a context's deadline and cancellation but none of its
// values.
type withoutValues struct{ context.Context }

func (withoutValues) Value(any) any { return nil }

type selfKey struct{}

// withSelf puts the complete handler into each request's context, so the
// MCP endpoint can call the API through it.
func withSelf(self *http.Handler, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), selfKey{}, *self)))
	})
}
