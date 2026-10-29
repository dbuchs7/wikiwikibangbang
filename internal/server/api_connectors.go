package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/dbuchs7/wikiwikibangbang/internal/audit"
	"github.com/dbuchs7/wikiwikibangbang/internal/auth"
	"github.com/dbuchs7/wikiwikibangbang/internal/connector"
	"github.com/dbuchs7/wikiwikibangbang/internal/item"
	"github.com/dbuchs7/wikiwikibangbang/internal/site"
)

// connectorRoutes set up the connectors that keep catalog spaces in step
// with external sources, and show how their runs went (ADR-0077).
func connectorRoutes(d Deps) []Route {
	h := authHandlers{d: d}
	return []Route{
		{Method: http.MethodGet, Pattern: "/api/connectors", Summary: "The connectors with their settings and last runs, and the templates to start one from (administrators)", Handler: http.HandlerFunc(h.connectors)},
		{Method: http.MethodPost, Pattern: "/api/connectors", Summary: "Add a connector: ID, name, kind, space, interval, steward and the settings of its kind (administrators)", Handler: http.HandlerFunc(h.addConnector)},
		{Method: http.MethodPut, Pattern: "/api/connectors/{id}", Summary: "Change a connector's settings; its ID stays (administrators)", Handler: http.HandlerFunc(h.setConnector)},
		{Method: http.MethodDelete, Pattern: "/api/connectors/{id}", Summary: "Remove a connector and its stored credentials; the entries it wrote stay, with their origin (administrators)", Handler: http.HandlerFunc(h.removeConnector)},
		{Method: http.MethodPost, Pattern: "/api/connectors/{id}/run", Summary: "Run a connector now, in the background; dryRun reports what it would change without writing; full reads in full a source that is otherwise read for what changed (administrators)", Handler: http.HandlerFunc(h.runConnector)},
		{Method: http.MethodPost, Pattern: "/api/connectors/{id}/release", Summary: "Cut over from an Atlassian import: its items become ordinary items people change, and its schedule is turned off (administrators)", Handler: http.HandlerFunc(h.releaseConnector)},
		{Method: http.MethodGet, Pattern: "/api/connectors/{id}/openapi", Summary: "The lists an API description at url (OpenAPI 3 or Swagger 2.0, JSON or YAML) offers, each as the settings of a JSON source; the connector's credential is sent (administrators)", Handler: http.HandlerFunc(h.connectorOpenAPI)},
		{Method: http.MethodGet, Pattern: "/api/connectors/{id}/formats", Summary: "The resource formats a CKAN source's data sets use, with how many data sets use each, to choose the interfaces from; baseURL and organizations as typed, else as saved (administrators)", Handler: http.HandlerFunc(h.connectorFormats)},
	}
}

func (h authHandlers) connectors(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.adminOnly(w, r); !ok {
		return
	}
	type entry struct {
		site.Connector
		Status connector.Status `json:"status"`
	}
	list := []entry{}
	for _, c := range h.d.DB.Site.Connectors() {
		list = append(list, entry{Connector: c, Status: h.d.Connectors.Status(c.ID)})
	}
	// The types an item can be written as, with the fields a connector
	// can fill (ADR-0127).
	type field struct {
		Name     string         `json:"name"`
		Kind     item.FieldKind `json:"kind"`
		Required bool           `json:"required"`
	}
	type target struct {
		Name   string  `json:"name"`
		Fields []field `json:"fields"`
	}
	types := []target{}
	h.d.DB.Read(func(s *item.Store) {
		for _, td := range s.Types() {
			if !td.Body {
				continue
			}
			t := target{Name: td.Name, Fields: []field{}}
			for _, f := range td.Fields {
				t.Fields = append(t.Fields, field{Name: f.Name, Kind: f.Kind, Required: f.Required})
			}
			types = append(types, t)
		}
	})
	writeJSON(w, http.StatusOK, map[string]any{
		"connectors": list,
		"templates":  site.ConnectorTemplates(),
		"kinds":      site.ConnectorKinds,
		"type":       connector.PageType,
		"types":      types,
		"ckanValues": site.CKANValues,
		"gitValues":  site.GitValues,
	})
}

