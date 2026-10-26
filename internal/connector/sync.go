package connector

import (
	"bytes"
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"math"
	"net/http"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/dbuchs7/wikiwikibangbang/internal/budget"
	"github.com/dbuchs7/wikiwikibangbang/internal/gitrepo"
	"github.com/dbuchs7/wikiwikibangbang/internal/item"
	"github.com/dbuchs7/wikiwikibangbang/internal/site"
	"github.com/dbuchs7/wikiwikibangbang/internal/storage"
)

// Actor is who a connector's changes are recorded as, e.g.
// sync:opendata.swiss. The connector's ID names the source in an item's
// origin (ADR-0023).
func Actor(c site.Connector) string { return "sync:" + c.ID }

// instance names the source's installation in an item's origin.
func instance(c site.Connector) string {
	switch {
	case c.CKAN != nil:
		return c.CKAN.BaseURL
	case c.JSON != nil:
		return c.JSON.URL
	case c.Git != nil:
		return c.Git.URL
	}
	return ""
}

// TargetType is the type of the items a connector writes: the one its
// settings name, else page (ADR-0127).
func TargetType(c site.Connector) string { return cmp.Or(c.Type, PageType) }

// PageType is the type of the catalog's pages: an ordinary wiki page,
// with a text and no fields or workflow (ADR-0093).
const PageType = "page"

// LegacyType is the type of the catalog's entries before ADR-0093, with
// fields and a workflow. A run removes them and writes pages instead.
const LegacyType = "interface"

// WithdrawnLabel marks a page whose data set the source no longer
// publishes; a page has no state to say so (ADR-0093).
const WithdrawnLabel = "withdrawn"

// prefix is what an entry's ID in its source is prefixed with in its
// item's origin: "page/" for CKAN, which differs from the ID of the entry
// a page replaces, so that entry's erasure does not keep the page from
// being written (ADR-0026, ADR-0093); "entry/" for the kinds of ADR-0127.
func prefix(c site.Connector) string {
	if c.Kind == site.KindCKAN {
		return "page/"
	}
	return "entry/"
}

// PageTypeDef is the type a connector defines when the site has no page
// type yet.
func PageTypeDef() item.TypeDef { return item.TypeDef{Name: PageType, Body: true} }

// Change is one entry a run created, changed or withdrew.
type Change struct {
	Kind  string `json:"kind"` // new, changed, withdrawn, removed
	Key   string `json:"key,omitempty"`
	Title string `json:"title"`
}

// Result is what a run did, or with DryRun would do.
type Result struct {
	Connector string    `json:"connector"`
	DryRun    bool      `json:"dryRun"`
	Started   time.Time `json:"started"`
	Seconds   float64   `json:"seconds"`
	Datasets  int       `json:"datasets"`
	// Formats are the source's formats the run read, as it writes them.
	Formats   []string `json:"formats"`
	Entries   int      `json:"entries"`
	New       int      `json:"new"`
	Changed   int      `json:"changed"`
	Unchanged int      `json:"unchanged"`
	Withdrawn int      `json:"withdrawn"`
	// Removed counts the entries of before ADR-0093 the run erased.
	Removed  int      `json:"removed"`
	Problems []string `json:"problems"`
	// Examples are the first changes, to judge a dry run by.
	Examples []Change `json:"examples"`
	Error    string   `json:"error,omitempty"`
	// Version is the source's version the run read, e.g. a commit, and
	// Same is set when it is the one the last run applied, with the same
	// settings, so the run read nothing further (ADR-0128).
	Version string `json:"version,omitempty"`
	Same    bool   `json:"same,omitempty"`
	// retry is set when an entry could not be read or written for a
	// reason that may pass, so the next run reads the source again.
	retry bool
}

// examplesShown and problemsShown bound the lists of a result.
const (
	examplesShown = 30
	problemsShown = 50
)

func (r *Result) problem(format string, args ...any) {
	if len(r.Problems) < problemsShown {
		r.Problems = append(r.Problems, fmt.Sprintf(format, args...))
	}
}

func (r *Result) count(kind, key, title string) {
	switch kind {
	case "new":
		r.New++
	case "changed":
		r.Changed++
	case "withdrawn":
		r.Withdrawn++
	case "removed":
		r.Removed++
	}
	if len(r.Examples) < examplesShown {
		r.Examples = append(r.Examples, Change{Kind: kind, Key: key, Title: title})
	}
}

// ErrNoSpace is a synchronisation into a space that does not exist.
var ErrNoSpace = errors.New("connector: the catalog's space does not exist")

