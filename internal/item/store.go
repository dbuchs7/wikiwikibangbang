package item

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"maps"
	"net/url"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

// Store holds items, relations and the event log in memory. Commands
// validate, record one event and apply it; apply is the only code that
// changes state, and it runs both live and on Replay.
type Store struct {
	now   func() time.Time
	newID func() ID
	// assess finds the indicators of data protection regulations in
	// content, for live commands only (ADR-0058); nil turns it off.
	assess Assessor

	log    []Event
	types  map[string][]TypeDef // all versions, oldest first
	spaces map[string]*space
	// kanbans are the kanbans of each space, in the order made (ADR-0060).
	kanbans  map[string][]Kanban
	items    map[ID]*Item
	keys     map[string]ID // current and former keys (D2)
	rels     map[ID]*Relation
	touching map[ID][]int // item -> indexes into log, for History

	groups     map[string]map[string]bool // group -> members
	spaceRoles map[string]map[string]Role // space -> principal or "@group" -> role
	sysRoles   map[string]map[string]bool // principal -> system roles

	bySource map[string]ID // Origin.key() -> imported item

	// order is the sibling order a placement set (ADR-0041): space and
	// parent -> children; children it does not list follow, oldest first.
	order map[string][]ID

	// watches are what each principal watches (ADR-0046): principal ->
	// watchKey -> the sequence number of the event that started it.
	watches map[string]map[string]int

	journal Journal
}

// Journal makes batches of events durable. Append returns only after the
// batch survives a crash; an error means it was not recorded, and the store
// does not apply it.
type Journal interface {
	Append(batch []Event) error
}

type space struct {
	name    string
	nextKey int
	// state is empty for a live space, else spaceTrashed or spacePurged
	// (ADR-0069).
	state string
}

// Option configures a Store.
type Option func(*Store)

// WithClock sets the clock used to timestamp events.
func WithClock(now func() time.Time) Option { return func(s *Store) { s.now = now } }

// WithIDs sets the ID generator.
func WithIDs(gen func() ID) Option { return func(s *Store) { s.newID = gen } }

