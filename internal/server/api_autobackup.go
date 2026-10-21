package server

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/dbuchs7/wikiwikibangbang/internal/audit"
	"github.com/dbuchs7/wikiwikibangbang/internal/autobackup"
	"github.com/dbuchs7/wikiwikibangbang/internal/backup"
	"github.com/dbuchs7/wikiwikibangbang/internal/backup/dest"
	"github.com/dbuchs7/wikiwikibangbang/internal/site"
)

// autoBackupRoutes set up automatic backups and show how they went
// (ADR-0073).
func autoBackupRoutes(d Deps) []Route {
	h := authHandlers{d: d}
	return []Route{
		{Method: http.MethodGet, Pattern: "/api/backups", Summary: "The schedule of automatic backups, the last runs and the versions at the destination (administrators)", Handler: http.HandlerFunc(h.autoBackups)},
		{Method: http.MethodPut, Pattern: "/api/backups/settings", Summary: "Set the schedule and destination of automatic backups; secrets come from the environment (administrators)", Handler: http.HandlerFunc(h.setAutoBackups)},
		{Method: http.MethodPost, Pattern: "/api/backups/run", Summary: "Take a full or incremental backup to the destination now, in the background (administrators)", Handler: http.HandlerFunc(h.runAutoBackup)},
		{Method: http.MethodPost, Pattern: "/api/backups/check", Summary: "Try a destination: store, list and delete a small file there (administrators)", Handler: http.HandlerFunc(h.checkBackupDestination)},
	}
}

// listTimeout bounds how long the status waits for the destination.
const listTimeout = 15 * time.Second

func (h authHandlers) autoBackups(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.adminOnly(w, r); !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), listTimeout)
	defer cancel()
	st, err := h.d.Backups.Status(ctx, true)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "the state of automatic backups cannot be read")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"settings":      h.d.DB.Site.Backup(),
		"defaults":      site.DefaultBackup,
		"maxVersions":   site.MaxBackupVersions,
		"passphraseEnv": autobackup.PassphraseEnv,
		"minPassphrase": backup.MinPassphrase,
		"status":        st,
	})
}

func (h authHandlers) setAutoBackups(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.adminOnly(w, r)
	if !ok {
		return
	}
	var b site.Backup
	if err := readJSON(w, r, &b); err != nil {
		badRequest(w, err)
		return
	}
	if b.Destination != "" {
		if err := dest.Check(b.Destination); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	if err := h.d.DB.Site.SetBackup(actor, b); err != nil {
		if errors.Is(err, site.ErrInvalid) {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "setting not saved")
		return
	}
	h.record(audit.Entry{Event: audit.BackupChanged, Actor: actor, Addr: clientAddr(r), Detail: map[string]string{
		"enabled": strconv.FormatBool(b.Enabled), "destination": b.Destination,
		"fullDays": strconv.Itoa(b.FullDays), "incrementalDays": strconv.Itoa(b.IncrementalDays), "keep": strconv.Itoa(b.Keep),
	}})
	writeJSON(w, http.StatusOK, b)
}

func (h authHandlers) runAutoBackup(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.adminOnly(w, r)
	if !ok {
		return
	}
	var req struct {
		Kind string `json:"kind"`
	}
	if err := readJSON(w, r, &req); err != nil {
		badRequest(w, err)
		return
	}
	if req.Kind != backup.Full && req.Kind != backup.Incremental {
		writeError(w, http.StatusBadRequest, "kind is full or incremental")
		return
	}
	if h.d.DB.Site.Backup().Destination == "" {
		writeError(w, http.StatusConflict, "set a destination first")
		return
	}
	// The backup outlives the request; it ends with the server.
	err := h.d.Backups.Start(req.Kind, actor, nil)
	if errors.Is(err, autobackup.ErrBusy) {
		writeError(w, http.StatusConflict, "a backup is running")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "the backup did not start")
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

// checkTimeout bounds a check of a destination.
const checkTimeout = 30 * time.Second

func (h authHandlers) checkBackupDestination(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.adminOnly(w, r); !ok {
		return
	}
	var req struct {
		Destination string `json:"destination"`
	}
	if err := readJSON(w, r, &req); err != nil {
		badRequest(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), checkTimeout)
	defer cancel()
	out := map[string]any{"ok": true}
	if err := h.d.Backups.Check(ctx, req.Destination); err != nil {
		out = map[string]any{"ok": false, "error": err.Error()}
	}
	writeJSON(w, http.StatusOK, out)
}
