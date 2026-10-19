package migrate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
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

// unreadableRestriction is the placeholder account a page is restricted to
// when its restriction cannot be read.
const unreadableRestriction = "restriction-unreadable"

// Read reads the given spaces, by key. Problems that do not stop the read
// (a page whose restriction is unreadable, a page under a non-page parent)
// are returned with the snapshot.
func (c ConfluenceCloud) Read(ctx context.Context, spaceKeys []string) (Snapshot, []Problem, error) {
	base, err := siteURL("confluence cloud", c.Site)
	if err != nil {
		return Snapshot{}, nil, err
	}
	r := cloudReader{site: cloudSite{label: "confluence cloud", base: base, email: c.Email, token: c.Token, client: c.Client}}
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
	site     cloudSite
	problems []Problem
}

func (r *cloudReader) get(ctx context.Context, path string, v any) error {
	return r.site.do(ctx, http.MethodGet, path, nil, v)
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
// nil when it is not restricted. A group becomes "group:<name>", which
// Mapping.Groups maps to a local group; unmapped, it is a placeholder no one
// holds, which is narrower than the source.
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
		}
	}
	return out, nil
}

func (r *cloudReader) problem(source, format string, args ...any) {
	r.problems = append(r.problems, Problem{Source: source, Message: fmt.Sprintf(format, args...)})
}
