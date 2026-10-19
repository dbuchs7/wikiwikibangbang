package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/dbuchs7/wikiwikibangbang/internal/item"
	"github.com/dbuchs7/wikiwikibangbang/internal/storage"
)

// fakeSite answers the requests of the Atlassian Cloud readers: one
// Confluence space with two pages, the second readable only by acc-anna,
// and one Jira project with one issue assigned to acc-anna.
func fakeSite(t *testing.T) *httptest.Server {
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
			answer(map[string]any{"restrictions": map[string]any{"user": map[string]any{"results": []any{map[string]any{"accountId": "acc-anna"}}}, "group": none}})
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

func TestImportFromConfluenceCloud(t *testing.T) {
	srv := fakeSite(t)
	dir := t.TempDir()
	db, _, err := storage.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	anna, err := db.Accounts.CreateUser("Anna", "a long enough password")
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	users := filepath.Join(t.TempDir(), "users")
	if err := os.WriteFile(users, []byte("# source account, local username\nacc-anna anna\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WWBB_IMPORT_EMAIL", "anna@acme.example")
	t.Setenv("WWBB_IMPORT_TOKEN", "tok")
	args := []string{"import", "-from", "atlassian-cloud", "-site", srv.URL, "-spaces", "ENG", "-projects", "PLAT", "-data-dir", dir, "-users", users}

	code, out, errOut := runCmd(t, context.Background(), append(args, "-dry-run")...)
	if code != 0 || !strings.Contains(out, "dry run, nothing written: 3 pages and issues created") {
		t.Fatalf("dry run: exit %d, %q, %q", code, out, errOut)
	}
	code, out, errOut = runCmd(t, context.Background(), args...)
	if code != 0 || !strings.Contains(out, "imported: 3 pages and issues created") {
		t.Fatalf("import: exit %d, %q, %q", code, out, errOut)
	}
	code, out, _ = runCmd(t, context.Background(), args...)
	if code != 0 || !strings.Contains(out, "0 pages and issues created, 0 updated, 3 unchanged") {
		t.Fatalf("re-run: exit %d, %q", code, out)
	}

	db, _, err = storage.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var items, issues []item.Item
	db.Read(func(s *item.Store) {
		items, issues = s.Find(item.Query{Space: "ENG"}), s.Find(item.Query{Space: "PLAT"})
	})
	if len(items) != 2 || len(issues) != 1 {
		t.Fatalf("%d items in ENG and %d in PLAT, want 2 and 1 (the dry run wrote nothing)", len(items), len(issues))
	}
	if got := issues[0].Properties["assignee"]; got != string(anna.ID) {
		t.Errorf("assignee %v, want the local account %s", got, anna.ID)
	}
	for _, it := range items {
		if it.Title == "Private" && !slices.Equal(it.Restriction.Read, []string{string(anna.ID)}) {
			t.Errorf("the restriction names %v, want the local account %s", it.Restriction.Read, anna.ID)
		}
	}
}

func TestImportRefusesUnsafeOrIncompleteInput(t *testing.T) {
	srv := fakeSite(t)
	dir := t.TempDir()
	users := filepath.Join(t.TempDir(), "users")
	if err := os.WriteFile(users, []byte("acc-anna nobody\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	base := []string{"import", "-from", "atlassian-cloud", "-spaces", "ENG", "-data-dir", dir}
	cases := []struct {
		env  bool
		args []string
		code int
		want string
	}{
		{true, []string{"import", "-from", "jira-server", "-site", srv.URL, "-spaces", "ENG"}, 2, "-from must be"},
		{true, append(slices.Clone(base), "-site", ""), 2, "required"},
		{true, []string{"import", "-from", "atlassian-cloud", "-site", srv.URL, "-data-dir", dir}, 2, "-projects are required"},
		{true, append(slices.Clone(base), "-site", "http://wiki.example"), 2, "only over https"},
		{false, append(slices.Clone(base), "-site", srv.URL), 2, "WWBB_IMPORT_TOKEN"},
		{true, append(slices.Clone(base), "-site", srv.URL, "-users", users), 1, `no local account "nobody"`},
	}
	for _, c := range cases {
		if c.env {
			t.Setenv("WWBB_IMPORT_EMAIL", "anna@acme.example")
			t.Setenv("WWBB_IMPORT_TOKEN", "tok")
		} else {
			t.Setenv("WWBB_IMPORT_TOKEN", "")
		}
		code, _, errOut := runCmd(t, context.Background(), c.args...)
		if code != c.code || !strings.Contains(errOut, c.want) {
			t.Errorf("%v: exit %d, %q; want %d and %q", c.args, code, errOut, c.code, c.want)
		}
	}
}
