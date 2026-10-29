package item

import (
	"fmt"
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

// GrantedRole is the role granted to a principal (or "@group") in a space
// directly, not through a group or a system role; 0 if there is none.
func (s *Store) GrantedRole(space, principal string) Role { return s.spaceRoles[space][principal] }

// GroupSize is the number of members of a group, named with or without its
// "@".
func (s *Store) GroupSize(group string) int { return len(s.groups[strings.TrimPrefix(group, "@")]) }

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
	// A space in the trash, or deleted, is out of everyone's sight,
	// administrators' too: they act on it as a whole (ADR-0069).
	if sp := s.spaces[space]; sp != nil && sp.state != "" {
		return 0
	}
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
	// While a data protection review is open, only those who may edit the
	// item read it (ADR-0058); like a restriction, it only narrows (D9).
	if it.Hold != nil {
		if role < Editor {
			return false
		}
		for _, a := range s.ancestry(it) {
			if !s.admits(a.Restriction.Edit, principal) {
				return false
			}
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
	// name is the principal's username, for the comments that mention
	// them by @username; empty when not known.
	name string
}

// Named returns the session with the principal's username, so their
// notifications include the comments that mention them.
func (x *Session) Named(username string) *Session {
	y := *x
	y.name = username
	return &y
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
// CanEdit reports whether the principal may change an item now.
func (x *Session) CanEdit(id ID) bool {
	_, err := x.editable(id)
	return err == nil
}

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

// MayEdit reports whether the principal may edit the item, as every
// command checks it (ADR-0022).
func (x *Session) MayEdit(id ID) bool {
	_, err := x.editable(id)
	return err == nil
}

// editableAll checks edit rights on every live item of a subtree.
func (x *Session) editableAll(root ID) error {
	if _, err := x.editable(root); err != nil {
		return err
	}
	var below []*Item
	for _, id := range x.s.subtree(root) {
		if it := x.s.items[id]; it.Presence == Live {
			below = append(below, it)
		}
	}
	return x.editableEach(root, below)
}

// editableEach checks edit rights on the items below root. An item the
// principal may not read is not named: its key would tell that it exists
// (D10).
func (x *Session) editableEach(root ID, below []*Item) error {
	for _, it := range below {
		if it.ID == root || (x.s.canEdit(x.who, it) && it.Managed == "") {
			continue
		}
		if !x.s.canRead(x.who, it) {
			return forbidden("%s may not change %s: it holds pages %s may not see", x.who, x.s.items[root].Key, x.who)
		}
		return forbidden("%s may not edit %s below %s", x.who, it.Key, x.s.items[root].Key)
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
	// Passages the principal may not read are veiled (ADR-0088).
	return x.s.veiled(x.who, it).clone(), nil
}

// ByKey resolves a current or former key to a readable item.
func (x *Session) ByKey(key string) (Item, error) {
	id, ok := x.s.keys[key]
	if !ok {
		return Item{}, notFound("key %q", key)
	}
	return x.Get(id)
}

// Role is the principal's role in a space: Viewer, Editor, SpaceAdmin, or
// zero when they hold none.
func (x *Session) Role(space string) Role { return x.s.spaceRole(x.who, space) }

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
// FindPage returns the readable items a query matches, in the order they
// were made: the page of up to limit after the item after (from the
// start when empty), how many there are in all, and the last of the page
// when more follow. Only the page is copied, so a listing of a large space
// costs a filter, not a copy of every item (ADR-0081).
func (x *Session) FindPage(q Query, after ID, limit int) (page []Item, total int, next ID, err error) {
	return x.page(x.s.matching(q, func(it *Item) bool { return x.s.canRead(x.who, it) }), after, limit)
}

// page cuts the page after the cursor out of a listing of readable items,
// veiled as the principal reads them.
func (x *Session) page(matches []*Item, after ID, limit int) (page []Item, total int, next ID, err error) {
	start := 0
	if after != "" {
		start = slices.IndexFunc(matches, func(it *Item) bool { return it.ID == after }) + 1
		if start == 0 {
			return nil, 0, "", invalid("cursor %q is not in this listing", after)
		}
	}
	end := min(start+limit, len(matches))
	for _, it := range matches[start:end] {
		page = append(page, x.s.veiled(x.who, it).clone())
	}
	if end < len(matches) && len(page) > 0 {
		next = page[len(page)-1].ID
	}
	return page, len(matches), next, nil
}

func (x *Session) Find(q Query) []Item {
	var out []Item
	for _, it := range x.s.Find(q) {
		if x.s.canRead(x.who, x.s.items[it.ID]) {
			out = append(out, *x.s.veiled(x.who, &it))
		}
	}
	return out
}

// History returns a readable item's history, without the events of other
// items the principal may not read: a body that embeds the item, or a
// relation to it, would otherwise disclose an unreadable item's content.
func (x *Session) History(id ID) ([]Event, error) {
	it, err := x.readable(id)
	if err != nil {
		return nil, err
	}
	var out []Event
	for _, e := range x.s.History(id) {
		if withheld(it, e) {
			e.Body, e.Erased = nil, true
		}
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

// BodyAt returns the body a readable item had after its BodyCommitted
// event seq (ADR-0043). An erased version is gone (ADR-0026).
func (x *Session) BodyAt(id ID, seq int) (Event, error) {
	it, err := x.readable(id)
	if err != nil {
		return Event{}, err
	}
	// An earlier version may hold in clear what is protected now; the
	// versions of an item with protected passages are shown to those who
	// read them all (ADR-0088).
	if !x.s.readsAllSecrets(x.who, it) {
		return Event{}, forbidden("%s holds protected passages that %s may not read; its earlier versions are shown to those who read them all", it.Key, x.who)
	}
	for _, e := range x.s.History(id) {
		if e.Seq != seq || e.Item != id || e.Kind != BodyCommitted {
			continue
		}
		if e.Erased || e.Body == nil || withheld(it, e) {
			return Event{}, notFound("version %d of item %s was erased", seq, id)
		}
		// A passage erased since is gone (ADR-0089): until the next start
		// the history in memory still holds its text.
		e.Body = ptrTo(veil(e.Body.clone(), x.s.hidden(x.who, it)))
		return e, nil
	}
	return Event{}, notFound("body version %d of item %s", seq, id)
}

// RestoreBody commits the body an editable item had after its
// BodyCommitted event seq as its new body; the current one stays in the
// history (ADR-0043).
func (x *Session) RestoreBody(id ID, seq int) error {
	v, err := x.BodyAt(id, seq)
	if err != nil {
		return err
	}
	// A passage protected then and not now reads as any other text.
	it := x.s.items[id]
	erased := map[ID]bool{}
	for _, sec := range it.ErasedSecrets {
		erased[sec] = true
	}
	body := cutSecrets(*v.Body, erased)
	for i, b := range body.Blocks {
		b.Marks = slices.DeleteFunc(b.Marks, func(m Mark) bool {
			_, known := it.secret(m.Ref)
			return m.Type == MarkSecret && !known
		})
		if len(b.Marks) == 0 {
			b.Marks = nil // as a decoded body has it
		}
		body.Blocks[i] = b
	}
	return x.CommitBody(id, body)
}

// withheld reports whether a version of an item's body is withheld: it is
// from before a passage erased since was protected, so it may hold the
// passage's text (ADR-0089).
func withheld(it *Item, e Event) bool {
	return e.Kind == BodyCommitted && e.Item == it.ID && e.Seq < it.erasedBefore
}

func ptrTo[T any](v T) *T { return &v }

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
	if n.Body != nil {
		for _, r := range n.Body.refs() {
			if _, err := x.readable(r.To); err != nil {
				return Item{}, err
			}
		}
	}
	// A person watches what they create (ADR-0046).
	n.Watch = true
	return x.s.Create(x.who, n)
}

// CreateEmbedded creates an item inline in a host the principal may edit.
func (x *Session) CreateEmbedded(host ID, n NewItem) (Item, error) {
	if _, err := x.editable(host); err != nil {
		return Item{}, err
	}
	n.Watch = true
	return x.s.CreateEmbedded(x.who, host, n)
}

// SetTitle renames an editable item.
func (x *Session) SetTitle(id ID, title string) error {
	it, err := x.editable(id)
	if err != nil {
		return err
	}
	if err := linkedRefusal(it, "its title is the task's name there"); err != nil {
		return err
	}
	return x.s.SetTitle(x.who, id, title)
}

// SetLabels replaces the labels of an editable item (ADR-0044).
func (x *Session) SetLabels(id ID, labels []string) error {
	if _, err := x.editable(id); err != nil {
		return err
	}
	return x.s.SetLabels(x.who, id, labels)
}

// LabelCount is a label and how many readable items carry it.
type LabelCount struct {
	Label string
	Count int
}

// Labels counts the labels of the readable live items of a space, or of
// all spaces when space is empty, most used first (ADR-0044).
func (x *Session) Labels(space string) []LabelCount {
	return x.labels(space, false)
}

// TaskLabels counts the labels of the readable tasks, the items whose type
// has a workflow: the labels a board by label shows (ADR-0092).
func (x *Session) TaskLabels(space string) []LabelCount {
	return x.labels(space, true)
}

func (x *Session) labels(space string, tasks bool) []LabelCount {
	counts := map[string]int{}
	for _, it := range x.s.items {
		if it.Presence != Live || (space != "" && it.Space != space) || len(it.Labels) == 0 || !x.s.canRead(x.who, it) {
			continue
		}
		if tasks {
			if td, ok := x.s.latest(it.Type); !ok || td.Workflow == nil {
				continue
			}
		}
		for _, l := range it.Labels {
			counts[l]++
		}
	}
	out := make([]LabelCount, 0, len(counts))
	for l, n := range counts {
		out = append(out, LabelCount{l, n})
	}
	slices.SortFunc(out, func(a, b LabelCount) int {
		if a.Count != b.Count {
			return b.Count - a.Count
		}
		return strings.Compare(a.Label, b.Label)
	})
	return out
}

// SetProperty sets a property of an editable item.
func (x *Session) SetProperty(id ID, field string, value any) error {
	it, err := x.editable(id)
	if err != nil {
		return err
	}
	if slices.Contains(it.LinkedFields, field) {
		if err := linkedRefusal(it, "the engine sets "+field); err != nil {
			return err
		}
	}
	return x.s.SetProperty(x.who, id, field, value)
}

// Transition moves an editable item along its workflow.
func (x *Session) Transition(id ID, name string) error {
	it, err := x.editable(id)
	if err != nil {
		return err
	}
	// A linked task is finished at its engine, which decides whether it may
	// be (ADR-0151); its other states are the wiki's.
	if td, _ := x.s.latest(it.Type); it.Linked != "" && td.Workflow != nil {
		if tr, ok := td.Workflow.transition(name); ok && td.Workflow.Finished(tr.To) {
			return linkedRefusal(it, "it is completed at the engine")
		}
	}
	return x.s.Transition(x.who, id, name)
}

// CommitBody commits a body of an editable item. New references must point
// at readable items; references the previous version had may stay.
func (x *Session) CommitBody(id ID, d Doc) error {
	it, err := x.editableBody(id)
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
	if err := x.keepsLinkedTasks(id, toSpace); err != nil {
		return err
	}
	if err := x.canCreateIn(toSpace, newParent); err != nil {
		return err
	}
	return x.s.Move(x.who, id, toSpace, newParent)
}

// Place moves an item like Move and puts it before a readable sibling, or
// last (ADR-0041). Siblings the principal may not read keep their places
// relative to each other.
func (x *Session) Place(id ID, toSpace string, newParent, before ID) error {
	if err := x.editableAll(id); err != nil {
		return err
	}
	if err := x.keepsLinkedTasks(id, toSpace); err != nil {
		return err
	}
	if err := x.canCreateIn(toSpace, newParent); err != nil {
		return err
	}
	if before != "" {
		if _, err := x.readable(before); err != nil {
			return err
		}
	}
	return x.s.Place(x.who, id, toSpace, newParent, before)
}

// openTaskBelow refuses removing an item when it, or an item below it, is
// a linked task its engine has not ended (ADR-0151).
func (x *Session) openTaskBelow(id ID) error {
	for _, sub := range x.s.subtree(id) {
		it := x.s.items[sub]
		if td, _ := x.s.latest(it.Type); it.Presence == Live && it.Linked != "" && !td.Workflow.Finished(it.State) {
			return linkedRefusal(it, "it is open there; it can be removed once the engine has ended it")
		}
	}
	return nil
}

// keepsLinkedTasks refuses moving an item to another space when it, or an
// item below it, is a linked task: its space is where the engine's
// readers were given their role (ADR-0151).
func (x *Session) keepsLinkedTasks(id ID, toSpace string) error {
	if it := x.s.items[id]; it == nil || it.Space == toSpace {
		return nil
	}
	for _, sub := range x.s.subtree(id) {
		if it := x.s.items[sub]; it.Presence == Live && it.Linked != "" {
			return linkedRefusal(it, "it stays in its space")
		}
	}
	return nil
}

// TreeNode is one readable child in a page tree. Item is its summary,
// without content (ADR-0081).
type TreeNode struct {
	Item        Item
	HasChildren bool // it has readable live children
}

// Tree returns the readable children of an item, or the readable items at
// the top of a space when parent is empty, in sibling order, each with
// whether it has readable children itself (ADR-0041).
func (x *Session) Tree(space string, parent ID) ([]TreeNode, error) {
	if !x.s.HasSpace(space) {
		return nil, notFound("space %q", space)
	}
	if parent != "" {
		p, err := x.readable(parent)
		if err != nil {
			return nil, err
		}
		if p.Space != space {
			return nil, notFound("item %s in space %s", parent, space)
		}
	}
	// The children of every item of the space, so that whether a node has
	// a readable child is asked only for the nodes listed (ADR-0081).
	kids := map[ID][]*Item{}
	for _, it := range x.s.items {
		if it.Presence == Live && it.Space == space && it.Parent != "" {
			kids[it.Parent] = append(kids[it.Parent], it)
		}
	}
	var out []TreeNode
	for _, it := range x.s.Children(space, parent) {
		if x.s.canRead(x.who, x.s.items[it.ID]) {
			has := slices.ContainsFunc(kids[it.ID], func(k *Item) bool { return x.s.canRead(x.who, k) })
			out = append(out, TreeNode{Item: it, HasChildren: has})
		}
	}
	return out, nil
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

// Trash trashes a subtree the principal may edit entirely. The items
// that still reference it are named only when the principal may read them
// (D10).
func (x *Session) Trash(id ID) error {
	if err := x.editableAll(id); err != nil {
		return err
	}
	if err := x.openTaskBelow(id); err != nil {
		return err
	}
	if refs := x.s.referrers(id); len(refs) > 0 {
		var named []string
		hidden := 0
		for _, r := range refs {
			if x.s.canRead(x.who, r) {
				named = append(named, r.Key)
			} else {
				hidden++
			}
		}
		if hidden > 0 {
			named = append(named, fmt.Sprintf("%d item(s) you may not see", hidden))
		}
		return fmt.Errorf("%w: %w: %s is referenced by %s; remove those references first",
			ErrValidation, ErrReferenced, x.s.items[id].Key, strings.Join(named, ", "))
	}
	return x.s.Trash(x.who, id)
}

// Restore restores a trash batch the principal may edit entirely: every
// item trashed with it, not only the one it was trashed from.
func (x *Session) Restore(id ID) error {
	if _, err := x.editable(id); err != nil {
		return err
	}
	members, err := x.s.trashBatch(id)
	if err != nil {
		return err
	}
	below := make([]*Item, len(members))
	for i, m := range members {
		below[i] = x.s.items[m]
	}
	if err := x.editableEach(id, below); err != nil {
		return err
	}
	return x.s.Restore(x.who, id)
}

// MayTrash reports whether the principal may trash an item with what is
// below it, as Trash checks it.
func (x *Session) MayTrash(id ID) bool { return x.editableAll(id) == nil && x.openTaskBelow(id) == nil }

// MayRestore reports whether the principal may restore an item from the
// trash: it is the item the trash started from, and the principal may
// edit everything trashed with it.
func (x *Session) MayRestore(id ID) bool {
	if _, err := x.editable(id); err != nil {
		return false
	}
	members, err := x.s.trashBatch(id)
	if err != nil {
		return false
	}
	below := make([]*Item, len(members))
	for i, m := range members {
		below[i] = x.s.items[m]
	}
	return x.editableEach(id, below) == nil
}

// MayPurge reports whether the principal may purge an item from the
// trash, as Purge checks it.
func (x *Session) MayPurge(id ID) bool {
	it, err := x.readable(id)
	if err != nil || x.s.spaceRole(x.who, it.Space) < SpaceAdmin {
		return false
	}
	_, err = x.s.trashBatch(id)
	return err == nil
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

// Restrict sets a restriction on an item. Every page below inherits it,
// so, as for moving it (ADR-0022), the principal must be able to edit them
// all. A principal who is not a space admin cannot restrict itself out.
func (x *Session) Restrict(id ID, r Restriction) error {
	if err := x.editableAll(id); err != nil {
		return err
	}
	it := x.s.items[id]
	if err := linkedRefusal(it, "who reads it, the engine decides"); err != nil {
		return err
	}
	if err := x.MayRestrictIn(it.Space, r); err != nil {
		return fmt.Errorf("%w of %s", err, it.Key)
	}
	return x.s.Restrict(x.who, id, r)
}

// KeepsInherited refuses a copy of src below toParent (or at the top of a
// space when empty) that would shed a restriction src inherits from a page
// above it: the copy carries only src's own restriction, so it would
// otherwise be open to people the original is closed to.
func (x *Session) KeepsInherited(src, toParent ID) error {
	it, err := x.readable(src)
	if err != nil {
		return err
	}
	under := map[ID]bool{}
	if toParent != "" {
		p, err := x.readable(toParent)
		if err != nil {
			return err
		}
		for _, a := range x.s.ancestry(p) {
			under[a.ID] = true
		}
	}
	for _, a := range x.s.ancestry(it)[1:] {
		if len(a.Restriction.Read)+len(a.Restriction.Edit) > 0 && !under[a.ID] {
			return invalid("%s is protected through a page above it; copy it below that page, or protect the copy's place first", it.Key)
		}
	}
	return nil
}

// MayRestrictIn checks, before anything is made, that the principal may
// put a restriction on an item of a space without locking itself out.
func (x *Session) MayRestrictIn(space string, r Restriction) error {
	if x.s.spaceRole(x.who, space) < SpaceAdmin && (!x.s.admits(r.Read, x.who) || !x.s.admits(r.Edit, x.who)) {
		return invalid("%s would lock itself out", x.who)
	}
	return nil
}

// Grant needs the space admin role.
func (x *Session) Grant(space, principal string, role Role) error {
	if x.s.spaceRole(x.who, space) < SpaceAdmin {
		return forbidden("%s may not grant roles in %s", x.who, space)
	}
	if err := x.keepsAnAdmin(space, principal, role); err != nil {
		return err
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
