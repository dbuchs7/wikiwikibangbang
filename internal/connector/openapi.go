package connector

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"maps"
	"net/url"
	"regexp"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/dbuchs7/wikiwikibangbang/internal/site"
)

// An OpenAPI description is no source of its own: it describes one
// (ADR-0130). Read, it offers the lists its API answers, each as the
// settings of a JSON source (ADR-0127) that an administrator picks and
// adjusts; the connector then reads the API as any JSON source.

// maxSpec bounds an API description; a larger one is refused.
const maxSpec = 16 << 20

// maxLists bounds how many lists a description offers.
const maxLists = 200

// specDepth bounds how deep a description's schemas are followed, so a
// schema that refers to itself ends.
const specDepth = 8

// List is a list an API description offers to read: the operation that
// answers it and the JSON source's settings that read it.
type List struct {
	Operation string          `json:"operation"`
	Summary   string          `json:"summary,omitempty"`
	Source    site.JSONSource `json:"source"`
}

// Description is what an API description offers.
type Description struct {
	Title string `json:"title"`
	Lists []List `json:"lists"`
	// Problems name the operations left out, and why.
	Problems []string `json:"problems"`
}

// ErrNoDescription is a document that is no OpenAPI or Swagger
// description.
var ErrNoDescription = errors.New("connector: not an OpenAPI description")

// DescribeOpenAPI reads the API description at addr, in JSON or YAML,
// sending the credential of the connector id as a bearer token if one
// is set; the request is guarded as every connector's (ADR-0127).
func (r *Runner) DescribeOpenAPI(ctx context.Context, id, addr string) (Description, error) {
	data, err := fetch(ctx, r.env(id), addr, "application/json, application/yaml;q=0.9, */*;q=0.5", maxSpec)
	if err != nil {
		return Description{}, err
	}
	return describeSpec(data, addr)
}

// describeSpec finds the lists of a description read from specURL: every
// GET operation without parameters that must be filled in, whose answer
// is a list of objects or holds one.
func describeSpec(data []byte, specURL string) (Description, error) {
	var raw any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return Description{}, fmt.Errorf("%w: %v", ErrNoDescription, err)
	}
	root := obj(textKeys(raw, 0))
	d := spec{root: root}
	v3, v2 := str(root["openapi"]), str(root["swagger"])
	if v2 == "2" {
		v2 = "2.0" // swagger: 2.0 read as a number
	}
	if !strings.HasPrefix(v3, "3.") && v2 != "2.0" {
		return Description{}, fmt.Errorf("%w: it names neither openapi 3 nor swagger 2.0", ErrNoDescription)
	}
	out := Description{Title: str(obj(root["info"])["title"]), Lists: []List{}, Problems: []string{}}
	base, err := d.base(specURL, v2 != "")
	if err != nil {
		return out, err
	}
	paths := obj(root["paths"])
	for _, p := range slices.Sorted(maps.Keys(paths)) {
		item := d.resolve(paths[p])
		op := obj(item["get"])
		if op == nil {
			continue
		}
		name := "GET " + p
		l, problem := d.list(base, p, item, op)
		switch {
		case problem != "":
			out.Problems = append(out.Problems, name+": "+problem)
		case len(out.Lists) == maxLists:
			out.Problems = append(out.Problems, fmt.Sprintf("%s: the description offers more than %d lists; the rest are left out", name, maxLists))
		default:
			l.Operation, l.Summary = name, oneLine(cmp.Or(str(op["summary"]), str(op["operationId"])))
			l.Source.Spec, l.Source.Operation = specURL, name
			out.Lists = append(out.Lists, l)
		}
	}
	return out, nil
}

// spec is a parsed description, for its references.
type spec struct{ root map[string]any }

