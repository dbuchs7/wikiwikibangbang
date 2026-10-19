package migrate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"sort"
	"strings"
	"time"
)

// JiraCloud reads projects and their issues from a Jira Cloud site through
// its REST API v3 (ADR-0023) and produces the Jira part of a Snapshot.
//
// Issue types are read with their statuses; Jira's transitions are not, so
// an imported type may move from any of its states to any other, and the
// report says so. A type's fields are the fields its issues hold a value
// for, and an option field's values are the values its issues use.
type JiraCloud struct {
	// Site is the site's address, e.g. https://example.atlassian.net.
	Site string
	// Email and Token are an Atlassian account's e-mail address and API
	// token; the token is sent only to Site.
	Email, Token string
	// Client sends the requests; nil means http.DefaultClient.
	Client *http.Client
}

// issueLimit is the page size of the issue search; the API caps it.
const issueLimit = 100

// jiraSystemFields are the system fields imported as properties. The others
// are the issue's structure (summary, status, parent, links …), handled on
// their own, or bookkeeping of the source (votes, watchers, last viewed …).
var jiraSystemFields = map[string]bool{
	"assignee": true, "reporter": true, "creator": true, "created": true, "updated": true,
	"duedate": true, "priority": true, "resolution": true, "resolutiondate": true,
	"environment": true, "labels": true, "components": true, "fixVersions": true,
}

// Read reads the given projects, by key.
func (j JiraCloud) Read(ctx context.Context, projectKeys []string) (Snapshot, []Problem, error) {
	base, err := siteURL("jira cloud", j.Site)
	if err != nil {
		return Snapshot{}, nil, err
	}
	snap := Snapshot{Instance: base.Host}
	if len(projectKeys) == 0 {
		return snap, nil, errors.New("jira cloud: name at least one project")
	}
	r := &jiraReader{
		cloudReader: cloudReader{site: cloudSite{label: "jira cloud", base: base, email: j.Email, token: j.Token, client: j.Client}},
		types:       map[string]*jiraType{}, users: map[string]bool{},
	}
	if err := r.readFields(ctx); err != nil {
		return snap, nil, err
	}
	for _, key := range projectKeys {
		var p struct{ Key, Name string }
		err := r.get(ctx, "/rest/api/3/project/"+url.PathEscape(key), &p)
		var status statusError
		if errors.As(err, &status) && status.code == http.StatusNotFound {
			return snap, r.problems, fmt.Errorf("jira cloud: project %s not found, or not readable with this token", key)
		}
		if err != nil {
			return snap, r.problems, err
		}
		snap.Projects = append(snap.Projects, SrcSpace{Key: p.Key, Name: p.Name})
		if err := r.readStatuses(ctx, p.Key); err != nil {
			return snap, r.problems, err
		}
		issues, err := r.readIssues(ctx, p.Key)
		if err != nil {
			return snap, r.problems, err
		}
		snap.Issues = append(snap.Issues, issues...)
	}
	r.detachForeignParents(snap.Issues)
	snap.IssueTypes = r.issueTypes()
	for id := range r.users {
		snap.Users = append(snap.Users, SrcUser{ID: id})
	}
	sort.Slice(snap.Users, func(a, b int) bool { return snap.Users[a].ID < snap.Users[b].ID })
	return snap, r.problems, nil
}

type jiraReader struct {
	cloudReader
	fields map[string]jiraField
	types  map[string]*jiraType
	order  []string // type names in the order they were met
	users  map[string]bool
}

type jiraField struct {
	ID, Name string
	Schema   struct{ Type, Items, Custom string }
}

// jiraType collects an issue type across projects.
type jiraType struct {
	statuses []string
	initial  string
	fields   map[string]*SrcField
}

func (r *jiraReader) readFields(ctx context.Context) error {
	var fields []jiraField
	if err := r.get(ctx, "/rest/api/3/field", &fields); err != nil {
		return err
	}
	r.fields = map[string]jiraField{}
	for _, f := range fields {
		r.fields[f.ID] = f
	}
	return nil
}

func (r *jiraReader) typ(name string) *jiraType {
	t, ok := r.types[name]
	if !ok {
		t = &jiraType{fields: map[string]*SrcField{}}
		r.types[name] = t
		r.order = append(r.order, name)
	}
	return t
}

