package server

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/dbuchs7/wikiwikibangbang/internal/audit"
	"github.com/dbuchs7/wikiwikibangbang/internal/engine"
	"github.com/dbuchs7/wikiwikibangbang/internal/site"
)

// engineRoutes name the process engines whose processes pages show, and
// read those processes from them when a page is shown (ADR-0129), and
// their forms and decisions (ADR-0144). Every signed-in principal reads;
// which processes, forms and decisions it reads is decided by the engine's
// account behind the credential.
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
		{Method: http.MethodGet, Pattern: "/api/engines/{id}/forms/{form}", Summary: "A form as it is saved, with its form-js schema, read from the engine now", Handler: http.HandlerFunc(h.form)},
		{Method: http.MethodGet, Pattern: "/api/engines/{id}/decisions", Summary: "The newest deployed version of each decision the engine lets its credential read, read from the engine now (ADR-0144)", Handler: http.HandlerFunc(h.decisions)},
		{Method: http.MethodGet, Pattern: "/api/engines/{id}/decisions/{decision}", Summary: "The newest deployed version of a decision with the DMN model it is deployed in, read from the engine now", Handler: http.HandlerFunc(h.decision)},
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
	_, existed := h.d.DB.Site.Engine(e.ID)
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
	if errors.Is(err, engine.ErrNotFound) {
		writeError(w, http.StatusNotFound, msg)
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
	f, err := h.d.Engines.Form(r.Context(), e, r.PathValue("form"))
	if err != nil {
		h.engineFailed(w, e, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{"engine": h.engineJSON(e, false), "form": f})
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
