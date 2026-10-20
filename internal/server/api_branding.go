package server

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strconv"

	"github.com/dbuchs7/wikiwikibangbang/internal/audit"
	"github.com/dbuchs7/wikiwikibangbang/internal/budget"
	"github.com/dbuchs7/wikiwikibangbang/internal/site"
)

// brandingRoutes are the corporate design of the installation (ADR-0052).
// Reading it is public, so the sign-in page carries it too; changing it is
// for administrators.
func brandingRoutes(d Deps) []Route {
	h := authHandlers{d: d}
	return []Route{
		{Method: http.MethodGet, Pattern: "/branding.css", Summary: "The corporate design as a stylesheet over the UI's own", Access: Public, Handler: http.HandlerFunc(h.brandingCSS)},
		{Method: http.MethodGet, Pattern: "/branding/{asset}", Summary: "A file of the corporate design: logo, background or font", Access: Public, Handler: http.HandlerFunc(h.brandingAsset)},
		{Method: http.MethodGet, Pattern: "/branding.json", Summary: "The corporate design: name, colours, font, own stylesheet, and which files it has", Access: Public, Handler: http.HandlerFunc(h.getBranding)},
		{Method: http.MethodPut, Pattern: "/api/branding", Summary: "Set the corporate design (administrators)", Handler: http.HandlerFunc(h.setBranding)},
		{Method: http.MethodPut, Pattern: "/api/branding/{asset}", Summary: "Set the logo, background image or font file (administrators; the body is the file, application/octet-stream)", Handler: http.HandlerFunc(h.setBrandingAsset)},
		{Method: http.MethodDelete, Pattern: "/api/branding/{asset}", Summary: "Remove the logo, background image or font file (administrators)", Handler: http.HandlerFunc(h.removeBrandingAsset)},
	}
}

type brandingJSON struct {
	site.Design
	// Assets names the content of each file the design has.
	Assets map[string]string `json:"assets"`
	Fonts  []string          `json:"fonts"`
}

func (h authHandlers) getBranding(w http.ResponseWriter, r *http.Request) {
	s := h.d.DB.Site
	out := brandingJSON{Design: s.Design(), Assets: map[string]string{}, Fonts: []string{"system", "humanist", "serif", "rounded", "mono"}}
	for _, name := range site.Assets {
		if a, ok := s.Asset(name); ok {
			out.Assets[name] = a.Tag
		}
	}
	w.Header().Set("Cache-Control", "no-cache")
	writeJSON(w, http.StatusOK, out)
}

func (h authHandlers) brandingCSS(w http.ResponseWriter, r *http.Request) {
	css := h.d.DB.Site.Stylesheet()
	sum := sha256.Sum256([]byte(css))
	tag := strconv.Quote(hex.EncodeToString(sum[:8]))
	hd := w.Header()
	hd.Set("Content-Type", "text/css; charset=utf-8")
	hd.Set("Cache-Control", "no-cache")
	hd.Set("ETag", tag)
	if r.Header.Get("If-None-Match") == tag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	_, _ = io.WriteString(w, css)
}

// brandingAsset sends a file of the design. It is never run as a page of
// this site: an SVG logo shows in an <img>, where it runs nothing, and on
// its own it is sandboxed.
func (h authHandlers) brandingAsset(w http.ResponseWriter, r *http.Request) {
	a, ok := h.d.DB.Site.Asset(r.PathValue("asset"))
	if !ok {
		writeError(w, http.StatusNotFound, "no such file in the design")
		return
	}
	tag := strconv.Quote(a.Tag)
	hd := w.Header()
	hd.Set("Content-Type", a.Type)
	hd.Set("X-Content-Type-Options", "nosniff")
	hd.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; sandbox")
	hd.Set("Cache-Control", "public, max-age=86400")
	hd.Set("ETag", tag)
	if r.Header.Get("If-None-Match") == tag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	hd.Set("Content-Length", strconv.Itoa(len(a.Data)))
	_, _ = w.Write(a.Data)
}

