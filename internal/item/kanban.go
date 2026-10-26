package item

import (
	"slices"
	"strings"
	"unicode/utf8"
)

// Kanbans (ADR-0060) are named boards on a topic within a space: the items
// of one type with a workflow that carry the kanban's label, in columns of
// the workflow's states. An item may carry several labels, so it may stand
// on several kanbans; the label is its membership. A kanban is a definition
// of the space, like the space itself, not content (D1): it holds no item
// data, and an empty kanban stays until it is removed.
type Kanban struct {
	ID    ID
	Name  string
	Type  string
	Label string
	// Columns, when set, are the kanban's columns in their order: one per
	// workflow state, renamed as wished, and columns of its own that hold
	// no state (ADR-0076). Unset, the columns are the workflow's states.
	Columns []KanbanColumn
	// CardFields name what a card shows besides its key and title: fields
	// of the type, or "labels" and "state".
	CardFields []string
	// ColorField, when set, names an enumeration field of the type whose
	// value colours a card (ADR-0134). The colour is the item's own value,
	// so it shows wherever the item does; the kanban only picks the field.
	ColorField string `json:",omitempty"`
	// Colors give values of the colour field a tone of KanbanTones; a
	// value without one takes the tone of its place among the values.
	Colors map[string]string `json:",omitempty"`
	// LaneField, when set, names a person or enumeration field of the type
	// whose value lays the cards out in lanes (ADR-0135). Moving a card to
	// another lane sets the item's value, so the lanes too are the item's
	// own information; the kanban only picks the field.
	LaneField string `json:",omitempty"`
}

// KanbanTones are the tones a kanban colours its cards with, in the
// order values take them when they have none of their own (ADR-0134).
var KanbanTones = []string{"blue", "green", "yellow", "orange", "red", "purple", "teal", "gray"}

// KanbanColumn is a column of a kanban. A column with a State holds the
// cards in that state, and putting a card there performs the transition
// to it; a column without one is the kanban's own, e.g. "Review", and
// putting a card there leaves its state as it is.
type KanbanColumn struct {
	ID    ID
	Name  string
	State string
	// Limit is how many cards the column should hold at most, 0 for no
	// limit (ADR-0134). It is a signal, not a rule: a move past it is
	// shown, not refused.
	Limit int `json:",omitempty"`
}

// stateColumn is the ID of the column of a workflow state.
func stateColumn(state string) ID { return ID("state:" + state) }

// placement is where a card was put on a kanban: in a column, while it
// was in a state.
type placement struct {
	Column ID
	State  string
}

func (s *Store) applyKanban(e Event) {
	if e.Kanban == nil {
		return
	}
	if e.Kind == KanbanCardPlaced {
		k := e.Kanban.ID
		if s.placed == nil {
			s.placed, s.orders = map[ID]map[ID]placement{}, map[ID]map[ID][]ID{}
		}
		if s.placed[k] == nil {
			s.placed[k], s.orders[k] = map[ID]placement{}, map[ID][]ID{}
		}
		s.placed[k][e.Item] = placement{Column: ID(e.Field), State: e.To}
		s.orders[k][ID(e.Field)] = slices.Clone(e.Order)
		return
	}
	list := s.kanbans[e.Space]
	i := slices.IndexFunc(list, func(k Kanban) bool { return k.ID == e.Kanban.ID })
	switch e.Kind {
	case KanbanSet:
		if i >= 0 {
			list[i] = *e.Kanban
		} else {
			list = append(list, *e.Kanban)
		}
	case KanbanRemoved:
		if i >= 0 {
			list = slices.Delete(list, i, i+1)
		}
	}
	if s.kanbans == nil {
		s.kanbans = map[string][]Kanban{}
	}
	s.kanbans[e.Space] = list
}

// Kanbans lists the kanbans of the spaces the principal holds a role in,
// or of one space, by space and in the order they were made.
func (x *Session) Kanbans(space string) map[string][]Kanban {
	out := map[string][]Kanban{}
	for sp, list := range x.s.kanbans {
		if (space == "" || sp == space) && x.s.spaceRole(x.who, sp) >= Viewer && len(list) > 0 {
			out[sp] = slices.Clone(list)
		}
	}
	return out
}

