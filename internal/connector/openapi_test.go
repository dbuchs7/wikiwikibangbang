package connector

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dbuchs7/wikiwikibangbang/internal/site"
)

// registrySpec describes a registry of services in OpenAPI 3.0, in JSON:
// a list answered as such, one held in an object with a next page, and
// operations that cannot be read as a list.
const registrySpec = `{
  "openapi": "3.0.3",
  "info": {"title": "Service registry"},
  "servers": [{"url": "/{version}", "variables": {"version": {"default": "v1"}}}],
  "paths": {
    "/services": {"get": {"summary": "All services", "responses": {"200": {"content": {"application/json": {
      "schema": {"type": "array", "items": {"$ref": "#/components/schemas/Service"}}}}}}}},
    "/services/{id}": {"get": {"responses": {"200": {"content": {"application/json": {"schema": {"$ref": "#/components/schemas/Service"}}}}}}},
    "/search": {"get": {"parameters": [{"in": "query", "name": "q", "required": true}], "responses": {"200": {}}}},
    "/tags": {"get": {"operationId": "listTags", "parameters": [{"$ref": "#/components/parameters/limit"}],
      "responses": {"2XX": {"content": {"application/json": {"schema": {"type": "object", "properties": {
        "_links": {"type": "object", "properties": {"next": {"type": "object", "properties": {"href": {"type": "string"}}}}},
        "data": {"type": "array", "items": {"type": "object", "properties": {"slug": {"type": "string"}, "label": {"type": "string"}}}}}}}}}}}},
    "/health": {"get": {"responses": {"200": {"content": {"application/json": {"schema": {"type": "object", "properties": {"ok": {"type": "boolean"}}}}}}}}},
    "/report": {"get": {"responses": {"200": {"content": {"text/csv": {}}}}}},
    "/numbers": {"get": {"responses": {"200": {"content": {"application/json": {"schema": {"type": "array", "items": {"type": "integer"}}}}}}}},
    "/anonymous": {"get": {"responses": {"200": {"content": {"application/json": {"schema": {"type": "array", "items": {"type": "object", "properties": {"colour": {"type": "string"}}}}}}}}}},
    "/upload": {"post": {}}
  },
  "components": {
    "parameters": {"limit": {"in": "query", "name": "limit", "schema": {"type": "integer", "default": 100}}},
    "schemas": {
      "Base": {"type": "object", "properties": {"id": {"type": "integer"}}},
      "Service": {"allOf": [{"$ref": "#/components/schemas/Base"}, {"type": "object", "properties": {
        "name": {"type": "string"}, "description": {"type": "string"}, "homepage": {"type": "string", "format": "uri"},
        "owner": {"type": "object", "properties": {"name": {"type": "string"}, "email": {"type": "string", "format": "email"}}},
        "tags": {"type": "array", "items": {"type": "string"}}, "links": {"type": "array", "items": {"type": "object"}},
        "odata.etag": {"type": "string"}, "live": {"type": "boolean"}, "since": {"format": "date"}}}]}
    }
  }
}`

func TestADescriptionOffersItsLists(t *testing.T) {
	d, err := describeSpec([]byte(registrySpec), "https://registry.example/openapi.json")
	if err != nil {
		t.Fatal(err)
	}
	if d.Title != "Service registry" || len(d.Lists) != 2 {
		t.Fatalf("%s: lists %+v\nproblems %v", d.Title, d.Lists, d.Problems)
	}
	services, tags := d.Lists[0], d.Lists[1]
	want := site.JSONSource{URL: "https://registry.example/v1/services", ID: "id", Title: "name", Link: "homepage", Text: "description",
		Values:   map[string]string{"owner.email": "owner.email", "owner.name": "owner.name", "tags": "tags", "live": "live", "since": "since"},
		Language: "de", Spec: "https://registry.example/openapi.json", Operation: "GET /services"}
	if services.Operation != "GET /services" || services.Summary != "All services" || fmt.Sprint(services.Source) != fmt.Sprint(want) {
		t.Errorf("services\n got %+v\nwant %+v", services.Source, want)
	}
	if tags.Summary != "listTags" || tags.Source.URL != "https://registry.example/v1/tags?limit=100" || tags.Source.Items != "data" ||
		tags.Source.ID != "slug" || tags.Source.Title != "label" || tags.Source.Next != "_links.next.href" {
		t.Errorf("tags %+v", tags.Source)
	}
	problems := strings.Join(d.Problems, "\n")
	for _, p := range []string{"GET /services/{id}: it needs path parameters", `GET /search: its parameter "q" must be filled in`,
		"GET /health: its answer holds no list", "GET /report: it describes no JSON answer", "GET /numbers: its answer holds no list",
		"GET /anonymous: its entries have no field that identifies them"} {
		if !strings.Contains(problems, p) {
			t.Errorf("no problem %q in\n%s", p, problems)
		}
	}
	if strings.Contains(problems, "upload") {
		t.Error("an operation that reads nothing is named")
	}
	for _, l := range d.Lists {
		c := site.Connector{ID: "x", Name: "x", Kind: site.KindJSON, IntervalHours: 1, JSON: &l.Source}
		if err := c.Validate(); err != nil {
			t.Errorf("%s does not give valid settings: %v", l.Operation, err)
		}
	}
}

