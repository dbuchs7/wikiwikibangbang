package item

import (
	"slices"
	"strings"
)

// Role is a principal's role in a space (ADR-0022). Higher includes lower.
type Role int

// The space roles.
const (
	Viewer Role = iota + 1
	Editor
	SpaceAdmin
)

// System roles, held across all spaces.
const (
	// RoleAdmin acts as SpaceAdmin in every space and may define types and spaces.
	RoleAdmin = "admin"
	// RoleErasure may redact an item at once, bypassing the reference gate
	// of ADR-0021, with a logged reason.
	RoleErasure = "erasure"
)

// Restriction narrows access to an item and everything below it. An empty
// list does not restrict. Entries name principals; "@name" names a group.
// A restriction never grants: the principal also needs a space role.
type Restriction struct {
	Read []string
	Edit []string
}

// --- trusted commands (no permission checks; used by setup and the API) ----

// AddToGroup adds a principal to a group.
func (s *Store) AddToGroup(actor, group, member string) error {
	if strings.TrimSpace(group) == "" || strings.TrimSpace(member) == "" {
		return invalid("group and member must not be empty")
	}
	return s.record(actor, Event{Kind: GroupMemberAdded, Group: group, Principal: member})
}

// GrantRole gives a principal (or "@group") a role in a space.
func (s *Store) GrantRole(actor, space, principal string, role Role) error {
	if _, ok := s.spaces[space]; !ok {
		return notFound("space %q", space)
	}
	if role < Viewer || role > SpaceAdmin {
		return invalid("unknown role %d", role)
	}
	return s.record(actor, Event{Kind: RoleGranted, Space: space, Principal: principal, Role: role})
}

// GrantSystemRole gives a principal a system role.
func (s *Store) GrantSystemRole(actor, principal, role string) error {
	if role != RoleAdmin && role != RoleErasure {
		return invalid("unknown system role %q", role)
	}
	return s.record(actor, Event{Kind: SystemRoleGranted, Principal: principal, SystemRole: role})
}

// Restrict replaces an item's restriction.
func (s *Store) Restrict(actor string, id ID, r Restriction) error {
	if _, err := s.live(id); err != nil {
		return err
	}
	r = Restriction{Read: slices.Clone(r.Read), Edit: slices.Clone(r.Edit)}
	return s.record(actor, Event{Kind: RestrictionSet, Item: id, Restriction: &r})
}

// Redact removes an item's content at once, whether or not it is
// referenced or in the trash, and leaves a tombstone. It is the logged
// exception to D8 for content that must go immediately (ADR-0022). Only the
// item itself is redacted; its children stay.
func (s *Store) Redact(actor string, id ID, reason string) error {
	it, err := s.item(id)
	if err != nil {
		return err
	}
	if it.Presence == Purged {
		return invalid("item %s is already a tombstone", it.Key)
	}
	if strings.TrimSpace(reason) == "" {
		return invalid("a redaction needs a reason")
	}
	return s.record(actor, Event{Kind: ItemRedacted, Item: id, Reason: reason})
}

// applyAccess applies the permission and redaction events.
func (s *Store) applyAccess(e Event, touch func(...ID)) {
	switch e.Kind {
	case GroupMemberAdded:
		if s.groups[e.Group] == nil {
			s.groups[e.Group] = map[string]bool{}
		}
		s.groups[e.Group][e.Principal] = true
	case RoleGranted:
		if s.spaceRoles[e.Space] == nil {
			s.spaceRoles[e.Space] = map[string]Role{}
		}
		s.spaceRoles[e.Space][e.Principal] = e.Role
	case SystemRoleGranted:
		if s.sysRoles[e.Principal] == nil {
			s.sysRoles[e.Principal] = map[string]bool{}
		}
		s.sysRoles[e.Principal][e.SystemRole] = true
	case RestrictionSet:
		it := s.items[e.Item]
		it.Restriction = Restriction{Read: slices.Clone(e.Restriction.Read), Edit: slices.Clone(e.Restriction.Edit)}
		it.Version++
		touch(it.ID)
	case ItemRedacted:
		s.purgeItems(e.Item)
		touch(e.Item)
	}
}

