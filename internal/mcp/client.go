package mcp

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"time"
)

// maxAnswer bounds how much of one API answer the adapter reads. The API's
// own budgets bound what it sends; this only guards the adapter against a
// server that misbehaves.
const maxAnswer = 16 << 20

// HTTPAPI calls the API at Base, sending Header with every request: the
// caller's Authorization or Cookie, and nothing of the adapter's own.
type HTTPAPI struct {
	Base   string
	Header http.Header
	Client *http.Client
}

// Call implements API.
func (a HTTPAPI) Call(ctx context.Context, method, path string, body []byte) (int, []byte, error) {
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimSuffix(a.Base, "/")+path, rd)
	if err != nil {
		return 0, nil, err
	}
	for k, vs := range a.Header {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	client := a.Client
	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	answer, err := io.ReadAll(io.LimitReader(resp.Body, maxAnswer))
	if err != nil {
		return 0, nil, err
	}
	return resp.StatusCode, answer, nil
}
