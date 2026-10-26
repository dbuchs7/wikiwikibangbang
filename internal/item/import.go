package item

import (
	"maps"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Origin records where an imported item came from (ADR-0023). System,
// Instance and ExternalID identify it; a later import run matches on them,
// never on titles. Raw keeps the source's original representation so the
// item can be re-converted after the source is gone; it is content and is
// removed by purge and redaction, while the identity fields stay.
type Origin struct {
	System      string
	Instance    string
	ExternalID  string
	ExternalKey string
	URL         string
	Version     string
	Raw         string
}

func (o Origin) key() string { return o.System + "\x00" + o.Instance + "\x00" + o.ExternalID }

// Upsert is one imported item as an import run wants it to be.
type Upsert struct {
	Origin     Origin
	Space      string
	Type       string
	Parent     ID
	Title      string
	Properties Props
	// State is the source's current state, set directly: imported history
	// is data, not a replay of transitions (ADR-0023). Empty keeps the
	// current state, or the initial state for a new item.
	State string
	// Key is the key the item had in the source. It is kept when it has the
	// space's prefix and is free; otherwise a new key is assigned.
	Key string
	// Managed marks the item as under a running one-way sync; sessions may
	// not change it until Release.
	Managed string
	// ID, if set, is the ID of a new item: a restore from a backup brings
	// an item back with its identity (ADR-0124). The store must not know
	// it, live, in the trash or as a tombstone, so no ID is used twice (D7).
	ID ID
}

var importedKeyRE = regexp.MustCompile(`^([A-Z][A-Z0-9]*)-([1-9][0-9]*)$`)

// Upsert creates or updates an imported item, matched by its origin. It
// returns the item and whether anything changed; an unchanged re-run writes
// no event. It is a trusted command: an import job calls it, not a person.
func (s *Store) Upsert(actor string, u Upsert) (Item, bool, error) {
	if u.Origin.System == "" || u.Origin.Instance == "" || u.Origin.ExternalID == "" {
		return Item{}, false, invalid("an imported item needs system, instance and external ID")
	}
	if !s.HasSpace(u.Space) {
		return Item{}, false, notFound("space %q", u.Space)
	}
	td, ok := s.latest(u.Type)
	if !ok {
		return Item{}, false, notFound("type %q", u.Type)
	}
	if strings.TrimSpace(u.Title) == "" {
		return Item{}, false, invalid("title is empty")
	}
	if err := checkProps(td, u.Properties); err != nil {
		return Item{}, false, err
	}
	for _, f := range td.Fields {
		if f.Required && u.Properties[f.Name] == nil {
			return Item{}, false, invalid("field %q is required", f.Name)
		}
	}
	if u.State != "" {
		if td.Workflow == nil || !slices.Contains(td.Workflow.States, u.State) {
			return Item{}, false, invalid("type %q has no state %q", td.Name, u.State)
		}
	}
	if u.Parent != "" {
		p, err := s.live(u.Parent)
		if err != nil {
			return Item{}, false, err
		}
		if p.Space != u.Space {
			return Item{}, false, invalid("parent %s is not in space %s", p.Key, u.Space)
		}
	}

	e := Event{Kind: ItemImported, Space: u.Space, TypeName: td.Name, TypeVersion: td.Version,
		Parent: u.Parent, Title: u.Title, Props: maps.Clone(u.Properties), To: u.State, Managed: u.Managed}
	origin := u.Origin
	e.Origin = &origin

	if id, ok := s.bySource[u.Origin.key()]; ok {
		it := s.items[id]
		switch {
		case it.Presence != Live:
			return Item{}, false, invalid("imported item %s is %s; it is not re-imported", it.Key, it.Presence)
		case it.Space != u.Space:
			return Item{}, false, invalid("imported item %s is in %s; an import does not move it to %s", it.Key, it.Space, u.Space)
		case it.Type != td.Name:
			return Item{}, false, invalid("imported item %s has type %s; an import does not change it to %s", it.Key, it.Type, td.Name)
		case u.Parent != "" && slices.Contains(s.subtree(id), u.Parent):
			return Item{}, false, invalid("cannot move an item below itself")
		}
		if e.To == "" {
			e.To = it.State
		}
		if it.Title == u.Title && it.Parent == u.Parent && it.State == e.To && it.Managed == u.Managed &&
			reflect.DeepEqual(it.Properties, nonNil(u.Properties)) && *it.Origin == origin {
			return it.clone(), false, nil
		}
		if err := s.duplicate(u.Space, td, id, u.Properties); err != nil {
			return Item{}, false, err
		}
		e.Item = id
		if err := s.record(actor, e); err != nil {
			return Item{}, false, err
		}
		return s.items[id].clone(), true, nil
	}

	if m := importedKeyRE.FindStringSubmatch(u.Key); m != nil && m[1] == u.Space {
		if _, taken := s.keys[u.Key]; !taken {
			e.Key = u.Key
		}
	}
	if e.To == "" && td.Workflow != nil {
		e.To = td.Workflow.Initial
	}
	if err := s.duplicate(u.Space, td, "", u.Properties); err != nil {
		return Item{}, false, err
	}
	e.Item = s.newID()
	if u.ID != "" {
		if !ValidItemID(string(u.ID)) {
			return Item{}, false, invalid("item ID %q", u.ID)
		}
		if _, known := s.items[u.ID]; known {
			return Item{}, false, invalid("item %s exists; its ID is not used again", u.ID)
		}
		e.Item = u.ID
	}
	if err := s.record(actor, e); err != nil {
		return Item{}, false, err
	}
	return s.items[e.Item].clone(), true, nil
}

// FindByOrigin returns the item imported from a source object.
func (s *Store) FindByOrigin(system, instance, externalID string) (Item, bool) {
	id, ok := s.bySource[Origin{System: system, Instance: instance, ExternalID: externalID}.key()]
	if !ok {
		return Item{}, false
	}
	return s.items[id].clone(), true
}

// FindBySystem returns the live items whose origin names a system, in the
// order they were made, e.g. the pages that mirror files of Git
// repositories (ADR-0137).
func (s *Store) FindBySystem(system string) []Item {
	var out []Item
	for key, id := range s.bySource {
		if it := s.items[id]; strings.HasPrefix(key, system+"\x00") && it.Presence == Live {
			out = append(out, it.clone())
		}
	}
	slices.SortFunc(out, func(a, b Item) int { return a.created - b.created })
	return out
}

// Mirror makes or updates, as the principal, a page that mirrors a file
// of another system (ADR-0137), matched by its origin as an import is. It
// counts as making a page below an item the principal may edit; a page
// made before must be one the principal may edit, and stays where it is.
func (x *Session) Mirror(u Upsert) (Item, error) {
	if u.Parent == "" {
		return Item{}, invalid("a mirrored page is made below an item")
	}
	id, had := x.s.bySource[u.Origin.key()]
	if had {
		it, err := x.editable(id)
		if err != nil {
			return Item{}, err
		}
		u.Space, u.Parent = it.Space, it.Parent
	} else {
		p, err := x.editable(u.Parent)
		if err != nil {
			return Item{}, err
		}
		u.Space = p.Space
	}
	u.Managed, u.Key, u.ID, u.State = "", "", "", ""
	it, _, err := x.s.Upsert(x.who, u)
	if err != nil || had {
		return it, err
	}
	// A person watches what they make (ADR-0046).
	return it, x.Watch(it.ID, true)
}

// Release ends a one-way sync for a space (cut-over, ADR-0023): items managed
// by the tag become ordinary items that sessions may change.
func (s *Store) Release(actor, managed, space string) error {
	if managed == "" {
		return invalid("release needs the sync's tag")
	}
	if !s.HasSpace(space) {
		return notFound("space %q", space)
	}
	var members []ID
	for _, it := range s.items {
		if it.Managed == managed && it.Space == space {
			members = append(members, it.ID)
		}
	}
	slices.SortFunc(members, func(a, b ID) int { return s.items[a].created - s.items[b].created })
	return s.record(actor, Event{Kind: ItemReleased, Space: space, Managed: managed, Members: members})
}

func (s *Store) applyImport(e Event, touch func(...ID)) {
	switch e.Kind {
	case ItemImported:
		origin := *e.Origin
		it, exists := s.items[e.Item]
		if !exists {
			it = &Item{ID: e.Item, Space: e.Space, Presence: Live, created: e.Seq}
			s.items[it.ID] = it
			if e.Key != "" {
				s.reserveKey(it, e.Key)
			} else {
				s.assignKey(it)
			}
		}
		switch {
		case e.To == "":
			it.StateSince = time.Time{}
		case !exists || it.State != e.To:
			it.StateSince = e.At
		}
		it.Type, it.TypeVersion = e.TypeName, e.TypeVersion
		it.Parent, it.Title, it.State = e.Parent, e.Title, e.To
		it.Properties = nonNil(maps.Clone(e.Props))
		it.Origin, it.Managed = &origin, e.Managed
		it.Version++
		s.bySource[origin.key()] = it.ID
		touch(it.ID)
	case ItemReleased:
		for _, id := range e.Members {
			s.items[id].Managed = ""
			s.items[id].Version++
		}
		touch(e.Members...)
	}
}

// reserveKey gives an imported item its source key and moves the space's
// counter above it, so later keys never collide with imported ones.
func (s *Store) reserveKey(it *Item, key string) {
	sp := s.spaces[it.Space]
	n, _ := strconv.Atoi(importedKeyRE.FindStringSubmatch(key)[2])
	if n > sp.nextKey {
		sp.nextKey = n
	}
	it.Key = key
	s.keys[key] = it.ID
}

func nonNil(p Props) Props {
	if p == nil {
		return Props{}
	}
	return p
}
