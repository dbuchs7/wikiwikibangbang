package connector

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"math"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"

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
	for _, e := range entries {
		seen[e.SourceID] = true
		old, had := existing[e.SourceID]
		u, bad := upsertFor(cfg, td, e, old, had)
		for _, p := range bad {
			res.problem("%s: %s", e.Title, p)
		}
		// The page is the source's: every run writes it anew, and only
		// when it changed (ADR-0080).
		body := item.Doc{Blocks: e.Body}
		labels := without(old.Labels, WithdrawnLabel)
		newBody, newLabels := !sameBody(old.Body, body), !slices.Equal(labels, old.Labels)
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
			var it item.Item
			err := db.Write(func(s *item.Store) error {
				var err error
				if it, _, err = s.Upsert(Actor(cfg), u); err != nil {
					return err
				}
				if newLabels {
					if err := s.SetLabels(Actor(cfg), it.ID, labels); err != nil {
						return err
					}
				}
				if !newBody {
					return nil
				}
				return s.CommitBody(Actor(cfg), it.ID, body)
			})
			if err != nil {
				res.problem("%s (%s): %v", e.Title, e.Dataset, err)
				continue
			}
			key = it.Key
			if !had && cfg.Steward != "" {
				watch(db, cfg, it.ID, &res)
			}
		}
		res.count(kind, key, e.Title)
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