// ErrTypeChanged is a synchronisation whose type differs from the one of
// the items it wrote before: an import does not change an item's type.
var ErrTypeChanged = errors.New("connector: the connector wrote its items as another type")

// Apply brings the space in step with the entries: an item of the
// connector's type is written for each new or changed entry, its fields
// filled as the connector maps them (ADR-0127), and an item whose entry
// the source no longer publishes is marked withdrawn. Entries of before
// ADR-0093 are erased, so their pages take their place. With dry,
// nothing is written.
func Apply(db *storage.DB, cfg site.Connector, entries []Entry, dry bool) (Result, error) {
	res := Result{Connector: cfg.ID, DryRun: dry, Entries: len(entries), Problems: []string{}, Examples: []Change{}}
	ours := func(it item.Item) bool {
		return it.Origin != nil && it.Origin.System == cfg.ID && it.Origin.Instance == instance(cfg)
	}
	target, pre := TargetType(cfg), prefix(cfg)
	var existing map[string]item.Item
	var legacy []item.Item
	var td item.TypeDef
	var typeErr error
	hasSpace, other := false, ""
	db.Read(func(s *item.Store) {
		hasSpace = s.HasSpace(cfg.Space)
		existing = map[string]item.Item{}
		for _, it := range s.Find(item.Query{Space: cfg.Space}) {
			id, ok := strings.CutPrefix(itemExternalID(it), pre)
			switch {
			case !ours(it):
			case it.Type == LegacyType && cfg.Kind == site.KindCKAN:
				legacy = append(legacy, it)
			case !ok:
			case it.Type == target:
				existing[id] = it
			default:
				other = it.Type
			}
		}
		if td = s.Type(target); td.Name != "" {
			typeErr = Fits(td, cfg)
		}
	})
	if !hasSpace {
		return res, ErrNoSpace
	}
	if other != "" {
		return res, fmt.Errorf("%w: its items in %s are of type %q, not %q; an import does not change an item's type", ErrTypeChanged, cfg.Space, other, target)
	}
	if typeErr != nil {
		return res, typeErr
	}
	if td.Name == "" {
		if target != PageType {
			return res, fmt.Errorf("connector: the type %q does not exist", target)
		}
		td = PageTypeDef()
		if !dry {
			if err := db.Write(func(s *item.Store) error { return s.DefineType(Actor(cfg), td) }); err != nil {
				return res, err
			}
		}
	}

	// The entries of before ADR-0093 go for good: their pages are written
	// anew below. One that something still points to stays, and says so.
	for _, it := range legacy {
		if !dry {
			err := db.Write(func(s *item.Store) error {
				if err := s.Trash(Actor(cfg), it.ID); err != nil {
					return err
				}
				return s.Purge(Actor(cfg), it.ID)
			})
			if err != nil {
				res.problem("%s is not removed: %v", it.Key, err)
				continue
			}
		}
		res.count("removed", it.Key, it.Title)
	}
	// Their type goes with the last of them (ADR-0094), unless other items
	// or a kanban still use it.
	if !dry && len(legacy) > 0 {
		err := db.Write(func(s *item.Store) error {
			if s.Type(LegacyType).Name == "" {
				return nil
			}
			return s.RemoveType(Actor(cfg), LegacyType)
		})
		if err != nil && !errors.Is(err, item.ErrReferenced) {
			res.problem("the type %s is not removed: %v", LegacyType, err)
		}
	}

	words := wordsIn(language(cfg))
	seen := map[string]bool{}
	// ids are the pages of the entries by SourceID, for the parents of new
	// pages and for links between pages (ADR-0128).
	ids := map[string]item.ID{}
	for sid, it := range existing {
		ids[sid] = it.ID
	}
	var later []pendingLinks
	for _, e := range entries {
		seen[e.SourceID] = true
		old, had := existing[e.SourceID]
		e, c, write, err := loadEntry(e, old, had)
		if err != nil {
			res.problem("%s: %v", cmp.Or(e.Title, e.SourceID), err)
			res.retry = res.retry || !errors.Is(err, ErrLeftOut)
			continue
		}
		u, bad := upsertFor(cfg, td, e, old, had)
		for _, p := range bad {
			res.problem("%s: %s", e.Title, p)
		}
		if !had && e.Parent != "" {
			u.Parent = ids[e.Parent]
		}
		labels := without(old.Labels, WithdrawnLabel)
		newLabels := !slices.Equal(labels, old.Labels)
		// The page is the source's: every run writes it anew, and only
		// when it changed (ADR-0080); a source read only when it changed
		// keeps its raw text, and its page is written only then (ADR-0128).
		body := item.Doc{Blocks: e.Body}
		if e.Load != nil {
			u.Origin.Version, u.Origin.Raw = e.Version, c.Raw
			if !write {
				u.Origin.Version, u.Origin.Raw = old.Origin.Version, old.Origin.Raw
				body = old.Body
			}
		}
		var pending *pendingLinks
		if write && len(c.Links) > 0 {
			var all bool
			body.Blocks, all = mentions(body.Blocks, c.Links, ids, old.ID)
			if !all {
				pending = &pendingLinks{source: e.SourceID, title: e.Title, blocks: slices.Clone(e.Body), links: c.Links}
			}
		}
		newBody := !sameBody(old.Body, body)
		kind := "new"
		if had {
			if same(old, u) && !newBody && !newLabels {
				res.Unchanged++
				continue
			}
			kind = "changed"
		}
		key := old.Key
		if !dry {
			it, err := writeEntry(db, cfg, u, labels, newLabels, body, newBody, c.Pictures)
			if err != nil {
				res.problem("%s (%s): %v", e.Title, cmp.Or(e.Dataset, e.SourceID), err)
				res.retry = true
				continue
			}
			key, ids[e.SourceID] = it.Key, it.ID
			if pending != nil {
				pending.id = it.ID
				pending.blocks = it.Body.Blocks
				later = append(later, *pending)
			}
			if !had && cfg.Steward != "" {
				watch(db, cfg, it.ID, &res)
			}
		}
		res.count(kind, key, e.Title)
	}
	// A page that links to pages made after it in this run names them now.
	for _, p := range later {
		blocks, _ := mentions(p.blocks, p.links, ids, p.id)
		if sameBody(item.Doc{Blocks: p.blocks}, item.Doc{Blocks: blocks}) {
			continue
		}
		if err := db.Write(func(s *item.Store) error { return s.CommitBody(Actor(cfg), p.id, item.Doc{Blocks: blocks}) }); err != nil {
			res.problem("%s: %v", p.title, err)
			res.retry = true
		}
	}

	// A page the source no longer publishes is marked, not deleted: its
	// history and what was added here stay. A source that answers far
	// fewer entries than the catalog holds marks nothing, as that is more
	// likely a failure than a mass withdrawal.
	active := 0
	for _, it := range existing {
		if !slices.Contains(it.Labels, WithdrawnLabel) {
			active++
		}
	}
	if len(entries) == 0 || len(seen)*2 < active {
		if active > 0 {
			res.problem("the source answered %d entries for the %d active ones here; nothing is withdrawn", len(seen), active)
		}
		return res, nil
	}
	for _, id := range slices.Sorted(maps.Keys(existing)) {
		it := existing[id]
		if seen[id] || slices.Contains(it.Labels, WithdrawnLabel) {
			continue
		}
		if !dry {
			body := item.Doc{Blocks: append([]item.Block{{Kind: item.Panel, Tone: "warning", Text: words.withdrawn}}, it.Body.Blocks...)}
			err := db.Write(func(s *item.Store) error {
				if err := s.SetLabels(Actor(cfg), it.ID, append(slices.Clone(it.Labels), WithdrawnLabel)); err != nil {
					return err
				}
				return s.CommitBody(Actor(cfg), it.ID, body)
			})
			if err != nil {
				res.problem("%s: %v", it.Key, err)
				continue
			}
		}
		res.count("withdrawn", it.Key, it.Title)
	}
	return res, nil
}

