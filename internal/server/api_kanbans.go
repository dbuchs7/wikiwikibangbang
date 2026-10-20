package server

import (
	"net/http"
	"slices"

	"github.com/dbuchs7/wikiwikibangbang/internal/item"
)

// kanbanRoutes are the kanbans of the spaces (ADR-0060): named boards on a
// topic, whose cards are the items of a type with the kanban's label. Cards
// join and leave a kanban through their labels (PUT …/labels).
func kanbanRoutes(h itemHandlers) []Route {
	return []Route{
		{Method: http.MethodGet, Pattern: "/api/kanbans", Summary: "The kanbans of the spaces one holds a role in (space narrows them)", Handler: http.HandlerFunc(h.kanbans)},
		{Method: http.MethodPost, Pattern: "/api/spaces/{space}/kanbans", Summary: "Make a kanban: a name, a type with a workflow and, optionally, the label of its cards (editors)", Handler: http.HandlerFunc(h.createKanban)},
		{Method: http.MethodPut, Pattern: "/api/spaces/{space}/kanbans/{id}", Summary: "Rename a kanban (editors)", Handler: http.HandlerFunc(h.renameKanban)},
		{Method: http.MethodDelete, Pattern: "/api/spaces/{space}/kanbans/{id}", Summary: "Remove a kanban; its cards keep their label (editors)", Handler: http.HandlerFunc(h.removeKanban)},
	}
}

type kanbanJSON struct {
	ID    item.ID `json:"id"`
	Space string  `json:"space"`
	Name  string  `json:"name"`
	Type  string  `json:"type"`
	Label string  `json:"label"`
}

func toKanbanJSON(space string, k item.Kanban) kanbanJSON {
	return kanbanJSON{ID: k.ID, Space: space, Name: k.Name, Type: k.Type, Label: k.Label}
}

func (h itemHandlers) kanbans(w http.ResponseWriter, r *http.Request) {
	h.read(w, r, func(x *item.Session) (any, error) {
		all := x.Kanbans(r.URL.Query().Get("space"))
		out := []kanbanJSON{}
		spaces := make([]string, 0, len(all))
		for sp := range all {
			spaces = append(spaces, sp)
		}
		slices.Sort(spaces)
		for _, sp := range spaces {
			for _, k := range all[sp] {
				out = append(out, toKanbanJSON(sp, k))
			}
		}
		return map[string]any{"kanbans": out}, nil
	})
}

func (h itemHandlers) createKanban(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name  string `json:"name"`
		Type  string `json:"type"`
		Label string `json:"label"`
	}
	if !decode(w, r, &req) {
		return
	}
	space := r.PathValue("space")
	h.write(w, r, http.StatusCreated, func(x *item.Session, _ *item.Store) (any, error) {
		k, err := x.SetKanban(space, item.Kanban{Name: req.Name, Type: req.Type, Label: req.Label})
		return map[string]kanbanJSON{"kanban": toKanbanJSON(space, k)}, err
	})
}

func (h itemHandlers) renameKanban(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if !decode(w, r, &req) {
		return
	}
	space := r.PathValue("space")
	h.write(w, r, http.StatusOK, func(x *item.Session, _ *item.Store) (any, error) {
		k, err := x.SetKanban(space, item.Kanban{ID: item.ID(r.PathValue("id")), Name: req.Name})
		return map[string]kanbanJSON{"kanban": toKanbanJSON(space, k)}, err
	})
}

func (h itemHandlers) removeKanban(w http.ResponseWriter, r *http.Request) {
	space := r.PathValue("space")
	h.write(w, r, http.StatusNoContent, func(x *item.Session, _ *item.Store) (any, error) {
		return nil, x.RemoveKanban(space, item.ID(r.PathValue("id")))
	})
}
