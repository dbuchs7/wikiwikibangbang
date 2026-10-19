package migrate

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/dbuchs7/wikiwikibangbang/internal/item"
)

// fakeJira serves the parts of the Jira Cloud REST API the reader uses, one
// issue per answer, so the reader has to follow the page tokens.
type fakeJira struct {
	t      *testing.T
	issues []map[string]any
}

func (f *fakeJira) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if u, p, ok := r.BasicAuth(); !ok || u != "anna@acme.example" || p != "secret-token" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	answer := func(v any) { _ = json.NewEncoder(w).Encode(v) }
	switch {
	case r.URL.Path == "/rest/api/3/field":
		answer([]any{
			map[string]any{"id": "summary", "name": "Summary", "schema": map[string]any{"type": "string", "system": "summary"}},
			map[string]any{"id": "assignee", "name": "Assignee", "schema": map[string]any{"type": "user", "system": "assignee"}},
			map[string]any{"id": "priority", "name": "Priority", "schema": map[string]any{"type": "priority", "system": "priority"}},
			map[string]any{"id": "labels", "name": "Labels", "schema": map[string]any{"type": "array", "items": "string", "system": "labels"}},
			map[string]any{"id": "created", "name": "Created", "schema": map[string]any{"type": "datetime", "system": "created"}},
			map[string]any{"id": "watches", "name": "Watchers", "schema": map[string]any{"type": "watches", "system": "watches"}},
			map[string]any{"id": "customfield_10016", "name": "Story Points", "custom": true, "schema": map[string]any{"type": "number"}},
			map[string]any{"id": "customfield_10020", "name": "Team", "custom": true, "schema": map[string]any{"type": "option"}},
			map[string]any{"id": "customfield_10030", "name": "Notes", "custom": true, "schema": map[string]any{"type": "string"}},
		})
	case r.URL.Path == "/rest/api/3/project/PLAT":
		answer(map[string]any{"id": "1", "key": "PLAT", "name": "Platform"})
	case r.URL.Path == "/rest/api/3/project/PLAT/statuses":
		st := func(name, cat string) map[string]any {
			return map[string]any{"name": name, "statusCategory": map[string]any{"key": cat}}
		}
		answer([]any{
			map[string]any{"name": "Bug", "statuses": []any{st("Triage", "indeterminate"), st("Open", "new"), st("Done", "done")}},
			map[string]any{"name": "Sub-task", "subtask": true, "statuses": []any{st("Open", "new"), st("Done", "done")}},
		})
	case r.URL.Path == "/rest/api/3/search/jql" && r.Method == http.MethodPost:
		var req struct {
			JQL           string   `json:"jql"`
			Fields        []string `json:"fields"`
			NextPageToken string   `json:"nextPageToken"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.JQL != `project = "PLAT" ORDER BY created ASC` || !slices.Equal(req.Fields, []string{"*all"}) {
			f.t.Errorf("search %+v, %v", req, err)
		}
		i := 0
		fmt.Sscan(req.NextPageToken, &i)
		res := map[string]any{"issues": f.issues[i : i+1]}
		if i+1 < len(f.issues) {
			res["nextPageToken"] = fmt.Sprint(i + 1)
		}
		answer(res)
	default:
		http.NotFound(w, r)
	}
}

func jiraIssueJSON(id, key, typ, status, summary string, fields map[string]any) map[string]any {
	f := map[string]any{"summary": summary, "updated": "2026-10-01T10:00:00.000+0200",
		"issuetype": map[string]any{"name": typ}, "status": map[string]any{"name": status}, "watches": map[string]any{"watchCount": 3}}
	for k, v := range fields {
		f[k] = v
	}
	return map[string]any{"id": id, "key": key, "fields": f}
}

func newFakeJira(t *testing.T) (*fakeJira, JiraCloud) {
	f := &fakeJira{t: t}
	adf := func(text string) map[string]any {
		return map[string]any{"type": "doc", "version": 1, "content": []any{map[string]any{"type": "paragraph",
			"content": []any{map[string]any{"type": "text", "text": text}}}}}
	}
	f.issues = []map[string]any{
		jiraIssueJSON("10001", "PLAT-1", "Bug", "Open", "Login fails", map[string]any{
			"description":       adf("Steps to reproduce"),
			"assignee":          map[string]any{"accountId": "acc-anna", "displayName": "Anna"},
			"priority":          map[string]any{"name": "High"},
			"labels":            []any{"auth"},
			"created":           "2026-09-30T08:15:00.000+0200",
			"customfield_10016": 3,
			"customfield_10020": map[string]any{"value": "Core"},
			"customfield_10030": adf("rich notes"),
			"issuelinks": []any{map[string]any{"type": map[string]any{"name": "Blocks"},
				"outwardIssue": map[string]any{"id": "10002", "key": "PLAT-2"}}},
		}),
		jiraIssueJSON("10002", "PLAT-2", "Bug", "Done", "Session expires", map[string]any{
			"customfield_10020": map[string]any{"value": "Edge"},
			"issuelinks": []any{map[string]any{"type": map[string]any{"name": "Blocks"},
				"inwardIssue": map[string]any{"id": "10001", "key": "PLAT-1"}}},
		}),
		jiraIssueJSON("10003", "PLAT-3", "Sub-task", "Open", "Write a test", map[string]any{
			"parent": map[string]any{"id": "10001", "key": "PLAT-1"},
		}),
		jiraIssueJSON("10004", "PLAT-4", "Bug", "Open", "Under a foreign epic", map[string]any{
			"parent": map[string]any{"id": "99999", "key": "OTHER-1"},
		}),
	}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	return f, JiraCloud{Site: srv.URL, Email: "anna@acme.example", Token: "secret-token", Client: srv.Client()}
}

func TestJiraCloudReadsProjectsTypesAndIssues(t *testing.T) {
	_, j := newFakeJira(t)
	snap, problems, err := j.Read(context.Background(), []string{"PLAT"})
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Projects) != 1 || snap.Projects[0] != (SrcSpace{Key: "PLAT", Name: "Platform"}) || len(snap.Issues) != 4 {
		t.Fatalf("projects %+v, %d issues", snap.Projects, len(snap.Issues))
	}
	is := snap.Issues[0]
	if is.Description != "Steps to reproduce" || is.Status != "Open" || is.Type != "Bug" || is.Version != "2026-10-01T10:00:00.000+0200" {
		t.Errorf("PLAT-1 %+v", is)
	}
	wantFields := map[string]any{"assignee": "acc-anna", "priority": "High", "created": "2026-09-30T08:15:00+02:00",
		"customfield_10016": 3.0, "customfield_10020": "Core", "customfield_10030": "rich notes"}
	for k, v := range wantFields {
		if is.Fields[k] != v {
			t.Errorf("field %s = %#v, want %#v", k, is.Fields[k], v)
		}
	}
	if _, ok := is.Fields["watches"]; ok {
		t.Error("bookkeeping of the source imported as a property")
	}
	if !slices.Equal(is.Links, []SrcLink{{Type: "Blocks", Target: "10002"}}) || len(snap.Issues[1].Links) != 0 {
		t.Errorf("links %+v / %+v: each link must be read once, from its outward side", is.Links, snap.Issues[1].Links)
	}
	if snap.Issues[2].Parent != "10001" || snap.Issues[3].Parent != "" {
		t.Errorf("parents %q, %q", snap.Issues[2].Parent, snap.Issues[3].Parent)
	}
	var bug SrcIssueType
	for _, it := range snap.IssueTypes {
		if it.Name == "Bug" {
			bug = it
		}
	}
	if bug.Workflow.Initial != "Open" || !slices.Equal(bug.Workflow.Statuses, []string{"Triage", "Open", "Done"}) || len(bug.Workflow.Transitions) != 3 {
		t.Errorf("bug workflow %+v", bug.Workflow)
	}
	team := slices.IndexFunc(bug.Fields, func(f SrcField) bool { return f.ID == "customfield_10020" })
	if team < 0 || bug.Fields[team].Kind != "option" || !slices.Equal(bug.Fields[team].Options, []string{"Core", "Edge"}) {
		t.Errorf("bug fields %+v", bug.Fields)
	}
	if !slices.ContainsFunc(bug.Fields, func(f SrcField) bool { return f.ID == "labels" && f.Kind == "array of string" }) {
		t.Errorf("labels are not passed on to be reported: %+v", bug.Fields)
	}
	if !slices.ContainsFunc(snap.Users, func(u SrcUser) bool { return u.ID == "acc-anna" }) {
		t.Errorf("users %+v", snap.Users)
	}
	text := fmt.Sprint(problems)
	for _, want := range []string{"PLAT-4", "99999", "transitions are not read"} {
		if !strings.Contains(text, want) {
			t.Errorf("problems lack %q: %v", want, problems)
		}
	}
}

func TestJiraCloudFailsClearly(t *testing.T) {
	_, j := newFakeJira(t)
	if _, _, err := j.Read(context.Background(), []string{"NOPE"}); err == nil || !strings.Contains(err.Error(), "project NOPE not found") {
		t.Errorf("unknown project: %v", err)
	}
	bad := j
	bad.Token = "wrong"
	if _, _, err := bad.Read(context.Background(), []string{"PLAT"}); err == nil || !strings.Contains(err.Error(), "401") {
		t.Errorf("wrong token: %v", err)
	}
	if _, _, err := j.Read(context.Background(), nil); err == nil {
		t.Error("no projects accepted")
	}
	if _, _, err := (JiraCloud{Site: "ftp://x"}).Read(context.Background(), []string{"PLAT"}); err == nil {
		t.Error("ftp site accepted")
	}
}

func TestJiraCloudImportsEndToEnd(t *testing.T) {
	_, j := newFakeJira(t)
	snap, _, err := j.Read(context.Background(), []string{"PLAT"})
	if err != nil {
		t.Fatal(err)
	}
	s := item.NewStore()
	m := Derive(snap)
	rep, err := Run(s, snap, m, Options{Actor: "import"})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Created != 4 {
		t.Fatalf("report %+v", rep)
	}
	var plat1, plat2, plat3 item.Item
	for _, it := range s.Find(item.Query{Space: "PLAT"}) {
		switch it.Key {
		case "PLAT-1":
			plat1 = it
		case "PLAT-2":
			plat2 = it
		case "PLAT-3":
			plat3 = it
		}
	}
	if plat1.State != "Open" || plat2.State != "Done" || plat3.Parent != plat1.ID {
		t.Fatalf("states or tree lost: %+v %+v %+v", plat1, plat2, plat3)
	}
	if plat1.Properties["story_points"] != 3.0 || plat1.Properties["team"] != "Core" || plat1.Properties["assignee"] != "external:acc-anna" {
		t.Errorf("properties %+v", plat1.Properties)
	}
	if !slices.ContainsFunc(s.Relations(plat1.ID, item.Outgoing), func(r item.Relation) bool { return r.Type == "blocks" && r.To == plat2.ID }) {
		t.Error("the link was not imported")
	}
	// The imported workflow lets the issue move on.
	if err := s.Transition("anna", plat1.ID, "done"); err != nil {
		t.Errorf("transition: %v", err)
	}
	again, err := Run(s, snap, m, Options{Actor: "import"})
	if err != nil || again.Created != 0 {
		t.Fatalf("re-run %+v, %v", again, err)
	}
}
