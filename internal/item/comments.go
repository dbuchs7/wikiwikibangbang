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
}

// AddComment adds a comment to a live item and returns it.
func (s *Store) AddComment(actor string, id ID, text string) (Comment, error) {
	if _, err := s.live(id); err != nil {
		return Comment{}, err
	}
	if strings.TrimSpace(text) == "" {
		return Comment{}, invalid("comment is empty")
	}
	cid := s.newID()
	if err := s.record(actor, Event{Kind: CommentAdded, Item: id, Comment: cid, Text: text}); err != nil {
		return Comment{}, err
	}
	c, _ := s.items[id].comment(cid)
	return *c, nil
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
		it.Comments = append(it.Comments, Comment{ID: e.Comment, Author: e.Actor, Created: e.At, Text: e.Text})
	case CommentEdited:
		if c, ok := it.comment(e.Comment); ok {
			c.Text, c.Edited = e.Text, e.At
		}
	case CommentDeleted:
		if c, ok := it.comment(e.Comment); ok {
			c.Text, c.Deleted = "", true
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
	return slices.Clone(it.Comments), nil
}

// AddComment comments on an item the principal may edit.
func (x *Session) AddComment(id ID, text string) (Comment, error) {
	if _, err := x.editable(id); err != nil {
		return Comment{}, err
	}
	return x.s.AddComment(x.who, id, text)
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
