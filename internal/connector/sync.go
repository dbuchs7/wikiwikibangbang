package connector

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
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
	if c.CKAN != nil {
		return c.CKAN.BaseURL
	}
	return ""
}

// PageType is the type of the catalog's pages: an ordinary wiki page,
// with a text and no fields or workflow (ADR-0093).
const PageType = "page"

// LegacyType is the type of the catalog's entries before ADR-0093, with
// fields and a workflow. A run removes them and writes pages instead.
const LegacyType = "interface"

// WithdrawnLabel marks a page whose data set the source no longer
// publishes; a page has no state to say so (ADR-0093).
const WithdrawnLabel = "withdrawn"

// pageExternalID is a page's ID in its source. It differs from the ID of
// the entry it replaces, so that entry's erasure does not keep the page
// from being written (ADR-0026, ADR-0093).
func pageExternalID(sourceID string) string { return "page/" + sourceID }

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

// Apply brings the space in step with the entries: a page is written for
// each new or changed data set, and a page whose data set the source no
// longer publishes is marked withdrawn. Entries of before ADR-0093 are
// erased, so their pages take their place. With dry, nothing is written.
func Apply(db *storage.DB, cfg site.Connector, entries []Entry, dry bool) (Result, error) {
	res := Result{Connector: cfg.ID, DryRun: dry, Entries: len(entries), Problems: []string{}, Examples: []Change{}}
	ours := func(it item.Item) bool {
		return it.Origin != nil && it.Origin.System == cfg.ID && it.Origin.Instance == instance(cfg)
	}
	var existing map[string]item.Item
	var legacy []item.Item
	var typeErr error
	hasSpace, hasType := false, false
	db.Read(func(s *item.Store) {
		hasSpace = s.HasSpace(cfg.Space)
		existing = map[string]item.Item{}
		for _, it := range s.Find(item.Query{Space: cfg.Space, Type: PageType}) {
			if id, ok := strings.CutPrefix(itemExternalID(it), "page/"); ok && ours(it) {
				existing[id] = it
			}
		}
		for _, it := range s.Find(item.Query{Space: cfg.Space, Type: LegacyType}) {
			if ours(it) {
				legacy = append(legacy, it)
			}
		}
		if td := s.Type(PageType); td.Name != "" {
			hasType, typeErr = true, fits(td)
		}
	})
	if !hasSpace {
		return res, ErrNoSpace
	}
	if typeErr != nil {
		return res, typeErr
	}
	if !hasType && !dry {
		if err := db.Write(func(s *item.Store) error { return s.DefineType(Actor(cfg), PageTypeDef()) }); err != nil {
			return res, err
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
		u := upsertFor(cfg, e, old, had)
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
	if cfg.CKAN != nil && cfg.CKAN.Language != "" {
		return cfg.CKAN.Language
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

// fits checks that an existing page type takes what the synchronisation
// writes: a text, and no field it would have to fill.
func fits(td item.TypeDef) error {
	if !td.Body {
		return fmt.Errorf("connector: type %q has no text", PageType)
	}
	for _, f := range td.Fields {
		if f.Required {
			return fmt.Errorf("connector: type %q requires the field %q, which a synchronisation cannot fill", PageType, f.Name)
		}
	}
	return nil
}

// upsertFor is the import of an entry: the source's title and place,
// the page's own fields, if its type has any, kept.
func upsertFor(cfg site.Connector, e Entry, old item.Item, had bool) item.Upsert {
	u := item.Upsert{
		Origin: item.Origin{System: cfg.ID, Instance: instance(cfg), ExternalID: pageExternalID(e.SourceID), ExternalKey: e.Dataset, URL: e.DatasetURL},
		Space:  cfg.Space, Type: PageType, Title: e.Title,
	}
	if had {
		u.Parent = old.Parent
		u.Properties = maps.Clone(old.Properties)
	}
	return u
}

// same reports whether an import would change nothing.
func same(it item.Item, u item.Upsert) bool {
	return it.Title == u.Title && it.Parent == u.Parent && it.Origin != nil && *it.Origin == u.Origin
}
