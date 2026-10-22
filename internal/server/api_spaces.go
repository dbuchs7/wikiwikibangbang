package server

import (
	"net/http"

	"github.com/dbuchs7/wikiwikibangbang/internal/audit"
	"github.com/dbuchs7/wikiwikibangbang/internal/item"
	"github.com/dbuchs7/wikiwikibangbang/internal/site"
)

// spaceTrashRoutes delete spaces in two steps (ADR-0069).
func spaceTrashRoutes(h itemHandlers) []Route {
	return []Route{
		{Method: http.MethodDelete, Pattern: "/api/spaces/{space}", Summary: "Put a space with everything in it into the trash, out of everyone's sight (administrators)", Handler: http.HandlerFunc(h.trashSpace)},
		{Method: http.MethodGet, Pattern: "/api/spaces/trash", Summary: "The spaces in the trash, with how many items each holds (administrators)", Handler: http.HandlerFunc(h.trashedSpaces)},
		{Method: http.MethodPost, Pattern: "/api/spaces/{space}/restore", Summary: "Bring a space back from the trash as it was (administrators)", Handler: http.HandlerFunc(h.restoreSpace)},
		{Method: http.MethodPost, Pattern: "/api/spaces/{space}/purge", Summary: "Delete a space in the trash for good; confirm repeats its key. Its items become tombstones and its key is not used again (administrators)", Handler: http.HandlerFunc(h.purgeSpace)},
	}
}

// spaceCommand runs an administrator's command on a space and records it in
// the audit trail when it succeeds.
func (h itemHandlers) spaceCommand(w http.ResponseWriter, r *http.Request, event string, run func(s *item.Store, key string) error) {
	key := r.PathValue("space")
	h.write(w, r, http.StatusNoContent, func(_ *item.Session, s *item.Store) (any, error) {
		if err := h.isAdmin(s, r); err != nil {
			return nil, err
		}
		if err := run(s, key); err != nil {
			return nil, err
		}
		h.d.DB.Audit.Record(audit.Entry{Event: event, Actor: who(r), Addr: clientAddr(r), Subject: key})
		return nil, nil
	})
}

func (h itemHandlers) trashSpace(w http.ResponseWriter, r *http.Request) {
	h.spaceCommand(w, r, audit.SpaceTrashed, func(s *item.Store, key string) error { return s.TrashSpace(who(r), key) })
}

func (h itemHandlers) restoreSpace(w http.ResponseWriter, r *http.Request) {
	h.spaceCommand(w, r, audit.SpaceRestored, func(s *item.Store, key string) error { return s.RestoreSpace(who(r), key) })
}

func (h itemHandlers) purgeSpace(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Confirm string `json:"confirm"`
	}
	if !decode(w, r, &req) {
		return
	}
	key := r.PathValue("space")
	if req.Confirm != key {
		writeError(w, http.StatusBadRequest, "confirm must repeat the space's key")
		return
	}
	purged := false
	h.spaceCommand(w, r, audit.SpacePurged, func(s *item.Store, key string) error {
		err := s.PurgeSpace(who(r), key)
		purged = err == nil
		return err
	})
	// The space's icon and its kanbans' pictures go with it; a failure leaves an icon nobody can
	// see, which the space's key never shows again.
	if _, ok := h.d.DB.Site.Asset(site.SpaceIcon(key)); purged && ok {
		_ = h.d.DB.Site.RemoveAsset(who(r), site.SpaceIcon(key))
	}
	if purged {
		for _, name := range h.d.DB.Site.KanbanIcons(key) {
			_ = h.d.DB.Site.RemoveAsset(who(r), name)
		}
	}
}

func (h itemHandlers) trashedSpaces(w http.ResponseWriter, r *http.Request) {
	type trashedJSON struct {
		Key   string `json:"key"`
		Name  string `json:"name"`
		Items int    `json:"items"`
	}
	out, admin := []trashedJSON{}, false
	h.d.DB.Read(func(s *item.Store) {
		if admin = s.HasSystemRole(who(r), item.RoleAdmin); !admin {
			return
		}
		for _, sp := range s.TrashedSpaces() {
			out = append(out, trashedJSON{Key: sp.Key, Name: sp.Name, Items: sp.Items})
		}
	})
	if !admin {
		writeError(w, http.StatusForbidden, "administrators only")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"spaces": out})
}
