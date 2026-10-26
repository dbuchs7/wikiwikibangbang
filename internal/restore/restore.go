// Package restore brings spaces back from a backup into a data directory
// that is in use, item by item, as an import does (ADR-0124, ADR-0023).
//
// The installation's own state decides what comes back. An item it still
// knows, live or in the trash, is skipped and reported: a restore never
// overwrites what is there. An item it deleted for good, or that was
// deleted for good or in the trash when the backup was taken, is reported
// and never brought back, so an erasure request stays fulfilled. Every
// other item of the chosen spaces comes back with its ID, key, content,
// files, comments and protected passages. An item it refers to in another
// space that the installation does not know comes back too, with its
// space if need be, so the reference holds.
package restore

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"maps"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/dbuchs7/wikiwikibangbang/internal/item"
)

// System names a backup in the origin of every item a restore writes.
const System = "backup"

// Why an item is not restored.
const (
	Exists          = "exists"           // the installation has it, live
	InTrash         = "in the trash"     // the installation has it in its trash
	Erased          = "deleted for good" // the installation deleted it for good
	ErasedInBackup  = "deleted for good in the backup"
	TrashedInBackup = "in the trash in the backup"
	SpaceTrashed    = "its space is in the trash"
	SpacePurged     = "its space was deleted for good"
)

// Outcome is what happened to one item of the backup. Title is empty for
// an item deleted for good: its content is not repeated.
type Outcome struct {
	ID     item.ID
	Key    string
	Space  string
	Title  string
	Reason string
}

// Report is what a restore did or, as a dry run, would do.
type Report struct {
	// Restored are the items brought back; Reason says why one outside the
	// chosen spaces came back ("referenced by KEY").
	Restored []Outcome
	// Skipped are the items not brought back, with the reason.
	Skipped []Outcome
	// Spaces are the spaces created; Granted counts the roles given.
	Spaces  []string
	Granted int
	// Notes are what came back changed: a reference left out, a file not
	// copied, an item placed at the top of its space.
	Notes []string
}

// Erased lists the skipped items that are deleted for good, in the
// installation or in the backup.
func (r Report) Erased() []Outcome {
	var out []Outcome
	for _, o := range r.Skipped {
		if o.Reason == Erased || o.Reason == ErasedInBackup {
			out = append(out, o)
		}
	}
	return out
}

// Target is the data directory restored into.
type Target interface {
	// Write runs fn as the store's only writer.
	Write(fn func(*item.Store) error) error
	// CopyFile stores the content of a file for an item and describes it;
	// recording it is the caller's step.
	CopyFile(id item.ID, content io.Reader, size int64) (item.Attachment, error)
	// Discard removes a stored file that was not recorded.
	Discard(id, attachment item.ID)
}

// Source is the backup's data.
type Source struct {
	Store *item.Store
	// Open returns the content of a file.
	Open func(id, attachment item.ID) (io.ReadCloser, error)
	// Instance names the backup in the origins, e.g. its ID.
	Instance string
}

// Options configure a restore.
type Options struct {
	// Spaces are the keys of the spaces to restore.
	Spaces []string
	// Actor is recorded as the author of the restore's events.
	Actor string
	// Users are the user IDs the installation knows: a created space gives
	// them the roles they had in the backup. Groups are not granted, as a
	// group of the same name may hold other people here.
	Users map[string]bool
	// Grants are given in every space the restore creates.
	Grants []item.Grant
}

type plan struct {
	src     *item.Store
	opt     Options
	rep     Report
	restore []item.ID          // in the backup's order, parents first
	why     map[item.ID]string // why an item outside the chosen spaces comes back
	queued  map[item.ID]bool
}

