package item

import (
	"slices"
	"strings"
	"time"
)

// The change feed (ADR-0120): every event of the log that touches what a
// principal watches, in the order of the log, after a cursor. Unlike
// notifications (ADR-0046) it lists events of every kind and by anyone,
// access changes included, so a system that copies items learns at once
// that a copy may be stale. An entry names its item only when the
// principal may read the item now; otherwise it says only that something
// they watch changed (D10). Nothing is stored per entry: the feed is the
// log, read forward from the cursor.

// Change is one entry of the change feed. Item is empty when the
// principal may not read the item now, or when the event is about a space
// or about the principal's own roles.
type Change struct {
	Seq  int
	At   time.Time
	Item ID
}

// watchSpan is the time a watch held: from the event that started it
// (exclusive) to the one that ended it (exclusive), or open when to is 0.
type watchSpan struct{ from, to int }

func (w watchSpan) holds(seq int) bool { return w.from < seq && (w.to == 0 || seq < w.to) }

// watchSpans returns every watch the principal ever held, by watch key.
func (x *Session) watchSpans() map[string][]watchSpan {
	out := map[string][]watchSpan{}
	for _, i := range x.s.watchLog[x.who] {
		e := x.s.log[i]
		k := watchKey(e.Item, e.Space)
		// SetWatch records only real changes, so a watch's events
		// alternate: Watched, Unwatched, Watched, …
		if spans := out[k]; e.Kind == Watched {
			out[k] = append(spans, watchSpan{from: e.Seq})
		} else if len(spans) > 0 {
			spans[len(spans)-1].to = e.Seq
		}
	}
	return out
}

func heldAt(spans []watchSpan, seq int) bool {
	return slices.ContainsFunc(spans, func(w watchSpan) bool { return w.holds(seq) })
}

// accessKinds are the events that change who holds which role.
var accessKinds = map[EventKind]bool{
	RoleGranted: true, RoleRevoked: true, GroupMemberAdded: true, GroupMemberRemoved: true,
	SystemRoleGranted: true, SystemRoleRevoked: true,
}

// spaceKinds are the events on a whole space.
var spaceKinds = map[EventKind]bool{SpaceTrashed: true, SpaceRestored: true, SpacePurged: true, SpaceRenamed: true}

// Changes returns the change feed after the event after, oldest first, at
// most limit entries. last is the highest sequence number considered: the
// newest event of the log, or the last entry's when the page is full, so
// a caller always moves its cursor on. An event counts when, at its time,
// the principal watched
//
//   - its item, or an item below it whose ancestor it is now;
//   - the item's space; or
//   - its space, or an item of it, for an event on a whole space;
//
// and always when it changes the principal's own roles or group
// memberships, or the state of a space they hold a role in. Watching and
// unwatching are not changes. An event on an item the principal may not
// read now counts only through a watch of that very item, or of an item
// below it they may not read either: a space watch never reveals an item
// the principal cannot read (D10).
func (x *Session) Changes(after, limit int) (out []Change, last int) {
	head := x.s.Seq()
	if after < 0 {
		after = 0
	}
	if after >= head {
		return nil, head
	}
	spans := x.watchSpans()
	// below maps each item to the watched items it is, now, an ancestor of
	// (itself included).
	below := map[ID][]ID{}
	for k := range spans {
		kind, target, _ := strings.Cut(k, "\x00")
		if kind != "i" {
			continue
		}
		// Items are never forgotten (D7), so every watched one is there.
		for _, a := range x.s.ancestry(x.s.items[ID(target)]) {
			below[a.ID] = append(below[a.ID], ID(target))
		}
	}
	for _, e := range x.s.log[after:] {
		c, ok := x.change(e, spans, below)
		if !ok {
			continue
		}
		if len(out) == limit {
			return out, out[len(out)-1].Seq
		}
		out = append(out, c)
	}
	return out, head
}

