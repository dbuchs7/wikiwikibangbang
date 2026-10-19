package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/dbuchs7/wikiwikibangbang/internal/audit"
	"github.com/dbuchs7/wikiwikibangbang/internal/budget"
	"github.com/dbuchs7/wikiwikibangbang/internal/item"
)

// itemRoutes are the item API (ADR-0028). Every handler acts through
// item.Store.As(principal), so the permission model (ADR-0022) decides, and
// through storage.DB.Read/Write, so commands are serialised (ADR-0008).
func itemRoutes(d Deps) []Route {
	h := itemHandlers{d: d, log: d.Log}
	if h.log == nil {
		h.log = slog.New(slog.DiscardHandler)
	}
	get, post, put, del := http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete
	routes := []Route{
		{Method: get, Pattern: "/api/types", Summary: "List item types", Handler: http.HandlerFunc(h.listTypes)},
		{Method: post, Pattern: "/api/types", Summary: "Define a type or a new version of it (administrators)", Handler: http.HandlerFunc(h.defineType)},
		{Method: get, Pattern: "/api/spaces", Summary: "List the spaces one holds a role in", Handler: http.HandlerFunc(h.listSpaces)},
		{Method: post, Pattern: "/api/spaces", Summary: "Create a space (administrators)", Handler: http.HandlerFunc(h.createSpace)},
		{Method: put, Pattern: "/api/spaces/{space}/roles/{principal}", Summary: "Grant a space role to a user ID or @group (space administrators)", Handler: http.HandlerFunc(h.grantRole)},
		{Method: put, Pattern: "/api/groups/{group}/members/{principal}", Summary: "Add a member to a group (administrators)", Handler: http.HandlerFunc(h.addMember)},
		{Method: put, Pattern: "/api/system-roles/{role}/{principal}", Summary: "Grant a system role, admin or erasure (administrators)", Handler: http.HandlerFunc(h.grantSystemRole)},
		{Method: get, Pattern: "/api/spaces/{space}/roles", Summary: "The roles granted in a space (space administrators)", Handler: http.HandlerFunc(h.listGrants)},
		{Method: del, Pattern: "/api/spaces/{space}/roles/{principal}", Summary: "Take back a role in a space (space administrators)", Handler: http.HandlerFunc(h.revokeRole)},
		{Method: get, Pattern: "/api/groups", Summary: "The groups and their members (administrators)", Handler: http.HandlerFunc(h.listGroups)},
		{Method: del, Pattern: "/api/groups/{group}/members/{principal}", Summary: "Take a member out of a group (administrators)", Handler: http.HandlerFunc(h.removeMember)},
		{Method: get, Pattern: "/api/system-roles", Summary: "Who holds the system roles admin and erasure (administrators)", Handler: http.HandlerFunc(h.listSystemRoles)},
		{Method: del, Pattern: "/api/system-roles/{role}/{principal}", Summary: "Take back a system role; the last administrator keeps it (administrators)", Handler: http.HandlerFunc(h.revokeSystemRole)},

		{Method: get, Pattern: "/api/audit", Summary: "The security audit trail, newest first, paged (administrators)", Handler: http.HandlerFunc(h.listAudit)},

		{Method: get, Pattern: "/api/items", Summary: "Find readable items, paged", Handler: http.HandlerFunc(h.findItems)},
		{Method: get, Pattern: "/api/spaces/{space}/tree", Summary: "The readable children of an item (parent) or the top of a space, in sibling order, paged", Handler: http.HandlerFunc(h.tree)},
		{Method: get, Pattern: "/api/search", Summary: "Full-text search over readable items (q; space, type, state), best first, paged", Handler: http.HandlerFunc(h.searchItems)},
		{Method: post, Pattern: "/api/items", Summary: "Create an item", Handler: http.HandlerFunc(h.createItem)},
		{Method: get, Pattern: "/api/items/{ref}", Summary: "Get an item by ID or key", Handler: http.HandlerFunc(h.getItem)},
		{Method: put, Pattern: "/api/items/{ref}/title", Summary: "Set the title", Handler: http.HandlerFunc(h.setTitle)},
		{Method: put, Pattern: "/api/items/{ref}/labels", Summary: "Replace the labels", Handler: http.HandlerFunc(h.setLabels)},
		{Method: get, Pattern: "/api/labels", Summary: "The labels of readable items with how many carry each (space), most used first", Handler: http.HandlerFunc(h.listLabels)},
		{Method: put, Pattern: "/api/items/{ref}/properties/{field}", Summary: "Set a property; null unsets it", Handler: http.HandlerFunc(h.setProperty)},
		{Method: post, Pattern: "/api/items/{ref}/transitions", Summary: "Perform a workflow transition", Handler: http.HandlerFunc(h.transition)},
		{Method: put, Pattern: "/api/items/{ref}/body", Summary: "Commit a new version of the body", Handler: http.HandlerFunc(h.commitBody)},
		{Method: post, Pattern: "/api/items/{ref}/move", Summary: "Move the item and its subtree; with before, also place it among its new siblings", Handler: http.HandlerFunc(h.move)},
		{Method: post, Pattern: "/api/items/{ref}/embedded", Summary: "Create an item embedded at the end of this item's body", Handler: http.HandlerFunc(h.createEmbedded)},
		{Method: get, Pattern: "/api/items/{ref}/embeds", Summary: "How the body's embeds render", Handler: http.HandlerFunc(h.embeds)},
		{Method: get, Pattern: "/api/items/{ref}/history", Summary: "The item's history", Handler: http.HandlerFunc(h.history)},
		{Method: get, Pattern: "/api/items/{ref}/versions/{seq}", Summary: "The body as a BodyCommitted event of the history left it", Handler: http.HandlerFunc(h.version)},
		{Method: post, Pattern: "/api/items/{ref}/versions/{seq}/restore", Summary: "Make an earlier body the current one; the current one stays in the history", Handler: http.HandlerFunc(h.restoreVersion)},
		{Method: get, Pattern: "/api/items/{ref}/relations", Summary: "Relations at one end of the item (direction=outgoing|incoming)", Handler: http.HandlerFunc(h.relations)},
		{Method: put, Pattern: "/api/items/{ref}/restriction", Summary: "Narrow who may read or edit the item and its subtree", Handler: http.HandlerFunc(h.restrict)},
		{Method: post, Pattern: "/api/items/{ref}/trash", Summary: "Move the item and its subtree to the trash", Handler: http.HandlerFunc(h.trash)},
		{Method: post, Pattern: "/api/items/{ref}/restore", Summary: "Restore a trashed item and its subtree", Handler: http.HandlerFunc(h.restore)},
		{Method: post, Pattern: "/api/items/{ref}/purge", Summary: "Delete a trashed item for good; its content becomes unrecoverable", Handler: http.HandlerFunc(h.purge)},
		{Method: post, Pattern: "/api/items/{ref}/redact", Summary: "Erase an item at once, with a logged reason (erasure role)", Handler: http.HandlerFunc(h.redact)},
		{Method: post, Pattern: "/api/relations", Summary: "Add a typed relation", Handler: http.HandlerFunc(h.addRelation)},
		{Method: del, Pattern: "/api/relations/{id}", Summary: "Remove a relation", Handler: http.HandlerFunc(h.removeRelation)},
	}
	routes = append(routes, commentRoutes(h)...)
	routes = append(routes, linkCheckRoutes(h)...)
	routes = append(routes, governanceRoutes(h)...)
	routes = append(routes, attachmentRoutes(h)...)
	routes = append(routes, watchRoutes(h)...)
	return append(routes, liveRoutes(h)...)
}