// Run restores. With a dry run, t writes into a copy and nothing is kept.
func Run(t Target, src Source, opt Options) (Report, error) {
	p := &plan{src: src.Store, opt: opt, why: map[item.ID]string{}, queued: map[item.ID]bool{}}
	var err error
	if err = t.Write(func(s *item.Store) error { return p.choose(s) }); err != nil {
		return p.rep, err
	}
	w := writer{p: p, t: t, src: src, files: map[item.ID]map[item.ID]item.ID{}}
	if err = t.Write(w.prepare); err != nil {
		return p.rep, err
	}
	for _, id := range p.restore {
		if err := t.Write(func(s *item.Store) error { w.item(s, id); return nil }); err != nil {
			return p.rep, err
		}
	}
	for _, id := range w.written {
		if err := w.content(id); err != nil {
			return p.rep, err
		}
	}
	return p.rep, nil
}

// choose decides, against the installation's state, what comes back.
func (p *plan) choose(t *item.Store) error {
	for _, key := range p.opt.Spaces {
		if _, known := p.src.SpaceState(key); !known {
			return fmt.Errorf("restore: the backup has no space %s", key)
		}
	}
	var start []item.Item
	for _, key := range p.opt.Spaces {
		start = append(start, p.src.Find(item.Query{Space: key, IncludeDeleted: true})...)
	}
	queue := make([]item.ID, 0, len(start))
	for _, it := range start {
		queue = append(queue, it.ID)
		p.queued[it.ID] = true
	}
	var chosen []item.ID
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		it, err := p.src.Get(id)
		if err != nil {
			continue
		}
		if reason := p.blocked(t, it); reason != "" {
			if p.why[id] == "" {
				p.skip(it, reason)
			} else {
				p.note("%s, referenced by %s, is not restored: %s", it.Key, p.why[id], reason)
			}
			continue
		}
		chosen = append(chosen, id)
		for _, ref := range p.refs(it) {
			if p.queued[ref] {
				continue
			}
			if _, err := t.Get(ref); err == nil {
				continue // the installation has it; a reference to it holds or is left out later
			}
			if _, err := p.src.Get(ref); err != nil {
				continue // the backup does not have it either; left out later
			}
			// A missing target comes back, with its space if need be.
			p.queued[ref] = true
			p.why[ref] = it.Key
			queue = append(queue, ref)
		}
	}
	// Parents first: the backup's order of creation, then by depth.
	depth := func(id item.ID) int {
		n := 0
		for it, err := p.src.Get(id); err == nil && it.Parent != ""; it, err = p.src.Get(it.Parent) {
			n++
		}
		return n
	}
	slices.SortStableFunc(chosen, func(a, b item.ID) int { return depth(a) - depth(b) })
	// An item whose key is taken here gets the next free one, which could
	// be another restored item's own: those items come last, and with them
	// the items below them, which need them first.
	var free, later []item.ID
	last := map[item.ID]bool{}
	for _, id := range chosen {
		it, _ := p.src.Get(id)
		if _, err := t.ByKey(it.Key); err == nil || last[it.Parent] {
			last[id] = true
			later = append(later, id)
			continue
		}
		free = append(free, id)
	}
	p.restore = append(free, later...)
	return nil
}

// blocked returns why an item of the backup does not come back, or "".
func (p *plan) blocked(t *item.Store, it item.Item) string {
	switch it.Presence {
	case item.Purged:
		return ErasedInBackup
	case item.Trashed:
		return TrashedInBackup
	}
	if here, err := t.Get(it.ID); err == nil {
		switch here.Presence {
		case item.Live:
			return Exists
		case item.Trashed:
			return InTrash
		default:
			return Erased
		}
	}
	switch state, _ := t.SpaceState(it.Space); state {
	case "trashed":
		return SpaceTrashed
	case "purged":
		return SpacePurged
	}
	return ""
}

// refs are the items an item needs or points at: its parent, the items its
// body shows or mentions, and the targets of its relations.
func (p *plan) refs(it item.Item) []item.ID {
	var out []item.ID
	add := func(id item.ID) {
		if id != "" && id != it.ID && !slices.Contains(out, id) {
			out = append(out, id)
		}
	}
	add(it.Parent)
	for _, b := range it.Body.Blocks {
		if b.Kind == item.Embed {
			add(b.Ref)
		}
		for _, m := range b.Mentions {
			add(m)
		}
	}
	for _, r := range p.src.Relations(it.ID, item.Outgoing) {
		if !r.Derived {
			add(r.To)
		}
	}
	return out
}

