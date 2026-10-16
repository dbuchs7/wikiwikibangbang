package item

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"maps"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"time"
)

// Store holds items, relations and the event log in memory. Commands
// validate, record one event and apply it; apply is the only code that
// changes state, and it runs both live and on Replay.
type Store struct {
	now   func() time.Time
	newID func() ID

	log      []Event
	types    map[string][]TypeDef // all versions, oldest first
	spaces   map[string]*space
	items    map[ID]*Item
	keys     map[string]ID // current and former keys (D2)
	rels     map[ID]*Relation
	touching map[ID][]int // item -> indexes into log, for History
}

type space struct {
	name    string
	nextKey int
}

// Option configures a Store.
type Option func(*Store)

// WithClock sets the clock used to timestamp events.
func WithClock(now func() time.Time) Option { return func(s *Store) { s.now = now } }

// WithIDs sets the ID generator.
func WithIDs(gen func() ID) Option { return func(s *Store) { s.newID = gen } }

// NewStore returns an empty store.
func NewStore(opts ...Option) *Store {
	s := &Store{
		now:      time.Now,
		newID:    randomID,
		types:    map[string][]TypeDef{},
		spaces:   map[string]*space{},
		items:    map[ID]*Item{},
		keys:     map[string]ID{},
		rels:     map[ID]*Relation{},
		touching: map[ID][]int{},
	}
	for _, o := range opts {
		o(s)
	}
	return s
}

func randomID() ID {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err) // crypto/rand does not fail on supported platforms
	}
	return ID(hex.EncodeToString(b[:]))
}

// Replay rebuilds a store from a log. The log must be complete: sequence
// numbers start at 1 without gaps.
func Replay(log []Event) (*Store, error) {
	s := NewStore()
	for i, e := range log {
		if e.Seq != i+1 {
			return nil, fmt.Errorf("event %d has sequence number %d", i+1, e.Seq)
		}
		s.apply(e)
	}
	return s, nil
}

// Log returns a copy of the event log.
func (s *Store) Log() []Event { return slices.Clone(s.log) }

func (s *Store) record(actor string, e Event) {
	e.Seq = len(s.log) + 1
	e.At = s.now()
	e.Actor = actor
	s.apply(e)
}

// --- commands ---------------------------------------------------------------

// DefineType defines a new type or a new version of an existing one.
func (s *Store) DefineType(actor string, td TypeDef) error {
	if err := td.validate(); err != nil {
		return err
	}
	td.Version = len(s.types[td.Name]) + 1
	s.record(actor, Event{Kind: TypeDefined, TypeDef: &td})
	return nil
}

var spaceKeyRE = regexp.MustCompile(`^[A-Z][A-Z0-9]*$`)

// CreateSpace creates a space. Its key prefixes the keys of its items.
func (s *Store) CreateSpace(actor, key, name string) error {
	if !spaceKeyRE.MatchString(key) {
		return invalid("space key %q must be upper-case letters and digits", key)
	}
	if _, ok := s.spaces[key]; ok {
		return invalid("space %q exists", key)
	}
	s.record(actor, Event{Kind: SpaceCreated, Space: key, SpaceName: name})
	return nil
}

// NewItem describes an item to create.
type NewItem struct {
	Space      string
	Type       string
	Parent     ID
	Title      string
	Properties Props
}