// itemExternalID is an imported item's ID in its source, or empty.
func itemExternalID(it item.Item) string {
	if it.Origin == nil {
		return ""
	}
	return it.Origin.ExternalID
}

// without returns labels without one of them, in their order.
func without(labels []string, label string) []string {
	return slices.DeleteFunc(slices.Clone(labels), func(l string) bool { return l == label })
}

// language is the one a connector's pages are written in.
func language(cfg site.Connector) string {
	switch {
	case cfg.CKAN != nil && cfg.CKAN.Language != "":
		return cfg.CKAN.Language
	case cfg.JSON != nil && cfg.JSON.Language != "":
		return cfg.JSON.Language
	case cfg.Git != nil && cfg.Git.Language != "":
		return cfg.Git.Language
	}
	return "de"
}

// watch has the connector's steward watch a new page, so they hear of
// what happens to it (ADR-0046, ADR-0093). A steward who may not read the
// space watches nothing, and the run says so.
func watch(db *storage.DB, cfg site.Connector, id item.ID, res *Result) {
	if err := db.Write(func(s *item.Store) error { return s.As(cfg.Steward).Watch(id, true) }); err != nil {
		res.problem("the steward does not watch the new page: %v", err)
	}
}

// sameBody reports whether a page is the one the source would write; a
// page without blocks and none written count as the same.
func sameBody(have, want item.Doc) bool {
	if len(have.Blocks) == 0 && len(want.Blocks) == 0 {
		return true
	}
	a, _ := json.Marshal(have.Blocks)
	b, _ := json.Marshal(want.Blocks)
	return string(a) == string(b)
}