// readStatuses reads the statuses of a project's issue types. A type's
// initial state is its first status of the category "new" (To Do).
func (r *jiraReader) readStatuses(ctx context.Context, project string) error {
	var types []struct {
		Name     string `json:"name"`
		Statuses []struct {
			Name     string `json:"name"`
			Category struct {
				Key string `json:"key"`
			} `json:"statusCategory"`
		} `json:"statuses"`
	}
	if err := r.get(ctx, "/rest/api/3/project/"+url.PathEscape(project)+"/statuses", &types); err != nil {
		return err
	}
	for _, it := range types {
		t := r.typ(it.Name)
		for _, st := range it.Statuses {
			if !slices.Contains(t.statuses, st.Name) {
				t.statuses = append(t.statuses, st.Name)
			}
			if t.initial == "" && st.Category.Key == "new" {
				t.initial = st.Name
			}
		}
	}
	return nil
}

func (r *jiraReader) readIssues(ctx context.Context, project string) ([]SrcIssue, error) {
	var out []SrcIssue
	req := struct {
		JQL           string   `json:"jql"`
		Fields        []string `json:"fields"`
		MaxResults    int      `json:"maxResults"`
		NextPageToken string   `json:"nextPageToken,omitempty"`
	}{JQL: fmt.Sprintf("project = %q ORDER BY created ASC", project), Fields: []string{"*all"}, MaxResults: issueLimit}
	for {
		var page struct {
			Issues        []json.RawMessage `json:"issues"`
			NextPageToken string            `json:"nextPageToken"`
		}
		if err := r.site.do(ctx, http.MethodPost, "/rest/api/3/search/jql", req, &page); err != nil {
			return out, err
		}
		for _, raw := range page.Issues {
			is, err := r.issue(project, raw)
			if err != nil {
				return out, err
			}
			out = append(out, is)
		}
		if page.NextPageToken == "" || len(page.Issues) == 0 {
			return out, nil
		}
		req.NextPageToken = page.NextPageToken
	}
}

func (r *jiraReader) issue(project string, raw json.RawMessage) (SrcIssue, error) {
	var is struct {
		ID     string                     `json:"id"`
		Key    string                     `json:"key"`
		Fields map[string]json.RawMessage `json:"fields"`
	}
	if err := json.Unmarshal(raw, &is); err != nil {
		return SrcIssue{}, fmt.Errorf("jira cloud: reading an issue: %w", err)
	}
	ref := "jira " + is.Key
	var head struct {
		Summary   string `json:"summary"`
		Updated   string `json:"updated"`
		IssueType struct {
			Name string `json:"name"`
		} `json:"issuetype"`
		Status struct {
			Name string `json:"name"`
		} `json:"status"`
		Parent *struct {
			ID string `json:"id"`
		} `json:"parent"`
		Links []struct {
			Type struct {
				Name string `json:"name"`
			} `json:"type"`
			Outward *struct {
				ID string `json:"id"`
			} `json:"outwardIssue"`
		} `json:"issuelinks"`
	}
	fields, _ := json.Marshal(is.Fields)
	if err := json.Unmarshal(fields, &head); err != nil {
		return SrcIssue{}, fmt.Errorf("jira cloud: reading %s: %w", is.Key, err)
	}
	out := SrcIssue{
		ID: is.ID, Key: is.Key, Project: project, Type: head.IssueType.Name, Summary: head.Summary,
		Status: head.Status.Name, Version: head.Updated, Raw: string(raw), Fields: map[string]any{},
	}
	if head.Parent != nil {
		out.Parent = head.Parent.ID
	}
	// Every link appears on both issues; the outward side imports it once.
	for _, l := range head.Links {
		if l.Outward != nil && l.Outward.ID != "" {
			out.Links = append(out.Links, SrcLink{Type: l.Type.Name, Target: l.Outward.ID})
		}
	}
	desc, dropped := ADFText(is.Fields["description"])
	out.Description = desc
	for _, d := range dropped {
		r.problem(ref, "the description holds %s, which are not imported", d)
	}

	t := r.typ(head.IssueType.Name)
	for _, id := range sortedKeys(is.Fields) {
		f, known := r.fields[id]
		if !known || !f.isProperty() {
			continue
		}
		v := is.Fields[id]
		if len(v) == 0 || string(v) == "null" || string(v) == "[]" {
			continue
		}
		kind, val, ok := r.value(f, v)
		sf, seen := t.fields[id]
		if !seen {
			sf = &SrcField{ID: id, Name: f.Name, Kind: kind}
			t.fields[id] = sf
		}
		if !ok {
			continue
		}
		if s, isOption := val.(string); isOption && kind == "option" && !slices.Contains(sf.Options, s) {
			sf.Options = append(sf.Options, s)
		}
		if kind == "user" {
			r.users[val.(string)] = true
		}
		out.Fields[id] = val
	}
	return out, nil
}

