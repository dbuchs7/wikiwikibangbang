package server

import (
	"net/http"

	"github.com/dbuchs7/wikiwikibangbang/internal/audit"
	"github.com/dbuchs7/wikiwikibangbang/internal/connector"
	"github.com/dbuchs7/wikiwikibangbang/internal/credential"
	"github.com/dbuchs7/wikiwikibangbang/internal/engine"
	"github.com/dbuchs7/wikiwikibangbang/internal/site"
)

// credentialRoutes let administrators store the credentials of the
// process engines and the connectors on the administration page, and
// remove them (ADR-0151). A stored value is written, never answered; the
// environment, a variable or the file it names, takes precedence.
func credentialRoutes(d Deps) []Route {
	h := authHandlers{d: d}
	return []Route{
		{Method: http.MethodPut, Pattern: "/api/credentials/{name}", Summary: "Store a credential of a process engine or a connector, named as its variable (WWBB_ENGINE_<ID>_TOKEN, WWBB_CONNECTOR_<ID>_TOKEN, WWBB_CONNECTOR_<ID>_EMAIL), with {\"value\": ...}; it takes effect at its next use. Refused while the environment sets it as the variable or the file <name>_FILE names; the value is never answered (administrators, ADR-0151)", Handler: http.HandlerFunc(h.setCredential)},
		{Method: http.MethodDelete, Pattern: "/api/credentials/{name}", Summary: "Remove a stored credential; its engine or connector then has none unless the environment sets it (administrators, ADR-0151)", Handler: http.HandlerFunc(h.removeCredential)},
	}
}

// credentialResolver returns how the credential name is resolved, by the
// engine or the connector it belongs to, or false when it belongs to
// none: only those credentials are stored.
func (h authHandlers) credentialResolver(name string) (*credential.Resolver, bool) {
	for _, e := range h.d.DB.Site.Engines() {
		if engine.TokenVar(e.ID) == name {
			return h.d.Engines.Credentials(), true
		}
	}
	for _, c := range h.d.DB.Site.Connectors() {
		if connector.TokenVar(c.ID) == name || c.Kind == site.KindAtlassian && connector.EmailVar(c.ID) == name {
			return h.d.Connectors.Credentials(), true
		}
	}
	return nil, false
}

func (h authHandlers) setCredential(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.adminOnly(w, r)
	if !ok {
		return
	}
	name := r.PathValue("name")
	resolver, ok := h.credentialResolver(name)
	if !ok {
		writeError(w, http.StatusNotFound, "no engine or connector has a credential named "+name)
		return
	}
	if resolver.InEnvironment(name) {
		writeError(w, http.StatusConflict, "the server's environment sets "+name+", as the variable or as the file "+credential.FileVar(name)+
			" names, and wins over a stored value; remove it there to store it here")
		return
	}
	var req struct {
		Value string `json:"value"`
	}
	if err := readJSON(w, r, &req); err != nil {
		badRequest(w, err)
		return
	}
	value, err := credential.Clean(req.Value)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.d.DB.SetCredential(name, value, actor); err != nil {
		writeError(w, http.StatusInternalServerError, "credential not stored")
		return
	}
	h.record(audit.Entry{Event: audit.CredentialSet, Actor: actor, Addr: clientAddr(r), Subject: name})
	writeJSON(w, http.StatusOK, resolver.State(name))
}

func (h authHandlers) removeCredential(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.adminOnly(w, r)
	if !ok {
		return
	}
	name := r.PathValue("name")
	gone, err := h.d.DB.RemoveCredential(name)
	switch {
	case err != nil:
		writeError(w, http.StatusInternalServerError, "credential not removed")
		return
	case !gone:
		writeError(w, http.StatusNotFound, "no credential "+name+" is stored")
		return
	}
	h.record(audit.Entry{Event: audit.CredentialRemoved, Actor: actor, Addr: clientAddr(r), Subject: name})
	w.WriteHeader(http.StatusNoContent)
}

// dropCredentials removes the stored credentials of an engine or a
// connector that was removed, unless another one has a credential of the
// same name (ADR-0151). The removal it follows stands either way.
func (h authHandlers) dropCredentials(r *http.Request, actor string, names ...string) {
	for _, name := range names {
		if _, inUse := h.credentialResolver(name); inUse {
			continue
		}
		gone, err := h.d.DB.RemoveCredential(name)
		if err != nil && h.d.Log != nil {
			h.d.Log.Warn("the stored credential of a removed engine or connector was not removed", "credential", name, "err", err)
		}
		if gone && err == nil {
			h.record(audit.Entry{Event: audit.CredentialRemoved, Actor: actor, Addr: clientAddr(r), Subject: name,
				Detail: map[string]string{"reason": "its engine or connector was removed"}})
		}
	}
}
