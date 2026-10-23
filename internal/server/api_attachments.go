package server

import (
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/dbuchs7/wikiwikibangbang/internal/budget"
	"github.com/dbuchs7/wikiwikibangbang/internal/item"
	"github.com/dbuchs7/wikiwikibangbang/internal/storage"
)

// attachmentRoutes are the routes of item attachments (ADR-0045).
func attachmentRoutes(h itemHandlers) []Route {
	return []Route{
		{Method: http.MethodGet, Pattern: "/api/items/{ref}/attachments", Summary: "List the item's files", Handler: http.HandlerFunc(h.listAttachments)},
		{Method: http.MethodPost, Pattern: "/api/items/{ref}/attachments", Summary: "Upload a file: the body is the content (application/octet-stream), name the file name; sourceItem and sourceAttachment name the file it is a copy or a new version of", Handler: http.HandlerFunc(h.uploadAttachment)},
		{Method: http.MethodGet, Pattern: "/api/items/{ref}/attachments/{id}", Summary: "Download a file; images show inline, everything else downloads", Handler: http.HandlerFunc(h.downloadAttachment)},
		{Method: http.MethodDelete, Pattern: "/api/items/{ref}/attachments/{id}", Summary: "Delete a file; its content is destroyed", Handler: http.HandlerFunc(h.deleteAttachment)},
		{Method: http.MethodPut, Pattern: "/api/items/{ref}/attachments/{id}/labels", Summary: "Set a picture's labels (labels); they narrow the album and are no labels of the item, so no kanban or label board shows them", Handler: http.HandlerFunc(h.setPictureLabels)},
		{Method: http.MethodGet, Pattern: "/api/album", Summary: "The album: images and drawings of every item one may read, newest first; space, folder (only its pictures), q (name, label, key or title), label, kind (image or drawing), offset and limit narrow it", Handler: http.HandlerFunc(h.album)},
		{Method: http.MethodGet, Pattern: "/api/album/folders", Summary: "The album's folders one may read, by name: in space (every space when empty), right below parent (the top when empty)", Handler: http.HandlerFunc(h.albumFolders)},
		{Method: http.MethodPost, Pattern: "/api/album/folders", Summary: "Make a folder of the album: name, and space or parent (a folder); who may create there may", Handler: http.HandlerFunc(h.createAlbumFolder)},
		{Method: http.MethodGet, Pattern: "/api/album/labels", Summary: "The labels of the pictures one may read, most used first (space; q: holding it)", Handler: http.HandlerFunc(h.albumLabels)},
	}
}

// DrawingSuffix ends the name of a drawing's file (ADR-0053).
const DrawingSuffix = ".drawing.png"

// albumLimit is the most pictures one album request returns.
const albumLimit = 200