// isProperty reports whether a field is imported as a property.
func (f jiraField) isProperty() bool {
	return strings.HasPrefix(f.ID, "customfield_") || jiraSystemFields[f.ID]
}

// value converts a field's value to the form of SrcIssue.Fields for its
// kind. A kind the engine cannot map is passed through as the schema's type,
// so the mapping reports the field once; its values stay in the raw source.
func (r *jiraReader) value(f jiraField, v json.RawMessage) (kind string, val any, ok bool) {
	switch f.Schema.Type {
	case "string":
		var s string
		if json.Unmarshal(v, &s) == nil {
			return "string", s, true
		}
		text, _ := ADFText(v) // a rich text field
		return "string", text, true
	case "number":
		var n float64
		return "number", n, json.Unmarshal(v, &n) == nil
	case "date", "datetime":
		var s string
		if json.Unmarshal(v, &s) != nil {
			return "date", nil, false
		}
		if t, err := time.Parse("2006-01-02T15:04:05.000-0700", s); err == nil {
			s = t.Format(time.RFC3339)
		}
		return "date", s, true
	case "user":
		var u struct {
			AccountID string `json:"accountId"`
		}
		return "user", u.AccountID, json.Unmarshal(v, &u) == nil && u.AccountID != ""
	case "option", "priority", "resolution":
		var o struct{ Value, Name string }
		if json.Unmarshal(v, &o) != nil {
			return "option", nil, false
		}
		if o.Value == "" {
			o.Value = o.Name
		}
		return "option", o.Value, o.Value != ""
	}
	kind = f.Schema.Type
	if f.Schema.Items != "" {
		kind += " of " + f.Schema.Items
	}
	return kind, nil, false
}

// detachForeignParents places issues whose parent was not read (an epic in
// another project) at the top, reported: the engine would refuse them.
func (r *jiraReader) detachForeignParents(issues []SrcIssue) {
	read := map[string]bool{}
	for _, is := range issues {
		read[is.ID] = true
	}
	for i, is := range issues {
		if is.Parent != "" && !read[is.Parent] {
			r.problem("jira "+is.Key, "its parent issue %s is not part of the import; the issue is placed at the top of its space", is.Parent)
			issues[i].Parent = ""
		}
	}
}

// issueTypes builds the types. Jira's transitions are not read: every state
// can be reached from every other, and the report says so once per type.
func (r *jiraReader) issueTypes() []SrcIssueType {
	var out []SrcIssueType
	for _, name := range r.order {
		t := r.types[name]
		it := SrcIssueType{Name: name}
		for _, id := range sortedKeys(t.fields) {
			it.Fields = append(it.Fields, *t.fields[id])
		}
		if len(t.statuses) > 0 {
			wf := SrcWorkflow{Initial: t.initial, Statuses: t.statuses}
			if wf.Initial == "" {
				wf.Initial = t.statuses[0]
			}
			for _, to := range t.statuses {
				var from []string
				for _, s := range t.statuses {
					if s != to {
						from = append(from, s)
					}
				}
				if len(from) == 0 {
					continue
				}
				wf.Transitions = append(wf.Transitions, SrcTransition{Name: to, From: from, To: to})
			}
			it.Workflow = wf
			r.problem("jira type "+name, "its workflow's transitions are not read; every state can be reached from every other until the type is edited")
		}
		out = append(out, it)
	}
	return out
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func unixDate(sec int64) string { return time.Unix(sec, 0).UTC().Format("2006-01-02") }