func (p *plan) skip(it item.Item, reason string) {
	o := Outcome{ID: it.ID, Key: it.Key, Space: it.Space, Reason: reason}
	if reason != Erased && reason != ErasedInBackup {
		o.Title = it.Title
	}
	p.rep.Skipped = append(p.rep.Skipped, o)
}

func (p *plan) note(format string, args ...any) {
	p.rep.Notes = append(p.rep.Notes, fmt.Sprintf(format, args...))
}

// writer writes what the plan chose.
type writer struct {
	p       *plan
	t       Target
	src     Source
	written []item.ID
	// files maps, per item, the backup's file IDs to the restored ones.
	files map[item.ID]map[item.ID]item.ID
}

// prepare creates the spaces and types the restored items need.
func (w *writer) prepare(t *item.Store) error {
	var spaces, types []string
	for _, id := range w.p.restore {
		it, _ := w.p.src.Get(id)
		if !slices.Contains(spaces, it.Space) {
			spaces = append(spaces, it.Space)
		}
		if !slices.Contains(types, it.Type) {
			types = append(types, it.Type)
		}
	}
	for _, key := range spaces {
		if _, known := t.SpaceState(key); known {
			continue
		}
		name := key
		for _, sp := range w.p.src.Spaces() {
			if sp.Key == key {
				name = sp.Name
			}
		}
		if err := t.CreateSpace(w.p.opt.Actor, key, name); err != nil {
			return fmt.Errorf("restore: creating space %s: %w", key, err)
		}
		w.p.rep.Spaces = append(w.p.rep.Spaces, key)
		grants := w.p.opt.Grants
		for _, g := range w.p.src.SpaceGrants(key) {
			switch {
			case strings.HasPrefix(g.Principal, "@"):
				w.p.note("space %s: group %s had the role %s in the backup; it is not given, as a group here may hold other people", key, g.Principal, roleName(g.Role))
			case w.p.opt.Users[g.Principal]:
				grants = append(grants, g)
			default:
				w.p.note("space %s: user %s of the backup has no account here; the role %s is not given", key, g.Principal, roleName(g.Role))
			}
		}
		for _, g := range grants {
			if err := t.GrantRole(w.p.opt.Actor, key, g.Principal, g.Role); err != nil {
				w.p.note("space %s: role %s for %s not given: %v", key, roleName(g.Role), g.Principal, err)
				continue
			}
			w.p.rep.Granted++
		}
	}
	for _, name := range types {
		if t.Type(name).Name != "" {
			continue
		}
		td := w.p.src.Type(name)
		if td.Name == "" {
			continue // removed in the backup; the item is refused and reported
		}
		if err := t.DefineType(w.p.opt.Actor, td); err != nil {
			w.p.note("type %s not defined: %v", name, err)
		}
	}
	return nil
}

// item writes one item without its content: the item itself, its labels
// and its restriction.
func (w *writer) item(t *item.Store, id item.ID) {
	it, _ := w.p.src.Get(id)
	parent := it.Parent
	if parent != "" {
		if here, err := t.Get(parent); err != nil || here.Presence != item.Live {
			w.p.note("%s is placed at the top of %s: its parent is not here", it.Key, it.Space)
			parent = ""
		}
	}
	u := item.Upsert{
		Origin: item.Origin{System: System, Instance: w.src.Instance, ExternalID: string(it.ID), ExternalKey: it.Key, Version: strconv.Itoa(it.Version)},
		ID:     it.ID, Key: it.Key, Space: it.Space, Type: it.Type, Parent: parent,
		Title: it.Title, Properties: it.Properties, State: it.State,
	}
	got, _, err := t.Upsert(w.p.opt.Actor, u)
	if err != nil {
		w.p.skip(it, "refused: "+err.Error())
		return
	}
	o := Outcome{ID: it.ID, Key: got.Key, Space: it.Space, Title: it.Title, Reason: ""}
	if by := w.p.why[it.ID]; by != "" {
		o.Reason = "referenced by " + by
	}
	if got.Key != it.Key {
		w.p.note("%s comes back as %s: its key is taken here", it.Key, got.Key)
	}
	w.p.rep.Restored = append(w.p.rep.Restored, o)
	w.written = append(w.written, it.ID)
	if len(it.Labels) > 0 {
		if err := t.SetLabels(w.p.opt.Actor, it.ID, it.Labels); err != nil {
			w.p.note("%s: labels not restored: %v", it.Key, err)
		}
	}
	// Until its content is written, only administrators read it.
	if err := t.Restrict(w.p.opt.Actor, it.ID, item.Restriction{Read: []string{w.p.opt.Actor}}); err != nil {
		w.p.note("%s: not restricted while restored: %v", it.Key, err)
	}
}

