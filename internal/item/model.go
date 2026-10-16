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
	Version int

	created int // sequence number of the creating event; orders items
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
	TypeDefined     EventKind = "TypeDefined"
	SpaceCreated    EventKind = "SpaceCreated"
	ItemCreated     EventKind = "ItemCreated"
	TitleSet        EventKind = "TitleSet"
	PropertySet     EventKind = "PropertySet"
	Transitioned    EventKind = "Transitioned"
	ItemMoved       EventKind = "ItemMoved"
	BodyCommitted   EventKind = "BodyCommitted"
	RelationAdded   EventKind = "RelationAdded"
	RelationRemoved EventKind = "RelationRemoved"
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