// WithJournal makes every command durable through j before it takes effect.
func WithJournal(j Journal) Option { return func(s *Store) { s.journal = j } }

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

		groups:     map[string]map[string]bool{},
		order:      map[string][]ID{},
		watches:    map[string]map[string]int{},
		spaceRoles: map[string]map[string]Role{},
		sysRoles:   map[string]map[string]bool{},

		bySource: map[string]ID{},
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
// numbers start at 1 without gaps. Options configure the returned store, e.g. the journal to continue with.
func Replay(log []Event, opts ...Option) (*Store, error) {
	s := NewStore(opts...)
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

func (s *Store) record(actor string, e Event) error { return s.recordBatch(actor, e) }

// recordBatch records the events of one command. Commands validate
// everything before calling it, so a batch is never applied in part. With a
// journal, the batch is made durable first and applied only if that
// succeeds: a change is never visible before it is on disk (ADR-0008).
func (s *Store) recordBatch(actor string, events ...Event) error {
	// Sensitive data restricts its item in the same batch (ADR-0040).
	events = append(events, s.guard(actor, events)...)
	// Data protection indicators hold an item open to its space (ADR-0058).
	events = append(events, s.holds(actor, events)...)
	first := len(s.log) + 1
	// UTC without a monotonic reading: the value that survives the journal.
	at := s.now().UTC()
	batch := make([]Event, len(events))
	for i, e := range events {
		e.Seq = first + i
		e.At = at
		if e.Actor == "" {
			e.Actor = actor
		}
		if len(events) > 1 {
			e.Batch = first
		}
		e.normalize()
		batch[i] = e
	}
	if s.journal != nil {
		if err := s.journal.Append(batch); err != nil {
			return fmt.Errorf("item: change not persisted: %w", err)
		}
	}
	for _, e := range batch {
		s.apply(e)
	}
	return nil
}

// --- commands ---------------------------------------------------------------

// DefineType defines a new type or a new version of an existing one.
func (s *Store) DefineType(actor string, td TypeDef) error {
	if err := td.validate(); err != nil {
		return err
	}
	if err := s.duplicatesAmong(td); err != nil {
		return err
	}
	td.Version = len(s.types[td.Name]) + 1
	return s.record(actor, Event{Kind: TypeDefined, TypeDef: &td})
}

var spaceKeyRE = regexp.MustCompile(`^[A-Z][A-Z0-9]*$`)

// CreateSpace creates a space. Its key prefixes the keys of its items.
func (s *Store) CreateSpace(actor, key, name string) error {
	if !spaceKeyRE.MatchString(key) {
		return invalid("space key %q must be upper-case letters and digits", key)
	}
	if sp, ok := s.spaces[key]; ok {
		if sp.state != "" {
			// Its items keep their keys as tombstones or in the trash (D7).
			return invalid("space key %q belonged to a deleted space and is not used again", key)
		}
		return invalid("space %q exists", key)
	}
	return s.record(actor, Event{Kind: SpaceCreated, Space: key, SpaceName: name})
}

// NewItem describes an item to create.
type NewItem struct {
	Space      string
	Type       string
	Parent     ID
	Title      string
	Properties Props
	// Watch makes the creator watch the new item (ADR-0046), recorded in
	// the same batch as its creation.
	Watch bool
	// Body, if given, is the new item's first body, committed in the same
	// batch: an item is never left empty because its body was refused.
	// It cannot show images yet, for the item has no files.
	Body *Doc
}

// firstBody validates the body given with a new item and returns its
// commit, as CommitBody would for an item without a body.
func (s *Store) firstBody(created Event, d Doc) (Event, error) {
	if td, _ := s.latest(created.TypeName); !td.Body {
		return Event{}, invalid("type %q has no body", td.Name)
	}
	for i, b := range d.Blocks {
		if b.Kind == Image {
			return Event{}, wrapf(invalid("an image shows a file of the item; add it after creating the item"), "block %d", i+1)
		}
		if err := checkBlock(b, created.Item); err != nil {
			return Event{}, wrapf(err, "block %d", i+1)
		}
	}
	for _, r := range d.refs() {
		target, ok := s.items[r.To]
		if !ok {
			return Event{}, notFound("referenced item %s", r.To)
		}
		if target.Presence != Live {
			return Event{}, invalid("item %s is %s and cannot be newly referenced", target.Key, target.Presence)
		}
	}
	body := d.clone()
	return Event{Kind: BodyCommitted, Item: created.Item, Body: &body}, nil
}

// watchOnCreate is the event of a creator's watch of a new item, if asked.
func watchOnCreate(actor string, n NewItem, created Event) []Event {
	if !n.Watch || strings.TrimSpace(actor) == "" {
		return nil
	}
	return []Event{{Kind: Watched, Item: created.Item, Principal: actor}}
}

// Create creates an item and returns it.
func (s *Store) Create(actor string, n NewItem) (Item, error) {
	e, err := s.prepareCreate(n)
	if err != nil {
		return Item{}, err
	}
	events := []Event{e}
	if n.Body != nil {
		body, err := s.firstBody(e, *n.Body)
		if err != nil {
			return Item{}, err
		}
		events = append(events, body)
	}
	if err := s.recordBatch(actor, append(events, watchOnCreate(actor, n, e)...)...); err != nil {
		return Item{}, err
	}
	return s.items[e.Item].clone(), nil
}

func (s *Store) prepareCreate(n NewItem) (Event, error) {
	if !s.HasSpace(n.Space) {
		return Event{}, notFound("space %q", n.Space)
	}
	td, ok := s.latest(n.Type)
	if !ok {
		return Event{}, notFound("type %q", n.Type)
	}
	if strings.TrimSpace(n.Title) == "" {
		return Event{}, invalid("title is empty")
	}
	if n.Parent != "" {
		p, err := s.live(n.Parent)
		if err != nil {
			return Event{}, err
		}
		if p.Space != n.Space {
			return Event{}, invalid("parent %s is in space %s, not %s", p.Key, p.Space, n.Space)
		}
	}
	if err := checkProps(td, n.Properties); err != nil {
		return Event{}, err
	}
	for _, f := range td.Fields {
		if f.Required && n.Properties[f.Name] == nil {
			return Event{}, invalid("field %q is required", f.Name)
		}
	}
	if err := s.duplicate(n.Space, td, "", n.Properties); err != nil {
		return Event{}, err
	}
	return Event{
		Kind: ItemCreated, Item: s.newID(), Space: n.Space, TypeName: td.Name, TypeVersion: td.Version,
		Parent: n.Parent, Title: n.Title, Props: maps.Clone(n.Properties),
	}, nil
}

// CreateEmbedded creates an item inside a host and appends an embed of it to
// the host's body, as one batch (ADR-0020: "turn this line into a task").
// The new item lives in the host: its space and parent are the host's, so
// n.Space must be empty or the host's space, and n.Parent must be empty.
func (s *Store) CreateEmbedded(actor string, host ID, n NewItem) (Item, error) {
	h, err := s.live(host)
	if err != nil {
		return Item{}, err
	}
	if td, _ := s.latest(h.Type); !td.Body {
		return Item{}, invalid("type %q has no body", td.Name)
	}
	if n.Space != "" && n.Space != h.Space {
		return Item{}, invalid("an inline item lives in its host's space %s", h.Space)
	}
	if n.Parent != "" {
		return Item{}, invalid("an inline item's parent is its host")
	}
	if n.Body != nil {
		return Item{}, invalid("an inline item gets its body after it is created")
	}
	n.Space, n.Parent = h.Space, host
	created, err := s.prepareCreate(n)
	if err != nil {
		return Item{}, err
	}
	body := h.Body.clone()
	body.Blocks = append(body.Blocks, Block{Kind: Embed, Ref: created.Item})
	events := append([]Event{created, {Kind: BodyCommitted, Item: host, Body: &body}}, watchOnCreate(actor, n, created)...)
	if err := s.recordBatch(actor, events...); err != nil {
		return Item{}, err
	}
	return s.items[created.Item].clone(), nil
}

// SetTitle renames an item.
func (s *Store) SetTitle(actor string, id ID, title string) error {
	if _, err := s.live(id); err != nil {
		return err
	}
	if strings.TrimSpace(title) == "" {
		return invalid("title is empty")
	}
	return s.record(actor, Event{Kind: TitleSet, Item: id, Title: title})
}

// labelRE is what a label may be (ADR-0044): lower-case letters and
// digits, joined by "-", "_" or ".", up to 64 characters.
var labelRE = regexp.MustCompile(`^[\p{Ll}\p{Lo}\p{N}](?:[\p{Ll}\p{Lo}\p{N}._-]{0,62}[\p{Ll}\p{Lo}\p{N}])?$`)

// NormalLabel is the label a person typed, as stored: trimmed and lower
// case, inner spaces as "-". ok is false when it is still not a label.
func NormalLabel(s string) (string, bool) {
	l := strings.Join(strings.Fields(strings.ToLower(s)), "-")
	return l, labelRE.MatchString(l)
}

// SetLabels replaces an item's labels; they are normalised, sorted and
// made unique (ADR-0044).
func (s *Store) SetLabels(actor string, id ID, labels []string) error {
	it, err := s.live(id)
	if err != nil {
		return err
	}
	var out []string
	for _, l := range labels {
		n, ok := NormalLabel(l)
		if !ok {
			return invalid("label %q: use letters and digits, joined by -, _ or ., up to 64 characters", l)
		}
		out = append(out, n)
	}
	slices.Sort(out)
	out = slices.Compact(out)
	if slices.Equal(out, it.Labels) {
		return nil
	}
	return s.record(actor, Event{Kind: LabelsSet, Item: id, Labels: out})
}

// SetProperty sets or (with nil) clears one property.
func (s *Store) SetProperty(actor string, id ID, field string, value any) error {
	it, err := s.live(id)
	if err != nil {
		return err
	}
	td, _ := s.latest(it.Type)
	if err := checkProps(td, Props{field: value}); err != nil {
		return err
	}
	if err := s.duplicate(it.Space, td, id, Props{field: value}); err != nil {
		return err
	}
	return s.record(actor, Event{Kind: PropertySet, Item: id, Field: field, Value: value})
}

// Transition moves an item along its type's workflow (D3).
func (s *Store) Transition(actor string, id ID, name string) error {
	it, err := s.live(id)
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
	return s.record(actor, Event{Kind: Transitioned, Item: id, Transition: name, From: it.State, To: tr.To})
}

// Move changes an item's space and parent. Moving to another space moves the
// whole subtree, because a parent and its children share a space; the moved
// items get new keys and their old keys keep resolving (D2, D5).
func (s *Store) Move(actor string, id ID, toSpace string, newParent ID) error {
	if err := s.checkMove(id, toSpace, newParent); err != nil {
		return err
	}
	return s.record(actor, Event{Kind: ItemMoved, Item: id, Space: toSpace, Parent: newParent})
}

// Place moves an item and its subtree like Move and puts it among the
// children of its new parent (or the top of the space): before the sibling
// before, or last when before is empty (ADR-0041).
func (s *Store) Place(actor string, id ID, toSpace string, newParent, before ID) error {
	if err := s.checkMove(id, toSpace, newParent); err != nil {
		return err
	}
	var order []ID
	for _, c := range s.Children(toSpace, newParent) {
		if c.ID == before {
			order = append(order, id)
		}
		if c.ID != id {
			order = append(order, c.ID)
		}
	}
	if before == "" {
		order = append(order, id)
	} else if before == id || !slices.Contains(order, id) {
		return invalid("%s is not another child of the target", before)
	}
	return s.record(actor, Event{Kind: ItemMoved, Item: id, Space: toSpace, Parent: newParent, Order: order})
}

func orderKey(space string, parent ID) string { return space + "\x00" + string(parent) }

// Children returns the live children of an item, or the live items at the
// top of a space when parent is empty, in their sibling order (ADR-0041).
func (s *Store) Children(space string, parent ID) []Item {
	var out []Item
	for _, it := range s.items {
		if it.Presence == Live && it.Space == space && it.Parent == parent {
			out = append(out, it.clone())
		}
	}
	s.sortSiblings(space, parent, out)
	return out
}

// sortSiblings orders items by the placed order, unplaced ones after it,
// oldest first.
func (s *Store) sortSiblings(space string, parent ID, items []Item) {
	pos := map[ID]int{}
	for i, id := range s.order[orderKey(space, parent)] {
		pos[id] = i
	}
	rank := func(it Item) (int, int) {
		if p, ok := pos[it.ID]; ok {
			return 0, p
		}
		return 1, it.created
	}
	slices.SortFunc(items, func(a, b Item) int {
		ga, ra := rank(a)
		gb, rb := rank(b)
		if ga != gb {
			return ga - gb
		}
		return ra - rb
	})
}

// checkMove validates a move of id to the given space and parent.
func (s *Store) checkMove(id ID, toSpace string, newParent ID) error {
	if _, err := s.live(id); err != nil {
		return err
	}
	if !s.HasSpace(toSpace) {
		return notFound("space %q", toSpace)
	}
	if newParent != "" {
		p, err := s.live(newParent)
		if err != nil {
			return err
		}
		if p.Space != toSpace {
			return invalid("parent %s is not in space %s", p.Key, toSpace)
		}
		if slices.Contains(s.subtree(id), newParent) {
			return invalid("cannot move an item below itself")
		}
	}
	return s.duplicatesMoving(id, toSpace)
}

// CommitBody commits a new version of an item's body.
func (s *Store) CommitBody(actor string, id ID, d Doc) error {
	it, err := s.live(id)
	if err != nil {
		return err
	}
	if td, _ := s.latest(it.Type); !td.Body {
		return invalid("type %q has no body", td.Name)
	}
	for i, b := range d.Blocks {
		if err := checkBlock(b, id); err != nil {
			return wrapf(err, "block %d", i+1)
		}
		// An image shows an attachment of this item; one deleted since
		// may stay if the current body still shows it (ADR-0045).
		if b.Kind == Image {
			a, ok := it.attachment(b.Ref)
			kept := slices.ContainsFunc(it.Body.Blocks, func(o Block) bool { return o.Kind == Image && o.Ref == b.Ref })
			if !ok || (a.Deleted && !kept) {
				return wrapf(notFound("attachment %s of item %s", b.Ref, id), "block %d", i+1)
			}
		}
	}
	// A body may keep references to items that were deleted since its last
	// version, so that the page stays editable; it may not add new ones.
	kept := map[ID]bool{}
	for _, r := range it.Body.refs() {
		kept[r.To] = true
	}
	for _, r := range d.refs() {
		target, ok := s.items[r.To]
		if !ok {
			return notFound("referenced item %s", r.To)
		}
		if target.Presence != Live && !kept[r.To] {
			return invalid("item %s is %s and cannot be newly referenced", target.Key, target.Presence)
		}
	}
	body := d.clone()
	return s.record(actor, Event{Kind: BodyCommitted, Item: id, Body: &body})
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
		if _, err := s.live(id); err != nil {
			return err
		}
	}
	for _, r := range s.rels {
		if !r.Derived && r.Type == typ && r.From == from && r.To == to {
			return invalid("relation exists")
		}
	}
	return s.record(actor, Event{Kind: RelationAdded, Relation: &Relation{ID: s.newID(), Type: typ, From: from, To: to}})
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
	for _, end := range []ID{r.From, r.To} {
		if _, err := s.live(end); err != nil {
			return err
		}
	}
	rc := *r
	rc.created = 0 // not persisted; the event must read the same after a restart
	return s.record(actor, Event{Kind: RelationRemoved, Relation: &rc})
}

