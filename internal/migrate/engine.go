package migrate

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/dbuchs7/wikiwikibangbang/internal/item"
)

// Options configure a run.
type Options struct {
	// Actor is recorded as the author of the import's events.
	Actor string
	// DryRun runs against a copy of the store and writes nothing.
	DryRun bool
	// Managed, if set, marks every imported item as owned by this one-way
	// sync: read-only for people until Store.Release (cut-over).
	Managed string
	// Partial means the snapshot holds only changed objects (an incremental
	// sync), so objects missing from it are not reported as removed.
	Partial bool
}

// Problem is something the run could not map, resolve or write.
type Problem struct {
	Source  string
	Message string
}

// Report summarises a run. Created, Updated and Unchanged count source
// issues and pages.
type Report struct {
	Created, Updated, Unchanged int
	Problems                    []Problem
}

type engine struct {
	s    *item.Store
	snap Snapshot
	m    Mapping
	opt  Options
	rep  Report

	status   map[string]string // source ref -> "created" | "updated" | "unchanged"
	skipType map[string]bool   // issue types that could not be defined
}

// Run imports a snapshot (ADR-0023). Pass one creates or updates every issue
// and page, parents first; pass two writes bodies, links and restrictions,
// when every target exists. Items the store refuses are reported, never
// forced. A re-run with the same snapshot changes nothing.
func Run(s *item.Store, snap Snapshot, m Mapping, opt Options) (Report, error) {
	if opt.DryRun {
		copy, err := item.Replay(s.Log())
		if err != nil {
			return Report{}, fmt.Errorf("dry run: copying the store: %w", err)
		}
		s = copy
	}
	e := &engine{s: s, snap: snap, m: m, opt: opt, status: map[string]string{}, skipType: map[string]bool{}}
	e.rep.Problems = slices.Clone(m.Notes)
	e.ensureSpaces()
	e.ensureTypes()
	issues, pages := e.orderedIssues(), e.orderedPages()
	for _, is := range issues {
		e.upsertIssue(is)
	}
	for _, p := range pages {
		e.upsertPage(p)
	}
	for _, is := range issues {
		e.issueContent(is)
	}
	for _, p := range pages {
		e.pageContent(p)
	}
	if !opt.Partial {
		e.reportRemoved()
	}
	for _, st := range e.status {
		switch st {
		case "created":
			e.rep.Created++
		case "updated":
			e.rep.Updated++
		default:
			e.rep.Unchanged++
		}
	}
	return e.rep, nil
}

func (e *engine) problem(source, format string, args ...any) {
	e.rep.Problems = append(e.rep.Problems, Problem{Source: source, Message: fmt.Sprintf(format, args...)})
}

func (e *engine) touched(ref string, changed bool) {
	if changed && e.status[ref] == "unchanged" {
		e.status[ref] = "updated"
	}
}

func (e *engine) ensureSpaces() {
	keys := []string{}
	names := map[string]string{}
	for _, p := range e.snap.Projects {
		names["jira:"+p.Key] = p.Name
	}
	for _, sp := range e.snap.Spaces {
		names["confluence:"+sp.Key] = sp.Name
	}
	for src, target := range e.m.Spaces {
		if !slices.Contains(keys, target) {
			keys = append(keys, target)
			names[target] = names[src]
		}
	}
	slices.Sort(keys)
	for _, k := range keys {
		if !e.s.HasSpace(k) {
			if err := e.s.CreateSpace(e.opt.Actor, k, names[k]); err != nil {
				e.problem("space "+k, "%v", err)
			}
		}
	}
}

func (e *engine) ensureTypes() {
	if e.s.Type(e.m.PageType).Name == "" {
		if err := e.s.DefineType(e.opt.Actor, item.TypeDef{Name: e.m.PageType, Body: true}); err != nil {
			e.problem("page type", "%v", err)
		}
	}
	names := make([]string, 0, len(e.m.Types))
	for n := range e.m.Types {
		names = append(names, n)
	}
	slices.Sort(names)
	for _, src := range names {
		tm := e.m.Types[src]
		if e.s.Type(tm.Def.Name).Name != "" {
			continue // an existing type is used as it is; mismatches surface per item
		}
		if err := e.s.DefineType(e.opt.Actor, tm.Def); err != nil {
			e.problem("jira type "+src, "%v", err)
			e.skipType[src] = true
		}
	}
}

// orderedIssues returns issues with parents before children.
func (e *engine) orderedIssues() []SrcIssue {
	byID := map[string]SrcIssue{}
	for _, is := range e.snap.Issues {
		byID[is.ID] = is
	}
	var out []SrcIssue
	seen := map[string]bool{}
	var visit func(SrcIssue)
	visit = func(is SrcIssue) {
		if seen[is.ID] {
			return
		}
		seen[is.ID] = true
		if p, ok := byID[is.Parent]; ok {
			visit(p)
		}
		out = append(out, is)
	}
	for _, is := range e.snap.Issues {
		visit(is)
	}
	return out
}

