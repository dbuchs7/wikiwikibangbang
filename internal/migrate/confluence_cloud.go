package migrate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// ConfluenceCloud reads spaces and pages from a Confluence Cloud site
// through its REST API (ADR-0023): spaces and pages with their storage
// format through API v2, page restrictions through API v1. It produces the
// Confluence part of a Snapshot.
//
// Restrictions are read conservatively: when the restriction of a page
// cannot be read or understood, the page is restricted to a principal no
// one holds, so the import never shows a page to more people than the
// source did; the report says so.
type ConfluenceCloud struct {
	// Site is the site's address, e.g. https://example.atlassian.net.
	Site string
	// Email and Token are an Atlassian account's e-mail address and API
	// token; the token is sent only to Site.
	Email, Token string
	// Client sends the requests; nil means http.DefaultClient.
	Client *http.Client
}

// pageLimit is the page size the reader asks for; the API caps it.
const pageLimit = 250

// maxSourceAnswer bounds one answer of the source site the import reads.
const maxSourceAnswer = 64 << 20

// cloudRetries bounds how often a rate-limited request is retried.
const cloudRetries = 3

// unreadableRestriction is the placeholder account a page is restricted to
// when its restriction cannot be read.
const unreadableRestriction = "restriction-unreadable"

// Read reads the given spaces, by key. Problems that do not stop the read
// (a page whose restriction is unreadable, a page under a non-page parent)
// are returned with the snapshot.
func (c ConfluenceCloud) Read(ctx context.Context, spaceKeys []string) (Snapshot, []Problem, error) {
	base, err := url.Parse(strings.TrimRight(c.Site, "/"))
	if err != nil || base.Scheme != "https" && base.Scheme != "http" || base.Host == "" {
		return Snapshot{}, nil, fmt.Errorf("confluence cloud: %q is not a site address", c.Site)
	}
	r := cloudReader{c: c, base: base}
	snap := Snapshot{Instance: base.Host}
	if len(spaceKeys) == 0 {
		return snap, nil, errors.New("confluence cloud: name at least one space")
	}

	var spaces struct {
		Results []struct{ ID, Key, Name string } `json:"results"`
	}
	q := url.Values{"keys": {strings.Join(spaceKeys, ",")}, "limit": {strconv.Itoa(pageLimit)}}
	if err := r.get(ctx, "/wiki/api/v2/spaces?"+q.Encode(), &spaces); err != nil {
		return snap, nil, err
	}
	ids := map[string]string{}
	for _, sp := range spaces.Results {
		ids[sp.Key] = sp.ID
		snap.Spaces = append(snap.Spaces, SrcSpace{Key: sp.Key, Name: sp.Name})
	}
	for _, k := range spaceKeys {
		if ids[k] == "" {
			return snap, nil, fmt.Errorf("confluence cloud: space %s not found, or not readable with this token", k)
		}
	}

	users := map[string]bool{}
	for _, sp := range snap.Spaces {
		next := "/wiki/api/v2/pages?" + url.Values{
			"space-id": {ids[sp.Key]}, "body-format": {"storage"}, "status": {"current"}, "limit": {strconv.Itoa(pageLimit)},
		}.Encode()
		for next != "" {
			var chunk struct {
				Results []json.RawMessage `json:"results"`
				Links   struct {
					Next string `json:"next"`
				} `json:"_links"`
			}
			if err := r.get(ctx, next, &chunk); err != nil {
				return snap, r.problems, err
			}
			for _, raw := range chunk.Results {
				p, err := r.page(ctx, sp.Key, raw, users)
				if err != nil {
					return snap, r.problems, err
				}
				snap.Pages = append(snap.Pages, p)
			}
			next = chunk.Links.Next
		}
	}
	for id := range users {
		snap.Users = append(snap.Users, SrcUser{ID: id})
	}
	return snap, r.problems, nil
}

type cloudReader struct {
	c        ConfluenceCloud
	base     *url.URL
	problems []Problem
}

type cloudPage struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	ParentID   string `json:"parentId"`
	ParentType string `json:"parentType"`
	AuthorID   string `json:"authorId"`
	Version    struct {
		Number int `json:"number"`
	} `json:"version"`
	Body struct {
		Storage struct {
			Value string `json:"value"`
		} `json:"storage"`
	} `json:"body"`
}

