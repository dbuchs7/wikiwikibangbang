package migrate

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/dbuchs7/wikiwikibangbang/internal/item"
)

// fakeConfluence serves the parts of the Confluence Cloud REST API the
// reader uses, with two pages per answer so the reader has to follow the
// next links.
type fakeConfluence struct {
	t        *testing.T
	pages    []map[string]any
	restrict map[string]map[string]any // page ID + "/" + operation -> answer; missing: unrestricted
	broken   map[string]bool           // page ID + "/" + operation -> answer 404
	limited  atomic.Int32              // 429 answers still to give
	calls    atomic.Int32
}

func (f *fakeConfluence) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.calls.Add(1)
	if user, pass, ok := r.BasicAuth(); !ok || user != "anna@acme.example" || pass != "secret-token" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if f.limited.Load() > 0 {
		f.limited.Add(-1)
		w.Header().Set("Retry-After", "0")
		http.Error(w, "slow down", http.StatusTooManyRequests)
		return
	}
	q := r.URL.Query()
	answer := func(v any) { _ = json.NewEncoder(w).Encode(v) }
	switch {
	case r.URL.Path == "/wiki/api/v2/spaces":
		if q.Get("keys") != "ENG" {
			answer(map[string]any{"results": []any{}})
			return
		}
		answer(map[string]any{"results": []any{map[string]any{"id": "77", "key": "ENG", "name": "Engineering"}}})
	case r.URL.Path == "/wiki/api/v2/pages":
		if q.Get("space-id") != "77" || q.Get("body-format") != "storage" {
			f.t.Errorf("pages asked with %v", q)
		}
		start := 0
		fmt.Sscan(q.Get("cursor"), &start)
		end := min(start+2, len(f.pages))
		res := map[string]any{"results": f.pages[start:end], "_links": map[string]any{}}
		if end < len(f.pages) {
			res["_links"] = map[string]any{"next": fmt.Sprintf("/wiki/api/v2/pages?space-id=77&body-format=storage&cursor=%d", end)}
		}
		answer(res)
	case strings.HasPrefix(r.URL.Path, "/wiki/rest/api/content/"):
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/wiki/rest/api/content/"), "/")
		key := parts[0] + "/" + parts[len(parts)-1]
		if f.broken[key] {
			http.NotFound(w, r)
			return
		}
		if a, ok := f.restrict[key]; ok {
			answer(a)
			return
		}
		answer(map[string]any{"operation": parts[len(parts)-1], "restrictions": map[string]any{
			"user": map[string]any{"results": []any{}}, "group": map[string]any{"results": []any{}}}})
	default:
		http.NotFound(w, r)
	}
}

func cloudPageJSON(id, title, parent, parentType, body string, version int) map[string]any {
	p := map[string]any{"id": id, "status": "current", "title": title, "spaceId": "77", "authorId": "acc-anna",
		"version": map[string]any{"number": version}, "body": map[string]any{"storage": map[string]any{"representation": "storage", "value": body}}}
	if parent != "" {
		p["parentId"], p["parentType"] = parent, parentType
	}
	return p
}

func newFakeConfluence(t *testing.T) (*fakeConfluence, ConfluenceCloud) {
	f := &fakeConfluence{t: t, restrict: map[string]map[string]any{}, broken: map[string]bool{}}
	f.pages = []map[string]any{
		cloudPageJSON("100", "Home", "", "", "<p>Welcome</p>", 3),
		cloudPageJSON("101", "Secrets", "100", "page", "<p>Only for a few</p>", 1),
		cloudPageJSON("102", "Unknown access", "100", "page", "<p>Who may read this?</p>", 1),
		cloudPageJSON("103", "In a folder", "900", "folder", "<h2>Notes</h2><p>Filed away</p>", 2),
		cloudPageJSON("104", "Child", "101", "page", `<p>See <ac:link><ri:page ri:content-title="Home"/></ac:link></p>`, 1),
	}
	f.restrict["101/read"] = map[string]any{"operation": "read", "restrictions": map[string]any{
		"user":  map[string]any{"results": []any{map[string]any{"accountId": "acc-anna"}}},
		"group": map[string]any{"results": []any{map[string]any{"name": "devs"}}},
	}}
	f.broken["102/read"] = true
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	return f, ConfluenceCloud{Site: srv.URL, Email: "anna@acme.example", Token: "secret-token", Client: srv.Client()}
}

