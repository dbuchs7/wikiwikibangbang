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
}

func (s *Store) applyKanban(e Event) {
	if e.Kanban == nil {
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

// SetKanban makes a kanban in a space or renames one; editors of the space
// may. A new kanban's label is its name as a label unless one is given; a
// kanban's type and label do not change, so its cards stay its cards.
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
	return k, x.s.record(x.who, Event{Kind: KanbanSet, Space: space, Kanban: &k})
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