// orderedPages returns pages with parents before children.
func (e *engine) orderedPages() []SrcPage {
	byID := map[string]SrcPage{}
	for _, p := range e.snap.Pages {
		byID[p.ID] = p
	}
	var out []SrcPage
	seen := map[string]bool{}
	var visit func(SrcPage)
	visit = func(p SrcPage) {
		if seen[p.ID] {
			return
		}
		seen[p.ID] = true
		if parent, ok := byID[p.Parent]; ok {
			visit(parent)
		}
		out = append(out, p)
	}
	for _, p := range e.snap.Pages {
		visit(p)
	}
	return out
}

func (e *engine) origin(system, id, key, version, raw string) item.Origin {
	return item.Origin{System: system, Instance: e.snap.Instance, ExternalID: id, ExternalKey: key, Version: version, Raw: raw}
}

func (e *engine) find(system, id string) (item.Item, bool) {
	return e.s.FindByOrigin(system, e.snap.Instance, id)
}

func (e *engine) upsert(ref string, u item.Upsert) {
	_, existed := e.s.FindByOrigin(u.Origin.System, u.Origin.Instance, u.Origin.ExternalID)
	it, changed, err := e.s.Upsert(e.opt.Actor, u)
	if err != nil {
		e.problem(ref, "not imported: %v", err)
		return
	}
	if !existed && u.Key != "" && it.Key != u.Key {
		e.problem(ref, "source key %s was taken in %s; imported as %s (the source key stays in the origin)", u.Key, u.Space, it.Key)
	}
	switch {
	case !existed:
		e.status[ref] = "created"
	case changed:
		e.status[ref] = "updated"
	default:
		e.status[ref] = "unchanged"
	}
}

func (e *engine) upsertIssue(is SrcIssue) {
	ref := "jira " + is.Key
	tm, ok := e.m.Types[is.Type]
	if !ok || e.skipType[is.Type] {
		e.problem(ref, "issue type %q is not mapped", is.Type)
		return
	}
	// A type without a workflow has no state; otherwise the status must map.
	state := ""
	if tm.Def.Workflow != nil {
		var ok bool
		if state, ok = tm.States[is.Status]; !ok {
			e.problem(ref, "not imported: status %q is not mapped", is.Status)
			return
		}
	}
	var parent item.ID
	if is.Parent != "" {
		p, ok := e.find("jira", is.Parent)
		if !ok {
			e.problem(ref, "not imported: parent issue %s was not imported", is.Parent)
			return
		}
		parent = p.ID
	}
	props := item.Props{}
	for fid, v := range is.Fields {
		name, ok := tm.Fields[fid]
		if !ok {
			continue
		}
		def := fieldDef(tm.Def, name)
		val, err := e.convert(def.Kind, v)
		if err != nil {
			e.problem(ref, "field %s: %v; left empty", name, err)
			continue
		}
		props[name] = val
	}
	e.upsert(ref, item.Upsert{
		Origin: e.origin("jira", is.ID, is.Key, is.Version, is.Raw),
		Space:  e.m.Spaces["jira:"+is.Project], Type: tm.Def.Name, Parent: parent,
		Title: is.Summary, Properties: props, State: state, Key: is.Key, Managed: e.opt.Managed,
	})
}

func (e *engine) upsertPage(p SrcPage) {
	ref := "confluence page " + p.ID + " (" + p.Title + ")"
	var parent item.ID
	if p.Parent != "" {
		pp, ok := e.find("confluence", p.Parent)
		if !ok {
			e.problem(ref, "not imported: parent page %s was not imported", p.Parent)
			return
		}
		parent = pp.ID
	}
	raw := p.Raw
	if raw == "" {
		raw = p.Body
	}
	e.upsert(ref, item.Upsert{
		Origin: e.origin("confluence", p.ID, "", p.Version, raw),
		Space:  e.m.Spaces["confluence:"+p.Space], Type: e.m.PageType, Parent: parent,
		Title: p.Title, Managed: e.opt.Managed,
	})
}

func fieldDef(td item.TypeDef, name string) item.FieldDef {
	for _, f := range td.Fields {
		if f.Name == name {
			return f
		}
	}
	return item.FieldDef{}
}

func (e *engine) principal(account string) string {
	if p, ok := e.m.Users[account]; ok {
		return p
	}
	return "external:" + account
}

func (e *engine) convert(kind item.FieldKind, v any) (any, error) {
	switch kind {
	case item.KindNumber:
		switch n := v.(type) {
		case int:
			return float64(n), nil
		case int64:
			return float64(n), nil
		case float64:
			return n, nil
		}
	case item.KindDate:
		switch d := v.(type) {
		case time.Time:
			return d, nil
		case string:
			for _, layout := range []string{"2006-01-02", time.RFC3339} {
				if t, err := time.Parse(layout, d); err == nil {
					return t, nil
				}
			}
		}
	case item.KindUser:
		if s, ok := v.(string); ok && s != "" {
			return e.principal(s), nil
		}
	default:
		if s, ok := v.(string); ok {
			return s, nil
		}
	}
	return nil, fmt.Errorf("value %#v is not a valid %s", v, kind)
}