func TestDescriptionsInYAMLAndSwagger(t *testing.T) {
	v31 := `
openapi: 3.1.0
info: {title: Tree}
servers: [{url: "https://api.example/"}]
paths:
  /nodes:
    get:
      responses:
        "200":
          content:
            application/hal+json:
              schema:
                type: [object, "null"]
                properties:
                  page: {type: object, properties: {nextPage: {type: string}}}
                  results:
                    type: array
                    items: {$ref: "#/components/schemas/Node"}
  /remote:
    get:
      responses:
        default: {content: {application/json: {schema: {$ref: "other.yaml#/Thing"}}}}
components:
  schemas:
    Node:
      type: object
      properties:
        uuid: {type: string}
        label: {type: string}
        child: {$ref: "#/components/schemas/Node"}
        choice: {oneOf: [{type: string}, {type: integer}]}
`
	d, err := describeSpec([]byte(v31), "https://api.example/spec.yaml")
	if err != nil || len(d.Lists) != 1 {
		t.Fatalf("%+v %v", d, err)
	}
	if s := d.Lists[0].Source; s.URL != "https://api.example/nodes" || s.Items != "results" || s.ID != "uuid" || s.Title != "label" ||
		s.Next != "page.nextPage" || s.Values["child.label"] != "child.label" || s.Values["choice"] != "choice" {
		t.Errorf("3.1 %+v", s)
	}

	v2 := `
swagger: 2.0
info: {title: Old}
host: legacy.example
basePath: /api
schemes: [https]
paths:
  /items:
    get:
      responses:
        200: {schema: {type: array, items: {$ref: "#/definitions/Item"}}}
definitions:
  Item: {properties: {key: {type: string}, title: {type: string}}}
`
	d, err = describeSpec([]byte(v2), "http://docs.example/swagger.yaml")
	if err != nil || len(d.Lists) != 1 || d.Lists[0].Source.URL != "https://legacy.example/api/items" || d.Lists[0].Source.ID != "key" {
		t.Fatalf("swagger %+v %v", d, err)
	}

	for name, doc := range map[string]string{
		"no description": `{"hello": "world"}`,
		"no YAML":        "openapi: [",
		"a bad server":   `{"openapi": "3.0.0", "servers": [{"url": "http://[::1"}], "paths": {}}`,
	} {
		if _, err := describeSpec([]byte(doc), "https://x.example/spec"); err == nil {
			t.Errorf("%s: read", name)
		}
	}
	if _, err := describeSpec([]byte(`{"openapi": "3.0.0"}`), "%zz"); err == nil {
		t.Error("a bad address of the description")
	}
	if d, _ := describeSpec([]byte(`{"openapi": "3.0.0", "servers": [{"url": "ftp://files.example"}], "paths": {"/a": {"get": {}}}}`), "https://x.example/"); len(d.Problems) != 1 || !strings.Contains(d.Problems[0], "no web address") {
		t.Errorf("a server that is no web address %+v", d.Problems)
	}
}

func TestManyListsAreBounded(t *testing.T) {
	var paths []string
	for i := range maxLists + 2 {
		paths = append(paths, fmt.Sprintf(`"/l%03d": {"get": {"responses": {"200": {"content": {"application/json": {"schema": {"type": "array", "items": {"type": "object", "properties": {"id": {"type": "string"}}}}}}}}}}`, i))
	}
	d, err := describeSpec([]byte(`{"openapi": "3.0.0", "paths": {`+strings.Join(paths, ",")+`}}`), "https://x.example/")
	if err != nil || len(d.Lists) != maxLists || len(d.Problems) != 2 {
		t.Errorf("%d lists, %d problems, %v", len(d.Lists), len(d.Problems), err)
	}
}

