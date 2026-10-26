package mcp

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
)

// kind is the JSON type an argument must have.
type kind string

const (
	str     kind = "string"
	integer kind = "integer" // a positive whole number
	boolean kind = "boolean"
	object  kind = "object"
	array   kind = "array"
	anyJSON kind = "" // any JSON value, null included
)

// place says where an argument goes in the request.
type place int

const (
	inPath place = iota
	inQuery
	inBody
)

type param struct {
	Name     string
	Kind     kind
	In       place
	Required bool
	Doc      string
}

// tool is one API request offered to agents.
type tool struct {
	Name, Description string
	Method, Path      string // Path names its path arguments as {name}
	Params            []param
}

// blockFormat describes the body format to agents, so they can write a
// page without reading one first.
const blockFormat = "A body is a list of blocks, each an object with kind and text. Kinds: paragraph, " +
	"heading (level 1-3), bullet and ordered (one block per list item), task (checked true or false), quote, code, " +
	"panel (tone info, note, warning, tip or error), table (text holds rows separated by \\n and cells by \\t; the first row " +
	"is the header), toc, children and divider (no text: a table of contents, a list of child pages, a horizontal rule), embed (ref: an item ID), " +
	"image (ref: an attachment ID of the item, text the caption, width 10-100 percent of the column or absent for its own size, align left or right to let text flow beside it, or center), " +
	"bpmn (ref: an attachment ID of the item holding a BPMN 2.0 model, text the caption), process (engine: a process engine's ID, process: a BPMN process ID, text the caption; shows the process's newest version). " +
	"Paragraphs, headings, quotes and panels may have align center, right or justify. " +
	"Inline formats are marks over the text, counted in Unicode code points: " +
	"{\"type\": \"bold\"|\"italic\"|\"underline\"|\"strike\"|\"highlight\"|\"code\"|\"link\" (with href)|\"mention\" (with ref, an item ID), \"start\": 0, \"end\": 5}. " +
	"Example: [{\"kind\":\"heading\",\"level\":1,\"text\":\"Runbook\"},{\"kind\":\"paragraph\",\"text\":\"Stop the writers first.\"," +
	"\"marks\":[{\"type\":\"bold\",\"start\":0,\"end\":4}]},{\"kind\":\"bullet\",\"text\":\"db1\"},{\"kind\":\"bullet\",\"text\":\"db2\"}]."

// queryLanguage describes the query language (ADR-0136) to agents.
const queryLanguage = "A query is conditions joined with AND, OR, NOT and parentheses, then optionally ORDER BY field [ASC|DESC], further fields after commas. " +
	"A condition is field = value, !=, <, <=, >, >=, ~ (every word of the value occurs), !~, field IN (a, b), field NOT IN (...), field IS EMPTY, field IS NOT EMPTY. " +
	"Fields: id, key, title, type, space, state, parent and ancestor (an item's key or ID), label, text (title, key, body, text properties, labels, comments; ~ only), " +
	"body and comment (~ and IS EMPTY), created, updated, creator, finished (true when the workflow is done), and any property by its name, as list_types names them; " +
	"quote a name with spaces, or a property whose name is also a built-in field's, e.g. \"due date\". Values with spaces or commas are quoted. " +
	"Dates are 2026-10-01, a time is RFC 3339, -7d, 2w, +12h and -1y count from now; functions: now(), startOfDay(), endOfDay(), startOfWeek(), endOfWeek(), " +
	"startOfMonth(), endOfMonth(), startOfYear(), endOfYear(), each with an optional offset such as -1, and currentUser(), your caller. " +
	"A user field takes a username or a user ID. != and NOT IN also hold for items without the field. A mistake is answered with the character where it is."

var (
	refParam = param{"ref", str, inPath, true, "The item's ID or key, e.g. PLAT-12."}
	paging   = []param{
		{"limit", integer, inQuery, false, "Items per page, 1 to the server's page budget; 100 if absent."},
		{"after", str, inQuery, false, "The 'next' cursor of the previous page."},
	}
)

