package item

import (
	"slices"
	"strings"
	"time"
)

// Comment is one comment on an item (ADR-0035). It is part of its item:
// readable when the item is, erased with it.
type Comment struct {
	ID      ID
	Author  string
	Created time.Time
	// Edited is when the text last changed, or zero.
	Edited time.Time
	// Text is plain text; it is empty once the comment is deleted.
	Text    string
	Deleted bool
	// Anchor is the text of the body an inline comment is on; nil for a
	// comment on the whole item (ADR-0047).
	Anchor *Anchor
	// Reply is the comment this one answers; empty for the first comment
	// of a thread. A thread is one level deep.
	Reply ID
	// Resolved is whether the thread was marked done, by ResolvedBy at
	// ResolvedAt; only the first comment of a thread is resolved.
	Resolved   bool
	ResolvedBy string
	ResolvedAt time.Time
}

// Anchor names a passage of a body by its text (ADR-0047): the quoted
// text and a little of what precedes and follows it, which tells apart
// passages of the same text. Its position is found again when the body is
// shown; when the quoted text is gone, the comment is no longer on any
// passage, and it says so.
type Anchor struct {
	Quote  string
	Prefix string `json:",omitempty"`
	Suffix string `json:",omitempty"`
}

// NewComment is a comment to add: on the whole item, on a passage of its
// body (Anchor) or in answer to another comment (Reply).
type NewComment struct {
	Text   string
	Anchor *Anchor
	Reply  ID
}

// AddComment adds a comment to a live item and returns it.
func (s *Store) AddComment(actor string, id ID, text string) (Comment, error) {
	return s.Comment(actor, id, NewComment{Text: text})
}

// Comment adds a comment, an inline comment or a reply to a live item and
// returns it.
func (s *Store) Comment(actor string, id ID, n NewComment) (Comment, error) {
	it, err := s.live(id)
	if err != nil {
		return Comment{}, err
	}
	if strings.TrimSpace(n.Text) == "" {
		return Comment{}, invalid("comment is empty")
	}
	if n.Anchor != nil && n.Reply != "" {
		return Comment{}, invalid("a reply is on its thread's passage, not on its own")
	}
	if n.Anchor != nil {
		if td, _ := s.latest(it.Type); !td.Body {
			return Comment{}, invalid("type %q has no body to comment on", it.Type)
		}
		a := *n.Anchor
		if strings.TrimSpace(a.Quote) == "" {
			return Comment{}, invalid("an inline comment needs the text it is on")
		}
		n.Anchor = &a
	}
	if n.Reply != "" {
		root, err := s.liveComment(id, n.Reply)
		if err != nil {
			return Comment{}, err
		}
		if root.Reply != "" {
			// Answering a reply answers its thread.
			if _, err := s.liveComment(id, root.Reply); err != nil {
				return Comment{}, err
			}
			n.Reply = root.Reply
		}
	}
	cid := s.newID()
	if err := s.record(actor, Event{Kind: CommentAdded, Item: id, Comment: cid, Text: n.Text, Anchor: n.Anchor, Reply: n.Reply}); err != nil {
		return Comment{}, err
	}
	c, _ := s.items[id].comment(cid)
	return *c, nil
}

// ResolveComment marks a thread done, or open again.
func (s *Store) ResolveComment(actor string, id, comment ID, resolved bool) error {
	c, err := s.liveComment(id, comment)
	if err != nil {
		return err
	}
	if c.Reply != "" {
		return invalid("only a thread is resolved, not a reply in it")
	}
	if c.Resolved == resolved {
		return nil
	}
	kind := CommentResolved
	if !resolved {
		kind = CommentReopened
	}
	return s.record(actor, Event{Kind: kind, Item: id, Comment: comment})
}

// EditComment replaces the text of a comment that is not deleted.
func (s *Store) EditComment(actor string, id, comment ID, text string) error {
	if _, err := s.liveComment(id, comment); err != nil {
		return err
	}
	if strings.TrimSpace(text) == "" {
		return invalid("comment is empty")
	}
	return s.record(actor, Event{Kind: CommentEdited, Item: id, Comment: comment, Text: text})
}