// commitBody writes a body when it differs from the current one.
func (e *engine) commitBody(ref string, it item.Item, d item.Doc) {
	if len(it.Body.Blocks) == 0 && len(d.Blocks) == 0 || reflect.DeepEqual(it.Body, d) {
		return
	}
	if err := e.s.CommitBody(e.opt.Actor, it.ID, d); err != nil {
		e.problem(ref, "body not written: %v", err)
		return
	}
	e.touched(ref, true)
}

func (e *engine) issueContent(is SrcIssue) {
	ref := "jira " + is.Key
	it, ok := e.find("jira", is.ID)
	if !ok || e.status[ref] == "" {
		return
	}
	var doc item.Doc
	for _, para := range strings.Split(is.Description, "\n\n") {
		if t := strings.TrimSpace(para); t != "" {
			doc.Blocks = append(doc.Blocks, item.Block{Kind: item.Paragraph, Text: t})
		}
	}
	e.commitBody(ref, it, doc)

	existing := e.s.Relations(it.ID, item.Outgoing)
	for _, l := range is.Links {
		target, ok := e.find("jira", l.Target)
		if !ok {
			e.problem(ref, "link %q to issue %s not imported: target missing", l.Type, l.Target)
			continue
		}
		typ := sanitize(l.Type, "-")
		if slices.ContainsFunc(existing, func(r item.Relation) bool { return !r.Derived && r.Type == typ && r.To == target.ID }) {
			continue
		}
		if err := e.s.AddRelation(e.opt.Actor, typ, it.ID, target.ID); err != nil {
			e.problem(ref, "link %q not imported: %v", l.Type, err)
			continue
		}
		e.touched(ref, true)
	}
}

// resolver resolves links in Confluence bodies against this run.
type resolver struct{ e *engine }

func (r resolver) Page(space, title string) (item.ID, bool) {
	for _, p := range r.e.snap.Pages {
		if p.Space == space && p.Title == title {
			if it, ok := r.e.find("confluence", p.ID); ok {
				return it.ID, true
			}
		}
	}
	return "", false
}

func (r resolver) Issue(key string) (item.ID, bool) {
	for _, is := range r.e.snap.Issues {
		if is.Key == key {
			if it, ok := r.e.find("jira", is.ID); ok {
				return it.ID, true
			}
		}
	}
	return "", false
}

func (r resolver) User(account string) (string, bool) {
	p, ok := r.e.m.Users[account]
	return p, ok
}

func (e *engine) pageContent(p SrcPage) {
	ref := "confluence page " + p.ID + " (" + p.Title + ")"
	it, ok := e.find("confluence", p.ID)
	if !ok || e.status[ref] == "" {
		return
	}
	doc, problems := ConvertStorage(p.Body, p.Space, resolver{e})
	for _, msg := range problems {
		e.problem(ref, "%s", msg)
	}
	for _, b := range doc.Blocks {
		if b.Kind == item.Unsupported {
			e.problem(ref, "content kept as unsupported block: %.60s", b.Text)
		}
	}
	e.commitBody(ref, it, doc)

	want := item.Restriction{Read: e.principals(p.ReadAllowList), Edit: e.principals(p.EditAllowList)}
	if !reflect.DeepEqual(it.Restriction, want) {
		if err := e.s.Restrict(e.opt.Actor, it.ID, want); err != nil {
			e.problem(ref, "restriction not written: %v", err)
			return
		}
		e.touched(ref, true)
	}
}

// principals maps source accounts conservatively: an unmapped account
// becomes a placeholder that no one holds, so a restriction never admits
// more people than in the source.
func (e *engine) principals(accounts []string) []string {
	if len(accounts) == 0 {
		return nil
	}
	out := make([]string, len(accounts))
	for i, a := range accounts {
		out[i] = e.principal(a)
	}
	return out
}

// reportRemoved lists imported items of this site whose source object is no
// longer in the snapshot. They are kept: an import never deletes (D8). Only
// the spaces this snapshot covers are looked at, so importing one project
// does not report the pages of another run as removed.
func (e *engine) reportRemoved() {
	covered := map[string]bool{}
	for _, p := range e.snap.Projects {
		covered["jira\x00"+e.m.Spaces["jira:"+p.Key]] = true
	}
	for _, sp := range e.snap.Spaces {
		covered["confluence\x00"+e.m.Spaces["confluence:"+sp.Key]] = true
	}
	seen := map[string]bool{}
	for _, is := range e.snap.Issues {
		seen["jira\x00"+is.ID] = true
	}
	for _, p := range e.snap.Pages {
		seen["confluence\x00"+p.ID] = true
	}
	for _, it := range e.s.Find(item.Query{}) {
		o := it.Origin
		if o == nil || o.Instance != e.snap.Instance || seen[o.System+"\x00"+o.ExternalID] || !covered[o.System+"\x00"+it.Space] {
			continue
		}
		e.problem(o.System+" "+it.Key, "was removed in the source; it is kept here and must be trashed by a person if no longer needed")
	}
}