// SetKanban makes a kanban in a space or changes one; editors of the
// space may. A new kanban's label is its name as a label unless one is
// given; a kanban's type and label do not change, so its cards stay its
// cards. When changing a kanban, Columns and CardFields left nil keep
// what the kanban has, and so do ColorField and Colors when both are
// unset; an empty ColorField with an empty, non-nil Colors takes the
// colouring off. LaneField is taken as given; the API keeps it when a
// change leaves it out.
func (x *Session) SetKanban(space string, k Kanban) (Kanban, error) {
	if _, ok := x.s.spaces[space]; !ok || x.s.spaceRole(x.who, space) < Viewer {
		return Kanban{}, notFound("space %q", space)
	}
	if x.s.spaceRole(x.who, space) < Editor {
		return Kanban{}, forbidden("%s may not make kanbans in %s", x.who, space)
	}
	k.Name = strings.TrimSpace(k.Name)
	if k.Name == "" || utf8.RuneCountInString(k.Name) > 80 || strings.ContainsAny(k.Name, "\r\n") {
		return Kanban{}, invalid("a kanban's name is a single line of 1 to 80 characters")
	}
	list := x.s.kanbans[space]
	if k.ID != "" {
		i := slices.IndexFunc(list, func(o Kanban) bool { return o.ID == k.ID })
		if i < 0 {
			return Kanban{}, notFound("kanban %s in %s", k.ID, space)
		}
		k.Type, k.Label = list[i].Type, list[i].Label
		if k.Columns == nil {
			k.Columns = list[i].Columns
		}
		if k.CardFields == nil {
			k.CardFields = list[i].CardFields
		}
		if k.ColorField == "" && k.Colors == nil {
			k.ColorField, k.Colors = list[i].ColorField, list[i].Colors
		}
	} else {
		td, ok := x.s.latest(k.Type)
		if !ok || td.Workflow == nil {
			return Kanban{}, invalid("type %q has no workflow; a kanban's columns are its states", k.Type)
		}
		if k.Label == "" {
			k.Label = k.Name
		}
		label, ok := NormalLabel(k.Label)
		if !ok {
			return Kanban{}, invalid("label %q: use letters and digits, joined by -, _ or ., up to 64 characters", k.Label)
		}
		k.Label = label
		if slices.ContainsFunc(list, func(o Kanban) bool { return o.Type == k.Type && o.Label == k.Label }) {
			return Kanban{}, invalid("%s has a kanban of %s for the label %s already", space, k.Type, k.Label)
		}
		k.ID = x.s.newID()
	}
	td, _ := x.s.latest(k.Type)
	if err := x.s.checkColumns(&k, td); err != nil {
		return Kanban{}, err
	}
	if err := checkCardFields(k.CardFields, td); err != nil {
		return Kanban{}, err
	}
	if err := checkColors(&k, td); err != nil {
		return Kanban{}, err
	}
	if f, ok := td.field(k.LaneField); k.LaneField != "" && (!ok || (f.Kind != KindUser && f.Kind != KindEnum)) {
		return Kanban{}, invalid("type %q has no person or enumeration field %q to lay the cards out in lanes by", td.Name, k.LaneField)
	}
	return k, x.s.record(x.who, Event{Kind: KanbanSet, Space: space, Kanban: &k})
}

// maxColumns and maxCardFields bound what a kanban shows, and
// maxColumnLimit the limit of cards a column may name.
const (
	maxColumns     = 30
	maxCardFields  = 12
	maxColumnLimit = 999
)

