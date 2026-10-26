package connector

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/dbuchs7/wikiwikibangbang/internal/item"
	"github.com/dbuchs7/wikiwikibangbang/internal/migrate"
	"github.com/dbuchs7/wikiwikibangbang/internal/site"
)

// atlassianSite answers the requests of the Atlassian Cloud readers: one
// Confluence space with two pages, the second readable only by acc-anna
// and a group, and one Jira project with one issue assigned to acc-anna.
func atlassianSite(t *testing.T) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if u, p, _ := r.BasicAuth(); u != "anna@acme.example" || p != "tok" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		answer := func(v any) { _ = json.NewEncoder(w).Encode(v) }
		page := func(id, title, parent string) map[string]any {
			p := map[string]any{"id": id, "title": title, "version": map[string]any{"number": 1},
				"body": map[string]any{"storage": map[string]any{"value": "<p>" + title + "</p>"}}}
			if parent != "" {
				p["parentId"], p["parentType"] = parent, "page"
			}
			return p
		}
		none := map[string]any{"results": []any{}}
		switch {
		case r.URL.Path == "/wiki/api/v2/spaces":
			answer(map[string]any{"results": []any{map[string]any{"id": "7", "key": "ENG", "name": "Engineering"}}})
		case r.URL.Path == "/wiki/api/v2/pages":
			answer(map[string]any{"results": []any{page("1", "Home", ""), page("2", "Private", "1")}})
		case r.URL.Path == "/wiki/rest/api/content/2/restriction/byOperation/read":
			answer(map[string]any{"restrictions": map[string]any{"user": map[string]any{"results": []any{map[string]any{"accountId": "acc-anna"}}},
				"group": map[string]any{"results": []any{map[string]any{"name": "dev team"}}}}})
		case strings.HasPrefix(r.URL.Path, "/wiki/rest/api/content/"):
			answer(map[string]any{"restrictions": map[string]any{"user": none, "group": none}})
		case r.URL.Path == "/rest/api/3/field":
			answer([]any{map[string]any{"id": "assignee", "name": "Assignee", "schema": map[string]any{"type": "user"}}})
		case r.URL.Path == "/rest/api/3/project/PLAT":
			answer(map[string]any{"key": "PLAT", "name": "Platform"})
		case r.URL.Path == "/rest/api/3/project/PLAT/statuses":
			answer([]any{map[string]any{"name": "Task", "statuses": []any{
				map[string]any{"name": "Open", "statusCategory": map[string]any{"key": "new"}},
				map[string]any{"name": "Done", "statusCategory": map[string]any{"key": "done"}}}}})
		case r.URL.Path == "/rest/api/3/search/jql":
			answer(map[string]any{"issues": []any{map[string]any{"id": "10001", "key": "PLAT-1", "fields": map[string]any{
				"summary": "Rotate keys", "issuetype": map[string]any{"name": "Task"}, "status": map[string]any{"name": "Open"},
				"assignee": map[string]any{"accountId": "acc-anna"}}}}})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// atlassianSetup is a fixture with an Atlassian connector "acme" to the
// fake site and a local account anna.
func atlassianSetup(t *testing.T) (*fixture, site.Connector, string) {
	t.Helper()
	f := setup(t)
	anna, err := f.db.Accounts.CreateUser("Anna", "a long enough password")
	if err != nil {
		t.Fatal(err)
	}
	srv := atlassianSite(t)
	c := site.ConnectorTemplates()[2]
	c.ID, c.Name, c.Enabled = "acme", "Acme", true
	c.Atlassian.Site, c.Atlassian.Spaces, c.Atlassian.Projects = srv.URL, []string{"ENG"}, []string{"PLAT"}
	c.Atlassian.Users = map[string]string{"acc-anna": "anna"}
	c.Atlassian.Groups = map[string]string{"dev team": "@engineering"}
	c.Atlassian.Grants = []string{"@engineering=editor", "anna=space-admin"}
	if c, err = f.db.Site.SetConnector("admin", c); err != nil {
		t.Fatal(err)
	}
	f.r.getenv = allowPrivate(func(k string) string {
		return map[string]string{EmailVar("acme"): "anna@acme.example", TokenVar("acme"): "tok"}[k]
	})
	return f, c, string(anna.ID)
}

func TestAnAtlassianSiteIsImportedByAConnector(t *testing.T) {
	f, _, anna := atlassianSetup(t)
	ctx := context.Background()
	dry, err := f.r.Run(ctx, "acme", true, "admin")
	if err != nil || dry.Datasets != 3 || dry.New != 3 || dry.Entries != 3 {
		t.Fatalf("dry run %+v %v", dry, err)
	}
	f.db.Read(func(s *item.Store) {
		if s.HasSpace("ENG") || s.HasSpace("PLAT") {
			t.Error("the dry run wrote")
		}
	})
	res, err := f.r.Run(ctx, "acme", false, "admin")
	if err != nil || res.New != 3 || res.Changed+res.Unchanged != 0 {
		t.Fatalf("run %+v %v", res, err)
	}
	var pages, issues []item.Item
	f.db.Read(func(s *item.Store) {
		pages, issues = s.Find(item.Query{Space: "ENG"}), s.Find(item.Query{Space: "PLAT"})
		for _, space := range []string{"ENG", "PLAT"} {
			if s.GrantedRole(space, "@engineering") != item.Editor || s.GrantedRole(space, anna) != item.SpaceAdmin {
				t.Errorf("roles in %s not granted", space)
			}
		}
	})
	if len(pages) != 2 || len(issues) != 1 || issues[0].Key != "PLAT-1" || issues[0].Properties["assignee"] != anna {
		t.Fatalf("pages %+v\nissues %+v", pages, issues)
	}
	for _, it := range append(pages, issues...) {
		if it.Managed != "sync:acme" || it.Origin.System != "confluence" && it.Origin.System != "jira" {
			t.Errorf("%s is managed by %q, from %q", it.Key, it.Managed, it.Origin.System)
		}
		if want := []string{anna, "@engineering"}; it.Title == "Private" && !slices.Equal(it.Restriction.Read, want) {
			t.Errorf("the restriction names %v, want %v", it.Restriction.Read, want)
		}
	}
	// People may not change what the connector keeps in step.
	if err := f.db.Write(func(s *item.Store) error { return s.As(anna).SetTitle(pages[0].ID, "Mine") }); err == nil {
		t.Error("a managed page was changed")
	}
	again, err := f.r.Run(ctx, "acme", false, "admin")
	if err != nil || again.Unchanged != 3 || again.New+again.Changed != 0 {
		t.Errorf("a second run %+v %v", again, err)
	}

	// The cut-over: the items become people's, and the schedule stops.
	spaces, err := f.r.ReleaseAtlassian("acme", "admin")
	if err != nil || !slices.Equal(spaces, []string{"ENG", "PLAT"}) {
		t.Fatalf("released %v %v", spaces, err)
	}
	if c, _ := f.db.Site.Connector("acme"); c.Enabled {
		t.Error("the schedule runs on after the cut-over")
	}
	if err := f.db.Write(func(s *item.Store) error { return s.As(anna).SetTitle(pages[0].ID, "Mine") }); err != nil {
		t.Errorf("a released page: %v", err)
	}
	if _, err := f.r.ReleaseAtlassian(id, "admin"); !errors.Is(err, ErrNotManaging) {
		t.Errorf("a CKAN connector released: %v", err)
	}
	if _, err := f.r.ReleaseAtlassian("nope", "admin"); !errors.Is(err, ErrUnknown) {
		t.Errorf("no connector: %v", err)
	}
}

func TestAConnectorContinuesTheImportCommand(t *testing.T) {
	f, c, _ := atlassianSetup(t)
	ctx := context.Background()
	// The import command wrote the site before, as "import".
	snap, _, err := migrate.ReadCloud(ctx, c.Atlassian.Site, "anna@acme.example", "tok", nil, c.Atlassian.Spaces, c.Atlassian.Projects)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.db.Write(func(s *item.Store) error {
		_, err := migrate.Run(s, snap, migrate.Derive(snap), migrate.Options{Actor: "import"})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	res, err := f.r.Run(ctx, "acme", false, "admin")
	if err != nil || res.New != 0 || res.Changed != 3 {
		t.Fatalf("the connector took the items over: %+v %v", res, err)
	}
}

func TestAtlassianRunsThatCannotWork(t *testing.T) {
	f, c, _ := atlassianSetup(t)
	ctx := context.Background()
	f.r.getenv = allowPrivate(func(string) string { return "" })
	if _, err := f.r.Run(ctx, "acme", true, "admin"); !errors.Is(err, ErrNoCredential) || !strings.Contains(err.Error(), "WWBB_CONNECTOR_ACME_EMAIL") {
		t.Errorf("no credential: %v", err)
	}
	if st := f.r.Status("acme"); st.EmailVar != "WWBB_CONNECTOR_ACME_EMAIL" || st.Email || st.Credential {
		t.Errorf("status %+v", st)
	}
	f.r.getenv = allowPrivate(func(k string) string {
		return map[string]string{EmailVar("acme"): "anna@acme.example", TokenVar("acme"): "wrong"}[k]
	})
	if _, err := f.r.Run(ctx, "acme", true, "admin"); err == nil || !strings.Contains(err.Error(), "401") {
		t.Errorf("a wrong token: %v", err)
	}
	f.r.getenv = allowPrivate(func(k string) string {
		return map[string]string{EmailVar("acme"): "anna@acme.example", TokenVar("acme"): "tok"}[k]
	})
	c.Atlassian.Users = map[string]string{"acc-anna": "nobody"}
	f.db.Site.SetConnector("admin", c)
	if _, err := f.r.Run(ctx, "acme", true, "admin"); err == nil || !strings.Contains(err.Error(), `no local account "nobody"`) {
		t.Errorf("an unknown username: %v", err)
	}
	if err := CheckAccounts(f.db.Accounts, c); err == nil {
		t.Error("an unknown username is not found on saving")
	}
	c.Atlassian.Users, c.Atlassian.Grants = nil, []string{"nobody=viewer"}
	f.db.Site.SetConnector("admin", c)
	if _, err := f.r.Run(ctx, "acme", true, "admin"); err == nil || !strings.Contains(err.Error(), `"nobody" (named in a grant)`) {
		t.Errorf("an unknown username in a grant: %v", err)
	}
	if CheckAccounts(f.db.Accounts, site.Connector{}) != nil {
		t.Error("a connector of another kind")
	}
}

func TestManyProblemsAreCounted(t *testing.T) {
	f, c, _ := atlassianSetup(t)
	var snap migrate.Snapshot
	snap.Instance = "acme.example"
	for i := range problemsShown + 5 {
		snap.Pages = append(snap.Pages, migrate.SrcPage{ID: strings.Repeat("x", i+1), Space: "NOPE", Title: "t"})
	}
	res, err := writeAtlassian(f.db, c, snap, true)
	if err != nil || len(res.Problems) != problemsShown || !strings.HasPrefix(res.Problems[problemsShown-1], "and ") {
		t.Fatalf("%d problems, the last %q: %v", len(res.Problems), res.Problems[len(res.Problems)-1], err)
	}
}