// tools are the API routes offered to agents (ADR-0012). Not offered, on
// purpose: moving, trashing, restoring, purging and redacting items, item
// restrictions, removing relations, editing or deleting comments and
// resolving comment threads (they
// change who sees what, or destroy content, and are a person's decision;
// ADR-0035); administration of types,
// spaces, roles, groups, users and backups; and everything about
// credentials. An agent with an administrator's token still cannot reach
// them through this adapter.
//
// A page as Markdown (ADR-0131) is offered as a read: it gives an agent the
// text as a person reads it, protected passages veiled, with exactly the
// caller's rights; the ZIP archive with pictures is for people.
//
// Of the change feed and the own-roles query (ADR-0121), the reads are
// offered: list_changes without its wait, as a tool call answers at once,
// and get_own_roles. Watching and unwatching are not offered: they are
// the caller's own subscription, which an agent does not take out or end
// on a person's behalf.
//
// The process engines' routes (ADR-0129), their forms and decisions
// included (ADR-0144), are not offered: whether agents read an engine's
// processes, forms, decisions and operations through this server, with
// the engine's credential rather than their own, is a decision of its
// own.
var tools = []tool{
	{Name: "list_spaces", Description: "List the spaces the caller holds a role in.", Method: "GET", Path: "/api/spaces"},
	{Name: "get_own_roles", Description: "The caller's own roles: the system roles held, and every space role granted to the caller, directly or through a group (via), one entry per grant.",
		Method: "GET", Path: "/api/me/roles"},
	{Name: "list_changes", Description: "The change feed: what happened, oldest first, to the items the caller watches (and their ancestors and watched spaces) after the event after, " +
		"and to the caller's own roles. An entry's item is empty when the caller may not read the item now. Continue from last; without after, the answer is the newest event to start from.",
		Method: "GET", Path: "/api/changes", Params: []param{
			{"after", integer, inQuery, false, "The last of the previous answer; absent to start from now."},
			{"limit", integer, inQuery, false, "Entries per page, 1 to the server's budget; 100 if absent."},
		}},
	{Name: "list_types", Description: "List the item types with their fields and workflows; read it before creating an item or setting a property.",
		Method: "GET", Path: "/api/types"},
	{Name: "find_items", Description: "Find readable items by space, type, state, parent, label or a property's value, paged. The answer's total says whether it is exact. " +
		"To keep a catalog in step with another system, find an item by the identifier it has there with where, before creating it.",
		Method: "GET", Path: "/api/items", Params: append([]param{
			{"space", str, inQuery, false, "A space key, e.g. PLAT."},
			{"type", str, inQuery, false, "A type name, e.g. task."},
			{"state", str, inQuery, false, "A workflow state, e.g. open."},
			{"parent", str, inQuery, false, "The ID of the containing item."},
			{"label", str, inQuery, false, "A label the items carry, e.g. runbook."},
			{"where", str, inQuery, false, "A property's value, as <field>=<value>, e.g. source-id=4f2a; a date as YYYY-MM-DD."},
		}, paging...)},
	{Name: "query_items", Description: "Select readable items with the query language, in its order, paged. " + queryLanguage,
		Method: "GET", Path: "/api/items", Params: append([]param{
			{"query", str, inQuery, true, "The query, e.g. type = task AND assignee = currentUser() AND state != done ORDER BY due."},
		}, paging...)},
	{Name: "get_governance", Description: "The governance check of a space: sensitive data, stale items, implausible values and empty recommended fields, and items due for review, " +
		"each with the person responsible. Fix what can be fixed, e.g. a missing contact; a review is a person's decision.",
		Method: "GET", Path: "/api/spaces/{space}/governance", Params: []param{
			{"space", str, inPath, true, "The space key."},
		}},
	{Name: "search_items", Description: "Full-text search over readable items, best match first, paged. An item holding any of the words is a hit; each hit names the words it holds (matched) and those it does not (missing), and rates itself with 1 to 5 stars.",
		Method: "GET", Path: "/api/search", Params: append([]param{
			{"q", str, inQuery, true, "Words to find; every word must occur."},
			{"space", str, inQuery, false, "Limit to a space key."},
			{"type", str, inQuery, false, "Limit to a type name."},
			{"state", str, inQuery, false, "Limit to a workflow state."},
		}, paging...)},
	{Name: "get_item", Description: "Get one item: title, type, state, properties and body. A protected passage your caller may not read shows as ▒▒▒▒▒▒▒▒ under a mark of type secret; secrets says who protected it.", Method: "GET", Path: "/api/items/{ref}",
		Params: []param{refParam}},
	{Name: "get_item_markdown", Description: "Get one item as Markdown to read: title, properties and body, the items it embeds as quotes, pictures linked to their attachments. " +
		"A protected passage your caller may not read shows as ▒▒▒▒▒▒▒▒. To change a body, use commit_body with blocks, not Markdown.",
		Method: "GET", Path: "/api/items/{ref}/export/md", Params: []param{refParam,
			{"children", boolean, inQuery, false, "true adds every readable page below the item, as a handbook."}}},
	{Name: "get_history", Description: "The item's history: who changed what, and when.", Method: "GET", Path: "/api/items/{ref}/history",
		Params: []param{refParam}},
	{Name: "get_relations", Description: "Typed relations at one end of the item.", Method: "GET", Path: "/api/items/{ref}/relations",
		Params: []param{refParam, {"direction", str, inQuery, false, "outgoing (default) or incoming."}}},
	{Name: "get_embeds", Description: "How the items embedded in the body render; unreadable ones show as restricted.",
		Method: "GET", Path: "/api/items/{ref}/embeds", Params: []param{refParam}},
	{Name: "create_item", Description: "Create an item, e.g. a page, with its body in the same step. Required fields of its type " +
		"must be given in properties. Read list_types first for the types and list_spaces for where you may write. " + blockFormat,
		Method: "POST", Path: "/api/items", Params: []param{
			{"space", str, inBody, true, "The space key."},
			{"type", str, inBody, true, "The type name, e.g. page."},
			{"title", str, inBody, true, "The title."},
			{"parent", str, inBody, false, "The ID of the item to place it under, e.g. a parent page."},
			{"properties", object, inBody, false, "Field values by field name."},
			{"body", array, inBody, false, "The item's first body, as blocks; only for types with a body."},
		}},
	{Name: "get_tree", Description: "The pages of a space in their order, one level at a time: the top of the space, or the children of parent. " +
		"Each entry says whether it has children.",
		Method: "GET", Path: "/api/spaces/{space}/tree", Params: append([]param{
			{"space", str, inPath, true, "The space key."},
			{"parent", str, inQuery, false, "The ID of the page whose children to list; absent for the top of the space."},
		}, paging...)},
	{Name: "set_labels", Description: "Replace an item's labels, e.g. [\"runbook\", \"ops\"]: lower case words, digits and dashes.",
		Method: "PUT", Path: "/api/items/{ref}/labels", Params: []param{refParam, {"labels", array, inBody, true, "All labels the item carries afterwards."}}},
	{Name: "set_title", Description: "Change an item's title.", Method: "PUT", Path: "/api/items/{ref}/title",
		Params: []param{refParam, {"title", str, inBody, true, "The new title."}}},
	{Name: "set_property", Description: "Set one property of an item; null unsets it. State changes go through transition.",
		Method: "PUT", Path: "/api/items/{ref}/properties/{field}", Params: []param{
			refParam,
			{"field", str, inPath, true, "The field name."},
			{"value", anyJSON, inBody, true, "The new value, of the field's kind, or null."},
		}},
	{Name: "transition", Description: "Perform a workflow transition, e.g. finish.", Method: "POST", Path: "/api/items/{ref}/transitions",
		Params: []param{refParam, {"name", str, inBody, true, "The transition's name."}}},
	{Name: "commit_body", Description: "Replace the item's body with a new version; the earlier one stays in the history. " +
		"Refused while people edit it in a live session, and when it holds protected passages your caller may not read. To add to a body, read it with get_item and send it back extended. " + blockFormat,
		Method: "PUT", Path: "/api/items/{ref}/body", Params: []param{refParam, {"blocks", array, inBody, true, "The body's blocks."}}},
	{Name: "add_relation", Description: "Relate two items with a typed relation, e.g. blocks or implements.",
		Method: "POST", Path: "/api/relations", Params: []param{
			{"type", str, inBody, true, "The relation type."},
			{"from", str, inBody, true, "The source item's ID."},
			{"to", str, inBody, true, "The target item's ID."},
		}},
	{Name: "list_comments", Description: "The item's comments, oldest first, paged. A deleted comment is listed without text.",
		Method: "GET", Path: "/api/items/{ref}/comments", Params: append([]param{refParam}, paging...)},
	{Name: "add_comment", Description: "Comment on an item, in plain text: on the whole item, on a passage of its body (anchor), or in answer to a comment (reply). Needs the right to edit the item.",
		Method: "POST", Path: "/api/items/{ref}/comments", Params: []param{refParam,
			{"text", str, inBody, true, "The comment, plain text."},
			{"anchor", object, inBody, false, "The passage commented on: {\"quote\": text of one body block, \"prefix\" and \"suffix\": up to 32 characters before and after it, to tell apart passages of the same text}."},
			{"reply", str, inBody, false, "The ID of the comment answered; the answer joins its thread."},
		}},
}