// checkColumns checks a kanban's own columns and gives new ones an ID: a
// state at most once, a name of 1 to 40 characters, no ID twice.
func (s *Store) checkColumns(k *Kanban, td TypeDef) error {
	if len(k.Columns) == 0 {
		k.Columns = nil
		return nil
	}
	if len(k.Columns) > maxColumns {
		return invalid("a kanban has at most %d columns", maxColumns)
	}
	ids, states := map[ID]bool{}, map[string]bool{}
	for i := range k.Columns {
		c := &k.Columns[i]
		c.Name = strings.TrimSpace(c.Name)
		if c.Name == "" || utf8.RuneCountInString(c.Name) > 40 || strings.ContainsAny(c.Name, "\r\n") {
			return invalid("a column's name is a single line of 1 to 40 characters")
		}
		if c.Limit < 0 || c.Limit > maxColumnLimit {
			return invalid("a column's limit is 0 for none or up to %d cards", maxColumnLimit)
		}
		switch {
		case c.State != "":
			if td.Workflow == nil || !slices.Contains(td.Workflow.States, c.State) {
				return invalid("%s has no state %q", td.Name, c.State)
			}
			if states[c.State] {
				return invalid("the state %q has two columns", c.State)
			}
			states[c.State] = true
			c.ID = stateColumn(c.State)
		case c.ID == "" || strings.HasPrefix(string(c.ID), "state:"):
			c.ID = s.newID()
		}
		if ids[c.ID] {
			return invalid("the column %s appears twice", c.ID)
		}
		ids[c.ID] = true
	}
	return nil
}

func checkCardFields(names []string, td TypeDef) error {
	if len(names) > maxCardFields {
		return invalid("a card shows at most %d fields", maxCardFields)
	}
	seen := map[string]bool{}
	for _, n := range names {
		if _, ok := td.field(n); !ok && n != "labels" && n != "state" {
			return invalid("type %q has no field %q to show on a card", td.Name, n)
		}
		if seen[n] {
			return invalid("the field %q is shown twice", n)
		}
		seen[n] = true
	}
	return nil
}

// checkColors checks a kanban's colouring: an enumeration field of the
// type, tones of KanbanTones for its values. Without a field there are no
// tones, and an empty set of tones is none.
func checkColors(k *Kanban, td TypeDef) error {
	if len(k.Colors) == 0 {
		k.Colors = nil
	}
	if k.ColorField == "" {
		if k.Colors != nil {
			return invalid("tones need a field to colour the cards by")
		}
		return nil
	}
	f, ok := td.field(k.ColorField)
	if !ok || f.Kind != KindEnum {
		return invalid("type %q has no enumeration field %q to colour the cards by", td.Name, k.ColorField)
	}
	for value, tone := range k.Colors {
		if !slices.Contains(f.Values, value) {
			return invalid("the field %q has no value %q", k.ColorField, value)
		}
		if !slices.Contains(KanbanTones, tone) {
			return invalid("%q is not a tone: use one of %s", tone, strings.Join(KanbanTones, ", "))
		}
	}
	return nil
}

// The relation types a kanban reads as dependencies (ADR-0135): an item
// waits on what it depends on and on what blocks it. They are the names
// the UI suggests; a relation's type is otherwise free.
const (
	RelDependsOn = "depends-on"
	RelBlocks    = "blocks"
)

// RelationSummary counts, for a readable item, its relations made by hand
// to items the principal may read, both ways, and how many of those items
// it waits on while they are not finished: those it depends on and those
// that block it (ADR-0135). Relations derived from a body are left out:
// they say that a text names an item, not that the work is linked.
func (x *Session) RelationSummary(id ID) (related, waiting int, err error) {
	if _, err := x.readable(id); err != nil {
		return 0, 0, err
	}
	sum := x.RelationSummaries([]ID{id})[id]
	return sum[0], sum[1], nil
}

// RelationSummaries is RelationSummary for the items of a listing, which
// the caller has read already: one pass over the relations, however many
// items, so a long listing does not walk them once per item.
func (x *Session) RelationSummaries(ids []ID) map[ID][2]int {
	out := make(map[ID][2]int, len(ids))
	for _, id := range ids {
		out[id] = [2]int{}
	}
	waits := func(r *Relation, other *Item, kind string) bool {
		td, ok := x.s.TypeAt(other.Type, other.TypeVersion)
		return ok && r.Type == kind && td.Workflow != nil && !td.Workflow.Finished(other.State)
	}
	count := func(id ID, r *Relation, other *Item, kind string) {
		sum, ok := out[id]
		if !ok || other == nil || other.Presence != Live || !x.s.canRead(x.who, other) {
			return
		}
		sum[0]++
		if waits(r, other, kind) {
			sum[1]++
		}
		out[id] = sum
	}
	for _, r := range x.s.rels {
		if r.Derived || x.s.items[r.From].Presence != Live || x.s.items[r.To].Presence != Live {
			continue
		}
		count(r.From, r, x.s.items[r.To], RelDependsOn)
		count(r.To, r, x.s.items[r.From], RelBlocks)
	}
	return out
}

