package server

import (
	"net/http"
	"slices"

	"github.com/dbuchs7/wikiwikibangbang/internal/item"
	"github.com/dbuchs7/wikiwikibangbang/internal/site"
)

// kanbanRoutes are the kanbans of the spaces (ADR-0060): named boards on a
// topic, whose cards are the items of a type with the kanban's label. Cards
// join and leave a kanban through their labels (PUT …/labels).
func kanbanRoutes(h itemHandlers) []Route {
	return []Route{
		{Method: http.MethodGet, Pattern: "/api/kanbans", Summary: "The kanbans of the spaces one holds a role in (space narrows them)", Handler: http.HandlerFunc(h.kanbans)},
		{Method: http.MethodPost, Pattern: "/api/spaces/{space}/kanbans", Summary: "Make a kanban: a name, a type with a workflow and, optionally, the label of its cards (editors)", Handler: http.HandlerFunc(h.createKanban)},
		{Method: http.MethodPut, Pattern: "/api/spaces/{space}/kanbans/{id}", Summary: "Change a kanban: its name, its columns (states renamed and ordered, columns of its own) and the fields its cards show (editors)", Handler: http.HandlerFunc(h.renameKanban)},
		{Method: http.MethodGet, Pattern: "/api/spaces/{space}/kanbans/{id}/board", Summary: "A kanban's columns with the cards one may read, in their order (ADR-0076)", Handler: http.HandlerFunc(h.kanbanBoard)},
		{Method: http.MethodPost, Pattern: "/api/spaces/{space}/kanbans/{id}/place", Summary: "Put a card in a column, before another card or last; a column of a state performs the transition to it (who may edit the card)", Handler: http.HandlerFunc(h.placeCard)},
		{Method: http.MethodPost, Pattern: "/api/types/{name}/transitions", Summary: "Add a transition from one state to another to a type's workflow, so a card can move there on a kanban (administrators)", Handler: http.HandlerFunc(h.addTransition)},
		{Method: http.MethodDelete, Pattern: "/api/spaces/{space}/kanbans/{id}", Summary: "Remove a kanban; its cards keep their label (editors)", Handler: http.HandlerFunc(h.removeKanban)},
	}
}

type kanbanJSON struct {
	ID    item.ID `json:"id"`
	Space string  `json:"space"`
	Name  string  `json:"name"`
	Type  string  `json:"type"`
	Label string  `json:"label"`
	// Columns are the kanban's own columns, empty for the workflow's
	// states; CardFields what its cards show (ADR-0076).
	Columns    []columnJSON `json:"columns"`
	CardFields []string     `json:"cardFields"`
	// ColorField names the enumeration field whose value colours a card,
	// and Colors the tones of its values that do not take their place's
	// (ADR-0133).
	ColorField string            `json:"colorField,omitempty"`
	Colors     map[string]string `json:"colors,omitempty"`
	// Icon names the content of the kanban's picture, empty for none
	// (ADR-0087).
	Icon string `json:"icon,omitempty"`
}

type columnJSON struct {
	ID    item.ID `json:"id,omitempty"`
	Name  string  `json:"name"`
	State string  `json:"state,omitempty"`
	// Limit is how many cards the column should hold, 0 for no limit
	// (ADR-0133).
	Limit int `json:"limit,omitempty"`
	// Cards are the column's cards in their order, on a board.
	Cards []item.ID `json:"cards,omitempty"`
}