// Trash moves an item and its live descendants to the trash, as one event
// (ADR-0021). It is refused while a live item outside that subtree still
// references it — by embed, mention or hand-made relation. References from
// inside the subtree, the subtree's own outgoing references and references
// from trashed items do not block. Descendants trashed earlier on their own
// stay in their batch.
func (s *Store) Trash(actor string, id ID) error {
	if _, err := s.live(id); err != nil {
		return err
	}
	var members []ID
	inSubtree := map[ID]bool{}
	for _, m := range s.subtree(id) {
		inSubtree[m] = true
		if s.items[m].Presence == Live {
			members = append(members, m)
		}
	}
	var referrers []string
	seen := map[ID]bool{}
	for _, r := range s.rels {
		from := s.items[r.From]
		if inSubtree[r.To] && !inSubtree[r.From] && from.Presence == Live && !seen[from.ID] {
			seen[from.ID] = true
			referrers = append(referrers, from.Key)
		}
	}
	if len(referrers) > 0 {
		slices.Sort(referrers)
		return fmt.Errorf("%w: %w: %s is referenced by %s; remove those references first",
			ErrValidation, ErrReferenced, s.items[id].Key, strings.Join(referrers, ", "))
	}
	return s.record(actor, Event{Kind: ItemTrashed, Item: id, Members: members})
}