// visible reports whether the principal may read an item now.
func (x *Session) visible(it *Item) bool {
	return it != nil && it.Presence != Purged && x.s.canRead(x.who, it)
}

// change decides whether e is an entry of the principal's feed.
func (x *Session) change(e Event, spans map[string][]watchSpan, below map[ID][]ID) (Change, bool) {
	c := Change{Seq: e.Seq, At: e.At}
	switch {
	case e.Kind == Watched || e.Kind == Unwatched:
		return c, false
	case accessKinds[e.Kind]:
		return c, x.concerns(e)
	case spaceKinds[e.Kind]:
		if x.holdsGrant(e.Space) || heldAt(spans[watchKey("", e.Space)], e.Seq) {
			return c, true
		}
		for k, s := range spans {
			if kind, target, _ := strings.Cut(k, "\x00"); kind == "i" && heldAt(s, e.Seq) {
				if w := x.s.items[ID(target)]; w != nil && w.Space == e.Space {
					return c, true
				}
			}
		}
		return c, false
	}
	listed := false
	for _, id := range append([]ID{e.Item}, e.Members...) {
		it := x.s.items[id]
		if it == nil {
			continue
		}
		readable := x.visible(it)
		// Through a watch of the item, or of an item below it.
		for _, w := range below[id] {
			if !heldAt(spans[watchKey(w, "")], e.Seq) {
				continue
			}
			if readable {
				c.Item = id
				return c, true
			}
			if w == id || !x.visible(x.s.items[w]) {
				listed = true
			}
		}
		if readable && heldAt(spans[watchKey("", it.Space)], e.Seq) {
			c.Item = id
			return c, true
		}
	}
	return c, listed
}

// concerns reports whether an access event changes the principal's own
// roles: a role of theirs or of a group they belong to, their membership
// of a group, or a system role of theirs.
func (x *Session) concerns(e Event) bool {
	if e.Principal == x.who {
		return true
	}
	if g, ok := strings.CutPrefix(e.Principal, "@"); ok && (e.Kind == RoleGranted || e.Kind == RoleRevoked) {
		return x.s.groups[g][x.who]
	}
	return false
}

// holdsGrant reports whether the principal is granted a role in a space,
// directly or through a group, whatever the space's state.
func (x *Session) holdsGrant(space string) bool {
	for entry := range x.s.spaceRoles[space] {
		if x.s.matches(x.who, entry) {
			return true
		}
	}
	return false
}

// OwnGrant is one space role granted to the principal: directly, when Via
// is empty, or to the group Via ("@name") they belong to.
type OwnGrant struct {
	Space string
	Role  Role
	Via   string
}

// OwnRoles lists the principal's system roles, sorted, and every space
// role granted to them, directly or through a group, one entry per grant,
// ordered by space and then by Via (direct grants first). Spaces in the
// trash are left out: they are out of everyone's sight and grant nothing
// (ADR-0069). It tells nothing about anyone else.
func (x *Session) OwnRoles() (system []string, grants []OwnGrant) {
	system = []string{}
	for r, held := range x.s.sysRoles[x.who] {
		if held {
			system = append(system, r)
		}
	}
	slices.Sort(system)
	grants = []OwnGrant{}
	for space, entries := range x.s.spaceRoles {
		if sp := x.s.spaces[space]; sp == nil || sp.state != "" {
			continue
		}
		for entry, role := range entries {
			switch {
			case entry == x.who:
				grants = append(grants, OwnGrant{Space: space, Role: role})
			case strings.HasPrefix(entry, "@") && x.s.matches(x.who, entry):
				grants = append(grants, OwnGrant{Space: space, Role: role, Via: entry})
			}
		}
	}
	slices.SortFunc(grants, func(a, b OwnGrant) int {
		if c := strings.Compare(a.Space, b.Space); c != 0 {
			return c
		}
		return strings.Compare(a.Via, b.Via)
	})
	return system, grants
}