// resolve follows a local reference, "#/components/schemas/X", and
// returns the object it names; an external reference resolves to nil.
func (d spec) resolve(v any) map[string]any {
	for range specDepth {
		m := obj(v)
		ref, ok := m["$ref"].(string)
		if !ok {
			return m
		}
		target, ok := strings.CutPrefix(ref, "#/")
		if !ok {
			return nil
		}
		var at any = d.root
		for part := range strings.SplitSeq(target, "/") {
			part = strings.ReplaceAll(strings.ReplaceAll(part, "~1", "/"), "~0", "~")
			if part, err := url.PathUnescape(part); err == nil {
				at = obj(at)[part]
			}
		}
		v = at
	}
	return nil
}

// base is the address the paths are relative to: the first server, its
// variables at their defaults, or for Swagger 2.0 the scheme, host and
// base path; relative to the description's own address.
func (d spec) base(specURL string, swagger2 bool) (*url.URL, error) {
	from, err := url.Parse(specURL)
	if err != nil {
		return nil, err
	}
	raw := "/"
	if swagger2 {
		u := url.URL{Scheme: from.Scheme, Host: cmp.Or(str(d.root["host"]), from.Host), Path: str(d.root["basePath"])}
		if schemes, _ := d.root["schemes"].([]any); len(schemes) > 0 && !slices.Contains(schemes, any(from.Scheme)) {
			u.Scheme = str(schemes[0])
		}
		raw = u.String()
	} else if servers, _ := d.root["servers"].([]any); len(servers) > 0 {
		s := obj(servers[0])
		raw = str(s["url"])
		for name, v := range obj(s["variables"]) {
			raw = strings.ReplaceAll(raw, "{"+name+"}", str(obj(v)["default"]))
		}
	}
	ref, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("%w: its server %q is no address", ErrNoDescription, raw)
	}
	return from.ResolveReference(ref), nil
}

// list makes the JSON source of an operation, or says why there is none.
func (d spec) list(base *url.URL, path string, item, op map[string]any) (List, string) {
	if strings.ContainsAny(path, "{}") {
		return List{}, "it needs path parameters"
	}
	q := url.Values{}
	for _, p := range append(slices.Clone(arr(item["parameters"])), arr(op["parameters"])...) {
		p := d.resolve(p)
		in, name := str(p["in"]), str(p["name"])
		def := cmp.Or(str(p["default"]), str(obj(p["schema"])["default"]), str(p["example"]))
		switch {
		case in == "query" && def != "":
			q.Set(name, def)
		case p["required"] == true && (in == "query" || in == "path"):
			return List{}, fmt.Sprintf("its parameter %q must be filled in", name)
		}
	}
	u := *base
	u.Path = strings.TrimRight(u.Path, "/") + path
	u.RawQuery = q.Encode()
	if !webLink(u.String()) {
		return List{}, fmt.Sprintf("%q is no web address", u.String())
	}
	schema := d.answer(op)
	if schema == nil {
		return List{}, "it describes no JSON answer"
	}
	items, entry, wrapper := d.findList(schema, "", 0)
	if entry == nil {
		return List{}, "its answer holds no list of objects"
	}
	fields := d.scalars(entry, "", 0)
	src := site.JSONSource{URL: u.String(), Items: items, Values: map[string]string{}, Language: "de"}
	src.ID = pick(fields, []string{"id", "identifier", "uuid", "key", "slug", "code"}, func(p string) bool {
		return strings.HasSuffix(strings.ToLower(p), "id")
	})
	if src.ID == "" {
		return List{}, "its entries have no field that identifies them"
	}
	used := map[string]bool{src.ID: true}
	free := func(p string) bool { return !used[p] }
	src.Title = pick(unused(fields, used), []string{"title", "name", "label", "displayname", "display_name", "summary", "subject"}, nil)
	src.Title = cmp.Or(src.Title, src.ID)
	used[src.Title] = true
	src.Link = pick(unused(fields, used), []string{"url", "href", "link", "web_url", "html_url", "homepage", "self"}, func(p string) bool {
		return fields[p] == "uri"
	})
	used[src.Link] = true
	src.Text = pick(unused(fields, used), []string{"description", "text", "summary", "abstract", "body", "notes"}, nil)
	used[src.Text] = true
	for _, p := range slices.Sorted(maps.Keys(fields)) {
		if free(p) && len(src.Values) < 50 {
			src.Values[p] = p
		}
	}
	if wrapper != nil {
		src.Next = d.nextLink(wrapper, "", 0)
	}
	return List{Source: src}, ""
}