type itemHandlers struct {
	d   Deps
	log *slog.Logger
}

// who is the item-store principal of a request: the user's permanent ID.
func who(r *http.Request) string {
	p, _ := PrincipalFrom(r.Context())
	return string(p.User)
}

// fail answers an item-model error with its HTTP status.
func (h itemHandlers) fail(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, item.ErrReferenced):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, item.ErrValidation):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, item.ErrNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, item.ErrForbidden):
		writeError(w, http.StatusForbidden, err.Error())
	case errors.Is(err, errBadInput):
		writeError(w, http.StatusBadRequest, err.Error())
	default:
		h.log.Error("request failed", "err", err)
		writeError(w, http.StatusInternalServerError, "the change was not made")
	}
}

var errBadInput = errors.New("bad input")

func badInput(format string, args ...any) error {
	return fmt.Errorf("%w: %s", errBadInput, fmt.Sprintf(format, args...))
}

// read runs fn on a read-only view and answers its result.
func (h itemHandlers) read(w http.ResponseWriter, r *http.Request, fn func(*item.Session) (any, error)) {
	var out any
	var err error
	h.d.DB.Read(func(s *item.Store) { out, err = fn(s.As(who(r))) })
	if err != nil {
		h.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// write runs fn as the single writer and answers its result; a nil result
// answers 204.
func (h itemHandlers) write(w http.ResponseWriter, r *http.Request, status int, fn func(*item.Session, *item.Store) (any, error)) {
	var out any
	err := h.d.DB.Write(func(s *item.Store) error {
		var err error
		out, err = fn(s.As(who(r)), s)
		return err
	})
	if err == nil && h.d.Collab != nil {
		// A command may have erased an item or changed who may see it;
		// live sessions follow at once (ADR-0010, ADR-0026). This runs
		// outside the store's lock: the hub calls the store under its own.
		h.d.Collab.Revalidate()
	}
	switch {
	case err != nil:
		h.fail(w, err)
	case out == nil:
		w.WriteHeader(http.StatusNoContent)
	default:
		writeJSON(w, status, out)
	}
}

// decode reads the request body or answers the error; it reports success.
func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := readJSON(w, r, v); err != nil {
		badRequest(w, err)
		return false
	}
	return true
}

// resolveItem finds a readable item by ID or by current or former key.
func resolveItem(x *item.Session, ref string) (item.Item, error) {
	it, err := x.Get(item.ID(ref))
	if errors.Is(err, item.ErrNotFound) {
		return x.ByKey(ref)
	}
	return it, err
}

func (h itemHandlers) isAdmin(s *item.Store, r *http.Request) error {
	if !s.HasSystemRole(who(r), item.RoleAdmin) {
		return fmt.Errorf("%w: administrators only", item.ErrForbidden)
	}
	return nil
}

// --- types, spaces, roles ---------------------------------------------------

func (h itemHandlers) listTypes(w http.ResponseWriter, r *http.Request) {
	var out []typeJSON
	h.d.DB.Read(func(s *item.Store) {
		for _, td := range s.Types() {
			out = append(out, toTypeJSON(td))
		}
	})
	writeJSON(w, http.StatusOK, map[string]any{"types": nonNilSlice(out)})
}

func (h itemHandlers) defineType(w http.ResponseWriter, r *http.Request) {
	var req typeJSON
	if !decode(w, r, &req) {
		return
	}
	h.write(w, r, http.StatusCreated, func(_ *item.Session, s *item.Store) (any, error) {
		if err := h.isAdmin(s, r); err != nil {
			return nil, err
		}
		if err := s.DefineType(who(r), req.toTypeDef()); err != nil {
			return nil, err
		}
		return map[string]typeJSON{"type": toTypeJSON(s.Type(req.Name))}, nil
	})
}

func (h itemHandlers) listSpaces(w http.ResponseWriter, r *http.Request) {
	h.read(w, r, func(x *item.Session) (any, error) {
		out := []spaceJSON{}
		for _, sp := range x.Spaces() {
			out = append(out, spaceJSON{Key: sp.Key, Name: sp.Name, Role: roleName(x.Role(sp.Key))})
		}
		return map[string]any{"spaces": out}, nil
	})
}

func (h itemHandlers) createSpace(w http.ResponseWriter, r *http.Request) {
	var req spaceJSON
	if !decode(w, r, &req) {
		return
	}
	h.write(w, r, http.StatusCreated, func(_ *item.Session, s *item.Store) (any, error) {
		if err := h.isAdmin(s, r); err != nil {
			return nil, err
		}
		if err := s.CreateSpace(who(r), req.Key, req.Name); err != nil {
			return nil, err
		}
		return map[string]spaceJSON{"space": req}, nil
	})
}

var roleByName = map[string]item.Role{"viewer": item.Viewer, "editor": item.Editor, "space-admin": item.SpaceAdmin}

// roleName is the API name of a role, or empty for none.
func roleName(r item.Role) string {
	for name, role := range roleByName {
		if role == r {
			return name
		}
	}
	return ""
}

func (h itemHandlers) grantRole(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Role string `json:"role"`
	}
	if !decode(w, r, &req) {
		return
	}
	role, ok := roleByName[req.Role]
	if !ok {
		writeError(w, http.StatusBadRequest, "role must be viewer, editor or space-admin")
		return
	}
	h.write(w, r, 0, func(x *item.Session, _ *item.Store) (any, error) {
		if err := x.Grant(r.PathValue("space"), r.PathValue("principal"), role); err != nil {
			return nil, err
		}
		h.d.DB.Audit.Record(audit.Entry{Event: audit.RoleGranted, Actor: who(r), Addr: clientAddr(r), Subject: r.PathValue("principal"),
			Detail: map[string]string{"space": r.PathValue("space"), "role": req.Role}})
		return nil, nil
	})
}