// --- access decisions -------------------------------------------------------

// matches reports whether a principal is named by an entry, directly or
// through a group ("@name").
func (s *Store) matches(principal, entry string) bool {
	if group, ok := strings.CutPrefix(entry, "@"); ok {
		return s.groups[group][principal]
	}
	return entry == principal
}

func (s *Store) admits(list []string, principal string) bool {
	if len(list) == 0 {
		return true
	}
	return slices.ContainsFunc(list, func(e string) bool { return s.matches(principal, e) })
}

func (s *Store) hasSystemRole(principal, role string) bool { return s.sysRoles[principal][role] }

// HasSystemRole reports whether a principal holds a system role.
func (s *Store) HasSystemRole(principal, role string) bool { return s.hasSystemRole(principal, role) }

// SystemRoleHeld reports whether any principal holds a system role, e.g.
// whether an administrator exists.
func (s *Store) SystemRoleHeld(role string) bool {
	for _, roles := range s.sysRoles {
		if roles[role] {
			return true
		}
	}
	return false
}

// spaceRole is the highest role a principal holds in a space, directly or
// through groups; a system admin is SpaceAdmin everywhere.
func (s *Store) spaceRole(principal, space string) Role {
	if s.hasSystemRole(principal, RoleAdmin) {
		return SpaceAdmin
	}
	best := Role(0)
	for entry, r := range s.spaceRoles[space] {
		if r > best && s.matches(principal, entry) {
			best = r
		}
	}
	return best
}

// ancestry returns an item and its ancestors, nearest first.
func (s *Store) ancestry(it *Item) []*Item {
	out := []*Item{it}
	for p := it.Parent; p != ""; p = s.items[p].Parent {
		out = append(out, s.items[p])
	}
	return out
}

func (s *Store) canRead(principal string, it *Item) bool {
	role := s.spaceRole(principal, it.Space)
	if role == SpaceAdmin {
		return true
	}
	if role < Viewer {
		return false
	}
	for _, a := range s.ancestry(it) {
		if !s.admits(a.Restriction.Read, principal) {
			return false
		}
	}
	return true
}

func (s *Store) canEdit(principal string, it *Item) bool {
	role := s.spaceRole(principal, it.Space)
	if role == SpaceAdmin {
		return true
	}
	if role < Editor || !s.canRead(principal, it) {
		return false
	}
	for _, a := range s.ancestry(it) {
		if !s.admits(a.Restriction.Edit, principal) {
			return false
		}
	}
	return true
}

// --- sessions ---------------------------------------------------------------

// Session performs commands and queries as one principal, with the checks of
// ADR-0022. An item the principal may not read behaves as if it did not
// exist (ErrNotFound); one it may read but not change yields ErrForbidden.
type Session struct {
	s   *Store
	who string
}

// As returns a session for a principal.
func (s *Store) As(principal string) *Session { return &Session{s: s, who: principal} }

func forbidden(format string, args ...any) error {
	return wrapf(ErrForbidden, format, args...)
}

// readable returns the item if the principal may read it.
func (x *Session) readable(id ID) (*Item, error) {
	it, err := x.s.item(id)
	if err != nil || !x.s.canRead(x.who, it) {
		return nil, notFound("item %s", id)
	}
	return it, nil
}

// editable returns the item if the principal may edit it.
func (x *Session) editable(id ID) (*Item, error) {
	it, err := x.readable(id)
	if err != nil {
		return nil, err
	}
	if !x.s.canEdit(x.who, it) {
		return nil, forbidden("%s may not edit %s", x.who, it.Key)
	}
	if it.Managed != "" {
		return nil, forbidden("%s is managed by %s; change it in the source until cut-over", it.Key, it.Managed)
	}
	return it, nil
}