// Restore brings back exactly the items trashed together with id. It must be
// called on the item the trash started from, and its parent must be live.
func (s *Store) Restore(actor string, id ID) error {
	members, err := s.trashBatch(id)
	if err != nil {
		return err
	}
	if p := s.items[id].Parent; p != "" && s.items[p].Presence != Live {
		return invalid("restore the parent %s first", s.items[p].Key)
	}
	for _, m := range members {
		it := s.items[m]
		td, _ := s.latest(it.Type)
		if err := s.duplicate(it.Space, td, m, it.Properties); err != nil {
			return err
		}
	}
	return s.record(actor, Event{Kind: ItemRestored, Item: id, Members: members})
}

// Purge irreversibly removes the content of the items trashed together with
// id and leaves tombstones (ADR-0021, D7).
func (s *Store) Purge(actor string, id ID) error {
	members, err := s.trashBatch(id)
	if err != nil {
		return err
	}
	return s.record(actor, Event{Kind: ItemPurged, Item: id, Members: members})
}

// trashBatch returns the items trashed together with id, which must be the
// root of that trash event.
func (s *Store) trashBatch(id ID) ([]ID, error) {
	it, err := s.item(id)
	if err != nil {
		return nil, err
	}
	if it.Presence != Trashed {
		return nil, invalid("item %s is %s, not in the trash", it.Key, it.Presence)
	}
	if root := s.log[it.trashedBy-1].Item; root != id {
		return nil, invalid("item %s was trashed with %s; act on %s", it.Key, s.items[root].Key, s.items[root].Key)
	}
	var members []ID
	for _, other := range s.items {
		if other.trashedBy == it.trashedBy {
			members = append(members, other.ID)
		}
	}
	slices.SortFunc(members, func(a, b ID) int { return s.items[a].created - s.items[b].created })
	return members, nil
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
			Parent: e.Parent, Title: e.Title, Properties: maps.Clone(e.Props), Version: 1,
			Presence: Live, created: e.Seq,
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
	case LabelsSet:
		it := s.items[e.Item]
		it.Labels = slices.Clone(e.Labels)
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
		if e.Order != nil {
			s.order[orderKey(e.Space, e.Parent)] = slices.Clone(e.Order)
		}
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
	case ItemTrashed:
		for _, id := range e.Members {
			it := s.items[id]
			it.Presence, it.trashedBy = Trashed, e.Seq
			it.Version++
		}
		touch(e.Members...)
	case ItemRestored:
		for _, id := range e.Members {
			it := s.items[id]
			it.Presence, it.trashedBy = Live, 0
			it.Version++
		}
		touch(e.Members...)
	case ItemPurged:
		touch(s.purgeItems(e.Members...)...)
	case ItemImported, ItemReleased:
		s.applyImport(e, touch)
	case CommentAdded, CommentEdited, CommentDeleted, CommentResolved, CommentReopened:
		s.applyComment(e, touch)
	case AttachmentAdded, AttachmentDeleted:
		s.applyAttachment(e, touch)
	case HoldSet, HoldReleased:
		s.applyHold(e, touch)
	case KanbanSet, KanbanRemoved:
		s.applyKanban(e)
	case SpaceTrashed, SpaceRestored, SpacePurged:
		s.applySpace(e, touch)
	case Watched, Unwatched:
		s.applyWatch(e)
	case RoleRevoked, GroupMemberRemoved, SystemRoleRevoked:
		s.applyRevocation(e)
	default:
		s.applyAccess(e, touch)
	}
}

