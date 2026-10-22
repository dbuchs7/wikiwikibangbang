package server

import (
	"net/http"

	"github.com/dbuchs7/wikiwikibangbang/internal/item"
)

// secretRoutes protect passages of a body and decide who reads them
// (ADR-0088).
func secretRoutes(h itemHandlers) []Route {
	return []Route{
		{Method: http.MethodPost, Pattern: "/api/items/{ref}/secrets", Summary: "Protect a passage of the body: block (from 0), start and end in characters of its text, and readers (users and @groups) besides oneself; who may edit the item and reads all its passages", Handler: http.HandlerFunc(h.protect)},
		{Method: http.MethodPut, Pattern: "/api/items/{ref}/secrets/{id}", Summary: "Set who reads a protected passage besides the one who protected it (that one and the space's administrators)", Handler: http.HandlerFunc(h.setSecretReaders)},
		{Method: http.MethodDelete, Pattern: "/api/items/{ref}/secrets/{id}", Summary: "Take a passage's protection off (the one who protected it and the space's administrators)", Handler: http.HandlerFunc(h.unprotect)},
		{Method: http.MethodPost, Pattern: "/api/items/{ref}/secrets/{id}/request", Summary: "Ask to read a protected passage; the one who protected it and the space's administrators are told", Handler: http.HandlerFunc(h.requestSecret)},
		{Method: http.MethodPost, Pattern: "/api/items/{ref}/secrets/{id}/requests/{principal}", Summary: "Answer a request to read a protected passage: grant true lets the one who asked read it, false declines", Handler: http.HandlerFunc(h.decideSecret)},
	}
}

// liveBusy answers 409 when the item's body is in a live session: changing
// it beside the session would be lost by the session's next commit.
func (h itemHandlers) liveBusy(w http.ResponseWriter, r *http.Request) bool {
	if id, err := h.resolveID(r); err == nil && h.d.Collab != nil && h.d.Collab.Busy(id) {
		writeError(w, http.StatusConflict, "the body is being edited in a live session; protect the passage when it is done")
		return true
	}
	return false
}

func (h itemHandlers) protect(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Block   int      `json:"block"`
		Start   int      `json:"start"`
		End     int      `json:"end"`
		Readers []string `json:"readers"`
	}
	if !decode(w, r, &req) || h.liveBusy(w, r) {
		return
	}
	h.change(w, r, func(x *item.Session, _ *item.Store, it item.Item) error {
		_, err := x.Protect(it.ID, req.Block, req.Start, req.End, req.Readers)
		return err
	})
}

func (h itemHandlers) setSecretReaders(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Readers []string `json:"readers"`
	}
	if !decode(w, r, &req) {
		return
	}
	h.change(w, r, func(x *item.Session, _ *item.Store, it item.Item) error {
		return x.SetSecretReaders(it.ID, item.ID(r.PathValue("id")), req.Readers)
	})
}

func (h itemHandlers) unprotect(w http.ResponseWriter, r *http.Request) {
	if h.liveBusy(w, r) {
		return
	}
	h.change(w, r, func(x *item.Session, _ *item.Store, it item.Item) error {
		return x.Unprotect(it.ID, item.ID(r.PathValue("id")))
	})
}

func (h itemHandlers) requestSecret(w http.ResponseWriter, r *http.Request) {
	h.change(w, r, func(x *item.Session, _ *item.Store, it item.Item) error {
		return x.RequestSecret(it.ID, item.ID(r.PathValue("id")))
	})
}

func (h itemHandlers) decideSecret(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Grant bool `json:"grant"`
	}
	if !decode(w, r, &req) {
		return
	}
	h.change(w, r, func(x *item.Session, _ *item.Store, it item.Item) error {
		return x.DecideSecret(it.ID, item.ID(r.PathValue("id")), r.PathValue("principal"), req.Grant)
	})
}
