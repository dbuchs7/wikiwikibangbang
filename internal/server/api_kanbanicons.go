package server

import (
	"net/http"
	"slices"

	"github.com/dbuchs7/wikiwikibangbang/internal/audit"
	"github.com/dbuchs7/wikiwikibangbang/internal/item"
	"github.com/dbuchs7/wikiwikibangbang/internal/site"
)

// kanbanIconRoutes give a kanban a picture or logo (ADR-0087), shown on
// its board and its tile, as a space has its icon.
func kanbanIconRoutes(d Deps) []Route {
	h := itemHandlers{d: d}
	return []Route{
		{Method: http.MethodGet, Pattern: "/api/spaces/{space}/kanbans/{id}/icon", Summary: "A kanban's picture or logo; not found when it has none", Handler: http.HandlerFunc(h.kanbanIcon)},
		{Method: http.MethodPut, Pattern: "/api/spaces/{space}/kanbans/{id}/icon", Summary: "Set a kanban's picture or logo: PNG, JPEG, GIF, WebP or SVG as application/octet-stream (editors)", Handler: http.HandlerFunc(h.setKanbanIcon)},
		{Method: http.MethodDelete, Pattern: "/api/spaces/{space}/kanbans/{id}/icon", Summary: "Remove a kanban's picture or logo (editors)", Handler: http.HandlerFunc(h.removeKanbanIcon)},
	}
}

// kanbanRole is the caller's role in the space of a kanban; 0 when the
// kanban does not exist or the caller has no role in its space.
func (h itemHandlers) kanbanRole(r *http.Request, space string, id item.ID) item.Role {
	var role item.Role
	h.d.DB.Read(func(s *item.Store) {
		x := s.As(who(r))
		if slices.ContainsFunc(x.Kanbans(space)[space], func(k item.Kanban) bool { return k.ID == id }) {
			role = x.Role(space)
		}
	})
	return role
}

// kanbanIconTag names the content of a kanban's icon, or is empty.
func kanbanIconTag(s *site.Settings, space string, id item.ID) string {
	if a, ok := s.Asset(site.KanbanIcon(space, string(id))); ok {
		return a.Tag
	}
	return ""
}

func (h itemHandlers) kanbanIcon(w http.ResponseWriter, r *http.Request) {
	space, id := r.PathValue("space"), item.ID(r.PathValue("id"))
	a, ok := h.d.DB.Site.Asset(site.KanbanIcon(space, string(id)))
	if !ok || h.kanbanRole(r, space, id) < item.Viewer {
		writeError(w, http.StatusNotFound, "this kanban has no picture")
		return
	}
	serveIcon(w, r, a)
}

func (h itemHandlers) setKanbanIcon(w http.ResponseWriter, r *http.Request) {
	space, id := r.PathValue("space"), item.ID(r.PathValue("id"))
	switch role := h.kanbanRole(r, space, id); {
	case role < item.Viewer:
		writeError(w, http.StatusNotFound, "no such kanban")
		return
	case role < item.Editor:
		writeError(w, http.StatusForbidden, "editors of the space only")
		return
	}
	data, mediaType, ok := readIcon(w, r)
	if !ok {
		return
	}
	if err := h.d.DB.Site.SetAsset(who(r), site.KanbanIcon(space, string(id)), mediaType, data); err != nil {
		writeError(w, http.StatusInternalServerError, "the picture was not saved")
		return
	}
	h.d.DB.Audit.Record(audit.Entry{Event: audit.DesignChanged, Actor: who(r), Addr: clientAddr(r), Detail: map[string]string{"part": "kanban-icon", "space": space, "kanban": string(id)}})
	writeJSON(w, http.StatusOK, map[string]string{"icon": kanbanIconTag(h.d.DB.Site, space, id)})
}

func (h itemHandlers) removeKanbanIcon(w http.ResponseWriter, r *http.Request) {
	space, id := r.PathValue("space"), item.ID(r.PathValue("id"))
	switch role := h.kanbanRole(r, space, id); {
	case role < item.Viewer:
		writeError(w, http.StatusNotFound, "no such kanban")
		return
	case role < item.Editor:
		writeError(w, http.StatusForbidden, "editors of the space only")
		return
	}
	if err := h.d.DB.Site.RemoveAsset(who(r), site.KanbanIcon(space, string(id))); err != nil {
		writeError(w, http.StatusInternalServerError, "the picture was not removed")
		return
	}
	h.d.DB.Audit.Record(audit.Entry{Event: audit.DesignChanged, Actor: who(r), Addr: clientAddr(r), Detail: map[string]string{"part": "kanban-icon", "space": space, "kanban": string(id), "removed": "true"}})
	w.WriteHeader(http.StatusNoContent)
}