func find(name string) *tool {
	for i := range tools {
		if tools[i].Name == name {
			return &tools[i]
		}
	}
	return nil
}

func (t tool) describe() map[string]any {
	props := map[string]any{}
	required := []string{}
	for _, p := range t.Params {
		schema := map[string]any{"description": p.Doc}
		if p.Kind != anyJSON {
			schema["type"] = string(p.Kind)
		}
		if p.Kind == integer {
			schema["minimum"] = 1
		}
		props[p.Name] = schema
		if p.Required {
			required = append(required, p.Name)
		}
	}
	return map[string]any{
		"name":        t.Name,
		"description": t.Description,
		"inputSchema": map[string]any{"type": "object", "properties": props, "required": required, "additionalProperties": false},
	}
}

// request turns a tool's arguments into the API request. Arguments are
// checked here only for shape; what they mean is the API's to judge.
func (t tool) request(raw json.RawMessage) (method, path string, body []byte, err error) {
	args := map[string]any{}
	if len(bytes.TrimSpace(raw)) > 0 && string(raw) != "null" {
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.UseNumber()
		if err := dec.Decode(&args); err != nil {
			return "", "", nil, errors.New("arguments must be a JSON object")
		}
	}
	known := map[string]bool{}
	for _, p := range t.Params {
		known[p.Name] = true
	}
	var unknown []string
	for name := range args {
		if !known[name] {
			unknown = append(unknown, name)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return "", "", nil, fmt.Errorf("unknown arguments %s", strings.Join(unknown, ", "))
	}
	path = t.Path
	query := url.Values{}
	fields := map[string]any{}
	for _, p := range t.Params {
		v, present := args[p.Name]
		if !present {
			if p.Required {
				return "", "", nil, fmt.Errorf("%s is required", p.Name)
			}
			continue
		}
		if err := check(p, v); err != nil {
			return "", "", nil, err
		}
		switch p.In {
		case inPath:
			path = strings.Replace(path, "{"+p.Name+"}", url.PathEscape(v.(string)), 1)
		case inQuery:
			query.Set(p.Name, fmt.Sprint(v))
		case inBody:
			fields[p.Name] = v
		}
	}
	if len(query) > 0 {
		path += "?" + query.Encode()
	}
	if t.Method != "GET" {
		if body, err = json.Marshal(fields); err != nil {
			return "", "", nil, err
		}
	}
	return t.Method, path, body, nil
}

func check(p param, v any) error {
	switch p.Kind {
	case str:
		s, ok := v.(string)
		if !ok {
			return fmt.Errorf("%s must be a string", p.Name)
		}
		if s == "" && (p.Required || p.In == inPath) {
			return fmt.Errorf("%s must not be empty", p.Name)
		}
	case boolean:
		if _, ok := v.(bool); !ok {
			return fmt.Errorf("%s must be true or false", p.Name)
		}
	case integer:
		n, ok := v.(json.Number)
		if !ok {
			return fmt.Errorf("%s must be a number", p.Name)
		}
		if i, err := n.Int64(); err != nil || i < 1 {
			return fmt.Errorf("%s must be a positive whole number", p.Name)
		}
	case object:
		if _, ok := v.(map[string]any); !ok {
			return fmt.Errorf("%s must be an object", p.Name)
		}
	case array:
		if _, ok := v.([]any); !ok {
			return fmt.Errorf("%s must be an array", p.Name)
		}
	}
	return nil
}