// album lists the images of the items the caller may read (ADR-0054). To
// use one in another item, the UI copies it there: a body shows only its
// own item's files, under that item's key and access.
func (h itemHandlers) album(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	kind, text := q.Get("kind"), strings.ToLower(strings.TrimSpace(q.Get("q")))
	if kind != "" && kind != "image" && kind != "drawing" {
		writeError(w, http.StatusBadRequest, "kind is image or drawing")
		return
	}
	offset, limit := 0, 48
	if v := q.Get("offset"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			writeError(w, http.StatusBadRequest, "offset must be a number from 0")
			return
		}
		offset = n
	}
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > albumLimit {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("limit must be from 1 to %d", albumLimit))
			return
		}
		limit = n
	}
	keep := func(a item.Attachment) bool {
		drawing := strings.HasSuffix(a.Name, DrawingSuffix)
		return inlineTypes[a.Type] && (kind == "" || (kind == "drawing") == drawing)
	}
	folder := item.ID(q.Get("folder"))
	label, _ := item.NormalLabel(q.Get("label"))
	h.read(w, r, func(x *item.Session) (any, error) {
		all := x.Pictures(q.Get("space"), keep)
		all = slices.DeleteFunc(all, func(p item.Picture) bool {
			return (folder != "" && p.Item.ID != folder) || (label != "" && !slices.Contains(p.Attachment.Labels, label)) ||
				(text != "" && !strings.Contains(strings.ToLower(p.Attachment.Name+" "+strings.Join(p.Attachment.Labels, " ")+" "+p.Item.Key+" "+p.Item.Title), text))
		})
		type itemRef struct {
			ID    item.ID `json:"id"`
			Key   string  `json:"key"`
			Title string  `json:"title"`
			Space string  `json:"space"`
			Type  string  `json:"type"`
		}
		type picture struct {
			Item       itemRef        `json:"item"`
			Attachment attachmentJSON `json:"attachment"`
			Drawing    bool           `json:"drawing"`
			// Used: its item's text shows it, so the album does not delete
			// it. Editable: the caller may label or delete it (ADR-0090).
			Used     bool `json:"used"`
			Editable bool `json:"editable"`
		}
		out := []picture{}
		for _, p := range all[min(offset, len(all)):min(offset+limit, len(all))] {
			out = append(out, picture{Item: itemRef{p.Item.ID, p.Item.Key, p.Item.Title, p.Item.Space, p.Item.Type},
				Attachment: toAttachmentJSON(p.Attachment), Drawing: strings.HasSuffix(p.Attachment.Name, DrawingSuffix), Used: p.Used, Editable: p.Editable})
		}
		next := 0
		if offset+limit < len(all) {
			next = offset + limit
		}
		return map[string]any{"pictures": out, "total": len(all), "next": next}, nil
	})
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
	// Origin says where a copy came from (ADR-0055), as far as the reader
	// may know; absent for a file that is no copy.
	Origin *copyOriginJSON `json:"origin,omitempty"`
	// Labels are the picture's own (ADR-0090).
	Labels []string `json:"labels,omitempty"`
}

type copyOriginJSON struct {
	// Readable is false when the source is gone or not the reader's to
	// see; nothing else is told then.
	Readable bool `json:"readable"`
	Item     *struct {
		ID    item.ID `json:"id"`
		Key   string  `json:"key"`
		Title string  `json:"title"`
	} `json:"item,omitempty"`
	// Newer is the newest version of the source, when it is newer than the
	// copy (a drawing edited since).
	Newer *attachmentJSON `json:"newer,omitempty"`
}

// withOrigin adds where a copy came from.
func withOrigin(x *item.Session, id item.ID, a attachmentJSON) attachmentJSON {
	o, ok, err := x.FileOrigin(id, a.ID)
	if err != nil || !ok {
		return a
	}
	a.Origin = &copyOriginJSON{Readable: o.Readable}
	if o.Readable {
		a.Origin.Item = &struct {
			ID    item.ID `json:"id"`
			Key   string  `json:"key"`
			Title string  `json:"title"`
		}{o.Item.ID, o.Item.Key, o.Item.Title}
		if o.Latest != nil {
			n := toAttachmentJSON(*o.Latest)
			a.Origin.Newer = &n
		}
	}
	return a
}

func toAttachmentJSON(a item.Attachment) attachmentJSON {
	return attachmentJSON{ID: a.ID, Name: a.Name, Type: a.Type, Size: a.Size, SHA256: a.SHA256, Author: a.Author,
		Created: time.Unix(a.Created, 0).UTC(), Inline: inlineTypes[a.Type], Labels: a.Labels}
}

func (h itemHandlers) setPictureLabels(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Labels []string `json:"labels"`
	}
	if decode(w, r, &req) {
		h.change(w, r, func(x *item.Session, _ *item.Store, it item.Item) error {
			return x.SetPictureLabels(it.ID, item.ID(r.PathValue("id")), req.Labels)
		})
	}
}

// albumFolders lists the album's folders (ADR-0090).
func (h itemHandlers) albumFolders(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	h.read(w, r, func(x *item.Session) (any, error) {
		out := []map[string]any{}
		for _, f := range x.Folders(q.Get("space"), item.ID(q.Get("parent"))) {
			out = append(out, map[string]any{"id": f.ID, "key": f.Key, "name": f.Title, "space": f.Space, "parent": f.Parent})
		}
		return map[string]any{"folders": out}, nil
	})
}

