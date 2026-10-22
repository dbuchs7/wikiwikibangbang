package item

import (
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"
)

// Deleting a space (ADR-0069) takes two steps, as deleting an item does
// (ADR-0021): an administrator puts the space into the trash, where it and
// everything in it is out of everyone's sight and can be restored, and then
// deletes it for good, which turns every item into a tombstone (D7). The key
// stays taken, so the items' keys keep resolving to their tombstones.

const (
	spaceTrashed = "trashed"
	spacePurged  = "purged"
)

// TrashSpace puts a live space with all its items into the trash. As for an
// item (D8), it is refused while a live item outside the space refers to an
// item in it.
func (s *Store) TrashSpace(actor, key string) error {
	sp, ok := s.spaces[key]
	if !ok || sp.state != "" {
		return notFound("space %q", key)
	}
	if err := s.referencedFromOutside(key); err != nil {
		return err
	}
	return s.record(actor, Event{Kind: SpaceTrashed, Space: key})
}

// RestoreSpace brings a space back from the trash as it was.
func (s *Store) RestoreSpace(actor, key string) error {
	if sp, ok := s.spaces[key]; !ok || sp.state != spaceTrashed {
		return notFound("space %q in the trash", key)
	}
	return s.record(actor, Event{Kind: SpaceRestored, Space: key})
}

// maxSpaceName bounds a space's display name, in characters.
const maxSpaceName = 80

// RenameSpace changes a live space's display name (ADR-0086). The key
// stays: it is part of every item's key.
func (s *Store) RenameSpace(actor, key, name string) error {
	sp, ok := s.spaces[key]
	if !ok || sp.state != "" {
		return notFound("space %q", key)
	}
	name = strings.TrimSpace(name)
	switch {
	case name == "":
		return invalid("a space needs a name")
	case utf8.RuneCountInString(name) > maxSpaceName:
		return invalid("a space's name has at most %d characters", maxSpaceName)
	case strings.ContainsAny(name, "\n\r\t"):
		return invalid("a space's name is a single line")
	case name == sp.name:
		return nil
	}
	return s.record(actor, Event{Kind: SpaceRenamed, Space: key, SpaceName: name})
}

// RenameSpace changes the name of a space the principal administers.
func (x *Session) RenameSpace(key, name string) error {
	if x.s.spaceRole(x.who, key) < SpaceAdmin {
		if x.s.spaceRole(x.who, key) >= Viewer {
			return forbidden("only the space's administrators rename it")
		}
		return notFound("space %q", key)
	}
	return x.s.RenameSpace(x.who, key, name)
}

// PurgeSpace deletes a space in the trash for good: its items become
// tombstones, its roles and kanbans go, its key stays taken.
func (s *Store) PurgeSpace(actor, key string) error {
	if sp, ok := s.spaces[key]; !ok || sp.state != spaceTrashed {
		return notFound("space %q in the trash", key)
	}
	if err := s.referencedFromOutside(key); err != nil {
		return err
	}
	var members []ID
	for _, it := range s.items {
		if it.Space == key && it.Presence != Purged {
			members = append(members, it.ID)
		}
	}
	slices.SortFunc(members, func(a, b ID) int { return s.items[a].created - s.items[b].created })
	return s.record(actor, Event{Kind: SpacePurged, Space: key, Members: members})
}

// TrashedSpaces lists the spaces in the trash, ordered by key, with how
// many items each holds.
func (s *Store) TrashedSpaces() []TrashedSpace {
	var out []TrashedSpace
	for k, sp := range s.spaces {
		if sp.state != spaceTrashed {
			continue
		}
		t := TrashedSpace{SpaceInfo: SpaceInfo{Key: k, Name: sp.name}}
		for _, it := range s.items {
			if it.Space == k && it.Presence != Purged {
				t.Items++
			}
		}
		out = append(out, t)
	}
	slices.SortFunc(out, func(a, b TrashedSpace) int { return strings.Compare(a.Key, b.Key) })
	return out
}

// TrashedSpace is a space in the trash.
type TrashedSpace struct {
	SpaceInfo
	Items int
}

// referencedFromOutside refuses when a live item outside the space refers
// to an item in it, naming the referring items. As for an item, references
// from what is in the trash do not block, an item in a space in the trash
// included.
func (s *Store) referencedFromOutside(key string) error {
	var referrers []string
	seen := map[ID]bool{}
	for _, r := range s.rels {
		from, to := s.items[r.From], s.items[r.To]
		if to == nil || from == nil || to.Space != key || from.Space == key || from.Presence != Live || !s.HasSpace(from.Space) || seen[from.ID] {
			continue
		}
		seen[from.ID] = true
		referrers = append(referrers, from.Key)
	}
	if len(referrers) == 0 {
		return nil
	}
	slices.Sort(referrers)
	return fmt.Errorf("%w: %w: items of space %s are referenced by %s; remove those references first",
		ErrValidation, ErrReferenced, key, strings.Join(referrers, ", "))
}

// applySpace applies a space's trash, restore, purge or new name; it runs
// live and on replay.
func (s *Store) applySpace(e Event, touch func(...ID)) {
	sp := s.spaces[e.Space]
	switch e.Kind {
	case SpaceTrashed:
		sp.state = spaceTrashed
	case SpaceRestored:
		sp.state = ""
	case SpaceRenamed:
		sp.name = e.SpaceName
	case SpacePurged:
		sp.state = spacePurged
		touch(s.purgeItems(e.Members...)...)
		delete(s.spaceRoles, e.Space)
		delete(s.kanbans, e.Space)
	}
}