// Create creates an item and returns it.
func (s *Store) Create(actor string, n NewItem) (Item, error) {
	if _, ok := s.spaces[n.Space]; !ok {
		return Item{}, notFound("space %q", n.Space)
	}
	td, ok := s.latest(n.Type)
	if !ok {
		return Item{}, notFound("type %q", n.Type)
	}
	if strings.TrimSpace(n.Title) == "" {
		return Item{}, invalid("title is empty")
	}
	if n.Parent != "" {
		p, ok := s.items[n.Parent]
		if !ok {
			return Item{}, notFound("parent %s", n.Parent)
		}
		if p.Space != n.Space {
			return Item{}, invalid("parent %s is in space %s, not %s", p.Key, p.Space, n.Space)
		}
	}
	if err := checkProps(td, n.Properties); err != nil {
		return Item{}, err
	}
	for _, f := range td.Fields {
		if f.Required && n.Properties[f.Name] == nil {
			return Item{}, invalid("field %q is required", f.Name)
		}
	}
	id := s.newID()
	s.record(actor, Event{
		Kind: ItemCreated, Item: id, Space: n.Space, TypeName: td.Name, TypeVersion: td.Version,
		Parent: n.Parent, Title: n.Title, Props: maps.Clone(n.Properties),
	})
	return s.items[id].clone(), nil
}

// SetTitle renames an item.
func (s *Store) SetTitle(actor string, id ID, title string) error {
	if _, err := s.item(id); err != nil {
		return err
	}
	if strings.TrimSpace(title) == "" {
		return invalid("title is empty")
	}
	s.record(actor, Event{Kind: TitleSet, Item: id, Title: title})
	return nil
}

// SetProperty sets or (with nil) clears one property.
func (s *Store) SetProperty(actor string, id ID, field string, value any) error {
	it, err := s.item(id)
	if err != nil {
		return err
	}
	td, _ := s.latest(it.Type)
	if err := checkProps(td, Props{field: value}); err != nil {
		return err
	}
	s.record(actor, Event{Kind: PropertySet, Item: id, Field: field, Value: value})
	return nil
}

// Transition moves an item along its type's workflow (D3).
func (s *Store) Transition(actor string, id ID, name string) error {
	it, err := s.item(id)
	if err != nil {
		return err
	}
	td, _ := s.latest(it.Type)
	if td.Workflow == nil {
		return invalid("type %q has no workflow", td.Name)
	}
	tr, ok := td.Workflow.transition(name)
	if !ok {
		return invalid("type %q has no transition %q", td.Name, name)
	}
	if !slices.Contains(tr.From, it.State) {
		return invalid("transition %q is not allowed from state %q", name, it.State)
	}
	for _, req := range tr.Requires {
		if it.Properties[req] == nil {
			return invalid("transition %q requires field %q", name, req)
		}
	}
	s.record(actor, Event{Kind: Transitioned, Item: id, Transition: name, From: it.State, To: tr.To})
	return nil
}

// Move changes an item's space and parent. Moving to another space moves the
// whole subtree, because a parent and its children share a space; the moved
// items get new keys and their old keys keep resolving (D2, D5).
func (s *Store) Move(actor string, id ID, toSpace string, newParent ID) error {
	if _, err := s.item(id); err != nil {
		return err
	}
	if _, ok := s.spaces[toSpace]; !ok {
		return notFound("space %q", toSpace)
	}
	if newParent != "" {
		p, ok := s.items[newParent]
		if !ok {
			return notFound("parent %s", newParent)
		}
		if p.Space != toSpace {
			return invalid("parent %s is not in space %s", p.Key, toSpace)
		}
		if slices.Contains(s.subtree(id), newParent) {
			return invalid("cannot move an item below itself")
		}
	}
	s.record(actor, Event{Kind: ItemMoved, Item: id, Space: toSpace, Parent: newParent})
	return nil
}

// CommitBody commits a new version of an item's body.
func (s *Store) CommitBody(actor string, id ID, d Doc) error {
	it, err := s.item(id)
	if err != nil {
		return err
	}
	if td, _ := s.latest(it.Type); !td.Body {
		return invalid("type %q has no body", td.Name)
	}
	for _, b := range d.Blocks {
		switch b.Kind {
		case Paragraph, Heading:
		case Embed:
			if b.Ref == "" || b.Ref == id {
				return invalid("an embed needs a reference to another item")
			}
		default:
			return invalid("unknown block kind %q", b.Kind)
		}
	}
	for _, r := range d.refs() {
		if _, ok := s.items[r.To]; !ok {
			return notFound("referenced item %s", r.To)
		}
	}
	body := d.clone()
	s.record(actor, Event{Kind: BodyCommitted, Item: id, Body: &body})
	return nil
}