func (h itemHandlers) addMember(w http.ResponseWriter, r *http.Request) {
	h.write(w, r, 0, func(_ *item.Session, s *item.Store) (any, error) {
		if err := h.isAdmin(s, r); err != nil {
			return nil, err
		}
		if err := s.AddToGroup(who(r), r.PathValue("group"), r.PathValue("principal")); err != nil {
			return nil, err
		}
		h.d.DB.Audit.Record(audit.Entry{Event: audit.GroupMember, Actor: who(r), Addr: clientAddr(r), Subject: r.PathValue("principal"),
			Detail: map[string]string{"group": r.PathValue("group")}})
		return nil, nil
	})
}

func (h itemHandlers) grantSystemRole(w http.ResponseWriter, r *http.Request) {
	role := r.PathValue("role")
	if role != item.RoleAdmin && role != item.RoleErasure {
		writeError(w, http.StatusBadRequest, "role must be admin or erasure")
		return
	}
	h.write(w, r, 0, func(_ *item.Session, s *item.Store) (any, error) {
		if err := h.isAdmin(s, r); err != nil {
			return nil, err
		}
		if err := s.GrantSystemRole(who(r), r.PathValue("principal"), role); err != nil {
			return nil, err
		}
		h.d.DB.Audit.Record(audit.Entry{Event: audit.SystemRole, Actor: who(r), Addr: clientAddr(r), Subject: r.PathValue("principal"), Detail: map[string]string{"role": role}})
		return nil, nil
	})
}