func (h itemHandlers) createAlbumFolder(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Space  string  `json:"space"`
		Parent item.ID `json:"parent"`
		Name   string  `json:"name"`
	}
	if !decode(w, r, &req) {
		return
	}
	h.write(w, r, http.StatusCreated, func(x *item.Session, _ *item.Store) (any, error) {
		f, err := x.CreateFolder(req.Space, req.Parent, req.Name)
		if err != nil {
			return nil, err
		}
		return map[string]itemJSON{"item": toItemJSON(f)}, nil
	})
}

// albumLabels counts the labels of the pictures one may read: the
// suggestions while one labels a picture, and the album's filter.
func (h itemHandlers) albumLabels(w http.ResponseWriter, r *http.Request) {
	h.read(w, r, func(x *item.Session) (any, error) {
		count := map[string]int{}
		for _, p := range x.Pictures(r.URL.Query().Get("space"), func(a item.Attachment) bool { return len(a.Labels) > 0 }) {
			for _, l := range p.Attachment.Labels {
				count[l]++
			}
		}
		q, _ := item.NormalLabel(r.URL.Query().Get("q"))
		var all []item.LabelCount
		for l, n := range count {
			if strings.Contains(l, q) {
				all = append(all, item.LabelCount{Label: l, Count: n})
			}
		}
		slices.SortFunc(all, func(a, b item.LabelCount) int {
			if a.Count != b.Count {
				return b.Count - a.Count
			}
			return strings.Compare(a.Label, b.Label)
		})
		out := []map[string]any{}
		for _, l := range all[:min(len(all), budget.Get().PageItems)] {
			out = append(out, map[string]any{"label": l.Label, "count": l.Count})
		}
		return map[string]any{"labels": out, "total": map[string]any{"value": len(all), "exact": true}}, nil
	})
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
			out = append(out, withOrigin(x, it.ID, toAttachmentJSON(a)))
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
	if q := r.URL.Query(); q.Get("sourceItem") != "" || q.Get("sourceAttachment") != "" {
		a.Source = &item.Source{Item: item.ID(q.Get("sourceItem")), Attachment: item.ID(q.Get("sourceAttachment"))}
	}
	h.write(w, r, http.StatusCreated, func(x *item.Session, _ *item.Store) (any, error) {
		got, err := x.AddAttachment(id, a)
		if err != nil {
			h.d.DB.DiscardAttachment(id, a.ID)
			return nil, err
		}
		return map[string]attachmentJSON{"attachment": withOrigin(x, id, toAttachmentJSON(got))}, nil
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
	if inlineTypes[a.Type] || slices.Contains(item.VideoTypes, a.Type) {
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
	hd.Set("X-Content-Type-Options", "nosniff")
	hd.Set("Content-Security-Policy", "default-src 'none'; sandbox")
	hd.Set("Cache-Control", "private, max-age=3600")
	hd.Set("ETag", strconv.Quote(a.SHA256))
	// Parts of a file are sent as asked for (Range), so a video plays and
	// seeks (ADR-0097); the ETag answers a repeated request.
	http.ServeContent(w, r, "", time.Time{}, readLogged{content, func(err error) {
		// The status is sent; a broken file shows as a cut-off download.
		h.log.Error("attachment content does not read", "item", it.ID, "attachment", a.ID, "err", err)
	}})
}

// readLogged reports a read that fails other than at the end.
type readLogged struct {
	io.ReadSeeker
	failed func(error)
}

func (r readLogged) Read(p []byte) (int, error) {
	n, err := r.ReadSeeker.Read(p)
	if err != nil && !errors.Is(err, io.EOF) {
		r.failed(err)
	}
	return n, err
}

func (h itemHandlers) deleteAttachment(w http.ResponseWriter, r *http.Request) {
	h.change(w, r, func(x *item.Session, _ *item.Store, it item.Item) error {
		return x.DeleteAttachment(it.ID, item.ID(r.PathValue("id")))
	})
}