// Columns are a kanban's columns as they show: its own, or the
// workflow's states; a state the columns miss, e.g. one added to the
// workflow later, gets a column at the end.
func (k Kanban) columns(td TypeDef) []KanbanColumn {
	out := slices.Clone(k.Columns)
	if td.Workflow == nil {
		return out
	}
	for _, st := range td.Workflow.States {
		if !slices.ContainsFunc(out, func(c KanbanColumn) bool { return c.State == st }) {
			out = append(out, KanbanColumn{ID: stateColumn(st), Name: st, State: st})
		}
	}
	return out
}

func (x *Session) kanban(space string, id ID) (Kanban, error) {
	if x.s.spaceRole(x.who, space) < Viewer {
		return Kanban{}, notFound("space %q", space)
	}
	i := slices.IndexFunc(x.s.kanbans[space], func(o Kanban) bool { return o.ID == id })
	if i < 0 {
		return Kanban{}, notFound("kanban %s in %s", id, space)
	}
	return x.s.kanbans[space][i], nil
}

// BoardColumn is a column of a kanban with its cards, in their order.
type BoardColumn struct {
	KanbanColumn
	Cards []ID
}

// Board lays a kanban's cards out that the principal may read: each in
// the column it was put in while it is still in the state it was put
// there in, otherwise in the column of its state; within a column in the
// order they were put, then in the order they were made.
func (x *Session) Board(space string, id ID) (Kanban, []BoardColumn, error) {
	k, err := x.kanban(space, id)
	if err != nil {
		return k, nil, err
	}
	td, _ := x.s.latest(k.Type)
	cols := k.columns(td)
	out := make([]BoardColumn, len(cols))
	index := map[ID]int{}
	for i, c := range cols {
		out[i] = BoardColumn{KanbanColumn: c, Cards: []ID{}}
		index[c.ID] = i
	}
	var cards []*Item
	for _, it := range x.s.items {
		if it.Presence == Live && it.Space == space && it.Type == k.Type && slices.Contains(it.Labels, k.Label) && x.s.canRead(x.who, it) {
			cards = append(cards, it)
		}
	}
	slices.SortFunc(cards, func(a, b *Item) int { return a.created - b.created })
	for _, it := range cards {
		col := -1
		if p, ok := x.s.placed[k.ID][it.ID]; ok && p.State == it.State {
			if i, ok := index[p.Column]; ok && (cols[i].State == "" || cols[i].State == it.State) {
				col = i
			}
		}
		if col < 0 {
			col = slices.IndexFunc(cols, func(c KanbanColumn) bool { return c.State == it.State })
		}
		if col < 0 {
			col = 0
		}
		out[col].Cards = append(out[col].Cards, it.ID)
	}
	for i := range out {
		order := x.s.orders[k.ID][out[i].ID]
		rank := func(id ID) int {
			if r := slices.Index(order, id); r >= 0 {
				return r
			}
			return len(order)
		}
		slices.SortStableFunc(out[i].Cards, func(a, b ID) int { return rank(a) - rank(b) })
	}
	return k, out, nil
}