func (h authHandlers) adminOnly(w http.ResponseWriter, r *http.Request) (string, bool) {
	p, _ := PrincipalFrom(r.Context())
	if !h.isAdmin(p) {
		writeError(w, http.StatusForbidden, "administrators only")
		return "", false
	}
	return string(p.User), true
}

func (h authHandlers) setBranding(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.adminOnly(w, r)
	if !ok {
		return
	}
	var d site.Design
	if err := readJSON(w, r, &d); err != nil {
		badRequest(w, err)
		return
	}
	if err := h.d.DB.Site.SetDesign(actor, d); err != nil {
		h.brandingFailed(w, err)
		return
	}
	h.d.DB.Audit.Record(audit.Entry{Event: audit.DesignChanged, Actor: actor, Addr: clientAddr(r), Detail: map[string]string{"part": "settings"}})
	h.getBranding(w, r)
}

// assetTypes are the formats each file of the design may have, read from
// its bytes.
var assetTypes = map[string]map[string]bool{
	site.AssetLogo:       {"image/png": true, "image/jpeg": true, "image/gif": true, "image/webp": true, "image/svg+xml": true},
	site.AssetBackground: {"image/png": true, "image/jpeg": true, "image/gif": true, "image/webp": true},
	site.AssetFont:       {"font/woff2": true, "font/woff": true, "font/ttf": true, "font/otf": true},
}

// sniff reads a file's format from its bytes; an SVG, which the standard
// sniffer calls XML or text, is recognised by its root element.
func sniff(data []byte) string {
	t := http.DetectContentType(data)
	if mt, _, _ := mime.ParseMediaType(t); mt == "text/xml" || mt == "text/plain" {
		head := data[:min(len(data), 1024)]
		if bytes.Contains(head, []byte("<svg")) {
			return "image/svg+xml"
		}
	}
	return t
}

func (h authHandlers) setBrandingAsset(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.adminOnly(w, r)
	if !ok {
		return
	}
	name := r.PathValue("asset")
	allowed, known := assetTypes[name]
	if !known {
		writeError(w, http.StatusNotFound, "no such file in the design")
		return
	}
	if mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || mt != "application/octet-stream" {
		writeError(w, http.StatusUnsupportedMediaType, "the request body must be application/octet-stream")
		return
	}
	limit := budget.Get().BrandingBytes
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, limit))
	var tooLarge *http.MaxBytesError
	switch {
	case errors.As(err, &tooLarge):
		writeError(w, http.StatusRequestEntityTooLarge, fmt.Sprintf("a file of the design may have at most %d bytes", limit))
		return
	case err != nil:
		writeError(w, http.StatusBadRequest, "the file could not be read")
		return
	}
	mediaType := sniff(data)
	if !allowed[mediaType] {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("this file is %s, which the design's %s cannot be", mediaType, name))
		return
	}
	if err := h.d.DB.Site.SetAsset(actor, name, mediaType, data); err != nil {
		h.brandingFailed(w, err)
		return
	}
	h.d.DB.Audit.Record(audit.Entry{Event: audit.DesignChanged, Actor: actor, Addr: clientAddr(r), Detail: map[string]string{"part": name}})
	h.getBranding(w, r)
}

func (h authHandlers) removeBrandingAsset(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.adminOnly(w, r)
	if !ok {
		return
	}
	if _, known := assetTypes[r.PathValue("asset")]; !known {
		writeError(w, http.StatusNotFound, "no such file in the design")
		return
	}
	if err := h.d.DB.Site.RemoveAsset(actor, r.PathValue("asset")); err != nil {
		h.brandingFailed(w, err)
		return
	}
	h.d.DB.Audit.Record(audit.Entry{Event: audit.DesignChanged, Actor: actor, Addr: clientAddr(r), Detail: map[string]string{"part": r.PathValue("asset"), "removed": "true"}})
	w.WriteHeader(http.StatusNoContent)
}

func (h authHandlers) brandingFailed(w http.ResponseWriter, err error) {
	if errors.Is(err, site.ErrInvalid) {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeError(w, http.StatusInternalServerError, "the design was not saved")
}