// content writes an item's files, protected passages, body, relations and
// comments, once every restored item exists.
func (w *writer) content(id item.ID) error {
	it, _ := w.p.src.Get(id)
	if err := w.copyFiles(it); err != nil {
		return err
	}
	return w.t.Write(func(t *item.Store) error {
		if len(it.Secrets) > 0 {
			if err := t.RestoreSecrets(w.p.opt.Actor, id, it.Secrets); err != nil {
				w.p.note("%s: protected passages not restored, so its text is left out: %v", it.Key, err)
				it.Body = item.Doc{}
			}
		}
		if body := w.body(t, it); len(body.Blocks) > 0 {
			if err := t.CommitBody(w.p.opt.Actor, id, body); err != nil {
				w.p.note("%s: text not restored: %v", it.Key, err)
			}
		}
		for _, r := range w.p.src.Relations(id, item.Outgoing) {
			if r.Derived {
				continue
			}
			if !w.live(t, r.To) {
				w.p.note("%s: relation %s to %s left out: it is not here", it.Key, r.Type, w.key(r.To))
				continue
			}
			if err := t.AddRelation(w.p.opt.Actor, r.Type, id, r.To); err != nil {
				w.p.note("%s: relation %s to %s not restored: %v", it.Key, r.Type, w.key(r.To), err)
			}
		}
		w.comments(t, it)
		// Last, the restriction it had, as written: it only narrows (D9).
		// Set after the text, it is the backup's and not the one the check
		// for sensitive data gives the restore as the author (ADR-0040).
		if err := t.Restrict(w.p.opt.Actor, id, it.Restriction); err != nil {
			w.p.note("%s: restriction not restored, so only administrators read it: %v", it.Key, err)
		}
		return nil
	})
}

func (w *writer) live(t *item.Store, id item.ID) bool {
	here, err := t.Get(id)
	return err == nil && here.Presence == item.Live
}

func (w *writer) key(id item.ID) string {
	if it, err := w.p.src.Get(id); err == nil {
		return it.Key
	}
	return string(id)
}

// copyFiles copies an item's files that were not deleted, in the order
// they were added; a file made from another keeps that link when the
// other came back before it.
func (w *writer) copyFiles(it item.Item) error {
	moved := map[item.ID]item.ID{}
	w.files[it.ID] = moved
	for _, a := range it.Attachments {
		if a.Deleted {
			continue
		}
		in, err := w.src.Open(it.ID, a.ID)
		if err != nil {
			w.p.note("%s: file %s not restored: %v", it.Key, a.Name, err)
			continue
		}
		stored, err := w.t.CopyFile(it.ID, in, a.Size)
		in.Close()
		if err != nil {
			w.p.note("%s: file %s not restored: %v", it.Key, a.Name, err)
			continue
		}
		stored.Name = a.Name
		if a.Source != nil {
			if to, ok := w.files[a.Source.Item][a.Source.Attachment]; ok {
				stored.Source = &item.Source{Item: a.Source.Item, Attachment: to}
			}
		}
		err = w.t.Write(func(t *item.Store) error {
			_, err := t.AddAttachment(w.p.opt.Actor, it.ID, stored)
			return err
		})
		if err != nil {
			w.t.Discard(it.ID, stored.ID)
			w.p.note("%s: file %s not restored: %v", it.Key, a.Name, err)
			continue
		}
		moved[a.ID] = stored.ID
	}
	return nil
}

