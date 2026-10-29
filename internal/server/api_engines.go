package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/dbuchs7/wikiwikibangbang/internal/audit"
	"github.com/dbuchs7/wikiwikibangbang/internal/engine"
	"github.com/dbuchs7/wikiwikibangbang/internal/item"
	"github.com/dbuchs7/wikiwikibangbang/internal/site"
)

// engineRoutes name the process engines whose processes pages show, and
// read those processes from them when a page is shown (ADR-0129), their
// forms and decisions (ADR-0144) and the architecture sketches of their
// applications' releases (ADR-0146). Every signed-in principal reads;
// which processes, forms, decisions and sketches it reads is decided by
// the engine's account behind the credential. The forms of a process's
// steps and the public start links of its processes (ADR-0148) are read
// alike; a start link's form is read and sent through the engine's
// public routes, without the credential, so that it starts a process as
// anyone with the link does.
func engineRoutes(d Deps) []Route {
	h := engineHandlers{authHandlers: authHandlers{d: d}, log: d.Log}
	if h.log == nil {
		h.log = slog.New(slog.DiscardHandler)
	}
	return []Route{
		{Method: http.MethodGet, Pattern: "/api/engines", Summary: "The process engines whose processes pages show: ID, name and whether their operations may be shown; administrators also see the kind, the address and whether the credential is set (ADR-0129)", Handler: http.HandlerFunc(h.engines)},
		{Method: http.MethodPut, Pattern: "/api/engines/{id}", Summary: "Add or change a process engine: name, kind, address and whether its operations may be shown; its credential is the environment variable WWBB_ENGINE_<ID>_TOKEN (administrators)", Handler: http.HandlerFunc(h.setEngine)},
		{Method: http.MethodDelete, Pattern: "/api/engines/{id}", Summary: "Remove a process engine; the blocks that show its processes stay (administrators)", Handler: http.HandlerFunc(h.removeEngine)},
		{Method: http.MethodGet, Pattern: "/api/engines/{id}/processes", Summary: "The newest version of each process the engine lets its credential read, read from the engine now", Handler: http.HandlerFunc(h.processes)},
		{Method: http.MethodGet, Pattern: "/api/engines/{id}/processes/{process}", Summary: "The newest version of a process with its BPMN model, read from the engine now", Handler: http.HandlerFunc(h.process)},
		{Method: http.MethodGet, Pattern: "/api/engines/{id}/processes/{process}/operations", Summary: "Where the instances of a process's newest version are: counts per element, when the engine's operations may be shown", Handler: http.HandlerFunc(h.operations)},
		{Method: http.MethodGet, Pattern: "/api/engines/{id}/forms", Summary: "The forms the engine lets its credential see, as they are saved, read from the engine now (ADR-0144)", Handler: http.HandlerFunc(h.forms)},
		{Method: http.MethodGet, Pattern: "/api/engines/{id}/forms/{form}", Summary: "A form as it is saved, with its form-js schema, read from the engine now; with the query parameter process, the form must be named by a step of that process's newest version, and steps lists those steps (ADR-0148)", Handler: http.HandlerFunc(h.form)},
		{Method: http.MethodGet, Pattern: "/api/engines/{id}/processes/{process}/forms", Summary: "The steps of a process's newest version that name a form, in the order of its model: element, kind, name and form; and whether it holds a public wizard (ADR-0148)", Handler: http.HandlerFunc(h.processForms)},
		{Method: http.MethodGet, Pattern: "/api/engines/{id}/public-links", Summary: "The public start links the engine lets its credential list: link, process and form (those who may edit in a space; ADR-0148)", Handler: http.HandlerFunc(h.publicLinks)},
		{Method: http.MethodGet, Pattern: "/api/engines/{id}/public-links/{link}", Summary: "The form of a public start link as anyone with the link reads it: the process's name, the form-js schema and the engine's own page of the link, read through the engine's public route without the credential; with the query parameter process, wizard says whether that process leads its filler on in the engine's own pages (ADR-0148)", Handler: http.HandlerFunc(h.publicForm)},
		{Method: http.MethodPost, Pattern: "/api/engines/{id}/public-links/{link}/start", Summary: "Start the process of a public start link with {\"variables\": {...}}, the form's values, and the process it starts if known, through the engine's public route without the credential: the engine records no initiator; a process that leads its filler on in the engine's own pages is not started (ADR-0148)", Handler: http.HandlerFunc(h.startPublic)},
		{Method: http.MethodGet, Pattern: "/api/engines/{id}/decisions", Summary: "The newest deployed version of each decision the engine lets its credential read, read from the engine now (ADR-0144)", Handler: http.HandlerFunc(h.decisions)},
		{Method: http.MethodGet, Pattern: "/api/engines/{id}/decisions/{decision}", Summary: "The newest deployed version of a decision with the DMN model it is deployed in, read from the engine now", Handler: http.HandlerFunc(h.decision)},
		{Method: http.MethodGet, Pattern: "/api/engines/{id}/architectures", Summary: "The applications whose releases carry an architecture sketch the engine lets its credential read, each with those releases, newest first: version, when published, when the sketch was taken and the layers it records, read from the engine now (ADR-0146)", Handler: http.HandlerFunc(h.architectures)},
		{Method: http.MethodGet, Pattern: "/api/engines/{id}/architectures/{application}/{release}", Summary: "The architecture sketch of a release as an image/svg+xml picture, in the view the engine answers every caller alike, read from the engine now: release is newest, followed on every read, or a version, a positive number; the query parameter layers names some of strategy, business, application, data, technology, separated by commas, or none for the engine's default; X-Release-Version and X-Release-Taken-At name the release read", Handler: http.HandlerFunc(h.architecture)},
	}
}

