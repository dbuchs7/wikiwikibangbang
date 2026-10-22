package server

import (
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"

	"github.com/dbuchs7/wikiwikibangbang/internal/audit"
	"github.com/dbuchs7/wikiwikibangbang/internal/budget"
	"github.com/dbuchs7/wikiwikibangbang/internal/item"
	"github.com/dbuchs7/wikiwikibangbang/internal/site"
)

// spaceIconRoutes are the icons or logos of spaces, shown on their tiles
// (ADR-0062).
func spaceIconRoutes(d Deps) []Route {
	h := itemHandlers{d: d}
	return []Route{
		{Method: http.MethodGet, Pattern: "/api/spaces/{space}/icon", Summary: "A space's icon or logo; not found when it has none", Handler: http.HandlerFunc(h.spaceIcon)},
		{Method: http.MethodPut, Pattern: "/api/spaces/{space}/icon", Summary: "Set a space's icon or logo: PNG, JPEG, GIF, WebP or SVG as application/octet-stream (space administrators)", Handler: http.HandlerFunc(h.setSpaceIcon)},
		{Method: http.MethodDelete, Pattern: "/api/spaces/{space}/icon", Summary: "Remove a space's icon (space administrators)", Handler: http.HandlerFunc(h.removeSpaceIcon)},
		{Method: http.MethodPut, Pattern: "/api/spaces/{space}/name", Summary: "Rename a space; its key stays (space administrators, ADR-0086)", Handler: http.HandlerFunc(h.renameSpace)},
	}
}

// renameSpace changes a space's display name (ADR-0086).
func (h itemHandlers) renameSpace(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if !decode(w, r, &req) {
		return
	}
	key := r.PathValue("space")
	h.write(w, r, http.StatusOK, func(x *item.Session, _ *item.Store) (any, error) {
		if err := x.RenameSpace(key, req.Name); err != nil {
			return nil, err
		}
		return map[string]string{"key": key, "name": strings.TrimSpace(req.Name)}, nil
	})
}

var spaceIconTypes = map[string]bool{"image/png": true, "image/jpeg": true, "image/gif": true, "image/webp": true, "image/svg+xml": true}

// spaceIconTag names the content of a space's icon, or is empty.
func spaceIconTag(s *site.Settings, key string) string {
	if a, ok := s.Asset(site.SpaceIcon(key)); ok {
		return a.Tag
	}
	return ""
}

// spaceRole is the caller's role in a space; 0 when the space does not
// exist or the caller has none.
func (h itemHandlers) spaceRole(r *http.Request, key string) item.Role {
	var role item.Role
	h.d.DB.Read(func(s *item.Store) {
		x := s.As(who(r))
		for _, sp := range x.Spaces() {
			if sp.Key == key {
				role = x.Role(key)
			}
		}
	})
	return role
}

// spaceIcon sends the icon to everyone with a role in the space; to
// others the space has none. Like a logo, it never runs as a page
// (ADR-0052).
func (h itemHandlers) spaceIcon(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("space")
	a, ok := h.d.DB.Site.Asset(site.SpaceIcon(key))
	if !ok || h.spaceRole(r, key) < item.Viewer {
		writeError(w, http.StatusNotFound, "this space has no icon")
		return
	}
	serveIcon(w, r, a)
}

// readIcon reads an uploaded icon: a picture of a kind a browser shows,
// within the design's budget. It answers the request itself when the
// upload is refused.
func readIcon(w http.ResponseWriter, r *http.Request) ([]byte, string, bool) {
	if mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || mt != "application/octet-stream" {
		writeError(w, http.StatusUnsupportedMediaType, "the request body must be application/octet-stream")
		return nil, "", false
	}
	limit := budget.Get().BrandingBytes
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, limit))
	var tooLarge *http.MaxBytesError
	switch {
	case errors.As(err, &tooLarge):
		writeError(w, http.StatusRequestEntityTooLarge, fmt.Sprintf("an icon may have at most %d bytes", limit))
		return nil, "", false
	case err != nil:
		writeError(w, http.StatusBadRequest, "the file could not be read")
		return nil, "", false
	}
	mediaType := sniff(data)
	if !spaceIconTypes[mediaType] {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("this file is %s; an icon is PNG, JPEG, GIF, WebP or SVG", mediaType))
		return nil, "", false
	}
	return data, mediaType, true
}

// serveIcon sends an icon so it never runs as a page (ADR-0052).
func serveIcon(w http.ResponseWriter, r *http.Request, a site.Asset) {
	tag := strconv.Quote(a.Tag)
	hd := w.Header()
	hd.Set("Content-Type", a.Type)
	hd.Set("X-Content-Type-Options", "nosniff")
	hd.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; sandbox")
	hd.Set("Cache-Control", "private, max-age=86400")
	hd.Set("ETag", tag)
	if r.Header.Get("If-None-Match") == tag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	hd.Set("Content-Length", strconv.Itoa(len(a.Data)))
	_, _ = w.Write(a.Data)
}

func (h itemHandlers) setSpaceIcon(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("space")
	if h.spaceRole(r, key) < item.SpaceAdmin {
		writeError(w, http.StatusForbidden, "space administrators only")
		return
	}
	data, mediaType, ok := readIcon(w, r)
	if !ok {
		return
	}
	if err := h.d.DB.Site.SetAsset(who(r), site.SpaceIcon(key), mediaType, data); err != nil {
		if errors.Is(err, site.ErrInvalid) {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "the icon was not saved")
		return
	}
	h.d.DB.Audit.Record(audit.Entry{Event: audit.DesignChanged, Actor: who(r), Addr: clientAddr(r), Detail: map[string]string{"part": "space-icon", "space": key}})
	writeJSON(w, http.StatusOK, map[string]string{"icon": spaceIconTag(h.d.DB.Site, key)})
}

func (h itemHandlers) removeSpaceIcon(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("space")
	if h.spaceRole(r, key) < item.SpaceAdmin {
		writeError(w, http.StatusForbidden, "space administrators only")
		return
	}
	if err := h.d.DB.Site.RemoveAsset(who(r), site.SpaceIcon(key)); err != nil {
		writeError(w, http.StatusInternalServerError, "the icon was not removed")
		return
	}
	h.d.DB.Audit.Record(audit.Entry{Event: audit.DesignChanged, Actor: who(r), Addr: clientAddr(r), Detail: map[string]string{"part": "space-icon", "space": key, "removed": "true"}})
	w.WriteHeader(http.StatusNoContent)
}
