package server

import (
	"net/http"
	"time"

	"github.com/dbuchs7/wikiwikibangbang/internal/item"
)

// commentRoutes are an item's comments (ADR-0035). Reading the item is
// reading its comments; the item model decides who may write.
func commentRoutes(h itemHandlers) []Route {
	return []Route{
		{Method: http.MethodGet, Pattern: "/api/items/{ref}/comments", Summary: "The item's comments, oldest first, paged", Handler: http.HandlerFunc(h.listComments)},
		{Method: http.MethodPost, Pattern: "/api/items/{ref}/comments", Summary: "Comment on the item", Handler: http.HandlerFunc(h.addComment)},
		{Method: http.MethodPut, Pattern: "/api/items/{ref}/comments/{id}", Summary: "Edit one's own comment", Handler: http.HandlerFunc(h.editComment)},
		{Method: http.MethodDelete, Pattern: "/api/items/{ref}/comments/{id}", Summary: "Delete one's own comment, or any as a space administrator", Handler: http.HandlerFunc(h.deleteComment)},
	}
}

// commentJSON is a comment; a deleted one keeps its place without text.
type commentJSON struct {
	ID      item.ID    `json:"id"`
	Author  string     `json:"author"`
	Created time.Time  `json:"created"`
	Edited  *time.Time `json:"edited,omitempty"`
	Text    string     `json:"text,omitempty"`
	Deleted bool       `json:"deleted,omitempty"`
}

func toCommentJSON(c item.Comment) commentJSON {
	j := commentJSON{ID: c.ID, Author: c.Author, Created: c.Created, Text: c.Text, Deleted: c.Deleted}
	if !c.Edited.IsZero() {
		j.Edited = &c.Edited
	}
	return j
}

type commentTextJSON struct {
	Text string `json:"text"`
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
	var req commentTextJSON
	if !decode(w, r, &req) {
		return
	}
	h.write(w, r, http.StatusCreated, func(x *item.Session, _ *item.Store) (any, error) {
		it, err := resolveItem(x, r.PathValue("ref"))
		if err != nil {
			return nil, err
		}
		c, err := x.AddComment(it.ID, req.Text)
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