// listGrants lists the roles granted in a space (ADR-0048).
func (h itemHandlers) listGrants(w http.ResponseWriter, r *http.Request) {
	h.read(w, r, func(x *item.Session) (any, error) {
		grants, err := x.SpaceGrants(r.PathValue("space"))
		if err != nil {
			return nil, err
		}
		out := []map[string]string{}
		for _, g := range grants {
			out = append(out, map[string]string{"principal": g.Principal, "role": roleName(g.Role)})
		}
		return map[string]any{"roles": out}, nil
	})
}

func (h itemHandlers) revokeRole(w http.ResponseWriter, r *http.Request) {
	h.write(w, r, 0, func(x *item.Session, _ *item.Store) (any, error) {
		if err := x.Revoke(r.PathValue("space"), r.PathValue("principal")); err != nil {
			return nil, err
		}
		h.d.DB.Audit.Record(audit.Entry{Event: audit.RoleRevoked, Actor: who(r), Addr: clientAddr(r), Subject: r.PathValue("principal"),
			Detail: map[string]string{"space": r.PathValue("space")}})
		return nil, nil
	})
}

func (h itemHandlers) listGroups(w http.ResponseWriter, r *http.Request) {
	h.adminRead(w, r, func(s *item.Store) any {
		out := []map[string]any{}
		groups := s.Groups()
		for _, name := range slices.Sorted(maps.Keys(groups)) {
			out = append(out, map[string]any{"name": name, "members": groups[name]})
		}
		return map[string]any{"groups": out}
	})
}

func (h itemHandlers) removeMember(w http.ResponseWriter, r *http.Request) {
	h.write(w, r, 0, func(_ *item.Session, s *item.Store) (any, error) {
		if err := h.isAdmin(s, r); err != nil {
			return nil, err
		}
		if err := s.RemoveFromGroup(who(r), r.PathValue("group"), r.PathValue("principal")); err != nil {
			return nil, err
		}
		h.d.DB.Audit.Record(audit.Entry{Event: audit.GroupLeft, Actor: who(r), Addr: clientAddr(r), Subject: r.PathValue("principal"),
			Detail: map[string]string{"group": r.PathValue("group")}})
		return nil, nil
	})
}

func (h itemHandlers) listSystemRoles(w http.ResponseWriter, r *http.Request) {
	h.adminRead(w, r, func(s *item.Store) any {
		out := []map[string]any{}
		holders := s.SystemRoles()
		for _, p := range slices.Sorted(maps.Keys(holders)) {
			if len(holders[p]) > 0 {
				out = append(out, map[string]any{"principal": p, "roles": holders[p]})
			}
		}
		return map[string]any{"holders": out}
	})
}

func (h itemHandlers) revokeSystemRole(w http.ResponseWriter, r *http.Request) {
	h.write(w, r, 0, func(_ *item.Session, s *item.Store) (any, error) {
		if err := h.isAdmin(s, r); err != nil {
			return nil, err
		}
		if err := s.RevokeSystemRole(who(r), r.PathValue("principal"), r.PathValue("role")); err != nil {
			return nil, err
		}
		h.d.DB.Audit.Record(audit.Entry{Event: audit.SystemRoleTaken, Actor: who(r), Addr: clientAddr(r), Subject: r.PathValue("principal"),
			Detail: map[string]string{"role": r.PathValue("role")}})
		return nil, nil
	})
}

