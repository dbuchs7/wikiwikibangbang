package server

import (
	"bytes"
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"

	"github.com/dbuchs7/wikiwikibangbang/internal/audit"
	"github.com/dbuchs7/wikiwikibangbang/internal/budget"
	"github.com/dbuchs7/wikiwikibangbang/internal/site"
)

// regulationRoutes are the data protection regulations of the installation
// (ADR-0058): everyone signed in reads them, as a held item names them;
// administrators keep them, from templates or by hand.
func regulationRoutes(d Deps) []Route {
	h := authHandlers{d: d}
	return []Route{
		{Method: http.MethodGet, Pattern: "/api/regulations", Summary: "The data protection regulations: categories, indicators, link, whether a document is attached", Handler: http.HandlerFunc(h.regulations)},
		{Method: http.MethodGet, Pattern: "/api/regulations/templates", Summary: "Regulations to adopt: the Swiss Data Protection Act and the GDPR (administrators)", Handler: http.HandlerFunc(h.regulationTemplates)},
		{Method: http.MethodPut, Pattern: "/api/regulations/{id}", Summary: "Add or replace a regulation (administrators)", Handler: http.HandlerFunc(h.setRegulation)},
		{Method: http.MethodDelete, Pattern: "/api/regulations/{id}", Summary: "Remove a regulation and its document (administrators)", Handler: http.HandlerFunc(h.removeRegulation)},
		{Method: http.MethodGet, Pattern: "/api/regulations/{id}/document", Summary: "The attached text of a regulation, a PDF", Handler: http.HandlerFunc(h.regulationDocument)},
		{Method: http.MethodPut, Pattern: "/api/regulations/{id}/document", Summary: "Attach the text of a regulation, a PDF (administrators; the body is the file, application/octet-stream)", Handler: http.HandlerFunc(h.setRegulationDocument)},
		{Method: http.MethodDelete, Pattern: "/api/regulations/{id}/document", Summary: "Remove the attached text of a regulation (administrators)", Handler: http.HandlerFunc(h.removeRegulationDocument)},
	}
}

type regulationJSON struct {
	site.Regulation
	Document bool `json:"document"`
}

func (h authHandlers) regulations(w http.ResponseWriter, _ *http.Request) {
	s := h.d.DB.Site
	out := []regulationJSON{}
	for _, r := range s.Regulations() {
		_, doc := s.Asset(site.DocumentAsset(r.ID))
		out = append(out, regulationJSON{Regulation: r, Document: doc})
	}
	writeJSON(w, http.StatusOK, map[string]any{"regulations": out})
}

func (h authHandlers) regulationTemplates(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.adminOnly(w, r); !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"templates": site.Templates})
}

func (h authHandlers) setRegulation(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.adminOnly(w, r)
	if !ok {
		return
	}
	var reg site.Regulation
	if err := readJSON(w, r, &reg); err != nil {
		badRequest(w, err)
		return
	}
	reg.ID = r.PathValue("id")
	if err := h.d.DB.Site.SetRegulation(actor, reg); err != nil {
		h.brandingFailed(w, err)
		return
	}
	h.d.DB.Audit.Record(audit.Entry{Event: audit.RegulationChanged, Actor: actor, Addr: clientAddr(r), Detail: map[string]string{"regulation": reg.ID, "part": "settings"}})
	h.regulations(w, r)
}

func (h authHandlers) removeRegulation(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.adminOnly(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	if err := h.d.DB.Site.RemoveRegulation(actor, id); err != nil {
		h.brandingFailed(w, err)
		return
	}
	h.d.DB.Audit.Record(audit.Entry{Event: audit.RegulationChanged, Actor: actor, Addr: clientAddr(r), Detail: map[string]string{"regulation": id, "part": "removed"}})
	w.WriteHeader(http.StatusNoContent)
}

func (h authHandlers) regulationDocument(w http.ResponseWriter, r *http.Request) {
	a, ok := h.d.DB.Site.Asset(site.DocumentAsset(r.PathValue("id")))
	if !ok {
		writeError(w, http.StatusNotFound, "no document")
		return
	}
	hd := w.Header()
	hd.Set("Content-Type", a.Type)
	hd.Set("X-Content-Type-Options", "nosniff")
	hd.Set("Content-Security-Policy", "default-src 'none'; sandbox")
	hd.Set("Content-Disposition", `inline; filename="`+r.PathValue("id")+`.pdf"`)
	hd.Set("Content-Length", strconv.Itoa(len(a.Data)))
	_, _ = w.Write(a.Data)
}

func (h authHandlers) setRegulationDocument(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.adminOnly(w, r)
	if !ok {
		return
	}
	// Like every upload, the body must be application/octet-stream, a type
	// a cross-site form cannot send.
	if mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || mt != "application/octet-stream" {
		writeError(w, http.StatusUnsupportedMediaType, "the request body must be application/octet-stream")
		return
	}
	limit := budget.Get().AttachmentBytes
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, limit))
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		writeError(w, http.StatusRequestEntityTooLarge, "the document is too large")
		return
	}
	if err != nil {
		badRequest(w, err)
		return
	}
	if !bytes.HasPrefix(data, []byte("%PDF-")) {
		writeError(w, http.StatusBadRequest, "the document must be a PDF")
		return
	}
	id := r.PathValue("id")
	if err := h.d.DB.Site.SetAsset(actor, site.DocumentAsset(id), "application/pdf", data); err != nil {
		h.brandingFailed(w, err)
		return
	}
	h.d.DB.Audit.Record(audit.Entry{Event: audit.RegulationChanged, Actor: actor, Addr: clientAddr(r), Detail: map[string]string{"regulation": id, "part": "document"}})
	h.regulations(w, r)
}

func (h authHandlers) removeRegulationDocument(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.adminOnly(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	if err := h.d.DB.Site.RemoveAsset(actor, site.DocumentAsset(id)); err != nil {
		h.brandingFailed(w, err)
		return
	}
	h.d.DB.Audit.Record(audit.Entry{Event: audit.RegulationChanged, Actor: actor, Addr: clientAddr(r), Detail: map[string]string{"regulation": id, "part": "document removed"}})
	w.WriteHeader(http.StatusNoContent)
}