// body returns the item's body as it can be written here: pictures,
// videos and BPMN diagrams show the restored files, and a reference to an item that is not
// here is left out, the text of a mention staying.
func (w *writer) body(t *item.Store, it item.Item) item.Doc {
	var out item.Doc
	for _, b := range it.Body.Blocks {
		b.Mentions = slices.Clone(b.Mentions)
		b.Marks = slices.Clone(b.Marks)
		switch b.Kind {
		case item.Image, item.Video, item.BPMN:
			to, ok := w.files[it.ID][b.Ref]
			if !ok {
				w.p.note("%s: a %s of the text is left out: its file did not come back", it.Key, b.Kind)
				continue
			}
			b.Ref = to
		case item.Embed:
			if !w.live(t, b.Ref) {
				w.p.note("%s: the embedded %s is left out: it is not here", it.Key, w.key(b.Ref))
				continue
			}
		}
		gone := map[item.ID]bool{}
		b.Mentions = slices.DeleteFunc(b.Mentions, func(id item.ID) bool {
			if w.live(t, id) {
				return false
			}
			gone[id] = true
			return true
		})
		b.Marks = slices.DeleteFunc(b.Marks, func(m item.Mark) bool { return m.Type == item.MarkMention && gone[m.Ref] })
		for _, id := range slices.Sorted(maps.Keys(gone)) {
			w.p.note("%s: the mention of %s is plain text now: it is not here", it.Key, w.key(id))
		}
		out.Blocks = append(out.Blocks, b)
	}
	return out
}

// comments writes the comments that were not deleted, threads first, each
// by its author.
func (w *writer) comments(t *item.Store, it item.Item) {
	ids := map[item.ID]item.ID{}
	for _, c := range it.Comments {
		if c.Deleted {
			continue
		}
		n := item.NewComment{Text: c.Text, Anchor: c.Anchor}
		if c.Reply != "" {
			to, ok := ids[c.Reply]
			if !ok {
				w.p.note("%s: a reply is left out: its thread was deleted", it.Key)
				continue
			}
			n.Reply = to
		}
		author := c.Author
		if author == "" {
			author = w.p.opt.Actor
		}
		got, err := t.Comment(author, it.ID, n)
		if err != nil {
			w.p.note("%s: a comment is left out: %v", it.Key, err)
			continue
		}
		ids[c.ID] = got.ID
		if c.Resolved {
			by := c.ResolvedBy
			if by == "" {
				by = author
			}
			if err := t.ResolveComment(by, it.ID, got.ID, true); err != nil {
				w.p.note("%s: a resolved thread is open again: %v", it.Key, err)
			}
		}
	}
}

func roleName(r item.Role) string {
	switch r {
	case item.Viewer:
		return "viewer"
	case item.Editor:
		return "editor"
	case item.SpaceAdmin:
		return "space-admin"
	}
	return strconv.Itoa(int(r))
}

// Dry is a target that writes into a copy of s, so a dry run reports what
// a restore would do and changes nothing.
func Dry(s *item.Store) (Target, error) {
	c, err := item.Replay(s.Log())
	if err != nil {
		return nil, fmt.Errorf("restore: copying the store: %w", err)
	}
	return dry{c}, nil
}

type dry struct{ s *item.Store }

func (d dry) Write(fn func(*item.Store) error) error { return fn(d.s) }

func (d dry) CopyFile(_ item.ID, content io.Reader, _ int64) (item.Attachment, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return item.Attachment{}, err
	}
	head := make([]byte, 512)
	n, _ := io.ReadFull(content, head)
	sum := sha256.New()
	sum.Write(head[:n])
	size, err := io.Copy(sum, content)
	if err != nil {
		return item.Attachment{}, err
	}
	return item.Attachment{ID: item.ID(hex.EncodeToString(raw[:])), Type: http.DetectContentType(head[:n]),
		Size: size + int64(n), SHA256: hex.EncodeToString(sum.Sum(nil))}, nil
}

func (dry) Discard(item.ID, item.ID) {}