type engineHandlers struct {
	authHandlers
	log *slog.Logger
}

// engineJSON is an engine as a reader sees it; an administrator also sees
// its settings and whether the credential is set, never the credential.
type engineJSON struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Operations bool   `json:"operations"`
	Kind       string `json:"kind,omitempty"`
	BaseURL    string `json:"baseURL,omitempty"`
	// CredentialVar names the variable of the credential, Credential says
	// whether it is set.
	CredentialVar string `json:"credentialVar,omitempty"`
	Credential    *bool  `json:"credential,omitempty"`
}

func (h engineHandlers) engineJSON(e site.Engine, admin bool) engineJSON {
	out := engineJSON{ID: e.ID, Name: e.Name, Operations: e.Operations}
	if admin {
		set := h.d.Engines.HasCredential(e.ID)
		out.Kind, out.BaseURL, out.CredentialVar, out.Credential = e.Kind, e.BaseURL, engine.TokenVar(e.ID), &set
	}
	return out
}

func (h engineHandlers) engines(w http.ResponseWriter, r *http.Request) {
	p, _ := PrincipalFrom(r.Context())
	admin := h.isAdmin(p)
	list := []engineJSON{}
	for _, e := range h.d.DB.Site.Engines() {
		list = append(list, h.engineJSON(e, admin))
	}
	out := map[string]any{"engines": list}
	if admin {
		out["kinds"] = site.EngineKinds
	}
	writeJSON(w, http.StatusOK, out)
}

func (h engineHandlers) setEngine(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.adminOnly(w, r)
	if !ok {
		return
	}
	var e site.Engine
	if err := readJSON(w, r, &e); err != nil {
		badRequest(w, err)
		return
	}
	e.ID = r.PathValue("id")
	before, existed := h.d.DB.Site.Engine(e.ID)
	kept, err := h.d.DB.Site.SetEngine(actor, e)
	if err != nil {
		if errors.Is(err, site.ErrInvalid) {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "engine not saved")
		return
	}
	h.record(audit.Entry{Event: audit.EngineChanged, Actor: actor, Addr: clientAddr(r), Subject: kept.ID,
		Detail: map[string]string{"kind": kept.Kind, "address": kept.BaseURL, "operations": strconv.FormatBool(kept.Operations)}})
	// People's grants belong to the engine at its address (ADR-0151).
	if existed && before.BaseURL != kept.BaseURL {
		endConnections(h.d, actor, clientAddr(r), kept.ID, "the engine's address changed")
	}
	status := http.StatusOK
	if !existed {
		status = http.StatusCreated
	}
	writeJSON(w, status, h.engineJSON(kept, true))
}

func (h engineHandlers) removeEngine(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.adminOnly(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	if _, exists := h.d.DB.Site.Engine(id); !exists {
		writeError(w, http.StatusNotFound, "no engine "+id)
		return
	}
	if err := h.d.DB.Site.RemoveEngine(actor, id); err != nil {
		writeError(w, http.StatusInternalServerError, "engine not removed")
		return
	}
	h.record(audit.Entry{Event: audit.EngineChanged, Actor: actor, Addr: clientAddr(r), Subject: id, Detail: map[string]string{"removed": "true"}})
	endConnections(h.d, actor, clientAddr(r), id, "the engine was removed")
	w.WriteHeader(http.StatusNoContent)
}

// engineOf returns the engine a request names, or answers that it is not
// set up.
func (h engineHandlers) engineOf(w http.ResponseWriter, r *http.Request) (site.Engine, bool) {
	e, ok := h.d.DB.Site.Engine(r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, "no engine "+r.PathValue("id")+" is set up")
	}
	return e, ok
}

