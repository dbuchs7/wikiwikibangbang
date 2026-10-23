package item

import (
	"slices"
	"strings"
	"time"
)

// Watching (ADR-0046): a principal watches an item or a whole space and is
// told of what others change there. Watches are events of the store, so
// they survive restarts and backups; they belong to no item's history.
// Notifications are not stored: they are the watched events of the log,
// read back for the watcher, so they cannot disagree with what happened.

// Watch is one thing a principal watches: an item, or a space when Item
// is empty. Since is the sequence number of the event that started it.
type Watch struct {
	Item  ID
	Space string
	Since int
}

func watchKey(item ID, space string) string {
	if item != "" {
		return "i\x00" + string(item)
	}
	return "s\x00" + space
}

// SetWatch starts or stops a principal's watch of an item or a space.
func (s *Store) SetWatch(actor, principal string, item ID, space string, on bool) error {
	switch {
	case strings.TrimSpace(principal) == "":
		return invalid("a watch needs a principal")
	case (item == "") == (space == ""):
		return invalid("a watch names an item or a space")
	case item != "":
		if _, err := s.live(item); err != nil {
			return err
		}
	default:
		if _, ok := s.spaces[space]; !ok {
			return notFound("space %q", space)
		}
	}
	_, watching := s.watches[principal][watchKey(item, space)]
	if watching == on {
		return nil
	}
	kind := Watched
	if !on {
		kind = Unwatched
	}
	return s.record(actor, Event{Kind: kind, Item: item, Space: space, Principal: principal})
}

// applyWatch keeps the watches. It touches no item: a watch is not part
// of what happened to the item.
func (s *Store) applyWatch(e Event) {
	k := watchKey(e.Item, e.Space)
	if e.Kind == Unwatched {
		delete(s.watches[e.Principal], k)
		return
	}
	if s.watches[e.Principal] == nil {
		s.watches[e.Principal] = map[string]int{}
	}
	s.watches[e.Principal][k] = e.Seq
}

// notifyKinds are the events a watcher is told of.
var notifyKinds = map[EventKind]bool{
	ItemCreated: true, TitleSet: true, PropertySet: true, Transitioned: true, ItemMoved: true, BodyCommitted: true,
	ItemTrashed: true, ItemRestored: true, CommentAdded: true, LabelsSet: true, AttachmentAdded: true, ItemImported: true,
}

// Notification is one change a watcher is told of. Key and Title are the
// item's current ones.
type Notification struct {
	Seq     int
	At      time.Time
	Actor   string
	Kind    EventKind
	Item    ID
	Key     string
	Title   string
	Space   string
	Created bool // a new item below a watched one
	// Mentioned reports a comment that names the principal by @username,
	// told whether or not they watch the item.
	Mentioned bool
}

// --- sessions ---------------------------------------------------------------

// Watch starts or stops watching a readable item.
func (x *Session) Watch(id ID, on bool) error {
	if _, err := x.readable(id); err != nil {
		return err
	}
	return x.s.SetWatch(x.who, x.who, id, "", on)
}

// WatchSpace starts or stops watching every item of a space one holds a
// role in.
func (x *Session) WatchSpace(space string, on bool) error {
	if _, ok := x.s.spaces[space]; !ok || x.s.spaceRole(x.who, space) < Viewer {
		return notFound("space %q", space)
	}
	return x.s.SetWatch(x.who, x.who, "", space, on)
}

// Watches lists what the principal watches, items they may no longer read
// left out.
func (x *Session) Watches() []Watch {
	var out []Watch
	for k, since := range x.s.watches[x.who] {
		kind, target, _ := strings.Cut(k, "\x00")
		if kind == "i" {
			if it := x.s.items[ID(target)]; it == nil || it.Presence == Purged || !x.s.canRead(x.who, it) {
				continue
			}
			out = append(out, Watch{Item: ID(target), Since: since})
		} else {
			out = append(out, Watch{Space: target, Since: since})
		}
	}
	slices.SortFunc(out, func(a, b Watch) int { return a.Since - b.Since })
	return out
}

