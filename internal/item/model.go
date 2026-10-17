// Package item is the in-memory prototype of the unified item model of
// ADR-0001. Every piece of user content is an Item with a type, properties,
// an optional document body, an optional workflow and typed relations.
//
// Every change is an Event appended to a log, and the state is a fold of
// that log (ADR-0008), so the prototype can show that the model replays
// deterministically. Persistence, permissions and collaboration are out of
// scope.
package item

import (
	"errors"
	"maps"
	"slices"
	"time"
)

// Errors returned by commands. Use errors.Is.
var (
	ErrValidation = errors.New("validation failed")
	ErrNotFound   = errors.New("not found")
	// ErrReferenced refuses a deletion while live items outside the deleted
	// subtree still point into it (ADR-0021). It also matches ErrValidation.
	ErrReferenced = errors.New("still referenced")
	// ErrForbidden refuses a command the principal may see but not perform.
	// Where the principal may not even read the item, ErrNotFound is
	// returned instead, so that existence does not leak (ADR-0022).
	ErrForbidden = errors.New("forbidden")
)

// ID is the immutable identity of an item or relation (D2).
type ID string

// Props holds property values by field name.
type Props map[string]any

// BlockKind is the kind of a body block.
type BlockKind string

// The supported block kinds.
const (
	Paragraph BlockKind = "paragraph"
	Heading   BlockKind = "heading"
	// Embed shows another item inline, e.g. a task inside a page.
	Embed BlockKind = "embed"
)

// Block is one block of a document body. Mentions and Ref point at items by
// ID; the body never holds another item's data (D4).
type Block struct {
	Kind     BlockKind
	Text     string
	Mentions []ID
	Ref      ID
}

// Doc is a structured document body (ADR-0001: a block document, not HTML
// or Markdown as storage format).
type Doc struct {
	Blocks []Block
}

// Presence is an item's lifecycle position, separate from its workflow
// state (ADR-0021).
type Presence string

// The presences. A trashed item can be restored; a purged item is a
// tombstone that keeps only its identity (D7).
const (
	Live    Presence = "live"
	Trashed Presence = "trashed"
	Purged  Presence = "purged"
)

// Item is one piece of user content.
type Item struct {
	ID          ID
	Key         string
	Type        string
	TypeVersion int
	Space       string
	Parent      ID
	Title       string
	Properties  Props
	State       string
	Body        Doc
	// Version counts the changes to the item itself.
	Version  int
	Presence Presence
	// Restriction narrows who may read or edit the item and everything
	// below it (ADR-0022). It is access data, not content: purge keeps it.
	Restriction Restriction

	created   int // sequence number of the creating event; orders items
	trashedBy int // sequence number of the event that trashed it, or 0
}

// EmbedView is how one embed block of a body renders (ADR-0020). Child is
// derived, not stored: the target lives in the host when its parent is the
// host. Title is empty for a purged target.
type EmbedView struct {
	Ref      ID
	Key      string
	Title    string
	Child    bool
	Presence Presence
	// Restricted is set when the viewer may not read the target; Ref, Key
	// and Title are then empty (ADR-0022).
	Restricted bool
}

// Relation is a typed, directed edge between two items. Derived relations
// are computed from a body (D4) and change only with it.
type Relation struct {
	ID      ID
	Type    string
	From    ID
	To      ID
	Derived bool

	created int
}

// Direction selects relations by end.
type Direction int

// Relation directions.
const (
	Outgoing Direction = iota
	Incoming
)

// Relation types derived from bodies. They cannot be added or removed by hand.
const (
	RelMentions = "mentions"
	RelEmbeds   = "embeds"
)

// EventKind names a change.
type EventKind string

// The event kinds.
const (
	TypeDefined       EventKind = "TypeDefined"
	SpaceCreated      EventKind = "SpaceCreated"
	ItemCreated       EventKind = "ItemCreated"
	TitleSet          EventKind = "TitleSet"
	PropertySet       EventKind = "PropertySet"
	Transitioned      EventKind = "Transitioned"
	ItemMoved         EventKind = "ItemMoved"
	BodyCommitted     EventKind = "BodyCommitted"
	RelationAdded     EventKind = "RelationAdded"
	RelationRemoved   EventKind = "RelationRemoved"
	ItemTrashed       EventKind = "ItemTrashed"
	ItemRestored      EventKind = "ItemRestored"
	ItemPurged        EventKind = "ItemPurged"
	ItemRedacted      EventKind = "ItemRedacted"
	GroupMemberAdded  EventKind = "GroupMemberAdded"
	RoleGranted       EventKind = "RoleGranted"
	SystemRoleGranted EventKind = "SystemRoleGranted"
	RestrictionSet    EventKind = "RestrictionSet"
)

// Event is one immutable change. Values that are not deterministic (time,
// generated IDs) are frozen into the event, so replaying the log yields the
// same state.
type Event struct {
	Seq   int
	At    time.Time
	Actor string
	Kind  EventKind
	Item  ID
	// Batch is the sequence number of the first event of a multi-event
	// command, shared by all its events; 0 for a single-event command. A
	// batch is applied as a whole or not at all (ADR-0020, ADR-0008).
	Batch int

	TypeDef     *TypeDef // TypeDefined
	Space       string   // SpaceCreated, ItemCreated, ItemMoved
	SpaceName   string   // SpaceCreated
	TypeName    string   // ItemCreated
	TypeVersion int      // ItemCreated
	Parent      ID       // ItemCreated, ItemMoved
	Title       string   // ItemCreated, TitleSet
	Props       Props    // ItemCreated
	Field       string   // PropertySet
	Value       any      // PropertySet
	Transition  string   // Transitioned
	From, To    string   // Transitioned
	Body        *Doc     // BodyCommitted
	Relation    *Relation
	Members     []ID // ItemTrashed, ItemRestored, ItemPurged: every item affected

	Group       string       // GroupMemberAdded
	Principal   string       // GroupMemberAdded, RoleGranted, SystemRoleGranted
	Role        Role         // RoleGranted
	SystemRole  string       // SystemRoleGranted
	Restriction *Restriction // RestrictionSet
	Reason      string       // ItemRedacted
}

func (it *Item) clone() Item {
	c := *it
	c.Properties = maps.Clone(it.Properties)
	c.Body = it.Body.clone()
	return c
}

func (d Doc) clone() Doc {
	blocks := make([]Block, len(d.Blocks))
	for i, b := range d.Blocks {
		b.Mentions = slices.Clone(b.Mentions)
		blocks[i] = b
	}
	return Doc{Blocks: blocks}
}

// refs returns the derived relations a body implies, deduplicated, in
// document order. It is a pure function of the body (D4).
func (d Doc) refs() []struct {
	Type string
	To   ID
} {
	type ref = struct {
		Type string
		To   ID
	}
	var out []ref
	seen := map[ref]bool{}
	add := func(r ref) {
		if !seen[r] {
			seen[r] = true
			out = append(out, r)
		}
	}
	for _, b := range d.Blocks {
		if b.Kind == Embed {
			add(ref{RelEmbeds, b.Ref})
		}
		for _, m := range b.Mentions {
			add(ref{RelMentions, m})
		}
	}
	return out
}
