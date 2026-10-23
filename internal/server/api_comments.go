package server

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/dbuchs7/wikiwikibangbang/internal/auth"
	"github.com/dbuchs7/wikiwikibangbang/internal/item"
)

// commentRoutes are an item's comments (ADR-0035). Reading the item is
// reading its comments; the item model decides who may write.
func commentRoutes(h itemHandlers) []Route {
	return []Route{
		{Method: http.MethodGet, Pattern: "/api/comments", Summary: "Comments on what one watches, one's own and those mentioning one by @username, newest first; only narrows to watched, mine or mentions; filtered by q and space; paged by offset (ADR-0063)", Handler: http.HandlerFunc(h.commentFeed)},
		{Method: http.MethodGet, Pattern: "/api/items/{ref}/comments", Summary: "The item's comments, oldest first, paged", Handler: http.HandlerFunc(h.listComments)},
		{Method: http.MethodPost, Pattern: "/api/items/{ref}/comments", Summary: "Comment on the item", Handler: http.HandlerFunc(h.addComment)},
		{Method: http.MethodPut, Pattern: "/api/items/{ref}/comments/{id}", Summary: "Edit one's own comment", Handler: http.HandlerFunc(h.editComment)},
		{Method: http.MethodDelete, Pattern: "/api/items/{ref}/comments/{id}", Summary: "Delete one's own comment, or any as a space administrator", Handler: http.HandlerFunc(h.deleteComment)},
		{Method: http.MethodPut, Pattern: "/api/items/{ref}/comments/{id}/resolved", Summary: "Mark a comment thread done (ADR-0047)", Handler: h.resolveComment(true)},
		{Method: http.MethodDelete, Pattern: "/api/items/{ref}/comments/{id}/resolved", Summary: "Open a comment thread again (ADR-0047)", Handler: h.resolveComment(false)},
	}
}

// commentJSON is a comment; a deleted one keeps its place without text.
// An inline comment names its passage (anchor), a reply the first comment
// of its thread (ADR-0047).
type commentJSON struct {
	ID         item.ID     `json:"id"`
	Author     string      `json:"author"`
	Created    time.Time   `json:"created"`
	Edited     *time.Time  `json:"edited,omitempty"`
	Text       string      `json:"text,omitempty"`
	Deleted    bool        `json:"deleted,omitempty"`
	Anchor     *anchorJSON `json:"anchor,omitempty"`
	Reply      item.ID     `json:"reply,omitempty"`
	Resolved   bool        `json:"resolved,omitempty"`
	ResolvedBy string      `json:"resolvedBy,omitempty"`
	ResolvedAt *time.Time  `json:"resolvedAt,omitempty"`
}

type anchorJSON struct {
	Quote  string `json:"quote"`
	Prefix string `json:"prefix,omitempty"`
	Suffix string `json:"suffix,omitempty"`
}

func toCommentJSON(c item.Comment) commentJSON {
	j := commentJSON{ID: c.ID, Author: c.Author, Created: c.Created, Text: c.Text, Deleted: c.Deleted,
		Reply: c.Reply, Resolved: c.Resolved, ResolvedBy: c.ResolvedBy}
	if !c.Edited.IsZero() {
		j.Edited = &c.Edited
	}
	if c.Anchor != nil {
		j.Anchor = &anchorJSON{Quote: c.Anchor.Quote, Prefix: c.Anchor.Prefix, Suffix: c.Anchor.Suffix}
	}
	if !c.ResolvedAt.IsZero() {
		j.ResolvedAt = &c.ResolvedAt
	}
	return j
}

type commentTextJSON struct {
	Text string `json:"text"`
}

type newCommentJSON struct {
	Text   string      `json:"text"`
	Anchor *anchorJSON `json:"anchor"`
	Reply  item.ID     `json:"reply"`
}

func (h itemHandlers) listComments(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, ok := parseLimit(w, q)
	if !ok {
		return
	}
	h.read(w, r, func(x *item.Session) (any, error) {
		it, err := resolveItem(x, r.PathValue("ref"))
		if err != nil {
			return nil, err
		}
		all, err := x.Comments(it.ID)
		if err != nil {
			return nil, err
		}
		page, res, err := paged(all, func(c item.Comment) item.ID { return c.ID }, item.ID(q.Get("after")), limit)
		if err != nil {
			return nil, err
		}
		out := make([]commentJSON, len(page))
		for i, c := range page {
			out[i] = toCommentJSON(c)
		}
		res["comments"] = out
		return res, nil
	})
}