// editableAll checks edit rights on every live item of a subtree.
func (x *Session) editableAll(root ID) error {
	if _, err := x.editable(root); err != nil {
		return err
	}
	for _, id := range x.s.subtree(root) {
		if it := x.s.items[id]; it.Presence == Live && (!x.s.canEdit(x.who, it) || it.Managed != "") {
			return forbidden("%s may not edit %s below %s", x.who, it.Key, x.s.items[root].Key)
		}
	}
	return nil
}

// canCreateIn checks the right to create an item at a place.
func (x *Session) canCreateIn(space string, parent ID) error {
	if parent != "" {
		_, err := x.editable(parent)
		return err
	}
	if x.s.spaceRole(x.who, space) < Editor {
		return forbidden("%s may not create in %s", x.who, space)
	}
	return nil
}

// Get returns a readable item.
func (x *Session) Get(id ID) (Item, error) {
	it, err := x.readable(id)
	if err != nil {
		return Item{}, err
	}
	return it.clone(), nil
}

// ByKey resolves a current or former key to a readable item.
func (x *Session) ByKey(key string) (Item, error) {
	id, ok := x.s.keys[key]
	if !ok {
		return Item{}, notFound("key %q", key)
	}
	return x.Get(id)
}

// Spaces lists the spaces in which the principal holds a role.
func (x *Session) Spaces() []SpaceInfo {
	var out []SpaceInfo
	for _, sp := range x.s.Spaces() {
		if x.s.spaceRole(x.who, sp.Key) >= Viewer {
			out = append(out, sp)
		}
	}
	return out
}

// Find returns the readable items matching q.
func (x *Session) Find(q Query) []Item {
	var out []Item
	for _, it := range x.s.Find(q) {
		if x.s.canRead(x.who, x.s.items[it.ID]) {
			out = append(out, it)
		}
	}
	return out
}

// History returns a readable item's history, without the events of other
// items the principal may not read: a body that embeds the item, or a
// relation to it, would otherwise disclose an unreadable item's content.
func (x *Session) History(id ID) ([]Event, error) {
	if _, err := x.readable(id); err != nil {
		return nil, err
	}
	var out []Event
	for _, e := range x.s.History(id) {
		ends := []ID{e.Item}
		if e.Relation != nil {
			ends = []ID{e.Relation.From, e.Relation.To}
		}
		visible := true
		for _, end := range ends {
			if end != "" && !x.s.canRead(x.who, x.s.items[end]) {
				visible = false
			}
		}
		if visible {
			out = append(out, e)
		}
	}
	return out, nil
}

// Relations returns the relations of a readable item whose other end is
// readable too.
func (x *Session) Relations(id ID, dir Direction) ([]Relation, error) {
	if _, err := x.readable(id); err != nil {
		return nil, err
	}
	var out []Relation
	for _, r := range x.s.Relations(id, dir) {
		other := r.To
		if dir == Incoming {
			other = r.From
		}
		if x.s.canRead(x.who, x.s.items[other]) {
			out = append(out, r)
		}
	}
	return out, nil
}

// Embeds renders a readable item's embeds; unreadable targets show as
// restricted, without identity or title.
func (x *Session) Embeds(host ID) ([]EmbedView, error) {
	if _, err := x.readable(host); err != nil {
		return nil, err
	}
	views, err := x.s.Embeds(host)
	for i, v := range views {
		if !x.s.canRead(x.who, x.s.items[v.Ref]) {
			views[i] = EmbedView{Restricted: true}
		}
	}
	return views, err
}

// Create creates an item where the principal may create.
func (x *Session) Create(n NewItem) (Item, error) {
	if err := x.canCreateIn(n.Space, n.Parent); err != nil {
		return Item{}, err
	}
	return x.s.Create(x.who, n)
}

// CreateEmbedded creates an item inline in a host the principal may edit.
func (x *Session) CreateEmbedded(host ID, n NewItem) (Item, error) {
	if _, err := x.editable(host); err != nil {
		return Item{}, err
	}
	return x.s.CreateEmbedded(x.who, host, n)
}

// SetTitle renames an editable item.
func (x *Session) SetTitle(id ID, title string) error {
	if _, err := x.editable(id); err != nil {
		return err
	}
	return x.s.SetTitle(x.who, id, title)
}