func TestADescriptionIsFetchedAsASource(t *testing.T) {
	auth := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		switch r.URL.Path {
		case "/openapi.json":
			fmt.Fprint(w, registrySpec)
		case "/huge":
			w.Write([]byte(strings.Repeat(" ", maxSpec+1)))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	f := setup(t)
	ctx := context.Background()
	d, err := f.r.DescribeOpenAPI(ctx, "feed", srv.URL+"/openapi.json")
	if err != nil || len(d.Lists) != 2 || !strings.HasPrefix(d.Lists[0].Source.URL, srv.URL+"/v1/") {
		t.Fatalf("%+v %v", d, err)
	}
	f.r.getenv = allowPrivate(func(k string) string { return map[string]string{TokenVar("feed"): "spec-token"}[k] })
	if _, err := f.r.DescribeOpenAPI(ctx, "feed", srv.URL+"/openapi.json"); err != nil || auth != "Bearer spec-token" {
		t.Errorf("the credential %q: %v", auth, err)
	}
	if _, err := f.r.DescribeOpenAPI(ctx, "feed", srv.URL+"/huge"); err == nil || !strings.Contains(err.Error(), "larger") {
		t.Errorf("a huge description: %v", err)
	}
	if _, err := f.r.DescribeOpenAPI(ctx, "feed", srv.URL+"/nope"); err == nil {
		t.Error("a missing description")
	}
	f.r.getenv = func(string) string { return "" }
	if _, err := f.r.DescribeOpenAPI(ctx, "feed", srv.URL+"/openapi.json"); !errors.Is(err, ErrPrivateAddress) {
		t.Errorf("a description in the server's own network: %v", err)
	}
}

func TestADescriptionNamesItsPaging(t *testing.T) {
	doc := `
openapi: 3.0.3
paths:
  /pages:
    get:
      parameters:
        - {in: query, name: page, required: true, schema: {type: integer, minimum: 0}}
        - {in: query, name: per_page, schema: {type: integer, default: 30, maximum: 5000}}
        - {in: query, name: from, schema: {type: string, format: date}}
        - {in: query, name: updatedSince, schema: {type: string, format: date-time}}
      responses:
        200:
          content:
            application/json:
              schema:
                type: object
                properties:
                  meta: {type: object, properties: {totalCount: {type: integer}}}
                  data: {type: array, items: {type: object, properties: {id: {type: string}}}}
  /skipped:
    get:
      parameters:
        - {in: query, name: modified_after, schema: {type: integer}}
        - {in: query, name: $skip, schema: {type: integer}}
        - {in: query, name: $top, schema: {type: integer, default: 25}}
      responses:
        200: {content: {application/json: {schema: {type: array, items: {type: object, properties: {id: {type: string}}}}}}}
  /linked:
    get:
      parameters: [{in: query, name: page, schema: {type: integer}}]
      responses:
        200: {content: {application/json: {schema: {type: object, properties: {next: {type: string}, items: {type: array, items: {properties: {id: {type: string}}}}}}}}}
  /sized:
    get:
      parameters: [{in: query, name: limit, required: true, schema: {type: integer}}]
      responses:
        200: {content: {application/json: {schema: {type: array, items: {properties: {id: {type: string}}}}}}}
`
	d, err := describeSpec([]byte(doc), "https://api.example/spec.yaml")
	if err != nil || len(d.Lists) != 4 {
		t.Fatalf("%+v %v", d, err)
	}
	byOp := map[string]site.JSONSource{}
	for _, l := range d.Lists {
		byOp[l.Operation] = l.Source
	}
	if p := byOp["GET /pages"].Paging; p == nil || *p != (site.Paging{Kind: site.PagingPage, Param: "page", Start: 0, SizeParam: "per_page", Size: 1000, Total: "meta.totalCount"}) {
		t.Errorf("by page %+v", p)
	}
	if s := byOp["GET /pages"].Since; s == nil || *s != (site.Since{Param: "updatedSince", Format: site.SinceRFC3339, FullDays: 7}) {
		t.Errorf("what changed by a time %+v", s)
	}
	if s := byOp["GET /skipped"].Since; s == nil || *s != (site.Since{Param: "modified_after", Format: site.SinceUnix, FullDays: 7}) {
		t.Errorf("what changed by seconds %+v", s)
	}
	if s := byOp["GET /linked"].Since; s != nil {
		t.Errorf("a list without %+v", s)
	}
	if s := byOp["GET /pages"]; s.URL != "https://api.example/pages" {
		t.Errorf("the address keeps the page parameters out %s", s.URL)
	}
	if p := byOp["GET /skipped"].Paging; p == nil || *p != (site.Paging{Kind: site.PagingOffset, Param: "$skip", SizeParam: "$top", Size: 25}) {
		t.Errorf("by offset %+v", p)
	}
	if s := byOp["GET /linked"]; s.Paging != nil || s.Next != "next" {
		t.Errorf("a next link is followed %+v", s)
	}
	if s := byOp["GET /sized"]; s.Paging != nil || s.URL != "https://api.example/sized?limit=100" {
		t.Errorf("a size alone %+v", s)
	}
	for _, l := range d.Lists {
		c := site.Connector{ID: "x", Name: "x", Kind: site.KindJSON, IntervalHours: 1, JSON: &l.Source}
		if err := c.Validate(); err != nil {
			t.Errorf("%s: %v", l.Operation, err)
		}
	}
}