// Fits checks that a type takes what a connector writes (ADR-0127): a
// text, every field it maps, of a kind a source's text converts to, and
// every required field mapped, as a synchronisation cannot fill others.
func Fits(td item.TypeDef, cfg site.Connector) error {
	if !td.Body {
		return fmt.Errorf("connector: type %q has no text", td.Name)
	}
	kinds := map[string]item.FieldKind{}
	for _, f := range td.Fields {
		kinds[f.Name] = f.Kind
		if _, mapped := cfg.Fields[f.Name]; f.Required && !mapped {
			return fmt.Errorf("connector: type %q requires the field %q, which the connector does not fill", td.Name, f.Name)
		}
	}
	for _, name := range slices.Sorted(maps.Keys(cfg.Fields)) {
		switch k, ok := kinds[name]; {
		case !ok:
			return fmt.Errorf("connector: type %q has no field %q", td.Name, name)
		case k == item.KindUser:
			return fmt.Errorf("connector: the field %q holds a user, which a source does not name", name)
		}
	}
	return nil
}

// upsertFor is the import of an entry: the source's title and place,
// the fields the connector maps from its values, and the item's other
// fields kept. A value that does not fit its field is left out, with
// the reason.
func upsertFor(cfg site.Connector, td item.TypeDef, e Entry, old item.Item, had bool) (item.Upsert, []string) {
	u := item.Upsert{
		Origin: item.Origin{System: cfg.ID, Instance: instance(cfg), ExternalID: prefix(cfg) + e.SourceID, ExternalKey: e.Dataset, URL: e.DatasetURL},
		Space:  cfg.Space, Type: td.Name, Title: e.Title,
	}
	if had {
		u.Parent = old.Parent
		u.Properties = maps.Clone(old.Properties)
	}
	var problems []string
	for _, f := range td.Fields {
		name, mapped := cfg.Fields[f.Name]
		if !mapped {
			continue
		}
		v, err := fieldValue(f, e.Values[name])
		if err != nil {
			problems = append(problems, fmt.Sprintf("the field %s is left as it was: %v", f.Name, err))
			continue
		}
		if u.Properties == nil {
			u.Properties = item.Props{}
		}
		if v == nil {
			delete(u.Properties, f.Name)
		} else {
			u.Properties[f.Name] = v
		}
	}
	return u, problems
}

// fieldValue converts a source's text to a value of a field's kind; an
// empty text is no value.
func fieldValue(f item.FieldDef, s string) (any, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	switch f.Kind {
	case item.KindText:
		return s, nil
	case item.KindNumber:
		n, err := strconv.ParseFloat(s, 64)
		if err != nil || math.IsNaN(n) || math.IsInf(n, 0) {
			return nil, fmt.Errorf("%q is not a number", s)
		}
		return n, nil
	case item.KindBool:
		b, err := strconv.ParseBool(s)
		if err != nil {
			return nil, fmt.Errorf("%q is neither true nor false", s)
		}
		return b, nil
	case item.KindDate:
		if t := parseDate(s); !t.IsZero() {
			return t, nil
		}
		return nil, fmt.Errorf("%q is not a date", s)
	case item.KindEnum:
		if slices.Contains(f.Values, s) {
			return s, nil
		}
		return nil, fmt.Errorf("%q is not one of its values", s)
	case item.KindURL:
		if webLink(s) && item.SafeHref(s) {
			return s, nil
		}
		return nil, fmt.Errorf("%q is not a web address", s)
	}
	return nil, fmt.Errorf("a %s field is not filled from a source", f.Kind)
}

// same reports whether an import would change nothing.
func same(it item.Item, u item.Upsert) bool {
	return it.Title == u.Title && it.Parent == u.Parent && it.Origin != nil && *it.Origin == u.Origin &&
		reflect.DeepEqual(nonEmpty(it.Properties), nonEmpty(u.Properties))
}

// nonEmpty treats no properties and an empty set of them alike.
func nonEmpty(p item.Props) item.Props {
	if len(p) == 0 {
		return nil
	}
	return p
}

