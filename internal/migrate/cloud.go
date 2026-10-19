package migrate

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// pageLimit is the page size the readers ask for; the APIs cap it.
const pageLimit = 250

// maxSourceAnswer bounds one answer of the source site the import reads.
const maxSourceAnswer = 64 << 20

// cloudRetries bounds how often a rate-limited request is retried.
const cloudRetries = 3

// cloudSite sends the requests of a reader to one Atlassian Cloud site,
// with an account's e-mail address and API token.
type cloudSite struct {
	label        string // prefixes errors, e.g. "jira cloud"
	base         *url.URL
	email, token string
	client       *http.Client
}

// siteURL parses a site address.
func siteURL(label, site string) (*url.URL, error) {
	base, err := url.Parse(strings.TrimRight(site, "/"))
	if err != nil || base.Scheme != "https" && base.Scheme != "http" || base.Host == "" {
		return nil, fmt.Errorf("%s: %q is not a site address", label, site)
	}
	return base, nil
}

// statusError is an answer other than 200.
type statusError struct {
	code int
	body string
}

func (e statusError) Error() string { return fmt.Sprintf("status %d: %s", e.code, e.body) }

// do sends a request for a path of the site (an absolute path, as the APIs'
// next links are), with body as JSON unless it is nil, and decodes the JSON
// answer. A rate-limited request is retried after the delay the site asks
// for.
func (c cloudSite) do(ctx context.Context, method, path string, body, v any) error {
	ref, err := url.Parse(path)
	// Only paths of the site are followed: an absolute or scheme-relative
	// link ("//other.example/…") would send the token to another host.
	if err != nil || ref.IsAbs() || ref.Host != "" || !strings.HasPrefix(ref.Path, "/") {
		return fmt.Errorf("%s: unexpected link %q", c.label, path)
	}
	var payload []byte
	if body != nil {
		var err error
		if payload, err = json.Marshal(body); err != nil {
			return err
		}
	}
	target := c.base.ResolveReference(ref)
	client := c.client
	if client == nil {
		client = http.DefaultClient
	}
	for attempt := 0; ; attempt++ {
		req, err := http.NewRequestWithContext(ctx, method, target.String(), bytes.NewReader(payload))
		if err != nil {
			return err
		}
		req.SetBasicAuth(c.email, c.token)
		req.Header.Set("Accept", "application/json")
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		res, err := client.Do(req)
		if err != nil {
			return fmt.Errorf("%s: %w", c.label, err)
		}
		answer, err := io.ReadAll(io.LimitReader(res.Body, maxSourceAnswer))
		res.Body.Close()
		if err != nil {
			return fmt.Errorf("%s: reading %s: %w", c.label, target.Path, err)
		}
		if res.StatusCode == http.StatusTooManyRequests && attempt < cloudRetries {
			delay := time.Second << attempt
			if s, err := strconv.Atoi(res.Header.Get("Retry-After")); err == nil && s >= 0 && s <= 60 {
				delay = time.Duration(s) * time.Second
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}
			continue
		}
		if res.StatusCode != http.StatusOK {
			msg := strings.TrimSpace(string(answer))
			if len(msg) > 200 {
				msg = msg[:200]
			}
			return fmt.Errorf("%s: %s %s: %w", c.label, method, target.Path, statusError{res.StatusCode, msg})
		}
		if err := json.Unmarshal(answer, v); err != nil {
			return fmt.Errorf("%s: %s %s: %w", c.label, method, target.Path, err)
		}
		return nil
	}
}