// answer is the schema of an operation's JSON answer: of 200, else of
// 2XX or default; in OpenAPI 3 of a JSON media type, in Swagger 2.0 the
// response's own.
func (d spec) answer(op map[string]any) any {
	responses := obj(op["responses"])
	for _, code := range []string{"200", "2XX", "default"} {
		r := d.resolve(responses[code])
		if r == nil {
			continue
		}
		if s, ok := r["schema"]; ok {
			return s
		}
		content := obj(r["content"])
		for _, mt := range slices.Sorted(maps.Keys(content)) {
			if strings.Contains(mt, "json") {
				return obj(content[mt])["schema"]
			}
		}
	}
	return nil
}

// preferred are the names a list is most likely held under.
var preferred = []string{"entries", "items", "results", "data", "records", "value", "values", "content", "rows", "elements", "list"}

// findList finds the list of objects in an answer: the answer itself,
// or a property, the preferred names first, up to three levels deep. It
// returns the path to it, its entries' schema and, if the list is held
// in an object, that object's schema.
func (d spec) findList(v any, prefix string, depth int) (string, map[string]any, map[string]any) {
	s := d.schema(v)
	if s == nil || depth > 3 {
		return "", nil, nil
	}
	if is(s, "array") {
		if entry := d.schema(s["items"]); entry != nil && is(entry, "object") {
			return prefix, entry, nil
		}
		return "", nil, nil
	}
	props := obj(s["properties"])
	names := slices.SortedFunc(maps.Keys(props), func(a, b string) int {
		ia, ib := slices.Index(preferred, strings.ToLower(a)), slices.Index(preferred, strings.ToLower(b))
		if ia < 0 {
			ia = len(preferred)
		}
		if ib < 0 {
			ib = len(preferred)
		}
		return cmp.Or(cmp.Compare(ia, ib), cmp.Compare(a, b))
	})
	for _, name := range names {
		if !pathPart.MatchString(name) {
			continue
		}
		if path, entry, wrapper := d.findList(props[name], join(prefix, name), depth+1); entry != nil {
			if wrapper == nil {
				wrapper = s
			}
			return path, entry, wrapper
		}
	}
	return "", nil, nil
}

// pathPart is a key a JSON source's path can name (site.JSONSource).
var pathPart = regexp.MustCompile(`^[A-Za-z0-9_@$-]+$`)

// scalars are an entry's values a JSON source can read: texts, numbers,
// truth values and lists of them, by path, with the format of a text (as
// "uri"); objects are followed two levels deep.
func (d spec) scalars(s map[string]any, prefix string, depth int) map[string]string {
	out := map[string]string{}
	props := obj(s["properties"])
	for _, name := range slices.Sorted(maps.Keys(props)) {
		if !pathPart.MatchString(name) || len(out) >= 100 {
			continue
		}
		p, path := d.schema(props[name]), join(prefix, name)
		switch {
		case p == nil:
		case is(p, "object") && depth < 2:
			maps.Copy(out, d.scalars(p, path, depth+1))
		case is(p, "array"):
			if e := d.schema(p["items"]); e != nil && !is(e, "object") && !is(e, "array") {
				out[path] = "list"
			}
		case is(p, "string") || is(p, "integer") || is(p, "number") || is(p, "boolean") || p["type"] == nil && p["format"] != nil:
			out[path] = cmp.Or(str(p["format"]), str(p["type"]))
		}
	}
	return out
}

