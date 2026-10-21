package opendata

import (
	"errors"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"time"

	"github.com/dbuchs7/wikiwikibangbang/internal/item"
	"github.com/dbuchs7/wikiwikibangbang/internal/site"
	"github.com/dbuchs7/wikiwikibangbang/internal/storage"
)

// System names the source in an item's origin (ADR-0023).
const System = "opendata.swiss"

// Actor is who the synchronisation's changes are recorded as.
const Actor = "sync:opendata.swiss"

// TypeName is the type of the catalog's entries.
const TypeName = "interface"

// The workflow states of an entry: active while the source publishes it,
// withdrawn once it no longer does.
const (
	Active    = "active"
	Withdrawn = "withdrawn"
)

// The fields of an entry. The synchronisation writes the ones the source
// knows; owner and reviewed belong to the catalog and are kept. Contact
// and specification are taken from the source when it has them, and
// otherwise kept, so a contact added here is not lost.
const (
	FieldSourceID      = "source-id"
	FieldFormat        = "format"
	FieldEndpoint      = "endpoint"
	FieldSpecification = "specification"
	FieldPublisher     = "publisher"
	FieldContact       = "contact"
	FieldDataset       = "dataset"
	FieldModified      = "modified"
	FieldOwner         = "owner"
	FieldReviewed      = "reviewed"
)

// TypeDef is the type the synchronisation defines when the space has none:
// an entry is identified by its source ID, has a publisher, should have an
// owner here, a contact and a specification, and is reviewed every year
// (ADR-0074).
func TypeDef() item.TypeDef {
	return item.TypeDef{Name: TypeName, Body: true, Fields: []item.FieldDef{
		{Name: FieldSourceID, Kind: item.KindText, Required: true, Unique: true},
		{Name: FieldFormat, Kind: item.KindText},
		{Name: FieldEndpoint, Kind: item.KindURL},
		{Name: FieldSpecification, Kind: item.KindURL, Recommended: true},
		{Name: FieldPublisher, Kind: item.KindText, Required: true},
		{Name: FieldOwner, Kind: item.KindUser, Recommended: true},
		{Name: FieldContact, Kind: item.KindText, Recommended: true},
		{Name: FieldDataset, Kind: item.KindURL},
		{Name: FieldModified, Kind: item.KindDate},
		{Name: FieldReviewed, Kind: item.KindDate},
	}, Workflow: &item.Workflow{Initial: Active, States: []string{Active, Withdrawn}, Transitions: []item.Transition{
		{Name: "withdraw", From: []string{Active}, To: Withdrawn},
		{Name: "reactivate", From: []string{Withdrawn}, To: Active},
	}}, Review: &item.Review{EveryDays: 365, Field: FieldReviewed, Owner: FieldOwner}}
}

// Change is one entry a run created, changed or withdrew.
type Change struct {
	Kind  string `json:"kind"` // new, changed, withdrawn
	Key   string `json:"key,omitempty"`
	Title string `json:"title"`
}

// Result is what a run did, or with DryRun would do.
type Result struct {
	DryRun   bool      `json:"dryRun"`
	Started  time.Time `json:"started"`
	Seconds  float64   `json:"seconds"`
	Datasets int       `json:"datasets"`
	// Formats are the source's formats the run read, as it writes them.
	Formats   []string `json:"formats"`
	Entries   int      `json:"entries"`
	New       int      `json:"new"`
	Changed   int      `json:"changed"`
	Unchanged int      `json:"unchanged"`
	Withdrawn int      `json:"withdrawn"`
	Problems  []string `json:"problems"`
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
	}
	if len(r.Examples) < examplesShown {
		r.Examples = append(r.Examples, Change{Kind: kind, Key: key, Title: title})
	}
}

// ErrNoSpace is a synchronisation into a space that does not exist.
var ErrNoSpace = errors.New("opendata: the catalog's space does not exist")

