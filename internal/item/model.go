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
	// Unsupported keeps content an importer could not convert; Text holds
	// the original markup, which renders as visible text, never as markup
	// (ADR-0014, ADR-0023).
	Unsupported BlockKind = "unsupported"
	// Rich-text block kinds (ADR-0032). List items are flat blocks, not
	// nested lists; consecutive items render as one list.
	BulletItem  BlockKind = "bullet"
	OrderedItem BlockKind = "ordered"
	Quote       BlockKind = "quote"
	CodeBlock   BlockKind = "code"
	// Block kinds of ADR-0042. A task is a checklist item (Checked); a
	// panel is a highlighted note with a Tone; consecutive tasks and panels
	// of one tone render as one list or box. A table holds its cells in
	// Text, rows separated by newlines and cells by tabs, the first row
	// being the header, so search and governance read it like any text. A
	// table of contents and a list of child pages hold nothing: they are
	// computed when shown.
	TaskItem BlockKind = "task"
	Panel    BlockKind = "panel"
	Table    BlockKind = "table"
	TOC      BlockKind = "toc"
	Children BlockKind = "children"
	// Image shows an image attachment of the item; Ref names the
	// attachment and Text is its caption (ADR-0045).
	Image BlockKind = "image"
)

// PanelTones are the tones of a panel block.
var PanelTones = []string{"info", "note", "warning", "tip", "error"}

// MarkType is an inline format over a range of a block's text (ADR-0032).
type MarkType string

const (
	MarkBold    MarkType = "bold"
	MarkItalic  MarkType = "italic"
	MarkCode    MarkType = "code"
	MarkLink    MarkType = "link"    // Href holds the target
	MarkMention MarkType = "mention" // Ref names the mentioned item
)

// Mark formats the text between Start and End, counted in Unicode code
// points of the block's Text. The text itself stays plain, so search and
// derived relations need no markup (D4).
type Mark struct {
	Type  MarkType
	Start int
	End   int
	Href  string `json:",omitempty"`
	Ref   ID     `json:",omitempty"`
}

// Block is one block of a document body. Mentions and Ref point at items by
// ID; the body never holds another item's data (D4).
type Block struct {
	Kind     BlockKind
	Text     string
	Mentions []ID
	Ref      ID
	// Level is a heading's level, 1 to 3; 0 reads as 1 (ADR-0032).
	Level int    `json:",omitempty"`
	Marks []Mark `json:",omitempty"`
	// Checked is whether a task is done; Tone is a panel's tone (ADR-0042).
	Checked bool   `json:",omitempty"`
	Tone    string `json:",omitempty"`
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
	// Origin is set on imported items (ADR-0023).
	Origin *Origin
	// Managed is the tag of the one-way sync that owns the item, or empty.
	Managed string
	// Comments are the item's comments in the order written (ADR-0035).
	Comments []Comment
	// Labels are the item's labels, sorted and unique (ADR-0044).
	Labels []string
	// Attachments are the item's files in the order added, deleted ones
	// without their description (ADR-0045).
	Attachments []Attachment

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
	ItemImported      EventKind = "ItemImported"
	ItemReleased      EventKind = "ItemReleased"
	CommentAdded      EventKind = "CommentAdded"
	CommentEdited     EventKind = "CommentEdited"
	LabelsSet         EventKind = "LabelsSet"
	AttachmentAdded   EventKind = "AttachmentAdded"
	AttachmentDeleted EventKind = "AttachmentDeleted"
	Watched           EventKind = "Watched"
	Unwatched         EventKind = "Unwatched"
	CommentDeleted    EventKind = "CommentDeleted"
	CommentResolved   EventKind = "CommentResolved"
	CommentReopened   EventKind = "CommentReopened"
	// Taking access back (ADR-0048).
	RoleRevoked        EventKind = "RoleRevoked"
	GroupMemberRemoved EventKind = "GroupMemberRemoved"
	SystemRoleRevoked  EventKind = "SystemRoleRevoked"
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
	// Order is the new order of the children of Parent (or the top of
	// Space) after an ItemMoved that placed the item (ADR-0041); nil keeps
	// the order.
	Order      []ID
	Title      string // ItemCreated, TitleSet
	Props      Props  // ItemCreated
	Field      string // PropertySet
	Value      any    // PropertySet
	Transition string // Transitioned
	From, To   string // Transitioned
	Body       *Doc   // BodyCommitted
	Relation   *Relation
	Members    []ID // ItemTrashed, ItemRestored, ItemPurged: every item affected

	Group       string       // GroupMemberAdded, GroupMemberRemoved
	Principal   string       // GroupMemberAdded, RoleGranted, SystemRoleGranted, and their revocations
	Role        Role         // RoleGranted
	SystemRole  string       // SystemRoleGranted
	Restriction *Restriction // RestrictionSet
	Reason      string       // ItemRedacted
	Origin      *Origin      // ItemImported
	Key         string       // ItemImported: the source key to keep, if free
	Managed     string       // ItemImported, ItemReleased
	Comment     ID           // CommentAdded, CommentEdited, CommentDeleted, CommentResolved, CommentReopened
	Text        string       // CommentAdded, CommentEdited
	Anchor      *Anchor      // CommentAdded: the text an inline comment is on (ADR-0047)
	Reply       ID           // CommentAdded: the comment a reply answers (ADR-0047)
	Labels      []string     // LabelsSet: the item's labels after the event
	Attachment  *Attachment  // AttachmentAdded; AttachmentDeleted: only its ID

	// Erased is set on an event whose content (title, properties, body, raw
	// source) was removed because its item was purged or redacted. The
	// event stays in the history, so who changed what and when remains
	// visible; what the content was does not (ADR-0026).
	Erased bool
}

// HasContent reports whether the event carries user content of its item:
// the fields that purge and redaction must make unrecoverable (ADR-0026),
// comment text included (ADR-0035).
func (e Event) HasContent() bool {
	switch e.Kind {
	case ItemCreated, TitleSet, PropertySet, BodyCommitted, ItemImported, CommentAdded, CommentEdited, LabelsSet, AttachmentAdded:
		return true
	}
	return false
}

// erase removes an event's content. Applying an erased event yields an item
// without content; the purge that follows clears the item anyway, so the
// final state is the same as with the original event.
func (e *Event) erase() {
	if !e.HasContent() {
		return
	}
	e.Title, e.Props, e.Value, e.Text, e.Labels, e.Anchor = "", nil, nil, "", nil, nil
	if e.Kind == AttachmentAdded {
		e.Attachment = nil
	}
	if e.Kind == BodyCommitted {
		e.Body = &Doc{}
	}
	if e.Origin != nil {
		o := *e.Origin
		o.Raw = ""
		e.Origin = &o
	}
	e.Erased = true
}

func (it *Item) clone() Item {
	c := *it
	c.Properties = maps.Clone(it.Properties)
	c.Body = it.Body.clone()
	c.Comments = slices.Clone(it.Comments)
	c.Labels = slices.Clone(it.Labels)
	c.Attachments = slices.Clone(it.Attachments)
	if it.Origin != nil {
		o := *it.Origin
		c.Origin = &o
	}
	return c
}

func (d Doc) clone() Doc {
	blocks := make([]Block, len(d.Blocks))
	for i, b := range d.Blocks {
		b.Mentions = slices.Clone(b.Mentions)
		b.Marks = slices.Clone(b.Marks)
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