// checkConnector refuses a space that does not exist and a steward who
// is not a user, which the settings cannot know.
func (h authHandlers) checkConnector(w http.ResponseWriter, c site.Connector) bool {
	space := strings.TrimSpace(c.Space)
	exists := true
	if space != "" {
		h.d.DB.Read(func(s *item.Store) { exists = s.HasSpace(space) })
	}
	if !exists {
		writeError(w, http.StatusBadRequest, "the space "+space+" does not exist; create it first")
		return false
	}
	// An Atlassian import writes as its mapping says (ADR-0128); the
	// accounts it names must exist.
	if c.Kind == site.KindAtlassian {
		if err := connector.CheckAccounts(h.d.DB.Accounts, c); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return false
		}
		return true
	}
	// The type must take what the connector writes; page is defined by
	// the first run if the site has none (ADR-0127).
	var err error
	h.d.DB.Read(func(s *item.Store) {
		target := connector.TargetType(c)
		switch td := s.Type(target); {
		case td.Name != "":
			err = connector.Fits(td, c)
		case target != connector.PageType || len(c.Fields) > 0:
			err = fmt.Errorf("the type %s does not exist", target)
		}
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return false
	}
	if c.Steward != "" {
		if _, ok := h.d.DB.Accounts.Usernames([]auth.UserID{auth.UserID(c.Steward)})[auth.UserID(c.Steward)]; !ok {
			writeError(w, http.StatusBadRequest, "the steward is not a user")
			return false
		}
	}
	return true
}

func (h authHandlers) addConnector(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.adminOnly(w, r)
	if !ok {
		return
	}
	var c site.Connector
	if err := readJSON(w, r, &c); err != nil {
		badRequest(w, err)
		return
	}
	if _, exists := h.d.DB.Site.Connector(strings.TrimSpace(c.ID)); exists {
		writeError(w, http.StatusConflict, "a connector "+c.ID+" exists")
		return
	}
	h.keepConnector(w, r, actor, c, http.StatusCreated)
}

func (h authHandlers) setConnector(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.adminOnly(w, r)
	if !ok {
		return
	}
	var c site.Connector
	if err := readJSON(w, r, &c); err != nil {
		badRequest(w, err)
		return
	}
	c.ID = r.PathValue("id")
	if _, exists := h.d.DB.Site.Connector(c.ID); !exists {
		writeError(w, http.StatusNotFound, "no connector "+c.ID)
		return
	}
	h.keepConnector(w, r, actor, c, http.StatusOK)
}

func (h authHandlers) keepConnector(w http.ResponseWriter, r *http.Request, actor string, c site.Connector, status int) {
	if !h.checkConnector(w, c) {
		return
	}
	kept, err := h.d.DB.Site.SetConnector(actor, c)
	if err != nil {
		if errors.Is(err, site.ErrInvalid) {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "connector not saved")
		return
	}
	detail := map[string]string{"connector": kept.ID, "kind": kept.Kind, "enabled": strconv.FormatBool(kept.Enabled), "intervalHours": strconv.Itoa(kept.IntervalHours)}
	if k := kept.CKAN; k != nil {
		detail["source"], detail["formats"], detail["organizations"] = k.BaseURL, strings.Join(k.Formats, " "), strings.Join(k.Organizations, " ")
	}
	if a := kept.Atlassian; a != nil {
		detail["source"], detail["spaces"], detail["projects"], detail["grants"] = a.Site, strings.Join(a.Spaces, " "), strings.Join(a.Projects, " "), strings.Join(a.Grants, " ")
	}
	if g := kept.Git; g != nil {
		detail["source"] = g.URL
	}
	h.record(audit.Entry{Event: audit.CatalogSyncChanged, Actor: actor, Addr: clientAddr(r), Subject: kept.Space, Detail: detail})
	writeJSON(w, status, kept)
}

