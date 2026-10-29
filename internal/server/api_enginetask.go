package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/dbuchs7/wikiwikibangbang/internal/engine"
	"github.com/dbuchs7/wikiwikibangbang/internal/engineauth"
	"github.com/dbuchs7/wikiwikibangbang/internal/engineconn"
	"github.com/dbuchs7/wikiwikibangbang/internal/item"
	"github.com/dbuchs7/wikiwikibangbang/internal/site"
	"github.com/dbuchs7/wikiwikibangbang/internal/tasklink"
)

// engineTaskRoutes work the task of a linked item at its engine, as the
// person, with their own grant (ADR-0151): the engine decides whether they
// may. The wiki's edit right on the item is not asked; reading it is.
func engineTaskRoutes(d Deps) []Route {
	h := engineTaskHandlers{authHandlers: authHandlers{d: d}}
	return []Route{
		{Method: http.MethodGet, Pattern: "/api/items/{ref}/engine-task", Summary: "The task of a linked item as one sees it at its engine, with one's own grant: who holds it, its form and the values the form opens with, and the item's states for a completed and a cancelled task; connected is false until one connects to the engine (ADR-0151)", Handler: http.HandlerFunc(h.get)},
		{Method: http.MethodPost, Pattern: "/api/items/{ref}/engine-task/claim", Summary: "Take the task of a linked item on at its engine, as oneself (ADR-0151)", Handler: http.HandlerFunc(h.claim)},
		{Method: http.MethodPost, Pattern: "/api/items/{ref}/engine-task/unclaim", Summary: "Give the task of a linked item back at its engine (ADR-0151)", Handler: http.HandlerFunc(h.unclaim)},
		{Method: http.MethodPost, Pattern: "/api/items/{ref}/engine-task/complete", Summary: "Complete the task of a linked item at its engine, as oneself, with the values of its form ({\"variables\": {…}}); once the engine accepts, the item moves to the state of a completed task (ADR-0151)", Handler: http.HandlerFunc(h.complete)},
	}
}

type engineTaskHandlers struct {
	authHandlers
}

// linkedTask is what a request about a linked item's task works with.
type linkedTask struct {
	user   string
	it     item.Item
	e      site.Engine
	key    uint64
	rule   site.TaskRule
	ruled  bool
	conn   engineconn.Connection
	linked bool // the person is connected
}

// of finds the linked item the request names, as the caller reads it
// (D10), its engine and the caller's connection to it.
func (h engineTaskHandlers) of(w http.ResponseWriter, r *http.Request) (linkedTask, bool) {
	p, _ := PrincipalFrom(r.Context())
	var it item.Item
	var err error
	h.d.DB.Read(func(s *item.Store) { it, err = resolveItem(s.As(who(r)), r.PathValue("ref")) })
	if errors.Is(err, item.ErrNotFound) || err == nil && (it.Linked == "" || it.Origin == nil) {
		writeError(w, http.StatusNotFound, "no linked task "+r.PathValue("ref"))
		return linkedTask{}, false
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return linkedTask{}, false
	}
	e, ok := h.d.DB.Site.Engine(it.Linked)
	key, kerr := strconv.ParseUint(it.Origin.ExternalKey, 10, 64)
	if !ok || kerr != nil {
		writeError(w, http.StatusNotFound, "the engine of "+it.Key+" is not set up")
		return linkedTask{}, false
	}
	lt := linkedTask{user: string(p.User), it: it, e: e, key: key}
	lt.rule, lt.ruled = tasklink.RuleOf(h.d.DB.Site.TaskLinks(e.ID).Rules, it)
	lt.conn, lt.linked = h.d.Connect.Connections().Get(lt.user, e.ID)
	return lt, true
}

// token is the access token of the caller's grant, or answers why there
// is none.
func (h engineTaskHandlers) token(w http.ResponseWriter, r *http.Request, lt linkedTask) (string, bool) {
	if !lt.linked {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "connect your account to " + lt.e.Name + " first", "connect": true})
		return "", false
	}
	tok, err := h.d.Connect.Token(r.Context(), lt.e, lt.user)
	switch {
	case errors.Is(err, engineauth.ErrEnded), errors.Is(err, engineconn.ErrNotConnected):
		writeJSON(w, http.StatusConflict, map[string]any{"error": lt.e.Name + " no longer accepts your connection; connect again", "connect": true})
		return "", false
	case err != nil:
		writeError(w, http.StatusBadGateway, "engine "+lt.e.ID+": "+err.Error())
		return "", false
	}
	return tok, true
}

// failed answers an engine's refusal of a person's work in its words.
func (h engineTaskHandlers) failed(w http.ResponseWriter, lt linkedTask, err error) {
	msg := "engine " + lt.e.ID + ": " + err.Error()
	switch {
	case errors.Is(err, engine.ErrNotAllowed):
		writeError(w, http.StatusForbidden, msg)
	case errors.Is(err, engine.ErrRejected):
		writeError(w, http.StatusUnprocessableEntity, msg)
	case errors.Is(err, engine.ErrNotFound):
		// Ended at the engine, or not the person's to see: the next round
		// tells which.
		h.d.TaskLinks.Nudge(lt.e.ID)
		writeError(w, http.StatusConflict, "the task is no longer open at "+lt.e.Name+", or not yours to see")
	case errors.Is(err, engine.ErrRefused):
		h.d.Connect.Refused(lt.user, lt.e.ID)
		writeJSON(w, http.StatusConflict, map[string]any{"error": lt.e.Name + " refused your connection; try again, or connect again", "connect": true})
	case errors.Is(err, engine.ErrBusy):
		w.Header().Set("Retry-After", "5")
		writeError(w, http.StatusServiceUnavailable, msg)
	default:
		writeError(w, http.StatusBadGateway, msg)
	}
}

