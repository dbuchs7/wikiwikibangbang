package item

import "slices"

// NothingNew decides whether a command that makes something new may be
// recorded (ADR-0163): it returns nil while it may, else the error the
// command fails with. The storage sets it, refusing new entries while the
// data directory's file system is almost full; nil lets everything be made.
type NothingNew func() error

// SetNothingNew sets the check for commands that make something new; nil
// turns it off. Only commands consult it, never the replay of the log.
func (s *Store) SetNothingNew(f NothingNew) { s.nothingNew = f }

// refusesNew is the error a command that makes something new fails with
// now, or nil (ADR-0163).
func (s *Store) refusesNew() error {
	if s.nothingNew == nil {
		return nil
	}
	return s.nothingNew()
}

// creates reports whether a batch makes something that did not exist
// before: an item (a page, a task, a folder, a copy, the task of an access
// request, an imported or linked item seen for the first time), a space
// or a kanban (ADR-0163). Changing, moving, commenting, labelling,
// protecting, trashing and purging what exists make nothing new; neither
// does a type definition, which is not an entry.
func (s *Store) creates(batch []Event) bool {
	for _, e := range batch {
		switch e.Kind {
		case ItemCreated, SpaceCreated:
			return true
		case ItemImported:
			if _, ok := s.items[e.Item]; !ok {
				return true
			}
		case KanbanSet:
			if e.Kanban != nil && !slices.ContainsFunc(s.kanbans[e.Space], func(k Kanban) bool { return k.ID == e.Kanban.ID }) {
				return true
			}
		}
	}
	return false
}
