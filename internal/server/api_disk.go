package server

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/dbuchs7/wikiwikibangbang/internal/audit"
	"github.com/dbuchs7/wikiwikibangbang/internal/diskspace"
	"github.com/dbuchs7/wikiwikibangbang/internal/site"
)

// diskRoutes show administrators how full the storage is and let them set
// the thresholds (ADR-0150).
func diskRoutes(d Deps) []Route {
	h := authHandlers{d: d}
	return []Route{
		{Method: http.MethodGet, Pattern: "/api/storage", Summary: "How full the data directory's file system is: free and total bytes, the level (ok, warning, critical, full), the bytes of each data file and of the attachments, the growth per day and the days left at that pace; the last measurement, or a new one with fresh (administrators)", Handler: http.HandlerFunc(h.storage)},
		{Method: http.MethodPut, Pattern: "/api/settings/storage", Summary: "Set the thresholds of free space: warnPercent, warnMB, criticalPercent, criticalMB; below critical, uploads are refused (administrators)", Handler: http.HandlerFunc(h.setStorage)},
	}
}

func (h authHandlers) storage(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.adminOnly(w, r); !ok {
		return
	}
	// The last measurement, at most a few minutes old; fresh measures now.
	if r.URL.Query().Get("fresh") != "" {
		writeJSON(w, http.StatusOK, h.d.Disk.Check())
		return
	}
	writeJSON(w, http.StatusOK, h.d.Disk.Report())
}

func (h authHandlers) setStorage(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.adminOnly(w, r)
	if !ok {
		return
	}
	var req diskspace.Thresholds
	if err := readJSON(w, r, &req); err != nil {
		badRequest(w, err)
		return
	}
	switch err := h.d.DB.Site.SetDiskSpace(actor, req); {
	case errors.Is(err, site.ErrInvalid):
		writeError(w, http.StatusBadRequest, err.Error())
		return
	case err != nil:
		writeError(w, http.StatusInternalServerError, "setting not saved")
		return
	}
	h.record(audit.Entry{Event: audit.DiskSpaceChanged, Actor: actor, Addr: clientAddr(r), Detail: map[string]string{
		"warnPercent": strconv.Itoa(req.WarnPercent), "warnMB": strconv.Itoa(req.WarnMB),
		"criticalPercent": strconv.Itoa(req.CriticalPercent), "criticalMB": strconv.Itoa(req.CriticalMB),
	}})
	// The new thresholds apply at once.
	writeJSON(w, http.StatusOK, h.d.Disk.Check())
}

// freesSpace reports whether a request removes rather than adds: deleting,
// moving to the trash and purging, and cleaning up older versions stay
// possible on a full file system, so administrators can make room
// (ADR-0150).
func freesSpace(r *http.Request) bool {
	p := r.URL.Path
	return r.Method == http.MethodDelete || strings.HasSuffix(p, "/trash") || strings.HasSuffix(p, "/purge") || strings.HasSuffix(p, "/retention/run") || strings.HasSuffix(p, "/retention/clean")
}