// Watching reports whether the principal watches an item itself.
func (x *Session) Watching(id ID) bool {
	_, ok := x.s.watches[x.who][watchKey(id, "")]
	return ok
}

// WatchingSpace reports whether the principal watches a space.
func (x *Session) WatchingSpace(space string) bool {
	_, ok := x.s.watches[x.who][watchKey("", space)]
	return ok
}

// Notifications returns what others changed on what the principal
// watches, the comments that mention them by @username when the session
// is Named, and the restrictions governance set because of sensitive data
// on what the principal saved or administers (ADR-0057), newest first, before the event before (all when 0), at most
// limit; more reports whether older ones follow. Only items the principal
// may read now count, so a notification never shows what a restriction
// hides.
func (x *Session) Notifications(before, limit int) (out []Notification, more bool) {
	return x.NotificationsWhere(before, limit, nil)
}

// NotificationsWhere is Notifications narrowed by keep, before paging, as a
// filter of the list asks (ADR-0098); nil keeps every notification.
func (x *Session) NotificationsWhere(before, limit int, keep func(Notification) bool) (out []Notification, more bool) {
	x.scan(before, 0, func(n Notification) bool {
		if keep != nil && !keep(n) {
			return true
		}
		if len(out) == limit {
			more = true
			return false
		}
		out = append(out, n)
		return true
	})
	return out, more
}

// Unread counts the notifications after the event seen, up to limit.
func (x *Session) Unread(seen, limit int) int {
	n := 0
	x.scan(0, seen, func(Notification) bool {
		n++
		return n < limit
	})
	return n
}

// scan walks the log backwards from before (exclusive; 0 is the end) down
// to after (exclusive) and calls fn with each notification until it
// returns false.
func (x *Session) scan(before, after int, fn func(Notification) bool) {
	watches := x.s.watches[x.who]
	// Governance's restrictions are told without watching, so the walk goes
	// back to after, not only to the oldest watch.
	since := func(item ID, space string) (int, bool) {
		s, ok := watches[watchKey(item, space)]
		return s, ok
	}
	for i := len(x.s.log) - 1; i >= 0; i-- {
		e := x.s.log[i]
		if e.Seq <= after {
			return
		}
		if (before > 0 && e.Seq >= before) || e.Item == "" {
			continue
		}
		if x.governanceAlert(e) {
			it := x.s.items[e.Item]
			if !fn(Notification{Seq: e.Seq, At: e.At, Actor: e.Actor, Kind: e.Kind, Item: it.ID, Key: it.Key, Title: it.Title, Space: it.Space}) {
				return
			}
			continue
		}
		if x.secretAlert(e) {
			it := x.s.items[e.Item]
			if !fn(Notification{Seq: e.Seq, At: e.At, Actor: e.Actor, Kind: e.Kind, Item: it.ID, Key: it.Key, Title: it.Title, Space: it.Space}) {
				return
			}
			continue
		}
		if x.mentioned(e) {
			it := x.s.items[e.Item]
			if !fn(Notification{Seq: e.Seq, At: e.At, Actor: e.Actor, Kind: e.Kind, Item: it.ID, Key: it.Key, Title: it.Title, Space: it.Space, Mentioned: true}) {
				return
			}
			continue
		}
		if len(watches) == 0 || !notifyKinds[e.Kind] || e.Actor == x.who {
			continue
		}
		it := x.s.items[e.Item]
		if it == nil || it.Presence == Purged || !x.s.canRead(x.who, it) {
			continue
		}
		watched := false
		created := false
		if s, ok := since(it.ID, ""); ok && s < e.Seq {
			watched = true
		}
		if s, ok := since("", it.Space); ok && s < e.Seq {
			watched = true
		}
		if e.Kind == ItemCreated && it.Parent != "" {
			if s, ok := since(it.Parent, ""); ok && s < e.Seq {
				watched, created = true, true
			}
		}
		if !watched {
			continue
		}
		if !fn(Notification{Seq: e.Seq, At: e.At, Actor: e.Actor, Kind: e.Kind, Item: it.ID, Key: it.Key, Title: it.Title, Space: it.Space, Created: created}) {
			return
		}
	}
}

