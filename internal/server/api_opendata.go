package server

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/dbuchs7/wikiwikibangbang/internal/audit"
	"github.com/dbuchs7/wikiwikibangbang/internal/auth"
	"github.com/dbuchs7/wikiwikibangbang/internal/item"
	"github.com/dbuchs7/wikiwikibangbang/internal/opendata"
	"github.com/dbuchs7/wikiwikibangbang/internal/site"
)

// openDataRoutes set up the catalog's synchronisation with opendata.swiss
// and show how it went (ADR-0075).
func openDataRoutes(d Deps) []Route {
	h := authHandlers{d: d}
	return []Route{
		{Method: http.MethodGet, Pattern: "/api/catalog/opendata", Summary: "The synchronisation of a catalog with opendata.swiss: settings and the last runs (administrators)", Handler: http.HandlerFunc(h.openData)},
		{Method: http.MethodPut, Pattern: "/api/catalog/opendata/settings", Summary: "Set the synchronisation with opendata.swiss: space, source, formats, organizations, interval, steward (administrators)", Handler: http.HandlerFunc(h.setOpenData)},
		{Method: http.MethodPost, Pattern: "/api/catalog/opendata/run", Summary: "Run the synchronisation now, in the background; dryRun reports what it would change without writing (administrators)", Handler: http.HandlerFunc(h.runOpenData)},
	}
}

func (h authHandlers) openData(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.adminOnly(w, r); !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"settings": h.d.DB.Site.OpenData(),
		"defaults": site.DefaultOpenData,
		"status":   h.d.OpenData.Status(),
		"type":     opendata.TypeName,
	})
}

func (h authHandlers) setOpenData(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.adminOnly(w, r)
	if !ok {
		return
	}
	var o site.OpenData
	if err := readJSON(w, r, &o); err != nil {
		badRequest(w, err)
		return
	}
	space := strings.TrimSpace(o.Space)
	exists := true
	if space != "" {
		h.d.DB.Read(func(s *item.Store) { exists = s.HasSpace(space) })
	}
	if !exists {
		writeError(w, http.StatusBadRequest, "the space "+space+" does not exist; create it first")
		return
	}
	if o.Steward != "" {
		if _, ok := h.d.DB.Accounts.Usernames([]auth.UserID{auth.UserID(o.Steward)})[auth.UserID(o.Steward)]; !ok {
			writeError(w, http.StatusBadRequest, "the steward is not a user")
			return
		}
	}
	kept, err := h.d.DB.Site.SetOpenData(actor, o)
	if err != nil {
		if errors.Is(err, site.ErrInvalid) {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "setting not saved")
		return
	}
	h.record(audit.Entry{Event: audit.CatalogSyncChanged, Actor: actor, Addr: clientAddr(r), Subject: kept.Space, Detail: map[string]string{
		"enabled": strconv.FormatBool(kept.Enabled), "source": kept.BaseURL, "formats": strings.Join(kept.Formats, " "),
		"organizations": strings.Join(kept.Organizations, " "), "intervalHours": strconv.Itoa(kept.IntervalHours),
	}})
	writeJSON(w, http.StatusOK, kept)
}

func (h authHandlers) runOpenData(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.adminOnly(w, r)
	if !ok {
		return
	}
	var req struct {
		DryRun bool `json:"dryRun"`
	}
	if err := readJSON(w, r, &req); err != nil {
		badRequest(w, err)
		return
	}
	if h.d.DB.Site.OpenData().Space == "" {
		writeError(w, http.StatusConflict, "set the catalog's space first")
		return
	}
	err := h.d.OpenData.Start(req.DryRun, actor)
	if errors.Is(err, opendata.ErrBusy) {
		writeError(w, http.StatusConflict, "a synchronisation is running")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "the synchronisation did not start")
		return
	}
	w.WriteHeader(http.StatusAccepted)
}