// adminRead answers a read only administrators may make.
func (h itemHandlers) adminRead(w http.ResponseWriter, r *http.Request, fn func(s *item.Store) any) {
	var out any
	var err error
	h.d.DB.Read(func(s *item.Store) {
		if err = h.isAdmin(s, r); err == nil {
			out = fn(s)
		}
	})
	if err != nil {
		h.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (h itemHandlers) listAudit(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, before := 100, 0
	var err error
	if v := q.Get("limit"); v != "" {
		if limit, err = strconv.Atoi(v); err != nil || limit < 1 || limit > budget.Get().PageItems {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("limit must be 1 to %d", budget.Get().PageItems))
			return
		}
	}
	if v := q.Get("before"); v != "" {
		if before, err = strconv.Atoi(v); err != nil || before < 1 {
			writeError(w, http.StatusBadRequest, "before must be a sequence number")
			return
		}
	}
	admin := false
	h.d.DB.Read(func(s *item.Store) { admin = s.HasSystemRole(who(r), item.RoleAdmin) })
	if !admin {
		writeError(w, http.StatusForbidden, "administrators only")
		return
	}
	entries := h.d.DB.Audit.List(before, limit)
	res := map[string]any{"entries": nonNilSlice(entries)}
	if len(entries) == limit {
		res["next"] = entries[len(entries)-1].Seq
	}
	writeJSON(w, http.StatusOK, res)
}

// --- items -------------------------------------------------------------------

func (h itemHandlers) findItems(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, ok := parseLimit(w, q)
	if !ok {
		return
	}
	query := item.Query{Space: q.Get("space"), Type: q.Get("type"), State: q.Get("state"), Parent: item.ID(q.Get("parent")),
		IncludeDeleted: q.Get("deleted") == "true", Label: q.Get("label")}
	h.read(w, r, func(x *item.Session) (any, error) {
		all := x.Find(query)
		page, res, err := paged(all, func(it item.Item) item.ID { return it.ID }, item.ID(q.Get("after")), limit)
		if err != nil {
			return nil, err
		}
		out := make([]itemJSON, len(page))
		for i, it := range page {
			out[i] = toItemJSON(it)
		}
		res["items"] = out
		return res, nil
	})
}

// searchItems answers a full-text search (ADR-0033).
func (h itemHandlers) searchItems(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, ok := parseLimit(w, q)
	if !ok {
		return
	}
	query := item.Query{Space: q.Get("space"), Type: q.Get("type"), State: q.Get("state"), Label: q.Get("label")}
	h.read(w, r, func(x *item.Session) (any, error) {
		start := time.Now()
		all, err := x.Search(q.Get("q"), query)
		if h.d.Metrics != nil {
			h.d.Metrics.ObserveSearch(time.Since(start))
		}
		if err != nil {
			return nil, err
		}
		page, res, err := paged(all, func(hit item.Hit) item.ID { return hit.Item.ID }, item.ID(q.Get("after")), limit)
		if err != nil {
			return nil, err
		}
		out := make([]hitJSON, len(page))
		for i, hit := range page {
			out[i] = hitJSON{Item: toItemJSON(hit.Item), Score: hit.Score, Snippet: hit.Snippet, Comment: hit.Comment}
		}
		res["hits"] = out
		return res, nil
	})
}

type hitJSON struct {
	Item    itemJSON `json:"item"`
	Score   int      `json:"score"`
	Snippet string   `json:"snippet,omitempty"`
	// Comment is the comment the snippet comes from (ADR-0035).
	Comment item.ID `json:"comment,omitempty"`
}

// parseLimit reads the page size, 100 by default; it answers 400 itself.
func parseLimit(w http.ResponseWriter, q url.Values) (int, bool) {
	v := q.Get("limit")
	if v == "" {
		return 100, true
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 || n > budget.Get().PageItems {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("limit must be 1 to %d", budget.Get().PageItems))
		return 0, false
	}
	return n, true
}

// paged cuts the page after the cursor out of a complete listing and
// returns it with the listing's total and, when more follow, the next
// cursor.
func paged[T any](all []T, id func(T) item.ID, after item.ID, limit int) ([]T, map[string]any, error) {
	start := 0
	if after != "" {
		start = -1
		for i, v := range all {
			if id(v) == after {
				start = i + 1
			}
		}
		if start < 0 {
			return nil, nil, badInput("cursor %q is not in this listing", after)
		}
	}
	page := all[start:min(start+limit, len(all))]
	res := map[string]any{"total": map[string]any{"value": len(all), "exact": true}}
	if start+limit < len(all) {
		res["next"] = id(page[len(page)-1])
	}
	return page, res, nil
}

type newItemJSON struct {
	Space      string                     `json:"space"`
	Type       string                     `json:"type"`
	Parent     item.ID                    `json:"parent"`
	Title      string                     `json:"title"`
	Properties map[string]json.RawMessage `json:"properties"`
	// Body is the item's first body, saved with it.
	Body []blockJSON `json:"body"`
}

func (n newItemJSON) toNewItem(s *item.Store) (item.NewItem, error) {
	props, err := coerceProps(s.Type(n.Type), n.Properties)
	if err != nil {
		return item.NewItem{}, err
	}
	out := item.NewItem{Space: n.Space, Type: n.Type, Parent: n.Parent, Title: n.Title, Properties: props}
	if n.Body != nil {
		doc, err := toDoc(n.Body, nil)
		if err != nil {
			return item.NewItem{}, err
		}
		out.Body = &doc
	}
	return out, nil
}

func (h itemHandlers) createItem(w http.ResponseWriter, r *http.Request) {
	var req newItemJSON
	if !decode(w, r, &req) {
		return
	}
	h.write(w, r, http.StatusCreated, func(x *item.Session, s *item.Store) (any, error) {
		n, err := req.toNewItem(s)
		if err != nil {
			return nil, err
		}
		it, err := x.Create(n)
		if err != nil {
			return nil, err
		}
		return map[string]itemJSON{"item": toItemJSON(it)}, nil
	})
}

func (h itemHandlers) getItem(w http.ResponseWriter, r *http.Request) {
	h.read(w, r, func(x *item.Session) (any, error) {
		it, err := resolveItem(x, r.PathValue("ref"))
		if err != nil {
			return nil, err
		}
		// editable tells the UI whether to offer changes: the caller's
		// role, the item's restriction and an import's hold decide it.
		return map[string]any{"item": toItemJSON(it), "editable": x.CanEdit(it.ID)}, nil
	})
}

// change runs a command on the item named in the path and answers the
// item as it is afterwards.
func (h itemHandlers) change(w http.ResponseWriter, r *http.Request, cmd func(x *item.Session, s *item.Store, it item.Item) error) {
	h.write(w, r, http.StatusOK, func(x *item.Session, s *item.Store) (any, error) {
		it, err := resolveItem(x, r.PathValue("ref"))
		if err != nil {
			return nil, err
		}
		if err := cmd(x, s, it); err != nil {
			return nil, err
		}
		after, err := s.Get(it.ID)
		if err != nil {
			return nil, err
		}
		return map[string]itemJSON{"item": toItemJSON(after)}, nil
	})
}

func (h itemHandlers) setTitle(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Title string `json:"title"`
	}
	if decode(w, r, &req) {
		h.change(w, r, func(x *item.Session, _ *item.Store, it item.Item) error { return x.SetTitle(it.ID, req.Title) })
	}
}