// AddRelation adds a typed relation by hand.
func (s *Store) AddRelation(actor, typ string, from, to ID) error {
	switch {
	case strings.TrimSpace(typ) == "":
		return invalid("relation type is empty")
	case typ == RelMentions || typ == RelEmbeds:
		return invalid("relation type %q is derived from bodies", typ)
	case from == to:
		return invalid("an item cannot relate to itself")
	}
	for _, id := range []ID{from, to} {
		if _, err := s.item(id); err != nil {
			return err
		}
	}
	for _, r := range s.rels {
		if !r.Derived && r.Type == typ && r.From == from && r.To == to {
			return invalid("relation exists")
		}
	}
	s.record(actor, Event{Kind: RelationAdded, Relation: &Relation{ID: s.newID(), Type: typ, From: from, To: to}})
	return nil
}

// RemoveRelation removes a relation added by hand.
func (s *Store) RemoveRelation(actor string, id ID) error {
	r, ok := s.rels[id]
	if !ok {
		return notFound("relation %s", id)
	}
	if r.Derived {
		return invalid("relation %s is derived from a body; change the body instead", id)
	}
	rc := *r
	s.record(actor, Event{Kind: RelationRemoved, Relation: &rc})
	return nil
}

// --- apply ------------------------------------------------------------------

// apply changes state for one event. It must be deterministic and free of
// side effects: it runs live and on Replay.
func (s *Store) apply(e Event) {
	s.log = append(s.log, e)
	idx := len(s.log) - 1
	touch := func(ids ...ID) {
		for _, id := range ids {
			if l := s.touching[id]; len(l) == 0 || l[len(l)-1] != idx {
				s.touching[id] = append(l, idx)
			}
		}
	}

	switch e.Kind {
	case TypeDefined:
		s.types[e.TypeDef.Name] = append(s.types[e.TypeDef.Name], *e.TypeDef)
	case SpaceCreated:
		s.spaces[e.Space] = &space{name: e.SpaceName}
	case ItemCreated:
		it := &Item{
			ID: e.Item, Type: e.TypeName, TypeVersion: e.TypeVersion, Space: e.Space,
			Parent: e.Parent, Title: e.Title, Properties: maps.Clone(e.Props), Version: 1, created: e.Seq,
		}
		if it.Properties == nil {
			it.Properties = Props{}
		}
		if td := s.types[e.TypeName][e.TypeVersion-1]; td.Workflow != nil {
			it.State = td.Workflow.Initial
		}
		s.items[it.ID] = it
		s.assignKey(it)
		touch(it.ID)
	case TitleSet:
		it := s.items[e.Item]
		it.Title = e.Title
		it.Version++
		touch(it.ID)
	case PropertySet:
		it := s.items[e.Item]
		if e.Value == nil {
			delete(it.Properties, e.Field)
		} else {
			it.Properties[e.Field] = e.Value
		}
		it.Version++
		touch(it.ID)
	case Transitioned:
		it := s.items[e.Item]
		it.State = e.To
		it.Version++
		touch(it.ID)
	case ItemMoved:
		root := s.items[e.Item]
		root.Parent = e.Parent
		root.Version++
		moved := s.subtree(root.ID)
		if root.Space != e.Space {
			slices.SortFunc(moved, func(a, b ID) int { return s.items[a].created - s.items[b].created })
			for _, id := range moved {
				it := s.items[id]
				it.Space = e.Space
				s.assignKey(it)
			}
		}
		touch(moved...)
	case BodyCommitted:
		it := s.items[e.Item]
		touch(it.ID)
		for id, r := range s.rels {
			if r.Derived && r.From == it.ID {
				touch(r.To)
				delete(s.rels, id)
			}
		}
		it.Body = e.Body.clone()
		it.Version++
		for _, ref := range it.Body.refs() {
			id := ID(fmt.Sprintf("%s/%s/%s", it.ID, ref.Type, ref.To))
			s.rels[id] = &Relation{ID: id, Type: ref.Type, From: it.ID, To: ref.To, Derived: true, created: e.Seq}
			touch(ref.To)
		}
	case RelationAdded:
		r := *e.Relation
		r.created = e.Seq
		s.rels[r.ID] = &r
		touch(r.From, r.To)
	case RelationRemoved:
		delete(s.rels, e.Relation.ID)
		touch(e.Relation.From, e.Relation.To)
	}
}