// purgeItems turns items into tombstones and returns every item whose
// relations changed. Hand-made relations touching a purged item go; derived
// ones go with the purged body. Derived relations from live bodies stay: they
// are a function of those bodies (D4).
func (s *Store) purgeItems(ids ...ID) []ID {
	purged := map[ID]bool{}
	touched := slices.Clone(ids)
	for _, id := range ids {
		purged[id] = true
		// The content leaves the history too, not only the item: what is
		// purged must not be readable through History (ADR-0026).
		for _, i := range s.touching[id] {
			if s.log[i].Item == id {
				s.log[i].erase()
			}
		}
		it := s.items[id]
		it.Presence, it.trashedBy = Purged, 0
		it.Title, it.Properties, it.Body, it.Comments, it.Labels, it.Attachments = "", Props{}, Doc{}, nil, nil, nil
		if it.Origin != nil {
			o := *it.Origin
			o.Raw = ""
			it.Origin = &o
		}
		it.Version++
	}
	for id, r := range s.rels {
		if (!r.Derived && (purged[r.From] || purged[r.To])) || (r.Derived && purged[r.From]) {
			touched = append(touched, r.From, r.To)
			delete(s.rels, id)
		}
	}
	return touched
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
	// IncludeDeleted also returns trashed items and tombstones.
	IncludeDeleted bool
	// Label requires the item to carry this label (ADR-0044).
	Label string
	// WhereText requires each named property to read as the given text
	// (FormatValue), as the API's where filter gives it (ADR-0074).
	WhereText map[string]string
}