// engineFailed answers a read of an engine that failed, saying which
// engine and how; the credential is never part of the message.
func (h engineHandlers) engineFailed(w http.ResponseWriter, e site.Engine, err error) {
	msg := "engine " + e.ID + ": " + err.Error()
	switch {
	case errors.Is(err, engine.ErrNotFound):
		writeError(w, http.StatusNotFound, msg)
		return
	case errors.Is(err, engine.ErrRejected):
		writeError(w, http.StatusUnprocessableEntity, msg)
		return
	case errors.Is(err, engine.ErrBusy):
		w.Header().Set("Retry-After", "5")
		writeError(w, http.StatusServiceUnavailable, msg)
		return
	}
	h.log.Warn("process engine read failed", "engine", e.ID, "err", err)
	writeError(w, http.StatusBadGateway, msg)
}

func (h engineHandlers) processes(w http.ResponseWriter, r *http.Request) {
	e, ok := h.engineOf(w, r)
	if !ok {
		return
	}
	list, err := h.d.Engines.Processes(r.Context(), e)
	if err != nil {
		h.engineFailed(w, e, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"engine": h.engineJSON(e, false), "processes": list})
}

// process answers the model as a field of JSON, so that it is never a
// document of this site.
func (h engineHandlers) process(w http.ResponseWriter, r *http.Request) {
	e, ok := h.engineOf(w, r)
	if !ok {
		return
	}
	d, err := h.d.Engines.Diagram(r.Context(), e, r.PathValue("process"))
	if err != nil {
		h.engineFailed(w, e, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{"engine": h.engineJSON(e, false), "diagram": d})
}

func (h engineHandlers) operations(w http.ResponseWriter, r *http.Request) {
	e, ok := h.engineOf(w, r)
	if !ok {
		return
	}
	if !e.Operations {
		writeError(w, http.StatusForbidden, "the operations of engine "+e.ID+" are not shown; an administrator may allow them")
		return
	}
	ops, err := h.d.Engines.Operations(r.Context(), e, r.PathValue("process"))
	if err != nil {
		h.engineFailed(w, e, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{"operations": ops})
}

func (h engineHandlers) forms(w http.ResponseWriter, r *http.Request) {
	e, ok := h.engineOf(w, r)
	if !ok {
		return
	}
	list, err := h.d.Engines.Forms(r.Context(), e)
	if err != nil {
		h.engineFailed(w, e, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"engine": h.engineJSON(e, false), "forms": list})
}

// form answers the schema as a field of JSON, as process answers a model.
func (h engineHandlers) form(w http.ResponseWriter, r *http.Request) {
	e, ok := h.engineOf(w, r)
	if !ok {
		return
	}
	f, err := h.d.Engines.Form(r.Context(), e, r.PathValue("form"), r.URL.Query().Get("process"))
	if err != nil {
		h.engineFailed(w, e, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{"engine": h.engineJSON(e, false), "form": f})
}

func (h engineHandlers) processForms(w http.ResponseWriter, r *http.Request) {
	e, ok := h.engineOf(w, r)
	if !ok {
		return
	}
	pf, err := h.d.Engines.ProcessForms(r.Context(), e, r.PathValue("process"))
	if err != nil {
		h.engineFailed(w, e, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{"engine": h.engineJSON(e, false), "process": pf})
}

// publicLinks lists the start links to those who may edit in a space, who
// place them on pages; a reader of a page reads the link its block names.
func (h engineHandlers) publicLinks(w http.ResponseWriter, r *http.Request) {
	p, _ := PrincipalFrom(r.Context())
	may := false
	h.d.DB.Read(func(s *item.Store) { may = s.As(string(p.User)).EditsASpace() })
	if !may {
		writeError(w, http.StatusForbidden, "only those who may edit in a space list the start links of an engine")
		return
	}
	e, ok := h.engineOf(w, r)
	if !ok {
		return
	}
	list, err := h.d.Engines.PublicLinks(r.Context(), e)
	if err != nil {
		h.engineFailed(w, e, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{"engine": h.engineJSON(e, false), "links": list})
}

// linkOf returns the start link a request names, or answers that it is no
// start link: the engine's own token, so that nothing else reaches its
// path.
func linkOf(w http.ResponseWriter, r *http.Request) (string, bool) {
	link := r.PathValue("link")
	if !item.ValidStartLink(link) {
		writeError(w, http.StatusBadRequest, "link "+strconv.Quote(link)+": a start link is the engine's token, 32 to 128 lower-case hexadecimal digits")
		return "", false
	}
	return link, true
}

func (h engineHandlers) publicForm(w http.ResponseWriter, r *http.Request) {
	e, ok := h.engineOf(w, r)
	if !ok {
		return
	}
	link, ok := linkOf(w, r)
	if !ok {
		return
	}
	f, err := h.d.Engines.PublicForm(r.Context(), e, link, r.URL.Query().Get("process"), clientAddr(r))
	if err != nil {
		h.engineFailed(w, e, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{"engine": h.engineJSON(e, false), "form": f})
}

// startPublic sends a start link's form for the person who filled it in.
// The engine records nobody as its initiator; the audit trail records who
// sent it, and the link only by its first eight digits, which start
// nothing.
func (h engineHandlers) startPublic(w http.ResponseWriter, r *http.Request) {
	e, ok := h.engineOf(w, r)
	if !ok {
		return
	}
	link, ok := linkOf(w, r)
	if !ok {
		return
	}
	var req struct {
		Variables json.RawMessage `json:"variables"`
		Process   string          `json:"process"`
	}
	if err := readJSON(w, r, &req); err != nil {
		badRequest(w, err)
		return
	}
	if v := bytes.TrimSpace(req.Variables); len(v) == 0 || v[0] != '{' {
		writeError(w, http.StatusBadRequest, "variables: the form's values are a JSON object")
		return
	}
	p, _ := PrincipalFrom(r.Context())
	addr := clientAddr(r)
	started, err := h.d.Engines.StartPublic(r.Context(), e, link, req.Process, req.Variables, addr)
	if err != nil {
		h.engineFailed(w, e, err)
		return
	}
	h.record(audit.Entry{Event: audit.EngineStarted, Actor: string(p.User), Addr: addr, Subject: e.ID, Detail: map[string]string{"link": link[:8]}})
	writeJSON(w, http.StatusOK, map[string]any{"started": true, "wizard": started.Wizard})
}

func (h engineHandlers) decisions(w http.ResponseWriter, r *http.Request) {
	e, ok := h.engineOf(w, r)
	if !ok {
		return
	}
	list, err := h.d.Engines.Decisions(r.Context(), e)
	if err != nil {
		h.engineFailed(w, e, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"engine": h.engineJSON(e, false), "decisions": list})
}

// decision answers the DMN model as a field of JSON, so that it is never a
// document of this site.
func (h engineHandlers) decision(w http.ResponseWriter, r *http.Request) {
	e, ok := h.engineOf(w, r)
	if !ok {
		return
	}
	d, err := h.d.Engines.DecisionModel(r.Context(), e, r.PathValue("decision"))
	if err != nil {
		h.engineFailed(w, e, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{"engine": h.engineJSON(e, false), "decision": d})
}

func (h engineHandlers) architectures(w http.ResponseWriter, r *http.Request) {
	e, ok := h.engineOf(w, r)
	if !ok {
		return
	}
	list, err := h.d.Engines.Architectures(r.Context(), e)
	if err != nil {
		h.engineFailed(w, e, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"engine": h.engineJSON(e, false), "applications": list})
}

// sketchPolicy is the content security policy of an architecture sketch,
// that of the engine's answer: the picture's own styles, nothing that
// runs, nothing loaded (ADR-0146).
const sketchPolicy = "default-src 'none'; style-src 'unsafe-inline'; sandbox"

// architecture answers a sketch as a picture of this site, which the UI
// shows as an image under its own policy and a new tab opens under the
// picture's; it is read anew each time and not kept.
func (h engineHandlers) architecture(w http.ResponseWriter, r *http.Request) {
	e, ok := h.engineOf(w, r)
	if !ok {
		return
	}
	var release int32
	if v := r.PathValue("release"); v != "newest" {
		n, err := strconv.ParseInt(v, 10, 32)
		if err != nil || n < 1 {
			writeError(w, http.StatusBadRequest, "release "+strconv.Quote(v)+": a release is newest or its version, a positive number")
			return
		}
		release = int32(n)
	}
	var layers []string
	if v := r.URL.Query().Get("layers"); v != "" {
		for _, l := range strings.Split(v, ",") {
			if !slices.Contains(item.ArchitectureLayers, l) {
				writeError(w, http.StatusBadRequest, "layer "+strconv.Quote(l)+": the layers are "+strings.Join(item.ArchitectureLayers, ", "))
				return
			}
		}
		// Each once, in the order the engine draws them, as a block names
		// them.
		for _, l := range item.ArchitectureLayers {
			if slices.Contains(strings.Split(v, ","), l) {
				layers = append(layers, l)
			}
		}
	}
	svg, rel, err := h.d.Engines.ArchitectureSketch(r.Context(), e, r.PathValue("application"), release, layers)
	if err != nil {
		h.engineFailed(w, e, err)
		return
	}
	hd := w.Header()
	hd.Set("Content-Type", "image/svg+xml")
	hd.Set("X-Content-Type-Options", "nosniff")
	hd.Set("Content-Security-Policy", sketchPolicy)
	hd.Set("Cache-Control", "private, no-store")
	hd.Set("X-Release-Version", strconv.Itoa(int(rel.Version)))
	hd.Set("X-Release-Taken-At", rel.TakenAt.UTC().Format(time.RFC3339))
	hd.Set("Content-Length", strconv.Itoa(len(svg)))
	_, _ = w.Write(svg)
}
