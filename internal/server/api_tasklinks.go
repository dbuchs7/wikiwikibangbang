package server

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/dbuchs7/wikiwikibangbang/internal/audit"
	"github.com/dbuchs7/wikiwikibangbang/internal/engine"
	"github.com/dbuchs7/wikiwikibangbang/internal/item"
	"github.com/dbuchs7/wikiwikibangbang/internal/site"
	"github.com/dbuchs7/wikiwikibangbang/internal/tasklink"
)

// taskLinkRoutes set the rules by which an engine's user tasks become
// linked tasks of the wiki, and run a round of them now (ADR-0151). All are
// for administrators.
func taskLinkRoutes(d Deps) []Route {
	h := taskLinkHandlers{authHandlers: authHandlers{d: d}}
	return []Route{
		{Method: http.MethodGet, Pattern: "/api/engines/{id}/task-links", Summary: "An engine's task links: whether they are switched on, their rules, the variable of the credential the open tasks are listed with and whether it is set, and the last round's report (administrators; ADR-0151)", Handler: http.HandlerFunc(h.get)},
		{Method: http.MethodPut, Pattern: "/api/engines/{id}/task-links", Summary: "Set an engine's task links: whether they are switched on and their rules, each a filter by processes, elements and groups, a space and a type with a workflow, labels, a date field for the due date and the finished states of a completed and a cancelled task (administrators; ADR-0151)", Handler: http.HandlerFunc(h.set)},
		{Method: http.MethodPost, Pattern: "/api/engines/{id}/task-links/run", Summary: "Run a round of an engine's task links now and answer its report; with dry=true, report what it would do and change nothing (administrators; ADR-0151)", Handler: http.HandlerFunc(h.run)},
	}
}

type taskLinkHandlers struct {
	authHandlers
}

// taskLinksJSON is an engine's task links as an administrator sees them.
type taskLinksJSON struct {
	site.TaskLinks
	// CredentialVar names the variable the open tasks are listed with;
	// Credential says whether it is set, never what it holds.
	CredentialVar string           `json:"credentialVar"`
	Credential    bool             `json:"credential"`
	Last          *tasklink.Report `json:"last,omitempty"`
	// Connect is how people connect their accounts to the engine.
	Connect connectJSON `json:"connect"`
}

// connectJSON is this server's registration at an engine, through which
// people connect: the variables of its client ID and secret, whether both
// are set, the address the engine sends people back to, which its
// administrator registers, and how many people are connected.
type connectJSON struct {
	ClientIDVar     string `json:"clientIdVar"`
	ClientSecretVar string `json:"clientSecretVar"`
	Configured      bool   `json:"configured"`
	Callback        string `json:"callback"`
	Connected       int    `json:"connected"`
}

func (h taskLinkHandlers) engine(w http.ResponseWriter, r *http.Request) (string, site.Engine, bool) {
	actor, ok := h.adminOnly(w, r)
	if !ok {
		return "", site.Engine{}, false
	}
	e, exists := h.d.DB.Site.Engine(r.PathValue("id"))
	if !exists {
		writeError(w, http.StatusNotFound, "no engine "+r.PathValue("id"))
		return "", site.Engine{}, false
	}
	return actor, e, true
}

func (h taskLinkHandlers) json(e site.Engine) taskLinksJSON {
	out := taskLinksJSON{TaskLinks: h.d.DB.Site.TaskLinks(e.ID), CredentialVar: engine.TaskListVar(e.ID),
		Credential: h.d.Engines.HasTaskListCredential(e.ID),
		Connect: connectJSON{ClientIDVar: engine.ClientIDVar(e.ID), ClientSecretVar: engine.ClientSecretVar(e.ID), Configured: h.d.Connect.Configured(e.ID),
			Callback: h.d.Connect.Callback(e.ID), Connected: len(h.d.Connect.Connections().Of("", e.ID))}}
	if last, ok := h.d.TaskLinks.Last(e.ID); ok {
		out.Last = &last
	}
	return out
}

func (h taskLinkHandlers) get(w http.ResponseWriter, r *http.Request) {
	if _, e, ok := h.engine(w, r); ok {
		writeJSON(w, http.StatusOK, h.json(e))
	}
}

func (h taskLinkHandlers) set(w http.ResponseWriter, r *http.Request) {
	actor, e, ok := h.engine(w, r)
	if !ok {
		return
	}
	var l site.TaskLinks
	if err := readJSON(w, r, &l); err != nil {
		badRequest(w, err)
		return
	}
	l.Engine = e.ID
	l = l.Normalize()
	if err := l.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	// Every rule's items must be ones the synchronisation can make and end.
	var reach error
	h.d.DB.Read(func(s *item.Store) {
		for _, rule := range l.Rules {
			var owned []string
			if rule.DueField != "" {
				owned = []string{rule.DueField}
				if f, ok := fieldOf(s.Type(rule.Type), rule.DueField); ok && f.Kind != item.KindDate {
					reach = fmt.Errorf("the due date goes into %q, which is no date field", rule.DueField)
					return
				}
			}
			if err := s.TaskReach(rule.Space, rule.Type, rule.Completed, rule.Cancelled, owned); err != nil {
				reach = fmt.Errorf("rule %s: %w", rule.ID, err)
				return
			}
		}
	})
	if reach != nil {
		writeError(w, http.StatusBadRequest, reach.Error())
		return
	}
	if _, err := h.d.DB.Site.SetTaskLinks(actor, l); err != nil {
		if errors.Is(err, site.ErrInvalid) {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "task links not saved")
		return
	}
	h.record(audit.Entry{Event: audit.TaskLinksChanged, Actor: actor, Addr: clientAddr(r), Subject: e.ID,
		Detail: map[string]string{"enabled": strconv.FormatBool(l.Enabled), "rules": strconv.Itoa(len(l.Rules))}})
	writeJSON(w, http.StatusOK, h.json(e))
}

func (h taskLinkHandlers) run(w http.ResponseWriter, r *http.Request) {
	actor, e, ok := h.engine(w, r)
	if !ok {
		return
	}
	dry := r.URL.Query().Get("dry") == "true"
	rep, err := h.d.TaskLinks.Run(r.Context(), e.ID, dry)
	if errors.Is(err, tasklink.ErrBusy) {
		writeError(w, http.StatusConflict, "a round of these task links is running")
		return
	}
	if !dry {
		h.record(audit.Entry{Event: audit.TaskLinksRun, Actor: actor, Addr: clientAddr(r), Subject: e.ID,
			Detail: map[string]string{"linked": strconv.Itoa(rep.Linked), "updated": strconv.Itoa(rep.Updated), "ended": strconv.Itoa(rep.Ended)}})
	}
	// A round that could not read the engine is answered with its report,
	// which says why: the request itself succeeded.
	writeJSON(w, http.StatusOK, rep)
}

// fieldOf is the field of a type by its name.
func fieldOf(td item.TypeDef, name string) (item.FieldDef, bool) {
	for _, f := range td.Fields {
		if f.Name == name {
			return f, true
		}
	}
	return item.FieldDef{}, false
}