func toKanbanJSON(space string, k item.Kanban) kanbanJSON {
	j := kanbanJSON{ID: k.ID, Space: space, Name: k.Name, Type: k.Type, Label: k.Label, Columns: []columnJSON{}, CardFields: []string{},
		ColorField: k.ColorField, Colors: k.Colors}
	for _, c := range k.Columns {
		j.Columns = append(j.Columns, columnJSON{ID: c.ID, Name: c.Name, State: c.State, Limit: c.Limit})
	}
	j.CardFields = append(j.CardFields, k.CardFields...)
	return j
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
				j := toKanbanJSON(sp, k)
				j.Icon = kanbanIconTag(h.d.DB.Site, sp, k.ID)
				out = append(out, j)
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
		Name       string             `json:"name"`
		Columns    *[]columnJSON      `json:"columns"`
		CardFields *[]string          `json:"cardFields"`
		ColorField *string            `json:"colorField"`
		Colors     *map[string]string `json:"colors"`
	}
	if !decode(w, r, &req) {
		return
	}
	space := r.PathValue("space")
	k := item.Kanban{ID: item.ID(r.PathValue("id")), Name: req.Name}
	if req.Columns != nil {
		k.Columns = []item.KanbanColumn{}
		for _, c := range *req.Columns {
			k.Columns = append(k.Columns, item.KanbanColumn{ID: c.ID, Name: c.Name, State: c.State, Limit: c.Limit})
		}
	}
	if req.CardFields != nil {
		k.CardFields = append([]string{}, *req.CardFields...)
	}
	// Left out, the colouring is kept; an empty field takes it off.
	if req.ColorField != nil || req.Colors != nil {
		k.Colors = map[string]string{}
		if req.ColorField != nil {
			k.ColorField = *req.ColorField
		}
		if req.Colors != nil && *req.Colors != nil {
			k.Colors = *req.Colors
		}
		if k.ColorField != "" && len(k.Colors) == 0 {
			k.Colors = nil
		}
	}
	h.write(w, r, http.StatusOK, func(x *item.Session, _ *item.Store) (any, error) {
		k, err := x.SetKanban(space, k)
		return map[string]kanbanJSON{"kanban": toKanbanJSON(space, k)}, err
	})
}

// kanbanBoard answers a kanban's columns with the cards one may read
// (ADR-0076); the cards themselves come from the item listing.
func (h itemHandlers) kanbanBoard(w http.ResponseWriter, r *http.Request) {
	space := r.PathValue("space")
	h.read(w, r, func(x *item.Session) (any, error) {
		k, board, err := x.Board(space, item.ID(r.PathValue("id")))
		if err != nil {
			return nil, err
		}
		cols := []columnJSON{}
		for _, c := range board {
			cols = append(cols, columnJSON{ID: c.ID, Name: c.Name, State: c.State, Limit: c.Limit, Cards: c.Cards})
		}
		j := toKanbanJSON(space, k)
		j.Icon = kanbanIconTag(h.d.DB.Site, space, k.ID)
		return map[string]any{"kanban": j, "columns": cols}, nil
	})
}

func (h itemHandlers) placeCard(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Card   string  `json:"card"`
		Column item.ID `json:"column"`
		Before string  `json:"before"`
	}
	if !decode(w, r, &req) {
		return
	}
	space := r.PathValue("space")
	h.write(w, r, http.StatusNoContent, func(x *item.Session, _ *item.Store) (any, error) {
		card, err := resolveItem(x, req.Card)
		if err != nil {
			return nil, err
		}
		var before item.ID
		if req.Before != "" {
			b, err := resolveItem(x, req.Before)
			if err != nil {
				return nil, err
			}
			before = b.ID
		}
		return nil, x.PlaceCard(space, item.ID(r.PathValue("id")), card.ID, req.Column, before)
	})
}

func (h itemHandlers) addTransition(w http.ResponseWriter, r *http.Request) {
	var req struct {
		From string `json:"from"`
		To   string `json:"to"`
	}
	if !decode(w, r, &req) {
		return
	}
	h.write(w, r, http.StatusOK, func(x *item.Session, _ *item.Store) (any, error) {
		td, err := x.AddTransition(r.PathValue("name"), req.From, req.To)
		return map[string]typeJSON{"type": toTypeJSON(td)}, err
	})
}

func (h itemHandlers) removeKanban(w http.ResponseWriter, r *http.Request) {
	space, id := r.PathValue("space"), item.ID(r.PathValue("id"))
	removed := false
	h.write(w, r, http.StatusNoContent, func(x *item.Session, _ *item.Store) (any, error) {
		err := x.RemoveKanban(space, id)
		removed = err == nil
		return nil, err
	})
	// The kanban's picture goes with it (ADR-0087).
	if _, ok := h.d.DB.Site.Asset(site.KanbanIcon(space, string(id))); removed && ok {
		_ = h.d.DB.Site.RemoveAsset(who(r), site.KanbanIcon(space, string(id)))
	}
}