func (s *Store) assignKey(it *Item) {
	sp := s.spaces[it.Space]
	sp.nextKey++
	it.Key = fmt.Sprintf("%s-%d", it.Space, sp.nextKey)
	s.keys[it.Key] = it.ID
}

// --- queries ----------------------------------------------------------------

// Query selects items. Zero fields do not filter.
type Query struct {
	Type   string
	Space  string
	State  string
	Parent ID
	// Where requires each named property to equal the given value.
	Where Props
}

// Find returns copies of the matching items in creation order.
func (s *Store) Find(q Query) []Item {
	var out []Item
	for _, it := range s.items {
		if (q.Type != "" && it.Type != q.Type) ||
			(q.Space != "" && it.Space != q.Space) ||
			(q.State != "" && it.State != q.State) ||
			(q.Parent != "" && it.Parent != q.Parent) {
			continue
		}
		match := true
		for k, v := range q.Where {
			if !reflect.DeepEqual(it.Properties[k], v) {
				match = false
			}
		}
		if match {
			out = append(out, it.clone())
		}
	}
	slices.SortFunc(out, func(a, b Item) int { return a.created - b.created })
	return out
}

// Get returns a copy of one item.
func (s *Store) Get(id ID) (Item, error) {
	it, err := s.item(id)
	if err != nil {
		return Item{}, err
	}
	return it.clone(), nil
}

// ByKey resolves a current or former key.
func (s *Store) ByKey(key string) (Item, error) {
	id, ok := s.keys[key]
	if !ok {
		return Item{}, notFound("key %q", key)
	}
	return s.Get(id)
}

// Type returns the latest version of a type, or the zero TypeDef.
func (s *Store) Type(name string) TypeDef {
	td, _ := s.latest(name)
	return td
}

// Relations returns the relations at one end of an item, in creation order.
func (s *Store) Relations(id ID, dir Direction) []Relation {
	var out []Relation
	for _, r := range s.rels {
		if (dir == Outgoing && r.From == id) || (dir == Incoming && r.To == id) {
			out = append(out, *r)
		}
	}
	slices.SortFunc(out, func(a, b Relation) int {
		if a.created != b.created {
			return a.created - b.created
		}
		return strings.Compare(string(a.ID), string(b.ID))
	})
	return out
}

// History returns the events that changed an item or its relations, oldest
// first. It is the same for every type (D6).
func (s *Store) History(id ID) []Event {
	var out []Event
	for _, i := range s.touching[id] {
		out = append(out, s.log[i])
	}
	return out
}

func (s *Store) item(id ID) (*Item, error) {
	it, ok := s.items[id]
	if !ok {
		return nil, notFound("item %s", id)
	}
	return it, nil
}

func (s *Store) latest(name string) (TypeDef, bool) {
	v := s.types[name]
	if len(v) == 0 {
		return TypeDef{}, false
	}
	return v[len(v)-1], true
}

// subtree returns an item and all its descendants.
func (s *Store) subtree(root ID) []ID {
	out := []ID{root}
	for i := 0; i < len(out); i++ {
		for _, it := range s.items {
			if it.Parent == out[i] {
				out = append(out, it.ID)
			}
		}
	}
	return out
}
