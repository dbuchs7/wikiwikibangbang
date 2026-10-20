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
	"image (ref: an attachment ID of the item, text the caption, width 10-100 percent of the column or absent for its own size, align left or right to let text flow beside it, or center). " +
	"Paragraphs, headings, quotes and panels may have align center, right or justify. " +
	"Inline formats are marks over the text, counted in Unicode code points: " +
	"{\"type\": \"bold\"|\"italic\"|\"underline\"|\"strike\"|\"highlight\"|\"code\"|\"link\" (with href)|\"mention\" (with ref, an item ID), \"start\": 0, \"end\": 5}. " +
	"Example: [{\"kind\":\"heading\",\"level\":1,\"text\":\"Runbook\"},{\"kind\":\"paragraph\",\"text\":\"Stop the writers first.\"," +
	"\"marks\":[{\"type\":\"bold\",\"start\":0,\"end\":4}]},{\"kind\":\"bullet\",\"text\":\"db1\"},{\"kind\":\"bullet\",\"text\":\"db2\"}]."

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
var tools = []tool{
	{Name: "list_spaces", Description: "List the spaces the caller holds a role in.", Method: "GET", Path: "/api/spaces"},
	{Name: "list_types", Description: "List the item types with their fields and workflows; read it before creating an item or setting a property.",
		Method: "GET", Path: "/api/types"},
	{Name: "find_items", Description: "Find readable items by space, type, state or parent, paged. The answer's total says whether it is exact.",
		Method: "GET", Path: "/api/items", Params: append([]param{
			{"space", str, inQuery, false, "A space key, e.g. PLAT."},
			{"type", str, inQuery, false, "A type name, e.g. task."},
			{"state", str, inQuery, false, "A workflow state, e.g. open."},
			{"parent", str, inQuery, false, "The ID of the containing item."},
			{"label", str, inQuery, false, "A label the items carry, e.g. runbook."},
		}, paging...)},
	{Name: "search_items", Description: "Full-text search over readable items, best match first, paged.",
		Method: "GET", Path: "/api/search", Params: append([]param{
			{"q", str, inQuery, true, "Words to find; every word must occur."},
			{"space", str, inQuery, false, "Limit to a space key."},
			{"type", str, inQuery, false, "Limit to a type name."},
			{"state", str, inQuery, false, "Limit to a workflow state."},
		}, paging...)},
	{Name: "get_item", Description: "Get one item: title, type, state, properties and body.", Method: "GET", Path: "/api/items/{ref}",
		Params: []param{refParam}},
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
		"Refused while people edit it in a live session. To add to a body, read it with get_item and send it back extended. " + blockFormat,
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