// DeleteComment hides a comment's text from every reader. The text stays in
// the encrypted log until the item is erased (ADR-0035).
func (s *Store) DeleteComment(actor string, id, comment ID) error {
	if _, err := s.liveComment(id, comment); err != nil {
		return err
	}
	return s.record(actor, Event{Kind: CommentDeleted, Item: id, Comment: comment})
}

// liveComment returns a comment that is not deleted, on a live item.
func (s *Store) liveComment(id, comment ID) (*Comment, error) {
	it, err := s.live(id)
	if err != nil {
		return nil, err
	}
	c, ok := it.comment(comment)
	if !ok {
		return nil, notFound("comment %s", comment)
	}
	if c.Deleted {
		return nil, invalid("comment %s is deleted", comment)
	}
	return c, nil
}

func (it *Item) comment(id ID) (*Comment, bool) {
	for i := range it.Comments {
		if it.Comments[i].ID == id {
			return &it.Comments[i], true
		}
	}
	return nil, false
}

// applyComment applies a comment event. Comments leave the item's Version
// alone: they are not a change of the item itself.
func (s *Store) applyComment(e Event, touch func(...ID)) {
	it := s.items[e.Item]
	touch(it.ID)
	switch e.Kind {
	case CommentAdded:
		it.Comments = append(it.Comments, Comment{ID: e.Comment, Author: e.Actor, Created: e.At, Text: e.Text, Anchor: e.Anchor, Reply: e.Reply})
	case CommentEdited:
		if c, ok := it.comment(e.Comment); ok {
			c.Text, c.Edited = e.Text, e.At
		}
	case CommentDeleted:
		if c, ok := it.comment(e.Comment); ok {
			c.Text, c.Anchor, c.Deleted = "", nil, true
		}
	case CommentResolved:
		if c, ok := it.comment(e.Comment); ok {
			c.Resolved, c.ResolvedBy, c.ResolvedAt = true, e.Actor, e.At
		}
	case CommentReopened:
		if c, ok := it.comment(e.Comment); ok {
			c.Resolved, c.ResolvedBy, c.ResolvedAt = false, "", time.Time{}
		}
	}
}

// --- sessions ---------------------------------------------------------------

// Comments returns the comments of a readable item.
func (x *Session) Comments(id ID) ([]Comment, error) {
	it, err := x.readable(id)
	if err != nil {
		return nil, err
	}
	out := slices.Clone(it.Comments)
	for i := range out {
		out[i].Anchor = x.veilAnchor(it, out[i].Anchor)
	}
	return out, nil
}

// AddComment comments on an item the principal may edit.
func (x *Session) AddComment(id ID, text string) (Comment, error) {
	return x.Comment(id, NewComment{Text: text})
}

// Comment adds a comment, an inline comment or a reply to an item the
// principal may edit.
func (x *Session) Comment(id ID, n NewComment) (Comment, error) {
	if _, err := x.editable(id); err != nil {
		return Comment{}, err
	}
	return x.s.Comment(x.who, id, n)
}

// ResolveComment marks a thread on an item the principal may edit done, or
// open again.
func (x *Session) ResolveComment(id, comment ID, resolved bool) error {
	if _, err := x.editable(id); err != nil {
		return err
	}
	return x.s.ResolveComment(x.who, id, comment, resolved)
}

// EditComment edits one's own comment on an item one may still edit.
func (x *Session) EditComment(id, comment ID, text string) error {
	if _, err := x.editable(id); err != nil {
		return err
	}
	c, err := x.s.liveComment(id, comment)
	if err != nil {
		return err
	}
	if c.Author != x.who {
		return forbidden("%s may only edit their own comments", x.who)
	}
	return x.s.EditComment(x.who, id, comment, text)
}

// DeleteComment deletes one's own comment on an item one may still edit;
// a space admin deletes any comment.
func (x *Session) DeleteComment(id, comment ID) error {
	it, err := x.readable(id)
	if err != nil {
		return err
	}
	if x.s.spaceRole(x.who, it.Space) < SpaceAdmin {
		if _, err := x.editable(id); err != nil {
			return err
		}
		c, err := x.s.liveComment(id, comment)
		if err != nil {
			return err
		}
		if c.Author != x.who {
			return forbidden("%s may only delete their own comments", x.who)
		}
	}
	return x.s.DeleteComment(x.who, id, comment)
}