func (h authHandlers) removeConnector(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.adminOnly(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	c, exists := h.d.DB.Site.Connector(id)
	if !exists {
		writeError(w, http.StatusNotFound, "no connector "+id)
		return
	}
	if h.d.Connectors.Status(id).Running {
		writeError(w, http.StatusConflict, "the connector is running")
		return
	}
	if err := h.d.DB.Site.RemoveConnector(actor, id); err != nil {
		writeError(w, http.StatusInternalServerError, "connector not removed")
		return
	}
	h.d.Connectors.Forget(id)
	h.record(audit.Entry{Event: audit.CatalogSyncChanged, Actor: actor, Addr: clientAddr(r), Subject: c.Space, Detail: map[string]string{"connector": id, "removed": "true"}})
	h.dropCredentials(r, actor, connector.TokenVar(id), connector.EmailVar(id))
	w.WriteHeader(http.StatusNoContent)
}

// releaseConnector is the cut-over from an Atlassian import (ADR-0128).
func (h authHandlers) releaseConnector(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.adminOnly(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	spaces, err := h.d.Connectors.ReleaseAtlassian(id, actor)
	switch {
	case errors.Is(err, connector.ErrUnknown):
		writeError(w, http.StatusNotFound, "no connector "+id)
	case errors.Is(err, connector.ErrBusy):
		writeError(w, http.StatusConflict, "the connector is running")
	case errors.Is(err, connector.ErrNotManaging):
		writeError(w, http.StatusConflict, err.Error())
	case err != nil:
		writeError(w, http.StatusInternalServerError, "the items were not released")
	default:
		if spaces == nil {
			spaces = []string{}
		}
		h.record(audit.Entry{Event: audit.CatalogSyncChanged, Actor: actor, Addr: clientAddr(r), Subject: id,
			Detail: map[string]string{"connector": id, "released": strings.Join(spaces, " "), "enabled": "false"}})
		writeJSON(w, http.StatusOK, map[string]any{"released": spaces})
	}
}

func (h authHandlers) runConnector(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.adminOnly(w, r)
	if !ok {
		return
	}
	var req struct {
		DryRun bool `json:"dryRun"`
		// Full reads the source in full, not only what changed (ADR-0140).
		Full bool `json:"full"`
	}
	if err := readJSON(w, r, &req); err != nil {
		badRequest(w, err)
		return
	}
	id := r.PathValue("id")
	c, exists := h.d.DB.Site.Connector(id)
	if !exists {
		writeError(w, http.StatusNotFound, "no connector "+id)
		return
	}
	if c.Space == "" && c.Kind != site.KindAtlassian {
		writeError(w, http.StatusConflict, "set the connector's space first")
		return
	}
	if req.Full && !req.DryRun && !h.d.Connectors.Status(id).Running {
		h.d.Connectors.ReadInFull(id)
	}
	err := h.d.Connectors.Start(id, req.DryRun, actor)
	switch {
	case errors.Is(err, connector.ErrBusy):
		writeError(w, http.StatusConflict, "the connector is running")
	case err != nil:
		writeError(w, http.StatusInternalServerError, "the connector did not start")
	default:
		w.WriteHeader(http.StatusAccepted)
	}
}

// formatsTimeout bounds asking the source for its formats.
const formatsTimeout = 30 * time.Second

// connectorOpenAPI reads an API description for an administrator to pick
// a list from (ADR-0130); the connector need not be saved yet.
func (h authHandlers) connectorOpenAPI(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.adminOnly(w, r); !ok {
		return
	}
	addr := strings.TrimSpace(r.URL.Query().Get("url"))
	probe := site.JSONSource{URL: addr, ID: "id", Title: "title", Spec: addr}
	c := site.Connector{ID: r.PathValue("id"), Name: "x", Kind: site.KindJSON, IntervalHours: 1, JSON: &probe}
	if err := c.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, "the description's address: "+err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), formatsTimeout)
	defer cancel()
	d, err := h.d.Connectors.DescribeOpenAPI(ctx, c.ID, addr)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, d)
}

func (h authHandlers) connectorFormats(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.adminOnly(w, r); !ok {
		return
	}
	// The source and organizations as typed, so they can be chosen before
	// they are saved; the saved ones otherwise. A connector not yet saved
	// starts from the first template.
	id := r.PathValue("id")
	c, exists := h.d.DB.Site.Connector(id)
	if !exists {
		c = site.ConnectorTemplates()[0]
		c.ID = id
	}
	if c.CKAN == nil {
		writeError(w, http.StatusBadRequest, "the connector does not read a CKAN source")
		return
	}
	q := r.URL.Query()
	if q.Has("baseURL") {
		c.CKAN.BaseURL = q.Get("baseURL")
	}
	if q.Has("organizations") {
		c.CKAN.Organizations = strings.FieldsFunc(q.Get("organizations"), func(c rune) bool { return c == ',' || c == ' ' || c == ';' })
	}
	c.Enabled = false
	if err := c.Normalize().Validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), formatsTimeout)
	defer cancel()
	formats, err := h.d.Connectors.SourceFormats(ctx, c.ID, c.CKAN.BaseURL, c.CKAN.Organizations)
	if errors.Is(err, connector.ErrUnknownOrganization) {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"formats": formats})
}