func (h itemHandlers) setProperty(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Value json.RawMessage `json:"value"`
	}
	if !decode(w, r, &req) {
		return
	}
	field := r.PathValue("field")
	h.change(w, r, func(x *item.Session, s *item.Store, it item.Item) error {
		td, _ := s.TypeAt(it.Type, it.TypeVersion)
		v, err := coerceValue(td, field, req.Value)
		if err != nil {
			return err
		}
		return x.SetProperty(it.ID, field, v)
	})
}

func (h itemHandlers) transition(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if decode(w, r, &req) {
		h.change(w, r, func(x *item.Session, _ *item.Store, it item.Item) error { return x.Transition(it.ID, req.Name) })
	}
}

func (h itemHandlers) commitBody(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Blocks []blockJSON `json:"blocks"`
	}
	if !decode(w, r, &req) {
		return
	}
	// Checked before the write: the hub must not be called under the
	// store's lock. A session that starts in between is reset by its next
	// commit instead (collab.ErrDiverged).
	if id, err := h.resolveID(r); err == nil && h.d.Collab != nil && h.d.Collab.Busy(id) {
		writeError(w, http.StatusConflict, "the body is being edited in a live session; join it instead")
		return
	}
	h.change(w, r, func(x *item.Session, _ *item.Store, it item.Item) error {
		doc, err := toDoc(req.Blocks, func(text string) bool {
			return slices.ContainsFunc(it.Body.Blocks, func(b item.Block) bool { return b.Kind == item.Unsupported && b.Text == text })
		})
		if err != nil {
			return err
		}
		return x.CommitBody(it.ID, doc)
	})
}

func (h itemHandlers) move(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Space  string  `json:"space"`
		Parent item.ID `json:"parent"`
		// Before places the item before this sibling (ID or key), or last
		// when empty (ADR-0041); absent, the sibling order is kept.
		Before *string `json:"before"`
	}
	if decode(w, r, &req) {
		h.change(w, r, func(x *item.Session, _ *item.Store, it item.Item) error {
			if req.Before == nil {
				return x.Move(it.ID, req.Space, req.Parent)
			}
			var before item.ID
			if *req.Before != "" {
				b, err := resolveItem(x, *req.Before)
				if err != nil {
					return err
				}
				before = b.ID
			}
			return x.Place(it.ID, req.Space, req.Parent, before)
		})
	}
}

// setLabels replaces an item's labels (ADR-0044).
func (h itemHandlers) setLabels(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Labels []string `json:"labels"`
	}
	if decode(w, r, &req) {
		h.change(w, r, func(x *item.Session, _ *item.Store, it item.Item) error { return x.SetLabels(it.ID, req.Labels) })
	}
}

// listLabels counts the labels of the readable items (ADR-0044).
func (h itemHandlers) listLabels(w http.ResponseWriter, r *http.Request) {
	h.read(w, r, func(x *item.Session) (any, error) {
		all := x.Labels(r.URL.Query().Get("space"))
		out := []map[string]any{}
		// The most used labels, as many as a page holds; the total says
		// how many there are (ADR-0015).
		for _, l := range all[:min(len(all), budget.Get().PageItems)] {
			out = append(out, map[string]any{"label": l.Label, "count": l.Count})
		}
		return map[string]any{"labels": out, "total": map[string]any{"value": len(all), "exact": true}}, nil
	})
}

// treeNodeJSON is one entry of a page tree.
type treeNodeJSON struct {
	ID          item.ID `json:"id"`
	Key         string  `json:"key"`
	Title       string  `json:"title"`
	Type        string  `json:"type"`
	HasChildren bool    `json:"hasChildren"`
}

