package server

import (
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strconv"
	"time"

	"github.com/dbuchs7/wikiwikibangbang/internal/budget"
	"github.com/dbuchs7/wikiwikibangbang/internal/item"
	"github.com/dbuchs7/wikiwikibangbang/internal/storage"
)

// attachmentRoutes are the routes of item attachments (ADR-0045).
func attachmentRoutes(h itemHandlers) []Route {
	return []Route{
		{Method: http.MethodGet, Pattern: "/api/items/{ref}/attachments", Summary: "List the item's files", Handler: http.HandlerFunc(h.listAttachments)},
		{Method: http.MethodPost, Pattern: "/api/items/{ref}/attachments", Summary: "Upload a file: the body is the content (application/octet-stream), name the file name", Handler: http.HandlerFunc(h.uploadAttachment)},
		{Method: http.MethodGet, Pattern: "/api/items/{ref}/attachments/{id}", Summary: "Download a file; images show inline, everything else downloads", Handler: http.HandlerFunc(h.downloadAttachment)},
		{Method: http.MethodDelete, Pattern: "/api/items/{ref}/attachments/{id}", Summary: "Delete a file; its content is destroyed", Handler: http.HandlerFunc(h.deleteAttachment)},
	}
}

type attachmentJSON struct {
	ID      item.ID   `json:"id"`
	Name    string    `json:"name"`
	Type    string    `json:"type"`
	Size    int64     `json:"size"`
	SHA256  string    `json:"sha256"`
	Author  string    `json:"author"`
	Created time.Time `json:"created"`
	// Inline is whether the file shows in the page (an image of a safe
	// format), rather than only downloads.
	Inline bool `json:"inline"`
}

func toAttachmentJSON(a item.Attachment) attachmentJSON {
	return attachmentJSON{ID: a.ID, Name: a.Name, Type: a.Type, Size: a.Size, SHA256: a.SHA256, Author: a.Author,
		Created: time.Unix(a.Created, 0).UTC(), Inline: inlineTypes[a.Type]}
}

// inlineTypes are the image formats a browser shows without running
// anything. SVG is not among them: it can hold scripts (ADR-0014).
var inlineTypes = map[string]bool{"image/png": true, "image/jpeg": true, "image/gif": true, "image/webp": true}

func (h itemHandlers) listAttachments(w http.ResponseWriter, r *http.Request) {
	h.read(w, r, func(x *item.Session) (any, error) {
		it, err := resolveItem(x, r.PathValue("ref"))
		if err != nil {
			return nil, err
		}
		list, err := x.Attachments(it.ID)
		out := []attachmentJSON{}
		for _, a := range list {
			out = append(out, toAttachmentJSON(a))
		}
		return map[string]any{"attachments": out}, err
	})
}

// uploadAttachment stores the request body as a file of the item. The body
// must be application/octet-stream: like application/json for the other
// routes (readJSON), it is not a type a cross-site form can send.
func (h itemHandlers) uploadAttachment(w http.ResponseWriter, r *http.Request) {
	if mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || mt != "application/octet-stream" {
		writeError(w, http.StatusUnsupportedMediaType, "the request body must be application/octet-stream")
		return
	}
	name := r.URL.Query().Get("name")
	var id item.ID
	var check error
	h.d.DB.Read(func(s *item.Store) {
		x := s.As(who(r))
		it, err := resolveItem(x, r.PathValue("ref"))
		if err == nil {
			id, err = it.ID, x.CanAttach(it.ID)
		}
		check = err
	})
	if check != nil {
		h.fail(w, check)
		return
	}
	limit := budget.Get().AttachmentBytes
	a, err := h.d.DB.StoreAttachment(id, http.MaxBytesReader(w, r.Body, limit+1), limit)
	var tooLarge *http.MaxBytesError
	switch {
	case errors.Is(err, storage.ErrAttachmentTooLarge) || errors.As(err, &tooLarge):
		writeError(w, http.StatusRequestEntityTooLarge, fmt.Sprintf("a file may have at most %d bytes", limit))
		return
	case err != nil:
		h.fail(w, err)
		return
	}
	a.Name = name
	h.write(w, r, http.StatusCreated, func(x *item.Session, _ *item.Store) (any, error) {
		got, err := x.AddAttachment(id, a)
		if err != nil {
			h.d.DB.DiscardAttachment(id, a.ID)
			return nil, err
		}
		return map[string]attachmentJSON{"attachment": toAttachmentJSON(got)}, nil
	})
}

// downloadAttachment sends a file. Only images of a safe format show
// inline; everything else is a download, and no file is ever run as a page
// of this site: the response forbids sniffing and sandboxes it.
func (h itemHandlers) downloadAttachment(w http.ResponseWriter, r *http.Request) {
	var it item.Item
	var a item.Attachment
	var err error
	h.d.DB.Read(func(s *item.Store) {
		x := s.As(who(r))
		if it, err = resolveItem(x, r.PathValue("ref")); err == nil {
			a, err = x.Attachment(it.ID, item.ID(r.PathValue("id")))
		}
	})
	if err != nil {
		h.fail(w, err)
		return
	}
	content, err := h.d.DB.OpenAttachment(it.ID, a.ID)
	if err != nil {
		h.log.Error("attachment content missing", "item", it.ID, "attachment", a.ID, "err", err)
		writeError(w, http.StatusGone, "the content of this file is not available")
		return
	}
	defer content.Close()
	hd := w.Header()
	disposition := "attachment"
	if inlineTypes[a.Type] {
		disposition = "inline"
		hd.Set("Content-Type", a.Type)
	} else {
		hd.Set("Content-Type", "application/octet-stream")
	}
	if cd := mime.FormatMediaType(disposition, map[string]string{"filename": a.Name}); cd != "" {
		hd.Set("Content-Disposition", cd)
	} else {
		hd.Set("Content-Disposition", disposition)
	}
	hd.Set("Content-Length", strconv.FormatInt(a.Size, 10))
	hd.Set("X-Content-Type-Options", "nosniff")
	hd.Set("Content-Security-Policy", "default-src 'none'; sandbox")
	hd.Set("Cache-Control", "private, max-age=3600")
	hd.Set("ETag", strconv.Quote(a.SHA256))
	if r.Header.Get("If-None-Match") == strconv.Quote(a.SHA256) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	if _, err := io.Copy(w, content); err != nil {
		// The status is sent; a broken file shows as a cut-off download.
		h.log.Error("attachment content does not read", "item", it.ID, "attachment", a.ID, "err", err)
	}
}

func (h itemHandlers) deleteAttachment(w http.ResponseWriter, r *http.Request) {
	h.change(w, r, func(x *item.Session, _ *item.Store, it item.Item) error {
		return x.DeleteAttachment(it.ID, item.ID(r.PathValue("id")))
	})
}