func (r *cloudReader) page(ctx context.Context, space string, raw json.RawMessage, users map[string]bool) (SrcPage, error) {
	var p cloudPage
	if err := json.Unmarshal(raw, &p); err != nil {
		return SrcPage{}, fmt.Errorf("confluence cloud: reading a page: %w", err)
	}
	ref := "confluence page " + p.ID + " (" + p.Title + ")"
	parent := ""
	switch {
	case p.ParentID == "" || p.ParentType == "":
	case p.ParentType == "page":
		parent = p.ParentID
	default:
		r.problem(ref, "its parent is a %s, which is not imported; the page is placed at the top of the space", p.ParentType)
	}
	if p.AuthorID != "" {
		users[p.AuthorID] = true
	}
	read, err := r.restriction(ctx, p.ID, "read", ref)
	if err != nil {
		return SrcPage{}, err
	}
	edit, err := r.restriction(ctx, p.ID, "update", ref)
	if err != nil {
		return SrcPage{}, err
	}
	for _, a := range append(read, edit...) {
		users[a] = true
	}
	return SrcPage{
		ID: p.ID, Space: space, Title: p.Title, Parent: parent, Body: p.Body.Storage.Value,
		ReadAllowList: read, EditAllowList: edit,
		Version: strconv.Itoa(p.Version.Number), Raw: string(raw),
	}, nil
}

// restriction returns the accounts a page restricts an operation to, or
// nil when it is not restricted. Groups cannot be mapped yet and become
// placeholder accounts: the page is then restricted to its listed users
// only, which is narrower than the source.
func (r *cloudReader) restriction(ctx context.Context, pageID, op, ref string) ([]string, error) {
	var res struct {
		Restrictions *struct {
			User *struct {
				Results []struct {
					AccountID string `json:"accountId"`
				} `json:"results"`
			} `json:"user"`
			Group *struct {
				Results []struct {
					Name string `json:"name"`
				} `json:"results"`
			} `json:"group"`
		} `json:"restrictions"`
	}
	path := fmt.Sprintf("/wiki/rest/api/content/%s/restriction/byOperation/%s?expand=restrictions.user,restrictions.group",
		url.PathEscape(pageID), op)
	err := r.get(ctx, path, &res)
	var status statusError
	switch {
	case errors.As(err, &status) && status.code != http.StatusTooManyRequests:
		r.problem(ref, "the %s restriction could not be read (%v); the page is restricted to no one until it is checked", op, err)
		return []string{unreadableRestriction}, nil
	case err != nil:
		return nil, err
	case res.Restrictions == nil:
		r.problem(ref, "the %s restriction has an unexpected form; the page is restricted to no one until it is checked", op)
		return []string{unreadableRestriction}, nil
	}
	var out []string
	if u := res.Restrictions.User; u != nil {
		for _, a := range u.Results {
			if a.AccountID != "" {
				out = append(out, a.AccountID)
			}
		}
	}
	if g := res.Restrictions.Group; g != nil {
		for _, grp := range g.Results {
			out = append(out, "group:"+grp.Name)
			r.problem(ref, "its %s restriction names the group %q, which is not mapped; members of the group lose access until it is", op, grp.Name)
		}
	}
	return out, nil
}

func (r *cloudReader) problem(source, format string, args ...any) {
	r.problems = append(r.problems, Problem{Source: source, Message: fmt.Sprintf(format, args...)})
}

// statusError is an answer other than 200.
type statusError struct {
	code int
	body string
}

func (e statusError) Error() string { return fmt.Sprintf("status %d: %s", e.code, e.body) }

// get fetches a path of the site (an absolute path, as the API's next links
// are) and decodes the JSON answer. A rate-limited request is retried after
// the delay the site asks for.
func (r *cloudReader) get(ctx context.Context, path string, v any) error {
	ref, err := url.Parse(path)
	// Only paths of the site are followed: an absolute or scheme-relative
	// link ("//other.example/…") would send the token to another host.
	if err != nil || ref.IsAbs() || ref.Host != "" || !strings.HasPrefix(ref.Path, "/") {
		return fmt.Errorf("confluence cloud: unexpected link %q", path)
	}
	target := r.base.ResolveReference(ref)
	client := r.c.Client
	if client == nil {
		client = http.DefaultClient
	}
	for attempt := 0; ; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
		if err != nil {
			return err
		}
		req.SetBasicAuth(r.c.Email, r.c.Token)
		req.Header.Set("Accept", "application/json")
		res, err := client.Do(req)
		if err != nil {
			return fmt.Errorf("confluence cloud: %w", err)
		}
		body, err := io.ReadAll(io.LimitReader(res.Body, maxSourceAnswer))
		res.Body.Close()
		if err != nil {
			return fmt.Errorf("confluence cloud: reading %s: %w", target.Path, err)
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
			msg := strings.TrimSpace(string(body))
			if len(msg) > 200 {
				msg = msg[:200]
			}
			return fmt.Errorf("confluence cloud: GET %s: %w", target.Path, statusError{res.StatusCode, msg})
		}
		if err := json.Unmarshal(body, v); err != nil {
			return fmt.Errorf("confluence cloud: GET %s: %w", target.Path, err)
		}
		return nil
	}
}