// PlaceCard puts a card in a column of a kanban, before another card or
// last (ADR-0076). Whoever may edit the card may. A column of a state
// performs the transition to that state first — the workflow must have
// one from the card's state (D3); a column of the kanban's own leaves the
// state as it is.
func (x *Session) PlaceCard(space string, kanban, card ID, column, before ID) error {
	k, err := x.kanban(space, kanban)
	if err != nil {
		return err
	}
	it, err := x.s.live(card)
	if err != nil || !x.s.canRead(x.who, it) {
		return notFound("item %s", card)
	}
	if it.Space != space || it.Type != k.Type || !slices.Contains(it.Labels, k.Label) {
		return invalid("%s is not a card of the kanban %s", it.Key, k.Name)
	}
	if !x.s.canEdit(x.who, it) || it.Managed != "" {
		return forbidden("%s may not move %s", x.who, it.Key)
	}
	td, _ := x.s.latest(k.Type)
	cols := k.columns(td)
	i := slices.IndexFunc(cols, func(c KanbanColumn) bool { return c.ID == column })
	if i < 0 {
		return notFound("column %s of the kanban %s", column, k.Name)
	}
	if to := cols[i].State; to != "" && to != it.State {
		var names []string
		for _, tr := range td.Workflow.Transitions {
			if tr.To == to && slices.Contains(tr.From, it.State) {
				names = append(names, tr.Name)
			}
		}
		switch len(names) {
		case 0:
			return invalid("the workflow of %s has no transition from %s to %s; add one to the type", td.Name, it.State, to)
		case 1:
			if err := x.Transition(card, names[0]); err != nil {
				return err
			}
		default:
			return invalid("the workflow of %s has several transitions from %s to %s (%s); choose one on the card", td.Name, it.State, to, strings.Join(names, ", "))
		}
	}
	_, board, _ := x.Board(space, kanban)
	var order []ID
	for _, c := range board {
		if c.ID == column {
			order = slices.DeleteFunc(slices.Clone(c.Cards), func(id ID) bool { return id == card })
		}
	}
	if j := slices.Index(order, before); before != "" && j >= 0 {
		order = slices.Insert(order, j, card)
	} else {
		order = append(order, card)
	}
	return x.s.record(x.who, Event{Kind: KanbanCardPlaced, Space: space, Item: card, Kanban: &Kanban{ID: kanban},
		Field: string(column), To: x.s.items[card].State, Order: order})
}

// AddTransition adds a transition to the latest version of a type, so a
// card can move between two states on a kanban (ADR-0076): named "to
// <state>", from the given state; an existing one of that name gains the
// state to start from. Administrators may, as they define types.
func (x *Session) AddTransition(typeName, from, to string) (TypeDef, error) {
	if !x.s.HasSystemRole(x.who, RoleAdmin) {
		return TypeDef{}, forbidden("%s may not change types", x.who)
	}
	td, ok := x.s.latest(typeName)
	if !ok || td.Workflow == nil {
		return TypeDef{}, notFound("type %q with a workflow", typeName)
	}
	if !slices.Contains(td.Workflow.States, from) || !slices.Contains(td.Workflow.States, to) || from == to {
		return TypeDef{}, invalid("%s → %s is not a move between two states of %s", from, to, typeName)
	}
	wf := *td.Workflow
	wf.Transitions = slices.Clone(wf.Transitions)
	name := "to " + to
	if j := slices.IndexFunc(wf.Transitions, func(tr Transition) bool { return tr.Name == name }); j >= 0 {
		tr := wf.Transitions[j]
		if tr.To != to {
			return TypeDef{}, invalid("%s has a transition %q to another state", typeName, name)
		}
		if slices.Contains(tr.From, from) {
			return td, nil
		}
		tr.From = append(slices.Clone(tr.From), from)
		wf.Transitions[j] = tr
	} else {
		wf.Transitions = append(wf.Transitions, Transition{Name: name, From: []string{from}, To: to})
	}
	td.Workflow = &wf
	if err := x.s.DefineType(x.who, td); err != nil {
		return TypeDef{}, err
	}
	td, _ = x.s.latest(typeName)
	return td, nil
}

// RemoveKanban removes a kanban; its items keep their label.
func (x *Session) RemoveKanban(space string, id ID) error {
	if x.s.spaceRole(x.who, space) < Editor {
		return forbidden("%s may not remove kanbans in %s", x.who, space)
	}
	if !slices.ContainsFunc(x.s.kanbans[space], func(o Kanban) bool { return o.ID == id }) {
		return notFound("kanban %s in %s", id, space)
	}
	return x.s.record(x.who, Event{Kind: KanbanRemoved, Space: space, Kanban: &Kanban{ID: id}})
}