// tree lists one level of a space's page tree (ADR-0041).
func (h itemHandlers) tree(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, ok := parseLimit(w, q)
	if !ok {
		return
	}
	h.read(w, r, func(x *item.Session) (any, error) {
		var parent item.ID
		if ref := q.Get("parent"); ref != "" {
			p, err := resolveItem(x, ref)
			if err != nil {
				return nil, err
			}
			parent = p.ID
		}
		nodes, err := x.Tree(r.PathValue("space"), parent)
		if err != nil {
			return nil, err
		}
		page, res, err := paged(nodes, func(n item.TreeNode) item.ID { return n.Item.ID }, item.ID(q.Get("after")), limit)
		if err != nil {
			return nil, err
		}
		out := make([]treeNodeJSON, len(page))
		for i, n := range page {
			out[i] = treeNodeJSON{ID: n.Item.ID, Key: n.Item.Key, Title: n.Item.Title, Type: n.Item.Type, HasChildren: n.HasChildren}
		}
		res["items"] = out
		return res, nil
	})
}

func (h itemHandlers) createEmbedded(w http.ResponseWriter, r *http.Request) {
	var req newItemJSON
	if !decode(w, r, &req) {
		return
	}
	h.write(w, r, http.StatusCreated, func(x *item.Session, s *item.Store) (any, error) {
		host, err := resolveItem(x, r.PathValue("ref"))
		if err != nil {
			return nil, err
		}
		n, err := req.toNewItem(s)
		if err != nil {
			return nil, err
		}
		it, err := x.CreateEmbedded(host.ID, n)
		if err != nil {
			return nil, err
		}
		return map[string]itemJSON{"item": toItemJSON(it)}, nil
	})
}

func (h itemHandlers) embeds(w http.ResponseWriter, r *http.Request) {
	h.read(w, r, func(x *item.Session) (any, error) {
		it, err := resolveItem(x, r.PathValue("ref"))
		if err != nil {
			return nil, err
		}
		views, err := x.Embeds(it.ID)
		out := []embedJSON{}
		for _, v := range views {
			out = append(out, embedJSON{Ref: v.Ref, Key: v.Key, Title: v.Title, Child: v.Child, Presence: string(v.Presence), Restricted: v.Restricted})
		}
		return map[string]any{"embeds": out}, err
	})
}

func (h itemHandlers) history(w http.ResponseWriter, r *http.Request) {
	h.read(w, r, func(x *item.Session) (any, error) {
		it, err := resolveItem(x, r.PathValue("ref"))
		if err != nil {
			return nil, err
		}
		events, err := x.History(it.ID)
		out := []eventJSON{}
		for _, e := range events {
			out = append(out, toEventJSON(e))
		}
		return map[string]any{"events": out}, err
	})
}

// versionSeq reads the {seq} of a version route.
func versionSeq(r *http.Request) (int, error) {
	seq, err := strconv.Atoi(r.PathValue("seq"))
	if err != nil || seq < 1 {
		return 0, badInput("version must be the sequence number of a BodyCommitted event")
	}
	return seq, nil
}

// version answers one earlier body of an item (ADR-0043).
func (h itemHandlers) version(w http.ResponseWriter, r *http.Request) {
	h.read(w, r, func(x *item.Session) (any, error) {
		it, err := resolveItem(x, r.PathValue("ref"))
		if err != nil {
			return nil, err
		}
		seq, err := versionSeq(r)
		if err != nil {
			return nil, err
		}
		v, err := x.BodyAt(it.ID, seq)
		if err != nil {
			return nil, err
		}
		body := []blockJSON{}
		for _, b := range v.Body.Blocks {
			body = append(body, toBlockJSON(b))
		}
		return map[string]any{"seq": v.Seq, "at": v.At, "actor": v.Actor, "body": body}, nil
	})
}

// restoreVersion makes an earlier body the current one (ADR-0043).
func (h itemHandlers) restoreVersion(w http.ResponseWriter, r *http.Request) {
	seq, err := versionSeq(r)
	if err != nil {
		h.fail(w, err)
		return
	}
	// As for a commit: a body being edited live is not replaced under the
	// people editing it.
	if id, err := h.resolveID(r); err == nil && h.d.Collab != nil && h.d.Collab.Busy(id) {
		writeError(w, http.StatusConflict, "the body is being edited in a live session; try again when it is saved")
		return
	}
	h.change(w, r, func(x *item.Session, _ *item.Store, it item.Item) error { return x.RestoreBody(it.ID, seq) })
}

func (h itemHandlers) relations(w http.ResponseWriter, r *http.Request) {
	dir := item.Outgoing
	switch r.URL.Query().Get("direction") {
	case "", "outgoing":
	case "incoming":
		dir = item.Incoming
	default:
		writeError(w, http.StatusBadRequest, "direction must be outgoing or incoming")
		return
	}
	h.read(w, r, func(x *item.Session) (any, error) {
		it, err := resolveItem(x, r.PathValue("ref"))
		if err != nil {
			return nil, err
		}
		rels, err := x.Relations(it.ID, dir)
		out := []relationJSON{}
		for _, rel := range rels {
			out = append(out, relationJSON{ID: rel.ID, Type: rel.Type, From: rel.From, To: rel.To, Derived: rel.Derived})
		}
		return map[string]any{"relations": out}, err
	})
}