// loadEntry reads an entry's content when the entry has to be read
// (ADR-0128): when it is new, withdrawn, or its Blob does not tell that
// the raw text kept in its origin is still the source's. write reports
// whether its page is to be written; an entry without Load always is.
func loadEntry(e Entry, old item.Item, had bool) (Entry, Content, bool, error) {
	if e.Load == nil {
		return e, Content{}, true, nil
	}
	kept := ""
	if had && old.Origin != nil {
		kept = old.Origin.Raw
	}
	fresh := !had || slices.Contains(old.Labels, WithdrawnLabel) || kept == ""
	if !fresh && e.Blob != "" && gitrepo.BlobID([]byte(kept), e.Blob) == e.Blob {
		e.Title = old.Title
		return e, Content{}, false, nil
	}
	c, err := e.Load()
	if err != nil {
		return e, c, false, err
	}
	if !fresh && c.Raw == kept {
		e.Title = old.Title
		return e, Content{}, false, nil
	}
	e.Title, e.Body = c.Title, c.Body
	return e, c, true, nil
}

// pendingLinks is a page whose links name pages that did not exist yet
// when it was written.
type pendingLinks struct {
	id     item.ID
	source string
	title  string
	blocks []item.Block
	links  map[string]string
}

// mentions turns the links of blocks that lead to other entries' pages
// into mentions of those pages (D2), and reports whether every such link
// found its page. A link to the page itself stays a link.
func mentions(blocks []item.Block, links map[string]string, ids map[string]item.ID, self item.ID) ([]item.Block, bool) {
	all := true
	out := make([]item.Block, len(blocks))
	for i, b := range blocks {
		b.Marks = slices.Clone(b.Marks)
		b.Mentions = slices.Clone(b.Mentions)
		for j, m := range b.Marks {
			if m.Type != item.MarkLink {
				continue
			}
			target, ok := links[strings.SplitN(m.Href, "#", 2)[0]]
			if !ok {
				continue
			}
			id := ids[target]
			switch {
			case id == "":
				all = false
			case id != self:
				b.Marks[j] = item.Mark{Type: item.MarkMention, Start: m.Start, End: m.End, Ref: id}
				if !slices.Contains(b.Mentions, id) {
					b.Mentions = append(b.Mentions, id)
				}
			}
		}
		out[i] = b
	}
	return out, all
}

// maxPictures bounds how many pictures one page keeps (ADR-0128).
const maxPictures = 50

// pictureTypes are the kinds of picture a page keeps: the ones a page
// shows inline (ADR-0045). An SVG can carry script and stays a link.
var pictureTypes = []string{"image/png", "image/jpeg", "image/gif", "image/webp"}

// writeEntry writes an entry's page: the import, its labels, the
// pictures it shows as attachments, and its body. A picture already
// attached with the same content is not stored again.
func writeEntry(db *storage.DB, cfg site.Connector, u item.Upsert, labels []string, newLabels bool, body item.Doc, newBody bool, pictures []Picture) (item.Item, error) {
	var it item.Item
	err := db.Write(func(s *item.Store) error {
		var err error
		if it, _, err = s.Upsert(Actor(cfg), u); err != nil {
			return err
		}
		if newLabels {
			return s.SetLabels(Actor(cfg), it.ID, labels)
		}
		return nil
	})
	if err != nil || !newBody {
		return it, err
	}
	body.Blocks = slices.Clone(body.Blocks)
	var added []item.Attachment
	for n, p := range pictures {
		if n == maxPictures || !slices.Contains(pictureTypes, http.DetectContentType(p.Data)) || int64(len(p.Data)) > budget.Get().AttachmentBytes {
			continue
		}
		sum := sha256.Sum256(p.Data)
		var ref item.ID
		for _, a := range it.Attachments {
			if !a.Deleted && a.SHA256 == hex.EncodeToString(sum[:]) {
				ref = a.ID
			}
		}
		if ref == "" {
			a, err := db.StoreAttachment(it.ID, bytes.NewReader(p.Data), budget.Get().AttachmentBytes)
			if err != nil {
				return it, err
			}
			a.Name = p.Name
			added = append(added, a)
			ref = a.ID
		}
		body.Blocks[p.Block] = item.Block{Kind: item.Image, Ref: ref, Text: p.Caption}
	}
	recorded := 0
	err = db.Write(func(s *item.Store) error {
		for _, a := range added {
			if _, err := s.AddAttachment(Actor(cfg), it.ID, a); err != nil {
				return err
			}
			recorded++
		}
		if err := s.CommitBody(Actor(cfg), it.ID, body); err != nil {
			return err
		}
		it, _ = s.Get(it.ID)
		return nil
	})
	// A stored file that was never recorded goes at once; the next start
	// would remove it too.
	for _, a := range added[recorded:] {
		db.DiscardAttachment(it.ID, a.ID)
	}
	return it, err
}