// SetProperty sets a property of an editable item.
func (x *Session) SetProperty(id ID, field string, value any) error {
	if _, err := x.editable(id); err != nil {
		return err
	}
	return x.s.SetProperty(x.who, id, field, value)
}

// Transition moves an editable item along its workflow.
func (x *Session) Transition(id ID, name string) error {
	if _, err := x.editable(id); err != nil {
		return err
	}
	return x.s.Transition(x.who, id, name)
}

// CommitBody commits a body of an editable item. New references must point
// at readable items; references the previous version had may stay.
func (x *Session) CommitBody(id ID, d Doc) error {
	it, err := x.editable(id)
	if err != nil {
		return err
	}
	kept := map[ID]bool{}
	for _, r := range it.Body.refs() {
		kept[r.To] = true
	}
	for _, r := range d.refs() {
		if !kept[r.To] {
			if _, err := x.readable(r.To); err != nil {
				return err
			}
		}
	}
	return x.s.CommitBody(x.who, id, d)
}

// Move moves a subtree the principal may edit entirely, to a place where it
// may create.
func (x *Session) Move(id ID, toSpace string, newParent ID) error {
	if err := x.editableAll(id); err != nil {
		return err
	}
	if err := x.canCreateIn(toSpace, newParent); err != nil {
		return err
	}
	return x.s.Move(x.who, id, toSpace, newParent)
}

// AddRelation relates an editable item to a readable one.
func (x *Session) AddRelation(typ string, from, to ID) error {
	if _, err := x.editable(from); err != nil {
		return err
	}
	if _, err := x.readable(to); err != nil {
		return err
	}
	return x.s.AddRelation(x.who, typ, from, to)
}

// RemoveRelation removes a relation from an editable item.
func (x *Session) RemoveRelation(id ID) error {
	r, ok := x.s.rels[id]
	if !ok {
		return notFound("relation %s", id)
	}
	if _, err := x.editable(r.From); err != nil {
		return err
	}
	return x.s.RemoveRelation(x.who, id)
}

// Trash trashes a subtree the principal may edit entirely.
func (x *Session) Trash(id ID) error {
	if err := x.editableAll(id); err != nil {
		return err
	}
	return x.s.Trash(x.who, id)
}

// Restore restores a trash batch the principal may edit.
func (x *Session) Restore(id ID) error {
	if err := x.editableAll(id); err != nil {
		return err
	}
	return x.s.Restore(x.who, id)
}

// Purge needs the space admin role.
func (x *Session) Purge(id ID) error {
	it, err := x.readable(id)
	if err != nil {
		return err
	}
	if x.s.spaceRole(x.who, it.Space) < SpaceAdmin {
		return forbidden("%s may not purge in %s", x.who, it.Space)
	}
	return x.s.Purge(x.who, id)
}

// Restrict sets a restriction on an editable item. A principal who is not a
// space admin cannot restrict itself out.
func (x *Session) Restrict(id ID, r Restriction) error {
	it, err := x.editable(id)
	if err != nil {
		return err
	}
	if x.s.spaceRole(x.who, it.Space) < SpaceAdmin && (!x.s.admits(r.Read, x.who) || !x.s.admits(r.Edit, x.who)) {
		return invalid("%s would lock itself out of %s", x.who, it.Key)
	}
	return x.s.Restrict(x.who, id, r)
}

// Grant needs the space admin role.
func (x *Session) Grant(space, principal string, role Role) error {
	if x.s.spaceRole(x.who, space) < SpaceAdmin {
		return forbidden("%s may not grant roles in %s", x.who, space)
	}
	return x.s.GrantRole(x.who, space, principal, role)
}

// Redact needs the system role "erasure".
func (x *Session) Redact(id ID, reason string) error {
	if !x.s.hasSystemRole(x.who, RoleErasure) {
		if _, err := x.s.item(id); err != nil {
			return err
		}
		return forbidden("%s may not redact", x.who)
	}
	return x.s.Redact(x.who, id, reason)
}
