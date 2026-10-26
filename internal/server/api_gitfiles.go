package server

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/dbuchs7/wikiwikibangbang/internal/connector"
	"github.com/dbuchs7/wikiwikibangbang/internal/gitrepo"
	"github.com/dbuchs7/wikiwikibangbang/internal/item"
)

// filesTimeout bounds reading a Markdown file and its pictures for a
// request.
const filesTimeout = 2 * time.Minute

// gitFileRoutes make pages from Markdown files of public repositories that
// a text shows (ADR-0132). They are off unless WWBB_GIT_FILES=on.
func gitFileRoutes(h itemHandlers) []Route {
	return []Route{
		{Method: http.MethodPost, Pattern: "/api/items/{ref}/markdown", Summary: "Make a page below the item from a Markdown file of a public repository, given by its address at the host; " +
			"version current follows the file's branch or tag every hour, this pins the commit read (off unless WWBB_GIT_FILES=on)", Handler: http.HandlerFunc(h.mirrorFile)},
		{Method: http.MethodPost, Pattern: "/api/items/{ref}/markdown/update", Summary: "Read the file of a page made from a Markdown file again now, and write the page if the file changed", Handler: http.HandlerFunc(h.updateFile)},
		{Method: http.MethodPut, Pattern: "/api/items/{ref}/markdown", Summary: "Pin a page made from a Markdown file at the commit it shows (version this), or let it follow its branch or tag again (version current)", Handler: http.HandlerFunc(h.pinFile)},
	}
}

// fileRequest names a file and the version wanted.
type fileRequest struct {
	URL     string `json:"url"`
	Version string `json:"version"`
}

// readFileRequest reads the request and the item it names, as the caller
// reads it.
func (h itemHandlers) readFileRequest(w http.ResponseWriter, r *http.Request, req *fileRequest) (item.Item, bool) {
	if err := readJSON(w, r, req); err != nil {
		badRequest(w, err)
		return item.Item{}, false
	}
	if req.Version != "" && req.Version != "current" && req.Version != "this" {
		writeError(w, http.StatusBadRequest, "the version is current or this")
		return item.Item{}, false
	}
	var it item.Item
	var err error
	h.d.DB.Read(func(s *item.Store) { it, err = resolveItem(s.As(who(r)), r.PathValue("ref")) })
	if err != nil {
		h.fail(w, err)
		return it, false
	}
	return it, true
}

func (h itemHandlers) mirrorFile(w http.ResponseWriter, r *http.Request) {
	var req fileRequest
	host, ok := h.readFileRequest(w, r, &req)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), filesTimeout)
	defer cancel()
	page, err := h.d.Connectors.MirrorFile(ctx, who(r), host.ID, req.URL, req.Version == "this")
	if err != nil {
		h.fileFailed(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]itemJSON{"item": toItemJSON(page)})
}

func (h itemHandlers) updateFile(w http.ResponseWriter, r *http.Request) {
	var it item.Item
	var err error
	h.d.DB.Read(func(s *item.Store) { it, err = resolveItem(s.As(who(r)), r.PathValue("ref")) })
	if err != nil {
		h.fail(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), filesTimeout)
	defer cancel()
	page, changed, err := h.d.Connectors.UpdateFile(ctx, who(r), it.ID)
	if err != nil {
		h.fileFailed(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"item": toItemJSON(page), "changed": changed})
}

func (h itemHandlers) pinFile(w http.ResponseWriter, r *http.Request) {
	var req fileRequest
	it, ok := h.readFileRequest(w, r, &req)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), filesTimeout)
	defer cancel()
	page, err := h.d.Connectors.PinFile(ctx, who(r), it.ID, req.Version == "this")
	if err != nil {
		h.fileFailed(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]itemJSON{"item": toItemJSON(page)})
}

// fileFailed answers why a file was not read: a wrong address or file is
// the caller's, a host that fails is the host's (502), and the function
// switched off is a conflict with the server's settings.
func (h itemHandlers) fileFailed(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, item.ErrValidation), errors.Is(err, item.ErrNotFound), errors.Is(err, item.ErrForbidden), errors.Is(err, item.ErrReferenced):
		h.fail(w, err)
	case errors.Is(err, connector.ErrFilesOff):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, connector.ErrNoMarkdownFile), errors.Is(err, connector.ErrNotMirrored), errors.Is(err, gitrepo.ErrNotFound),
		errors.Is(err, gitrepo.ErrTooLarge), errors.Is(err, connector.ErrLeftOut), errors.Is(err, connector.ErrPrivateAddress):
		writeError(w, http.StatusBadRequest, err.Error())
	default:
		writeError(w, http.StatusBadGateway, err.Error())
	}
}