func (h itemHandlers) addComment(w http.ResponseWriter, r *http.Request) {
	var req newCommentJSON
	if !decode(w, r, &req) {
		return
	}
	n := item.NewComment{Text: req.Text, Reply: req.Reply}
	if req.Anchor != nil {
		n.Anchor = &item.Anchor{Quote: req.Anchor.Quote, Prefix: req.Anchor.Prefix, Suffix: req.Anchor.Suffix}
	}
	h.write(w, r, http.StatusCreated, func(x *item.Session, _ *item.Store) (any, error) {
		it, err := resolveItem(x, r.PathValue("ref"))
		if err != nil {
			return nil, err
		}
		c, err := x.Comment(it.ID, n)
		if err != nil {
			return nil, err
		}
		return map[string]commentJSON{"comment": toCommentJSON(c)}, nil
	})
}

func (h itemHandlers) editComment(w http.ResponseWriter, r *http.Request) {
	var req commentTextJSON
	if !decode(w, r, &req) {
		return
	}
	h.write(w, r, http.StatusOK, func(x *item.Session, _ *item.Store) (any, error) {
		it, err := resolveItem(x, r.PathValue("ref"))
		if err != nil {
			return nil, err
		}
		id := item.ID(r.PathValue("id"))
		if err := x.EditComment(it.ID, id, req.Text); err != nil {
			return nil, err
		}
		comments, err := x.Comments(it.ID)
		for _, c := range comments {
			if c.ID == id {
				return map[string]commentJSON{"comment": toCommentJSON(c)}, err
			}
		}
		return nil, err
	})
}

func (h itemHandlers) deleteComment(w http.ResponseWriter, r *http.Request) {
	h.write(w, r, 0, func(x *item.Session, _ *item.Store) (any, error) {
		it, err := resolveItem(x, r.PathValue("ref"))
		if err != nil {
			return nil, err
		}
		return nil, x.DeleteComment(it.ID, item.ID(r.PathValue("id")))
	})
}

// resolveComment marks a thread done, or open again.
func (h itemHandlers) resolveComment(resolved bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		h.write(w, r, 0, func(x *item.Session, _ *item.Store) (any, error) {
			it, err := resolveItem(x, r.PathValue("ref"))
			if err != nil {
				return nil, err
			}
			return nil, x.ResolveComment(it.ID, item.ID(r.PathValue("id")), resolved)
		})
	}
}

type feedEntryJSON struct {
	Item    item.ID     `json:"item"`
	Key     string      `json:"key"`
	Title   string      `json:"title"`
	Space   string      `json:"space"`
	Comment commentJSON `json:"comment"`
	Why     []string    `json:"why"`
}

// commentFeed is the comment overview (ADR-0063).
func (h itemHandlers) commentFeed(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, ok := parseLimit(w, q)
	if !ok {
		return
	}
	only := q.Get("only")
	switch only {
	case "", item.FeedWatched, item.FeedMine, item.FeedMentions:
	default:
		writeError(w, http.StatusBadRequest, "only is watched, mine or mentions")
		return
	}
	offset := 0
	if v := q.Get("offset"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			writeError(w, http.StatusBadRequest, "offset must be a number from 0")
			return
		}
		offset = n
	}
	p, _ := PrincipalFrom(r.Context())
	h.read(w, r, func(x *item.Session) (any, error) {
		// The list's filter (ADR-0098): words of the comment, its item or
		// its author, and a space.
		text, space := q.Get("q"), q.Get("space")
		var keep func(item.FeedEntry) bool
		if strings.TrimSpace(text) != "" || space != "" {
			keep = func(e item.FeedEntry) bool {
				if space != "" && e.Space != space {
					return false
				}
				author := h.d.DB.Accounts.Usernames([]auth.UserID{auth.UserID(e.Comment.Author)})[auth.UserID(e.Comment.Author)]
				return item.HasWords(e.Key+" "+e.Title+" "+e.Comment.Text+" "+author, text)
			}
		}
		list, more := x.CommentFeedWhere(p.Username, only, offset, limit, keep)
		out := []feedEntryJSON{}
		for _, e := range list {
			out = append(out, feedEntryJSON{Item: e.Item, Key: e.Key, Title: e.Title, Space: e.Space, Comment: toCommentJSON(e.Comment), Why: e.Why})
		}
		res := map[string]any{"comments": out}
		if more {
			res["next"] = offset + len(list)
		}
		return res, nil
	})
}