// engineTaskJSON is a linked item's task as its person sees it.
type engineTaskJSON struct {
	Engine    map[string]string `json:"engine"`
	Connected bool              `json:"connected"`
	// Open is false when the engine answers no open task: it ended, or is
	// not the person's to see.
	Open bool `json:"open"`
	// TaskApp is where the engine shows the task in its own app.
	TaskApp string    `json:"taskApp"`
	Task    *taskJSON `json:"task,omitempty"`
	// Mine says the person holds it.
	Mine bool `json:"mine"`
	// Form is the task's form, with its form-js schema; Values what it
	// opens with.
	Form   *engine.FormSchema `json:"form,omitempty"`
	Values json.RawMessage    `json:"values,omitempty"`
	// Completed and Cancelled are the item's states for an ended task.
	Completed string `json:"completed,omitempty"`
	Cancelled string `json:"cancelled,omitempty"`
	// Fresh is when the engine's tasks were last read in full.
	Fresh freshJSON `json:"fresh"`
}

type taskJSON struct {
	Key      string   `json:"key"`
	Name     string   `json:"name"`
	Assignee string   `json:"assignee,omitempty"`
	Groups   []string `json:"groups,omitempty"`
	Form     string   `json:"form,omitempty"`
	Due      string   `json:"due,omitempty"`
}

func (h engineTaskHandlers) get(w http.ResponseWriter, r *http.Request) {
	lt, ok := h.of(w, r)
	if !ok {
		return
	}
	out := engineTaskJSON{Engine: map[string]string{"id": lt.e.ID, "name": lt.e.Name}, Connected: lt.linked, TaskApp: engine.TaskURL(lt.e, lt.key),
		Fresh: freshOf(h.d, lt.e.ID)}
	if lt.ruled {
		out.Completed, out.Cancelled = lt.rule.Completed, lt.rule.Cancelled
	}
	if !lt.linked {
		writeJSON(w, http.StatusOK, out)
		return
	}
	tok, ok := h.token(w, r, lt)
	if !ok {
		return
	}
	t, err := h.d.Engines.TaskAs(r.Context(), lt.e, tok, lt.key)
	if errors.Is(err, engine.ErrNotFound) {
		writeJSON(w, http.StatusOK, out)
		return
	}
	if err != nil {
		h.failed(w, lt, err)
		return
	}
	out.Open = true
	out.Task = &taskJSON{Key: strconv.FormatUint(t.Key, 10), Name: t.Name, Assignee: t.Assignee, Groups: t.Groups, Form: t.Form}
	if !t.Due.IsZero() {
		out.Task.Due = t.Due.Format("2006-01-02T15:04:05Z07:00")
	}
	out.Mine = t.Assignee != "" && (strings.EqualFold(t.Assignee, lt.conn.Username) || t.Assignee == lt.conn.Subject)
	if t.Form != "" {
		f, err := h.d.Engines.FormAs(r.Context(), lt.e, tok, t.Form)
		if err != nil {
			h.failed(w, lt, err)
			return
		}
		out.Form = &f
		if out.Values, err = h.d.Engines.ValuesAs(r.Context(), lt.e, tok, t); err != nil {
			h.failed(w, lt, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (h engineTaskHandlers) claim(w http.ResponseWriter, r *http.Request)   { h.assign(w, r, true) }
func (h engineTaskHandlers) unclaim(w http.ResponseWriter, r *http.Request) { h.assign(w, r, false) }

func (h engineTaskHandlers) assign(w http.ResponseWriter, r *http.Request, claim bool) {
	lt, ok := h.of(w, r)
	if !ok {
		return
	}
	tok, ok := h.token(w, r, lt)
	if !ok {
		return
	}
	do := h.d.Engines.Unclaim
	if claim {
		do = h.d.Engines.Claim
	}
	assignee, err := do(r.Context(), lt.e, tok, lt.key)
	if err != nil {
		h.failed(w, lt, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"assignee": assignee})
}

func (h engineTaskHandlers) complete(w http.ResponseWriter, r *http.Request) {
	lt, ok := h.of(w, r)
	if !ok {
		return
	}
	var body struct {
		Variables json.RawMessage `json:"variables"`
	}
	if err := readJSON(w, r, &body); err != nil {
		badRequest(w, err)
		return
	}
	if v := strings.TrimSpace(string(body.Variables)); v != "" && v != "null" && !strings.HasPrefix(v, "{") {
		writeError(w, http.StatusBadRequest, "variables must be a JSON object")
		return
	}
	tok, ok := h.token(w, r, lt)
	if !ok {
		return
	}
	if err := h.d.Engines.Complete(r.Context(), lt.e, tok, lt.key, body.Variables); err != nil {
		h.failed(w, lt, err)
		return
	}
	// The engine accepted: the item moves to the state of a completed task
	// by its one transition, recorded as the person's (D3).
	var moved item.Item
	err := h.d.DB.Write(func(s *item.Store) error {
		if lt.ruled {
			if err := s.EndTask(lt.user, lt.it.ID, lt.rule.Completed); err != nil {
				return err
			}
		}
		var err error
		moved, err = resolveItem(s.As(lt.user), string(lt.it.ID))
		return err
	})
	h.d.TaskLinks.Nudge(lt.e.ID)
	if err != nil {
		// Completed at the engine; the next round ends the item.
		writeJSON(w, http.StatusOK, map[string]any{"completed": true, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"completed": true, "item": toItemJSON(moved)})
}
