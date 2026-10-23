package server

import (
	"net/http"

	"github.com/dbuchs7/wikiwikibangbang/internal/item"
	"github.com/dbuchs7/wikiwikibangbang/internal/notes"
)

// noteRoutes are personal notes on passages of items (ADR-0107): each
// person reads and changes only their own, on items they may read.
func noteRoutes(h itemHandlers) []Route {
	return []Route{
		{Method: http.MethodGet, Pattern: "/api/notes", Summary: "One's own personal notes on every item one may still read, oldest first, with the item's key and title", Handler: http.HandlerFunc(h.myNotes)},
		{Method: http.MethodGet, Pattern: "/api/items/{ref}/notes", Summary: "One's own personal notes on the item; no one else's are ever shown", Handler: http.HandlerFunc(h.itemNotes)},
		{Method: http.MethodPost, Pattern: "/api/items/{ref}/notes", Summary: "Make a personal note on a passage of the item (anchor: quote, prefix, suffix; text), seen by no one else", Handler: http.HandlerFunc(h.addNote)},
		{Method: http.MethodPut, Pattern: "/api/items/{ref}/notes/{id}", Summary: "Change the text of one's own note", Handler: http.HandlerFunc(h.editNote)},
		{Method: http.MethodDelete, Pattern: "/api/items/{ref}/notes/{id}", Summary: "Delete one's own note", Handler: http.HandlerFunc(h.removeNote)},
	}
}

func (h itemHandlers) itemNotes(w http.ResponseWriter, r *http.Request) {
	h.read(w, r, func(x *item.Session) (any, error) {
		it, err := resolveItem(x, r.PathValue("ref"))
		if err != nil {
			return nil, err
		}
		return map[string]any{"notes": h.d.DB.Notes.Of(who(r), it.ID)}, nil
	})
}

func (h itemHandlers) myNotes(w http.ResponseWriter, r *http.Request) {
	type entry struct {
		notes.Note
		Key   string `json:"key"`
		Title string `json:"title"`
	}
	h.read(w, r, func(x *item.Session) (any, error) {
		out := []entry{}
		for _, n := range h.d.DB.Notes.Of(who(r), "") {
			// Only what one may still read: access taken away hides one's
			// notes on it, too.
			it, err := x.Get(n.Item)
			if err != nil || it.Presence != item.Live {
				continue
			}
			out = append(out, entry{n, it.Key, it.Title})
		}
		return map[string]any{"notes": out}, nil
	})
}

// noteChange runs fn as the single writer on an item the caller may read:
// notes are sealed with the item's key.
func (h itemHandlers) noteChange(w http.ResponseWriter, r *http.Request, status int, fn func(it item.Item) (any, error)) {
	h.write(w, r, status, func(x *item.Session, _ *item.Store) (any, error) {
		it, err := resolveItem(x, r.PathValue("ref"))
		if err != nil {
			return nil, err
		}
		return fn(it)
	})
}

func (h itemHandlers) addNote(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Anchor notes.Anchor `json:"anchor"`
		Text   string       `json:"text"`
	}
	if !decode(w, r, &req) {
		return
	}
	h.noteChange(w, r, http.StatusCreated, func(it item.Item) (any, error) {
		if it.Presence != item.Live {
			return nil, badInput("%s is %s", it.Key, it.Presence)
		}
		n, err := h.d.DB.Notes.Add(who(r), it.ID, req.Anchor, req.Text)
		return map[string]notes.Note{"note": n}, err
	})
}

func (h itemHandlers) editNote(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Text string `json:"text"`
	}
	if !decode(w, r, &req) {
		return
	}
	h.noteChange(w, r, http.StatusOK, func(it item.Item) (any, error) {
		if n, ok := h.d.DB.Notes.Get(who(r), r.PathValue("id")); !ok || n.Item != it.ID {
			return nil, notes.ErrNotFound
		}
		n, err := h.d.DB.Notes.Edit(who(r), r.PathValue("id"), req.Text)
		return map[string]notes.Note{"note": n}, err
	})
}

func (h itemHandlers) removeNote(w http.ResponseWriter, r *http.Request) {
	h.noteChange(w, r, http.StatusOK, func(it item.Item) (any, error) {
		if n, ok := h.d.DB.Notes.Get(who(r), r.PathValue("id")); !ok || n.Item != it.ID {
			return nil, notes.ErrNotFound
		}
		return nil, h.d.DB.Notes.Remove(who(r), r.PathValue("id"))
	})
}
