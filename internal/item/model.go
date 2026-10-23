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
	// Divider is a horizontal rule between blocks; it holds nothing
	// (ADR-0059).
	Divider BlockKind = "divider"
	// Image shows an image attachment of the item; Ref names the
	// attachment and Text is its caption (ADR-0045).
	Image BlockKind = "image"
	// Video plays a video attachment of the item, like an image shows a
	// picture: Ref names it and Text is its caption (ADR-0097).
	Video BlockKind = "video"
)

// VideoTypes are the formats of a video a body plays, as read from the
// file's bytes; browsers play them without a plug-in (ADR-0097).
var VideoTypes = []string{"video/mp4", "video/webm"}

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
	// Underline, strike-through and highlight (ADR-0059).
	MarkUnderline MarkType = "underline"
	MarkStrike    MarkType = "strike"
	MarkHighlight MarkType = "highlight"
	// MarkSecret protects its text (ADR-0088): Ref names the item's Secret
	// that says who reads it in clear; others read a placeholder.
	MarkSecret MarkType = "secret"
)

// TextAligns are the alignments of a block of text, ImageAligns those of
// an image: left and right float it with the text flowing beside it
// (ADR-0059). Empty is the default: left for text, centred for images.
var (
	TextAligns  = []string{"center", "right", "justify"}
	ImageAligns = []string{"left", "center", "right"}
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
	// Width is an image's width in percent of the text column, 10 to 100;
	// 0 shows it at its own size, at most the column (ADR-0056).
	Width int `json:",omitempty"`
	// Align is the alignment of a paragraph, heading, quote or panel (one of
	// TextAligns) or of an image (one of ImageAligns); empty is the default
	// (ADR-0059).
	Align string `json:",omitempty"`
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
	// Hold is set while the item waits for a data protection review; only
	// those who may edit it read it then (ADR-0058).
	Hold *Hold
	// Cleared are the findings a review judged false alarms; they do not
	// hold the item again.
	Cleared []string
	// Secrets are the protected passages of the body, in the order they
	// were protected (ADR-0088).
	Secrets []Secret
	// ErasedSecrets are the passages erased for good, whose keys are
	// destroyed (ADR-0089); the text of earlier versions shows them veiled.
	ErasedSecrets []ID

	created int // sequence number of the creating event; orders items
	// erasedBefore withholds the versions of the body before it: they may
	// hold an erased passage's text from before it was protected
	// (ADR-0089).
	erasedBefore int
	trashedBy    int // sequence number of the event that trashed it, or 0
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
	TypeDefined EventKind = "TypeDefined"
	// TypeRemoved takes the type TypeName out of use (ADR-0094).
	TypeRemoved       EventKind = "TypeRemoved"
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
	// AttachmentLabelled sets the labels of a picture (ADR-0090).
	AttachmentLabelled EventKind = "AttachmentLabelled"
	Watched            EventKind = "Watched"
	Unwatched          EventKind = "Unwatched"
	CommentDeleted     EventKind = "CommentDeleted"
	CommentResolved    EventKind = "CommentResolved"
	CommentReopened    EventKind = "CommentReopened"
	// Taking access back (ADR-0048).
	RoleRevoked        EventKind = "RoleRevoked"
	GroupMemberRemoved EventKind = "GroupMemberRemoved"
	SystemRoleRevoked  EventKind = "SystemRoleRevoked"
	// HoldSet puts an item on hold for a data protection review, HoldReleased
	// takes it off (ADR-0058).
	HoldSet      EventKind = "HoldSet"
	HoldReleased EventKind = "HoldReleased"
	// KanbanSet makes or renames a kanban of a space, KanbanRemoved removes
	// it (ADR-0060).
	KanbanSet     EventKind = "KanbanSet"
	KanbanRemoved EventKind = "KanbanRemoved"
	// KanbanCardPlaced puts a card in a column of a kanban, at a place
	// among the column's cards (ADR-0076): Kanban names the kanban, Field
	// the column, To the card's state then, Order the column's cards.
	KanbanCardPlaced EventKind = "KanbanCardPlaced"
	// SpaceTrashed puts a space with everything in it out of sight,
	// SpaceRestored brings it back, SpacePurged turns its items into
	// tombstones and keeps only its key (ADR-0069).
	SpaceTrashed  EventKind = "SpaceTrashed"
	SpaceRestored EventKind = "SpaceRestored"
	// SpaceRenamed changes a space's display name; its key stays.
	SpaceRenamed EventKind = "SpaceRenamed"
	SpacePurged  EventKind = "SpacePurged"
	// Protected passages of a body (ADR-0088). SecretSet protects a
	// passage or changes who reads it; SecretRemoved takes the protection
	// off; SecretRequested asks to read it (Principal), SecretGranted lets
	// the one who asked read it, SecretDeclined says no. SecretErased
	// erases a passage for good by destroying its key (ADR-0089).
	SecretSet       EventKind = "SecretSet"
	SecretRemoved   EventKind = "SecretRemoved"
	SecretRequested EventKind = "SecretRequested"
	SecretGranted   EventKind = "SecretGranted"
	SecretDeclined  EventKind = "SecretDeclined"
	SecretErased    EventKind = "SecretErased"
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
	Space       string   // SpaceCreated, ItemCreated, ItemMoved, SpaceTrashed, SpaceRestored, SpacePurged
	SpaceName   string   // SpaceCreated, SpaceRenamed
	TypeName    string   // ItemCreated, TypeRemoved
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
	Members    []ID // ItemTrashed, ItemRestored, ItemPurged, SpacePurged: every item affected; SecretErased: the comments whose anchor quoted the passage

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
	Hold        *Hold        // HoldSet: the hold as it is after the event
	Outcome     string       // HoldReleased: HoldRestricted or HoldFalseAlarm; Text holds the reason
	Kanban      *Kanban      // KanbanSet; KanbanRemoved and KanbanCardPlaced: only its ID
	Secret      *Secret      // SecretSet; the other secret kinds: only its ID

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
	case ItemCreated, TitleSet, PropertySet, BodyCommitted, ItemImported, CommentAdded, CommentEdited, LabelsSet, AttachmentAdded,
		AttachmentLabelled, HoldSet, HoldReleased:
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
	if e.Kind == HoldSet && e.Hold != nil {
		e.Hold = &Hold{Savers: e.Hold.Savers}
	}
	if e.Kind == AttachmentAdded || e.Kind == AttachmentLabelled {
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
	c.ErasedSecrets = slices.Clone(it.ErasedSecrets)
	c.Secrets = make([]Secret, len(it.Secrets))
	for i, sec := range it.Secrets {
		c.Secrets[i] = sec.clone()
	}
	if it.Secrets == nil {
		c.Secrets = nil
	}
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