func (h itemHandlers) restrict(w http.ResponseWriter, r *http.Request) {
	var req restrictionJSON
	if decode(w, r, &req) {
		h.change(w, r, func(x *item.Session, _ *item.Store, it item.Item) error {
			return x.Restrict(it.ID, item.Restriction{Read: req.Read, Edit: req.Edit})
		})
	}
}

func (h itemHandlers) trash(w http.ResponseWriter, r *http.Request) {
	h.change(w, r, func(x *item.Session, _ *item.Store, it item.Item) error { return x.Trash(it.ID) })
}

func (h itemHandlers) restore(w http.ResponseWriter, r *http.Request) {
	h.change(w, r, func(x *item.Session, _ *item.Store, it item.Item) error { return x.Restore(it.ID) })
}

func (h itemHandlers) purge(w http.ResponseWriter, r *http.Request) {
	h.change(w, r, func(x *item.Session, _ *item.Store, it item.Item) error {
		if err := x.Purge(it.ID); err != nil {
			return err
		}
		h.d.DB.Audit.Record(audit.Entry{Event: audit.ItemPurged, Actor: who(r), Addr: clientAddr(r), Subject: string(it.ID), Detail: map[string]string{"key": it.Key}})
		return nil
	})
}

func (h itemHandlers) redact(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Reason string `json:"reason"`
	}
	if decode(w, r, &req) {
		h.change(w, r, func(x *item.Session, _ *item.Store, it item.Item) error {
			if err := x.Redact(it.ID, req.Reason); err != nil {
				return err
			}
			h.d.DB.Audit.Record(audit.Entry{Event: audit.ItemRedacted, Actor: who(r), Addr: clientAddr(r), Subject: string(it.ID), Detail: map[string]string{"key": it.Key, "reason": req.Reason}})
			return nil
		})
	}
}

func (h itemHandlers) addRelation(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Type string  `json:"type"`
		From item.ID `json:"from"`
		To   item.ID `json:"to"`
	}
	if !decode(w, r, &req) {
		return
	}
	h.write(w, r, http.StatusCreated, func(x *item.Session, _ *item.Store) (any, error) {
		if err := x.AddRelation(req.Type, req.From, req.To); err != nil {
			return nil, err
		}
		rels, err := x.Relations(req.From, item.Outgoing)
		if err != nil {
			return nil, err
		}
		// Relations are listed in creation order: the last match is the new one.
		var rel item.Relation
		for _, c := range rels {
			if !c.Derived && c.Type == req.Type && c.To == req.To {
				rel = c
			}
		}
		return map[string]relationJSON{"relation": {ID: rel.ID, Type: rel.Type, From: rel.From, To: rel.To}}, nil
	})
}

func (h itemHandlers) removeRelation(w http.ResponseWriter, r *http.Request) {
	h.write(w, r, 0, func(x *item.Session, _ *item.Store) (any, error) {
		return nil, x.RemoveRelation(item.ID(r.PathValue("id")))
	})
}

// --- values -----------------------------------------------------------------

func coerceProps(td item.TypeDef, raw map[string]json.RawMessage) (item.Props, error) {
	if raw == nil {
		return nil, nil
	}
	props := item.Props{}
	for k, v := range raw {
		val, err := coerceValue(td, k, v)
		if err != nil {
			return nil, err
		}
		if val != nil {
			props[k] = val
		}
	}
	return props, nil
}

// coerceValue turns a JSON value into the Go value the field's kind takes:
// whole numbers become int, other numbers float64, dates time.Time. Fields
// the type does not know keep their JSON value, so the store's validation
// names them.
func coerceValue(td item.TypeDef, field string, raw json.RawMessage) (any, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var kind item.FieldKind
	for _, f := range td.Fields {
		if f.Name == field {
			kind = f.Kind
		}
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, badInput("field %q: %v", field, err)
	}
	switch kind {
	case item.KindNumber:
		n, ok := v.(json.Number)
		if !ok {
			return v, nil // the store reports the wrong kind
		}
		if !strings.ContainsAny(n.String(), ".eE") {
			if i, err := strconv.Atoi(n.String()); err == nil {
				return i, nil
			}
		}
		f, err := n.Float64()
		if err != nil {
			return nil, badInput("field %q: %v", field, err)
		}
		return f, nil
	case item.KindDate:
		s, ok := v.(string)
		if !ok {
			return v, nil
		}
		for _, layout := range []string{time.RFC3339Nano, time.DateOnly} {
			if t, err := time.Parse(layout, s); err == nil {
				return t, nil
			}
		}
		return nil, badInput("field %q: %q is not an RFC 3339 date-time or a YYYY-MM-DD date", field, s)
	}
	return v, nil
}

func nonNilSlice[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}