// nextLink finds the address of the next page in the object a list is
// held in: a text whose path says "next", as next or _links.next.href,
// up to three levels deep.
func (d spec) nextLink(s map[string]any, prefix string, depth int) string {
	props := obj(s["properties"])
	for _, name := range slices.Sorted(maps.Keys(props)) {
		p := d.schema(props[name])
		if p == nil || !pathPart.MatchString(name) {
			continue
		}
		path := join(prefix, name)
		if strings.Contains(strings.ToLower(path), "next") && is(p, "string") {
			return path
		}
		if is(p, "object") && depth < 3 {
			if n := d.nextLink(p, path, depth+1); n != "" {
				return n
			}
		}
	}
	return ""
}

// schema resolves a schema and merges the properties of its allOf
// members; of oneOf and anyOf, the first member counts.
func (d spec) schema(v any) map[string]any {
	s := d.resolve(v)
	if s == nil {
		return nil
	}
	first := arr(s["oneOf"])
	if len(first) == 0 {
		first = arr(s["anyOf"])
	}
	if len(first) > 0 && s["properties"] == nil && s["type"] == nil {
		return d.resolve(first[0])
	}
	members := arr(s["allOf"])
	if len(members) == 0 {
		return s
	}
	merged := maps.Clone(s)
	props := maps.Clone(obj(s["properties"]))
	if props == nil {
		props = map[string]any{}
	}
	for _, m := range members {
		if ms := d.resolve(m); ms != nil {
			maps.Copy(props, obj(ms["properties"]))
			if merged["type"] == nil {
				merged["type"] = ms["type"]
			}
		}
	}
	merged["properties"] = props
	delete(merged, "allOf")
	return merged
}

// is reports whether a schema has a type; OpenAPI 3.1 may list several,
// and an object may only name its properties.
func is(s map[string]any, typ string) bool {
	switch t := s["type"].(type) {
	case string:
		return t == typ
	case []any:
		return slices.Contains(t, any(typ))
	case nil:
		return typ == "object" && s["properties"] != nil
	}
	return false
}

// pick is the first path of the fields named one of names, by its last
// key, ignoring case; else the first that fits, if fits is given.
func pick(fields map[string]string, names []string, fits func(string) bool) string {
	paths := slices.Sorted(maps.Keys(fields))
	slices.SortStableFunc(paths, func(a, b string) int { return cmp.Compare(strings.Count(a, "."), strings.Count(b, ".")) })
	for _, n := range names {
		for _, p := range paths {
			if strings.EqualFold(last(p), n) {
				return p
			}
		}
	}
	if fits != nil {
		for _, p := range paths {
			if fits(p) && !strings.Contains(p, ".") {
				return p
			}
		}
	}
	return ""
}

func unused(fields map[string]string, used map[string]bool) map[string]string {
	out := maps.Clone(fields)
	maps.DeleteFunc(out, func(p, _ string) bool { return used[p] })
	return out
}

func last(path string) string { return path[strings.LastIndex(path, ".")+1:] }

func join(prefix, name string) string {
	if prefix == "" {
		return name
	}
	return prefix + "." + name
}

// textKeys turns the keys of YAML mappings into texts: a response code
// written 200 is read as a number. Deeper than a description goes, the
// rest is left out.
func textKeys(v any, depth int) any {
	if depth > 64 {
		return nil
	}
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, e := range x {
			out[k] = textKeys(e, depth+1)
		}
		return out
	case map[any]any:
		out := make(map[string]any, len(x))
		for k, e := range x {
			out[str(k)] = textKeys(e, depth+1)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = textKeys(e, depth+1)
		}
		return out
	}
	return v
}

func obj(v any) map[string]any { m, _ := v.(map[string]any); return m }

func arr(v any) []any { a, _ := v.([]any); return a }

// str is a scalar as text: a description's numbers, as "3.0", may be
// read as numbers.
func str(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case nil, map[string]any, []any:
		return ""
	}
	return fmt.Sprint(v)
}