// Apply brings the space in step with the entries: new ones are created,
// changed ones updated, and those the source no longer publishes are
// withdrawn. With dry, nothing is written.
func Apply(db *storage.DB, cfg site.OpenData, entries []Entry, dry bool) (Result, error) {
	res := Result{DryRun: dry, Entries: len(entries), Problems: []string{}, Examples: []Change{}}
	var existing map[string]item.Item
	var typeErr error
	hasSpace, hasType := false, false
	db.Read(func(s *item.Store) {
		hasSpace = s.HasSpace(cfg.Space)
		existing = map[string]item.Item{}
		for _, it := range s.Find(item.Query{Space: cfg.Space, Type: TypeName}) {
			if it.Origin != nil && it.Origin.System == System && it.Origin.Instance == cfg.BaseURL {
				existing[it.Origin.ExternalID] = it
			}
		}
		if td := s.Type(TypeName); td.Name != "" {
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
		if err := db.Write(func(s *item.Store) error { return s.DefineType(Actor, TypeDef()) }); err != nil {
			return res, err
		}
	}

	seen := map[string]bool{}
	for _, e := range entries {
		seen[e.SourceID] = true
		old, had := existing[e.SourceID]
		u := upsertFor(cfg, e, old, had)
		kind := "new"
		if had {
			if same(old, u) {
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
				it, _, err = s.Upsert(Actor, u)
				return err
			})
			if err != nil {
				res.problem("%s (%s): %v", e.Title, e.Dataset, err)
				continue
			}
			key = it.Key
		}
		res.count(kind, key, e.Title)
	}

	// An entry the source no longer publishes is withdrawn, not deleted:
	// its history and what was added here stay. A source that answers far
	// fewer entries than the catalog holds withdraws nothing, as that is
	// more likely a failure than a mass withdrawal.
	active := 0
	for _, it := range existing {
		if it.State != Withdrawn {
			active++
		}
	}
	if len(entries) == 0 || len(seen)*2 < active {
		if active > 0 {
			res.problem("the source answered %d entries for the %d active ones here; nothing is withdrawn", len(seen), active)
		}
		return res, nil
	}
	ids := slices.Sorted(maps.Keys(existing))
	for _, id := range ids {
		it := existing[id]
		if seen[id] || it.State == Withdrawn {
			continue
		}
		u := item.Upsert{Origin: *it.Origin, Space: it.Space, Type: it.Type, Parent: it.Parent, Title: it.Title,
			Properties: it.Properties, State: Withdrawn}
		if !dry {
			if err := db.Write(func(s *item.Store) error { _, _, err := s.Upsert(Actor, u); return err }); err != nil {
				res.problem("%s: %v", it.Key, err)
				continue
			}
		}
		res.count("withdrawn", it.Key, it.Title)
	}
	return res, nil
}

// fits checks that an existing type has the fields the synchronisation
// writes, of the kinds it writes.
func fits(td item.TypeDef) error {
	for _, need := range TypeDef().Fields {
		name, kind := need.Name, need.Kind
		found := false
		for _, f := range td.Fields {
			if f.Name == name {
				if f.Kind != kind {
					return fmt.Errorf("opendata: the field %q of type %q is %s, not %s", name, TypeName, f.Kind, kind)
				}
				found = true
			}
		}
		if !found {
			return fmt.Errorf("opendata: type %q has no field %q", TypeName, name)
		}
	}
	if td.Workflow == nil || !slices.Contains(td.Workflow.States, Active) || !slices.Contains(td.Workflow.States, Withdrawn) {
		return fmt.Errorf("opendata: type %q needs the states %s and %s", TypeName, Active, Withdrawn)
	}
	return nil
}

// upsertFor is the import of an entry: the source's fields over what the
// item holds, the catalog's own fields kept.
func upsertFor(cfg site.OpenData, e Entry, old item.Item, had bool) item.Upsert {
	props := item.Props{}
	if had {
		props = maps.Clone(old.Properties)
	} else if cfg.Steward != "" {
		props[FieldOwner] = cfg.Steward
	}
	set := func(name string, v any, keepWhenEmpty bool) {
		switch x := v.(type) {
		case string:
			if x == "" {
				if !keepWhenEmpty {
					delete(props, name)
				}
				return
			}
		case time.Time:
			if x.IsZero() {
				if !keepWhenEmpty {
					delete(props, name)
				}
				return
			}
		}
		props[name] = v
	}
	set(FieldSourceID, e.SourceID, false)
	set(FieldFormat, e.Format, false)
	set(FieldEndpoint, e.Endpoint, false)
	set(FieldSpecification, e.Specification, true)
	set(FieldPublisher, e.Publisher, false)
	set(FieldContact, e.Contact, true)
	set(FieldDataset, e.DatasetURL, false)
	set(FieldModified, e.Modified, false)
	if props[FieldPublisher] == nil {
		props[FieldPublisher] = "unknown"
	}
	u := item.Upsert{
		Origin: item.Origin{System: System, Instance: cfg.BaseURL, ExternalID: e.SourceID, ExternalKey: e.Dataset, URL: e.DatasetURL},
		Space:  cfg.Space, Type: TypeName, Title: e.Title, Properties: props, State: Active,
	}
	if had {
		u.Parent = old.Parent
	}
	return u
}

// same reports whether an import would change nothing.
func same(it item.Item, u item.Upsert) bool {
	return it.Title == u.Title && it.State == u.State && it.Parent == u.Parent &&
		reflect.DeepEqual(it.Properties, u.Properties) && it.Origin != nil && *it.Origin == u.Origin
}
