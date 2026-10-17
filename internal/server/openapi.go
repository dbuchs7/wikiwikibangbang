package server

import (
	"net/http"
	"strings"

	"github.com/dbuchs7/wikiwikibangbang/internal/buildinfo"
)

// openAPI describes the routes it is given as an OpenAPI 3.1 document. It
// is generated from the route table that also registers the handlers, so
// the description cannot drift from what is served (ADR-0011). Request and
// response schemas are not described yet.
func openAPI(routes []Route) http.Handler {
	paths := map[string]map[string]any{}
	for _, rt := range routes {
		if !strings.HasPrefix(rt.Pattern, "/api/") {
			continue
		}
		op := map[string]any{"summary": rt.Summary, "x-access": rt.Access.String()}
		if rt.Access != Public {
			op["security"] = []map[string][]string{{"session": {}}, {"token": {}}}
		}
		var params []map[string]any
		for _, seg := range strings.Split(rt.Pattern, "/") {
			if name, ok := strings.CutPrefix(seg, "{"); ok {
				name = strings.TrimSuffix(name, "}")
				params = append(params, map[string]any{"name": name, "in": "path", "required": true, "schema": map[string]string{"type": "string"}})
			}
		}
		if params != nil {
			op["parameters"] = params
		}
		if paths[rt.Pattern] == nil {
			paths[rt.Pattern] = map[string]any{}
		}
		paths[rt.Pattern][strings.ToLower(rt.Method)] = op
	}
	doc := map[string]any{
		"openapi": "3.1.0",
		"info":    map[string]string{"title": "wikiwikibangbang", "version": buildinfo.Version},
		"paths":   paths,
		"components": map[string]any{"securitySchemes": map[string]any{
			"session": map[string]string{"type": "apiKey", "in": "cookie", "name": SessionCookie},
			"token":   map[string]string{"type": "http", "scheme": "bearer"},
		}},
	}
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, http.StatusOK, doc) })
}