func TestConfluenceCloudReadsSpacesPagesAndRestrictions(t *testing.T) {
	f, c := newFakeConfluence(t)
	f.limited.Store(2) // the site asks the reader to slow down twice
	snap, problems, err := c.Read(context.Background(), []string{"ENG"})
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Spaces) != 1 || snap.Spaces[0] != (SrcSpace{Key: "ENG", Name: "Engineering"}) || !strings.HasPrefix(snap.Instance, "127.0.0.1:") {
		t.Fatalf("spaces %+v, instance %q", snap.Spaces, snap.Instance)
	}
	if len(snap.Pages) != 5 {
		t.Fatalf("%d pages, want 5 across three answers", len(snap.Pages))
	}
	byID := map[string]SrcPage{}
	for _, p := range snap.Pages {
		byID[p.ID] = p
	}
	if p := byID["100"]; p.Parent != "" || p.Body != "<p>Welcome</p>" || p.Version != "3" || p.Space != "ENG" || !strings.Contains(p.Raw, `"title":"Home"`) {
		t.Errorf("home %+v", p)
	}
	if p := byID["101"]; p.Parent != "100" || !slices.Equal(p.ReadAllowList, []string{"acc-anna", "group:devs"}) || p.EditAllowList != nil {
		t.Errorf("secrets %+v", p)
	}
	// An unreadable restriction restricts; it never opens the page.
	if p := byID["102"]; !slices.Equal(p.ReadAllowList, []string{unreadableRestriction}) {
		t.Errorf("unknown access %+v", p)
	}
	if p := byID["103"]; p.Parent != "" {
		t.Errorf("a page in a folder keeps a parent: %+v", p)
	}
	text := fmt.Sprint(problems)
	for _, want := range []string{"folder", `group "devs"`, "102", "restricted to no one"} {
		if !strings.Contains(text, want) {
			t.Errorf("problems lack %q: %v", want, problems)
		}
	}
	if len(snap.Users) == 0 || !slices.ContainsFunc(snap.Users, func(u SrcUser) bool { return u.ID == "acc-anna" }) {
		t.Errorf("users %+v", snap.Users)
	}
}

func TestConfluenceCloudFailsClearly(t *testing.T) {
	_, c := newFakeConfluence(t)
	if _, _, err := c.Read(context.Background(), []string{"NOPE"}); err == nil || !strings.Contains(err.Error(), "space NOPE not found") {
		t.Errorf("unknown space: %v", err)
	}
	bad := c
	bad.Token = "wrong"
	if _, _, err := bad.Read(context.Background(), []string{"ENG"}); err == nil || !strings.Contains(err.Error(), "401") {
		t.Errorf("wrong token: %v", err)
	}
	for _, site := range []string{"", "ftp://x", "not a url"} {
		if _, _, err := (ConfluenceCloud{Site: site}).Read(context.Background(), []string{"ENG"}); err == nil {
			t.Errorf("site %q accepted", site)
		}
	}
	if _, _, err := c.Read(context.Background(), nil); err == nil {
		t.Error("no spaces accepted")
	}
}

func TestConfluenceCloudRefusesForeignLinks(t *testing.T) {
	f, c := newFakeConfluence(t)
	f.pages = f.pages[:3]
	// A next link to another host would carry the token elsewhere.
	r := cloudReader{c: c}
	for _, link := range []string{"https://evil.example/wiki/api/v2/pages", "//evil.example/wiki/api/v2/pages", "wiki/api/v2/pages"} {
		if err := r.get(context.Background(), link, &struct{}{}); err == nil || !strings.Contains(err.Error(), "unexpected link") {
			t.Errorf("%s: %v", link, err)
		}
	}
}

func TestConfluenceCloudImportsEndToEnd(t *testing.T) {
	_, c := newFakeConfluence(t)
	snap, problems, err := c.Read(context.Background(), []string{"ENG"})
	if err != nil {
		t.Fatal(err)
	}
	s := item.NewStore()
	m := Derive(snap)
	rep, err := Run(s, snap, m, Options{Actor: "import"})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Created != 5 {
		t.Fatalf("report %+v (reader problems %v)", rep, problems)
	}
	items := s.Find(item.Query{Space: "ENG"})
	byTitle := map[string]item.Item{}
	for _, it := range items {
		byTitle[it.Title] = it
	}
	home, child, secrets := byTitle["Home"], byTitle["Child"], byTitle["Secrets"]
	if child.Parent != secrets.ID || secrets.Parent != home.ID {
		t.Fatalf("tree lost: %+v", items)
	}
	if len(child.Body.Blocks) == 0 || !slices.Contains(child.Body.Blocks[0].Mentions, home.ID) {
		t.Fatalf("the page link did not become a mention: %+v", child.Body)
	}
	// Restrictions name placeholders nobody holds: no one gains access.
	if r := byTitle["Unknown access"].Restriction.Read; len(r) != 1 || !strings.Contains(r[0], unreadableRestriction) {
		t.Fatalf("unknown access restriction %v", r)
	}
	if r := secrets.Restriction.Read; len(r) != 2 {
		t.Fatalf("secrets restriction %v", r)
	}
	// A second run changes nothing.
	again, err := Run(s, snap, m, Options{Actor: "import"})
	if err != nil || again.Created+again.Updated != 0 {
		t.Fatalf("re-run %+v, %v", again, err)
	}
}

func TestMapAccountsOverridesTheDerivedMapping(t *testing.T) {
	m := Derive(Snapshot{Users: []SrcUser{{ID: "acc-anna"}, {ID: "acc-bert"}}})
	if len(m.Notes) != 2 {
		t.Fatalf("notes %v", m.Notes)
	}
	mapped := m.MapAccounts(map[string]string{"acc-anna": "u-1"})
	if mapped.Users["acc-anna"] != "u-1" || mapped.Users["acc-bert"] != "external:acc-bert" {
		t.Errorf("users %v", mapped.Users)
	}
	if len(mapped.Notes) != 1 || !strings.Contains(mapped.Notes[0].Source, "acc-bert") {
		t.Errorf("notes %v", mapped.Notes)
	}
	if m.Users["acc-anna"] != "external:acc-anna" || len(m.Notes) != 2 {
		t.Error("MapAccounts changed the mapping it was called on")
	}
}
