package migrate

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/dbuchs7/wikiwikibangbang/internal/item"
)

// Mapping says how source concepts become items. It is configuration, not
// code (ADR-0023): Derive proposes one, people adjust it, and the import is
// re-run.
type Mapping struct {
	// Spaces maps "jira:KEY" and "confluence:KEY" to a target space key.
	Spaces map[string]string
	// Types maps a Jira issue type name to a target type.
	Types map[string]TypeMap
	// PageType is the target type of Confluence pages.
	PageType string
	// Users maps source account IDs to principals.
	Users map[string]string
	// Notes are the problems found while deriving the mapping.
	Notes []Problem
}

// TypeMap maps one issue type.
type TypeMap struct {
	Def item.TypeDef
	// Fields maps source field IDs to target field names. Fields that are
	// not listed are not imported as properties (they stay in the raw source).
	Fields map[string]string
	// States maps source statuses to target states.
	States map[string]string
}

var nonWord = regexp.MustCompile(`[^a-z0-9]+`)

func sanitize(s, sep string) string {
	return strings.Trim(nonWord.ReplaceAllString(strings.ToLower(s), sep), sep)
}

var kinds = map[string]item.FieldKind{
	"string": item.KindText, "number": item.KindNumber, "date": item.KindDate,
	"user": item.KindUser, "option": item.KindEnum,
}

// Derive proposes a mapping from the source's own configuration: projects
// and spaces keep their keys, issue types become types with their fields and
// workflows, users map to their e-mail address.
func Derive(snap Snapshot) Mapping {
	m := Mapping{Spaces: map[string]string{}, Types: map[string]TypeMap{}, PageType: "page", Users: map[string]string{}}
	note := func(source, format string, args ...any) {
		m.Notes = append(m.Notes, Problem{Source: source, Message: fmt.Sprintf(format, args...)})
	}

	for _, p := range snap.Projects {
		m.Spaces["jira:"+p.Key] = strings.ToUpper(sanitize(p.Key, ""))
	}
	for _, sp := range snap.Spaces {
		m.Spaces["confluence:"+sp.Key] = strings.ToUpper(sanitize(sp.Key, ""))
	}

	for _, it := range snap.IssueTypes {
		tm := TypeMap{Fields: map[string]string{}, States: map[string]string{}}
		tm.Def = item.TypeDef{Name: sanitize(it.Name, "-"), Body: true}
		used := map[string]bool{}
		for _, f := range it.Fields {
			kind, ok := kinds[f.Kind]
			if !ok {
				note("jira type "+it.Name, "field %q (%s) of kind %q cannot be mapped; it stays in the raw source", f.Name, f.ID, f.Kind)
				continue
			}
			name := sanitize(f.Name, "_")
			if item.ReservedField(name) {
				note("jira type "+it.Name, "field %q collides with the reserved name %q; renamed to %q", f.Name, name, "jira_"+name)
				name = "jira_" + name
			}
			if used[name] {
				name += "_" + sanitize(f.ID, "_")
			}
			used[name] = true
			tm.Fields[f.ID] = name
			tm.Def.Fields = append(tm.Def.Fields, item.FieldDef{Name: name, Kind: kind, Values: f.Options})
		}
		wf := it.Workflow
		if len(wf.Statuses) > 0 {
			def := &item.Workflow{Initial: wf.Initial, States: wf.Statuses}
			for _, st := range wf.Statuses {
				tm.States[st] = st
			}
			for _, tr := range wf.Transitions {
				def.Transitions = append(def.Transitions, item.Transition{Name: sanitize(tr.Name, "-"), From: tr.From, To: tr.To})
			}
			tm.Def.Workflow = def
		}
		m.Types[it.Name] = tm
	}

	for _, u := range snap.Users {
		if u.Email != "" {
			m.Users[u.ID] = strings.ToLower(u.Email)
			continue
		}
		m.Users[u.ID] = "external:" + u.ID
		note("user "+u.ID, "%q has no e-mail address; mapped to placeholder principal %q", u.Name, "external:"+u.ID)
	}
	return m
}

// MapAccounts maps source accounts to local principals, overriding what
// Derive proposed for them, and drops Derive's notes about those accounts.
// A source that hides e-mail addresses (as Confluence Cloud does by default)
// is mapped this way, from a list people write.
func (m Mapping) MapAccounts(local map[string]string) Mapping {
	if len(local) == 0 {
		return m
	}
	users := make(map[string]string, len(m.Users)+len(local))
	for k, v := range m.Users {
		users[k] = v
	}
	for k, v := range local {
		users[k] = v
	}
	m.Users = users
	m.Notes = slices.DeleteFunc(slices.Clone(m.Notes), func(p Problem) bool {
		id, ok := strings.CutPrefix(p.Source, "user ")
		_, mapped := local[id]
		return ok && mapped
	})
	return m
}