// Find returns copies of the matching items in creation order.
func (s *Store) Find(q Query) []Item {
	var out []Item
	for _, it := range s.items {
		if (!q.IncludeDeleted && it.Presence != Live) ||
			(q.Type != "" && it.Type != q.Type) ||
			(q.Space != "" && it.Space != q.Space) ||
			(q.State != "" && it.State != q.State) ||
			(q.Parent != "" && it.Parent != q.Parent) ||
			(q.Label != "" && !slices.Contains(it.Labels, q.Label)) ||
			!whereMatches(it, q.WhereText) {
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

// Counts reports how many items are in each presence state, for
// operational metrics (ADR-0016).
func (s *Store) Counts() map[Presence]int {
	out := map[Presence]int{}
	for _, it := range s.items {
		out[it.Presence]++
	}
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

// SpaceInfo describes a space.
type SpaceInfo struct {
	Key  string
	Name string
}

// Seq is the sequence number of the newest event, or 0 for none.
func (s *Store) Seq() int {
	if len(s.log) == 0 {
		return 0
	}
	return s.log[len(s.log)-1].Seq
}

// Spaces lists the live spaces, ordered by key.
func (s *Store) Spaces() []SpaceInfo {
	out := make([]SpaceInfo, 0, len(s.spaces))
	for k, sp := range s.spaces {
		if sp.state == "" {
			out = append(out, SpaceInfo{Key: k, Name: sp.name})
		}
	}
	slices.SortFunc(out, func(a, b SpaceInfo) int { return strings.Compare(a.Key, b.Key) })
	return out
}

// Types lists the latest version of every type, ordered by name.
func (s *Store) Types() []TypeDef {
	out := make([]TypeDef, 0, len(s.types))
	for name := range s.types {
		td, _ := s.latest(name)
		out = append(out, td)
	}
	slices.SortFunc(out, func(a, b TypeDef) int { return strings.Compare(a.Name, b.Name) })
	return out
}

// TypeAt returns one version of a type.
func (s *Store) TypeAt(name string, version int) (TypeDef, bool) {
	v := s.types[name]
	if version < 1 || version > len(v) {
		return TypeDef{}, false
	}
	return v[version-1], true
}

// HasSpace reports whether a live space exists; a space in the trash or
// deleted does not (ADR-0069).
func (s *Store) HasSpace(key string) bool { sp, ok := s.spaces[key]; return ok && sp.state == "" }

// Type returns the latest version of a type, or the zero TypeDef.
func (s *Store) Type(name string) TypeDef {
	td, _ := s.latest(name)
	return td
}

// Relations returns the relations at one end of an item, in creation order.
// Relations whose ends are not both live are hidden, not removed (ADR-0021).
func (s *Store) Relations(id ID, dir Direction) []Relation {
	var out []Relation
	for _, r := range s.rels {
		if s.items[r.From].Presence != Live || s.items[r.To].Presence != Live {
			continue
		}
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

// Embeds returns how the embed blocks of an item's body render (ADR-0020).
func (s *Store) Embeds(host ID) ([]EmbedView, error) {
	h, err := s.item(host)
	if err != nil {
		return nil, err
	}
	var out []EmbedView
	for _, b := range h.Body.Blocks {
		if b.Kind != Embed {
			continue
		}
		t := s.items[b.Ref]
		out = append(out, EmbedView{Ref: t.ID, Key: t.Key, Title: t.Title, Child: t.Parent == host, Presence: t.Presence})
	}
	return out, nil
}

// live returns an item that exists and is neither trashed nor purged.
func (s *Store) live(id ID) (*Item, error) {
	it, err := s.item(id)
	if err != nil {
		return nil, err
	}
	if it.Presence != Live {
		return nil, invalid("item %s is %s", it.Key, it.Presence)
	}
	return it, nil
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

// CheckBlock validates one block of the body of item self, as CommitBody
// does for every block. A live editing session (ADR-0010) uses it to refuse
// a bad block when it is typed, not only when it is committed.
func CheckBlock(b Block, self ID) error { return checkBlock(b, self) }

// checkBlock validates one block of a body (ADR-0001, ADR-0032).
func checkBlock(b Block, self ID) error {
	switch b.Kind {
	case Paragraph, Heading, Unsupported, BulletItem, OrderedItem, Quote, CodeBlock, TaskItem, Panel:
	case Embed:
		if b.Ref == "" || b.Ref == self {
			return invalid("an embed needs a reference to another item")
		}
		if len(b.Marks) > 0 || b.Text != "" {
			return invalid("an embed has no text")
		}
	case Table:
		if err := checkTable(b.Text); err != nil {
			return err
		}
	case TOC, Children, Divider:
		if b.Text != "" || len(b.Marks) > 0 || len(b.Mentions) > 0 {
			return invalid("a %s block has no text", b.Kind)
		}
	case Image:
		if !ValidAttachmentID(string(b.Ref)) || len(b.Marks) > 0 || len(b.Mentions) > 0 {
			return invalid("an image names an attachment and has a plain caption")
		}
	default:
		return invalid("unknown block kind %q", b.Kind)
	}
	if b.Kind != Embed && b.Kind != Image && b.Ref != "" {
		return invalid("only an embed or an image has a reference")
	}
	if b.Checked && b.Kind != TaskItem {
		return invalid("only a task is checked")
	}
	if (b.Kind == Panel) != slices.Contains(PanelTones, b.Tone) {
		return invalid("tone %q: a panel has one of %s, other blocks none", b.Tone, strings.Join(PanelTones, ", "))
	}
	if b.Align != "" {
		switch b.Kind {
		case Paragraph, Heading, Quote, Panel:
			if !slices.Contains(TextAligns, b.Align) {
				return invalid("align %q: text is aligned %s", b.Align, strings.Join(TextAligns, ", "))
			}
		case Image:
			if !slices.Contains(ImageAligns, b.Align) {
				return invalid("align %q: an image is aligned %s", b.Align, strings.Join(ImageAligns, ", "))
			}
		default:
			return invalid("a %s block has no alignment", b.Kind)
		}
	}
	if b.Width != 0 && (b.Kind != Image || b.Width < 10 || b.Width > 100) {
		return invalid("width %d: only images have a width, 10 to 100 percent", b.Width)
	}
	if b.Kind != Heading && b.Level != 0 || b.Level < 0 || b.Level > 3 {
		return invalid("level %d: only headings have a level, 1 to 3", b.Level)
	}
	if len(b.Marks) > 0 && (b.Kind == CodeBlock || b.Kind == Unsupported || b.Kind == Table) {
		return invalid("a %s block has no formatting", b.Kind)
	}
	n := utf8.RuneCountInString(b.Text)
	for _, m := range b.Marks {
		if m.Start < 0 || m.End <= m.Start || m.End > n {
			return invalid("mark %s %d-%d is outside the text of %d characters", m.Type, m.Start, m.End, n)
		}
		switch m.Type {
		case MarkBold, MarkItalic, MarkCode, MarkUnderline, MarkStrike, MarkHighlight:
			if m.Href != "" || m.Ref != "" {
				return invalid("a %s mark has no target", m.Type)
			}
		case MarkLink:
			if !SafeHref(m.Href) || m.Ref != "" {
				return invalid("link target %q must be an http, https or mailto URL", m.Href)
			}
		case MarkMention:
			if m.Ref == "" || m.Href != "" || !slices.Contains(b.Mentions, m.Ref) {
				return invalid("a mention mark needs a reference that the block mentions")
			}
		default:
			return invalid("unknown mark %q", m.Type)
		}
	}
	return nil
}

// checkTable validates a table's text: at least one row, every row with
// the same number of cells (ADR-0042).
func checkTable(text string) error {
	if text == "" {
		return invalid("a table has at least one row")
	}
	rows := strings.Split(text, "\n")
	cols := strings.Count(rows[0], "\t") + 1
	for i, r := range rows {
		if n := strings.Count(r, "\t") + 1; n != cols {
			return invalid("table row %d has %d cells, the first has %d", i+1, n, cols)
		}
	}
	return nil
}

// SafeHref reports whether a link target is allowed in content: http,
// https and mailto URLs only (ADR-0014). javascript:, data: and relative
// targets are refused.
func SafeHref(href string) bool {
	u, err := url.Parse(href)
	if err != nil {
		return false
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
		return u.Host != ""
	case "mailto":
		return u.Opaque != ""
	}
	return false
}