// mentioned reports whether e added a comment by someone else that names
// the principal by @username. The comment is read as it is now, so one
// deleted or erased since is no longer told. A text names them when an
// edit by someone else adds the name to it (ADR-0096).
func (x *Session) mentioned(e Event) bool {
	if (e.Kind != CommentAdded && e.Kind != BodyCommitted) || x.name == "" || e.Actor == x.who {
		return false
	}
	it := x.s.items[e.Item]
	if it == nil || it.Presence == Purged || !x.s.canRead(x.who, it) {
		return false
	}
	if e.Kind == BodyCommitted {
		if e.Body == nil || !bodyMentions(*e.Body, x.name) {
			return false
		}
		t := x.s.touching[e.Item]
		for i := len(t) - 1; i >= 0; i-- {
			if p := x.s.log[t[i]]; p.Seq < e.Seq && p.Item == e.Item && p.Kind == BodyCommitted && p.Body != nil {
				return !bodyMentions(*p.Body, x.name)
			}
		}
		return true
	}
	for _, c := range it.Comments {
		if c.ID == e.Comment {
			return !c.Deleted && Mentions(c.Text, x.name)
		}
	}
	return false
}

// bodyMentions reports whether a text names username as "@username"
// outside its protected passages, which not everyone may read (ADR-0088).
func bodyMentions(d Doc, username string) bool {
	want := "@" + strings.ToLower(username)
	for _, b := range d.Blocks {
		if !strings.Contains(strings.ToLower(b.Text), want) {
			continue
		}
		text := []rune(b.Text)
		for _, m := range b.Marks {
			if m.Type == MarkSecret {
				for i := max(m.Start, 0); i < min(m.End, len(text)); i++ {
					text[i] = ' '
				}
			}
		}
		if Mentions(string(text), username) {
			return true
		}
	}
	return false
}

// secretAlert reports whether e is about a protected passage the
// principal is to hear of (ADR-0088): a request to read one they decide
// about, or the answer to their own request.
func (x *Session) secretAlert(e Event) bool {
	if e.Secret == nil || e.Actor == x.who {
		return false
	}
	it := x.s.items[e.Item]
	if it == nil || it.Presence == Purged || !x.s.canRead(x.who, it) {
		return false
	}
	switch e.Kind {
	case SecretRequested:
		i, ok := it.secret(e.Secret.ID)
		return ok && x.s.decidesSecret(x.who, it, it.Secrets[i])
	case SecretGranted, SecretDeclined:
		return e.Principal == x.who
	}
	return false
}

// governanceAlert reports whether e is a restriction governance set
// because of sensitive data that the principal is to hear of: as the one
// who saved the data (the one the item is restricted to) or as an
// administrator of the item's space, who may lift it (ADR-0040, ADR-0057).
//
// Data protection holds are told the same way: a hold to those whose saves
// brought the findings in and to the space's administrators, a release to
// those who brought them in (ADR-0058).
func (x *Session) governanceAlert(e Event) bool {
	var concerned []string
	admins := true
	switch {
	case e.Kind == RestrictionSet && e.Actor == GovernanceActor && e.Restriction != nil:
		concerned = e.Restriction.Read
	case e.Kind == HoldSet && e.Hold != nil:
		concerned = e.Hold.Savers
	case e.Kind == HoldReleased && e.Actor != x.who:
		admins = false
		t := x.s.touching[e.Item]
		for i := len(t) - 1; i >= 0; i-- {
			if p := x.s.log[t[i]]; p.Seq < e.Seq && p.Item == e.Item && p.Kind == HoldSet && p.Hold != nil {
				concerned = p.Hold.Savers
				break
			}
		}
	default:
		return false
	}
	it := x.s.items[e.Item]
	if it == nil || it.Presence == Purged || !x.s.canRead(x.who, it) {
		return false
	}
	return slices.Contains(concerned, x.who) || (admins && x.s.spaceRole(x.who, it.Space) == SpaceAdmin)
}
