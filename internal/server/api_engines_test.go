package server

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dbuchs7/wikiwikibangbang/internal/audit"
	"github.com/dbuchs7/wikiwikibangbang/internal/engine"
)

// fakeEngine answers as an atlas engine does, for the credential "secret".
func fakeEngine(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/api/v1/processes":
			_, _ = w.Write([]byte(`[{"key":11,"processId":"order","name":"Order","version":1,"deployedAt":1761400000},
				{"key":12,"processId":"order","name":"Order to cash","version":2,"deployedAt":1761480000}]`))
		case "/api/v1/processes/12/xml":
			_, _ = w.Write([]byte(`<?xml version="1.0"?><bpmn:definitions id="v2"/>`))
		case "/api/v1/forms":
			_, _ = w.Write([]byte(`[{"id":"travel","name":"Travel request","savedAt":1791795101},{"id":"a/b","name":"Slashed","savedAt":1791795000}]`))
		case "/api/v1/forms/travel":
			_, _ = w.Write([]byte(`{"id":"travel","name":"Travel request","savedAt":1791795101,"schema":{"type":"default","components":[{"key":"to","type":"textfield"}]}}`))
		case "/api/v1/forms/a/b":
			_, _ = w.Write([]byte(`{"id":"a/b","schema":{"components":[]}}`))
		case "/api/v1/decision-deployments":
			_, _ = w.Write([]byte(`[{"key":21,"decisionId":"Decision 1","version":3,"modelName":"Credit","deployedAt":1790367024},
				{"key":20,"decisionId":"Decision 1","version":2,"modelName":"Credit","deployedAt":1790235601}]`))
		case "/api/v1/decision-deployments/21/xml":
			_, _ = w.Write([]byte(`<?xml version="1.0"?><definitions id="credit-v3"/>`))
		case "/api/v1/architectures":
			_, _ = w.Write([]byte(`{"applications":[{"applicationId":"c46e","application":"Bestellwesen","releases":[
				{"version":3,"releaseId":"cfc9","publishedAt":1793094979,"takenAt":1793094979,"recorded":["strategy","business","application","data","technology"]},
				{"version":2,"releaseId":"b1a0","publishedAt":1793000000,"takenAt":1793000001,"recorded":["application","technology"]}]}]}`))
		case "/api/v1/applications/c46e/releases/3/architecture.embed.svg":
			w.Header().Set("Content-Type", "image/svg+xml; charset=utf-8")
			_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>` + "\n" + `<svg xmlns="http://www.w3.org/2000/svg" width="1140" height="300"><title>v3 ` + r.URL.Query().Get("layers") + `</title><use href="#a"/></svg>`))
		case "/api/v1/applications/c46e/releases/2/architecture.embed.svg":
			w.Header().Set("Content-Type", "image/svg+xml")
			_, _ = w.Write([]byte(`<svg xmlns="http://www.w3.org/2000/svg"><image href="https://elsewhere.example.org/tracker.png"/></svg>`))
		case "/api/v1/processes/12/runtime":
			_, _ = w.Write([]byte(`{"instances":2,"finished":5,"tokens":2,"incidentTotal":1,"incidentCountsExact":true,
				"elements":[{"elementId":"check","tokens":2,"visits":7,"incidents":1,"terminated":0}],
				"incidents":[{"elementId":"check","processInstanceKey":4711,"message":"customer Paul Muster has no address"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestProcessesFromAnEngine covers ADR-0129 through the API: administrators
// set engines up; every signed-in person lists an engine's processes and
// reads the newest version's model; operations only where allowed, and
// only counts; the credential is never answered.
func TestProcessesFromAnEngine(t *testing.T) {
	srv := fakeEngine(t)
	env := map[string]string{"WWBB_ENGINE_ATLAS_TOKEN": "secret"}
	w := newWorld(t, func(d *Deps) { d.Engines = engine.NewReader(func(k string) string { return env[k] }, srv.Client()) })

	w.bert.expect("PUT", "/api/engines/atlas", `{"name":"Atlas","kind":"atlas","baseURL":"`+srv.URL+`"}`, http.StatusForbidden)
	w.admin.expect("PUT", "/api/engines/Atlas", `{"name":"Atlas","kind":"atlas","baseURL":"`+srv.URL+`"}`, http.StatusBadRequest)
	w.admin.expect("PUT", "/api/engines/atlas", `{"name":"Atlas","kind":"atlas","baseURL":"ftp://x"}`, http.StatusBadRequest)
	set := w.admin.expect("PUT", "/api/engines/atlas", `{"name":"Atlas","kind":"atlas","baseURL":"`+srv.URL+`/"}`, http.StatusCreated)
	if set["credentialVar"] != "WWBB_ENGINE_ATLAS_TOKEN" || set["credential"] != true || set["baseURL"] != srv.URL {
		t.Fatalf("set %v", set)
	}
	w.admin.expect("PUT", "/api/engines/test", `{"name":"Test","kind":"atlas","baseURL":"http://127.0.0.1:1"}`, http.StatusCreated)

	admin := w.admin.expect("GET", "/api/engines", "", http.StatusOK)["engines"].([]any)
	reader := w.vera.expect("GET", "/api/engines", "", http.StatusOK)
	list := reader["engines"].([]any)
	if len(admin) != 2 || len(list) != 2 || admin[1].(map[string]any)["credential"] != false || reader["kinds"] != nil {
		t.Fatalf("engines: admin %v, reader %v", admin, reader)
	}
	if e := list[0].(map[string]any); e["id"] != "atlas" || e["name"] != "Atlas" || e["operations"] != false || e["baseURL"] != nil || e["credentialVar"] != nil {
		t.Fatalf("a reader sees %v", e)
	}

	procs := w.vera.expect("GET", "/api/engines/atlas/processes", "", http.StatusOK)["processes"].([]any)
	if len(procs) != 1 || procs[0].(map[string]any)["process"] != "order" || procs[0].(map[string]any)["version"] != float64(2) || procs[0].(map[string]any)["key"] != nil {
		t.Fatalf("processes %v", procs)
	}
	rec := w.carla.do("GET", "/api/engines/atlas/processes/order", "")
	if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Header().Get("Content-Type"), "application/json") || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("diagram: %d %v", rec.Code, rec.Header())
	}
	d := field(decodeBody(t, rec), "diagram").(map[string]any)
	if d["xml"] != `<?xml version="1.0"?><bpmn:definitions id="v2"/>` || d["name"] != "Order to cash" || d["version"] != float64(2) {
		t.Fatalf("diagram %v", d)
	}
	w.vera.expect("GET", "/api/engines/atlas/processes/nothing", "", http.StatusNotFound)
	w.vera.expect("GET", "/api/engines/gone/processes", "", http.StatusNotFound)
	if r := w.vera.do("GET", "/api/engines/test/processes", ""); r.Code != http.StatusBadGateway || !strings.Contains(r.Body.String(), "WWBB_ENGINE_TEST_TOKEN") {
		t.Fatalf("an engine without credential: %d %s", r.Code, r.Body)
	}

	// Operations are refused until an administrator allows them, then
	// they are counts and nothing else.
	w.vera.expect("GET", "/api/engines/atlas/processes/order/operations", "", http.StatusForbidden)
	w.admin.expect("PUT", "/api/engines/atlas", `{"name":"Atlas","kind":"atlas","baseURL":"`+srv.URL+`","operations":true}`, http.StatusOK)
	rec = w.vera.do("GET", "/api/engines/atlas/processes/order/operations", "")
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), "Paul Muster") || strings.Contains(rec.Body.String(), "4711") {
		t.Fatalf("operations: %d %s", rec.Code, rec.Body)
	}
	ops := field(decodeBody(t, rec), "operations").(map[string]any)
	if ops["instances"] != float64(2) || ops["incidents"] != float64(1) || ops["version"] != float64(2) ||
		ops["elements"].([]any)[0].(map[string]any)["passed"] != float64(5) {
		t.Fatalf("operations %v", ops)
	}

	env["WWBB_ENGINE_ATLAS_TOKEN"] = "wrong"
	if r := w.vera.do("GET", "/api/engines/atlas/processes", ""); r.Code != http.StatusBadGateway || strings.Contains(r.Body.String(), "wrong") || strings.Contains(r.Body.String(), "secret") {
		t.Fatalf("a refused credential: %d %s", r.Code, r.Body)
	}

	w.bert.expect("DELETE", "/api/engines/test", "", http.StatusForbidden)
	w.admin.expect("DELETE", "/api/engines/test", "", http.StatusNoContent)
	w.admin.expect("DELETE", "/api/engines/test", "", http.StatusNotFound)
	changes := 0
	for _, e := range w.db.Audit.List(0, 1000) {
		if e.Event == audit.EngineChanged {
			changes++
		}
	}
	if changes != 4 {
		t.Fatalf("%d engine changes audited, want 4", changes)
	}
}

// TestFormsAndDecisionsFromAnEngine covers ADR-0144 through the API: every
// signed-in person lists an engine's forms and decisions and reads a form's
// schema and a decision's newest deployed model; IDs with spaces and
// slashes are one path segment; the key of a deployment stays on the
// server.
func TestFormsAndDecisionsFromAnEngine(t *testing.T) {
	srv := fakeEngine(t)
	env := map[string]string{"WWBB_ENGINE_ATLAS_TOKEN": "secret"}
	w := newWorld(t, func(d *Deps) { d.Engines = engine.NewReader(func(k string) string { return env[k] }, srv.Client()) })
	w.admin.expect("PUT", "/api/engines/atlas", `{"name":"Atlas","kind":"atlas","baseURL":"`+srv.URL+`"}`, http.StatusCreated)

	forms := w.vera.expect("GET", "/api/engines/atlas/forms", "", http.StatusOK)["forms"].([]any)
	if len(forms) != 2 || forms[0].(map[string]any)["form"] != "a/b" || forms[1].(map[string]any)["name"] != "Travel request" {
		t.Fatalf("forms %v", forms)
	}
	rec := w.carla.do("GET", "/api/engines/atlas/forms/travel", "")
	if rec.Code != http.StatusOK || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("form: %d %v", rec.Code, rec.Header())
	}
	f := field(decodeBody(t, rec), "form").(map[string]any)
	if f["form"] != "travel" || f["schema"].(map[string]any)["type"] != "default" {
		t.Fatalf("form %v", f)
	}
	if f := field(w.vera.expect("GET", "/api/engines/atlas/forms/a%2Fb", "", http.StatusOK), "form").(map[string]any); f["form"] != "a/b" {
		t.Fatalf("a form with a slash: %v", f)
	}
	w.vera.expect("GET", "/api/engines/atlas/forms/nothing", "", http.StatusNotFound)

	decisions := w.vera.expect("GET", "/api/engines/atlas/decisions", "", http.StatusOK)["decisions"].([]any)
	if len(decisions) != 1 || decisions[0].(map[string]any)["decision"] != "Decision 1" || decisions[0].(map[string]any)["version"] != float64(3) || decisions[0].(map[string]any)["key"] != nil {
		t.Fatalf("decisions %v", decisions)
	}
	d := field(w.vera.expect("GET", "/api/engines/atlas/decisions/Decision%201", "", http.StatusOK), "decision").(map[string]any)
	if d["xml"] != `<?xml version="1.0"?><definitions id="credit-v3"/>` || d["model"] != "Credit" || d["version"] != float64(3) {
		t.Fatalf("decision %v", d)
	}
	w.vera.expect("GET", "/api/engines/atlas/decisions/nothing", "", http.StatusNotFound)
	w.vera.expect("GET", "/api/engines/gone/forms", "", http.StatusNotFound)
	w.vera.expect("GET", "/api/engines/gone/decisions/x", "", http.StatusNotFound)

	env["WWBB_ENGINE_ATLAS_TOKEN"] = "wrong"
	for _, path := range []string{"/api/engines/atlas/forms", "/api/engines/atlas/forms/travel", "/api/engines/atlas/decisions", "/api/engines/atlas/decisions/Decision%201"} {
		if r := w.vera.do("GET", path, ""); r.Code != http.StatusBadGateway || strings.Contains(r.Body.String(), "wrong") {
			t.Errorf("%s with a refused credential: %d %s", path, r.Code, r.Body)
		}
	}
}

// TestArchitectureSketchesFromAnEngine covers ADR-0145 through the API:
// every signed-in person and token lists the applications whose releases
// carry a sketch and reads a sketch as an image of this site, the newest
// release or a fixed one, with the layers asked; what is no release, no
// layer or no safe picture is refused, and the credential is never
// answered.
func TestArchitectureSketchesFromAnEngine(t *testing.T) {
	srv := fakeEngine(t)
	env := map[string]string{"WWBB_ENGINE_ATLAS_TOKEN": "secret"}
	w := newWorld(t, func(d *Deps) { d.Engines = engine.NewReader(func(k string) string { return env[k] }, srv.Client()) })
	w.admin.expect("PUT", "/api/engines/atlas", `{"name":"Atlas","kind":"atlas","baseURL":"`+srv.URL+`"}`, http.StatusCreated)

	listed := w.vera.expect("GET", "/api/engines/atlas/architectures", "", http.StatusOK)
	apps := listed["applications"].([]any)
	if len(apps) != 1 || field(listed, "engine", "name") != "Atlas" {
		t.Fatalf("architectures %v", listed)
	}
	app := apps[0].(map[string]any)
	rel := app["releases"].([]any)[0].(map[string]any)
	if app["application"] != "c46e" || app["name"] != "Bestellwesen" || len(app["releases"].([]any)) != 2 || rel["version"] != float64(3) ||
		rel["takenAt"] != "2026-10-27T09:56:19Z" || len(rel["recorded"].([]any)) != 5 || rel["releaseId"] != nil {
		t.Fatalf("application %v", app)
	}

	rec := readToken(w.carla).do("GET", "/api/engines/atlas/architectures/c46e/newest?layers=technology,application,application", "")
	hd := rec.Header()
	if rec.Code != http.StatusOK || hd.Get("Content-Type") != "image/svg+xml" || hd.Get("X-Content-Type-Options") != "nosniff" ||
		hd.Get("Content-Security-Policy") != "default-src 'none'; style-src 'unsafe-inline'; sandbox" || hd.Get("Cache-Control") != "private, no-store" ||
		hd.Get("X-Release-Version") != "3" || hd.Get("X-Release-Taken-At") != "2026-10-27T09:56:19Z" {
		t.Fatalf("the newest sketch: %d %v %s", rec.Code, hd, rec.Body)
	}
	// The layers reach the engine once each, in the engine's order.
	if !strings.Contains(rec.Body.String(), "<title>v3 application,technology</title>") {
		t.Fatalf("the sketch %s", rec.Body)
	}
	if rec := w.vera.do("GET", "/api/engines/atlas/architectures/c46e/3", ""); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "<title>v3 </title>") {
		t.Fatalf("a fixed release with the engine's layers: %d %s", rec.Code, rec.Body)
	}

	for path, status := range map[string]int{
		"/api/engines/atlas/architectures/c46e/0":                     http.StatusBadRequest,
		"/api/engines/atlas/architectures/c46e/-3":                    http.StatusBadRequest,
		"/api/engines/atlas/architectures/c46e/latest":                http.StatusBadRequest,
		"/api/engines/atlas/architectures/c46e/4294967299":            http.StatusBadRequest,
		"/api/engines/atlas/architectures/c46e/newest?layers=physics": http.StatusBadRequest,
		"/api/engines/atlas/architectures/c46e/newest?layers=data,":   http.StatusBadRequest,
		"/api/engines/atlas/architectures/c46e/7":                     http.StatusNotFound,
		"/api/engines/atlas/architectures/hidden/newest":              http.StatusNotFound,
		"/api/engines/gone/architectures":                             http.StatusNotFound,
		"/api/engines/gone/architectures/c46e/newest":                 http.StatusNotFound,
		"/api/engines/atlas/architectures/c46e/2":                     http.StatusBadGateway, // a picture that reaches outside itself
	} {
		if rec := w.vera.do("GET", path, ""); rec.Code != status || rec.Header().Get("Content-Type") == "image/svg+xml" {
			t.Errorf("%s: %d %s, want %d", path, rec.Code, rec.Body, status)
		}
	}
	if rec := w.vera.do("GET", "/api/engines/atlas/architectures/c46e/2", ""); !strings.Contains(rec.Body.String(), "engine atlas") || !strings.Contains(rec.Body.String(), "a reference outside the sketch") {
		t.Errorf("an unsafe picture says why: %s", rec.Body)
	}

	env["WWBB_ENGINE_ATLAS_TOKEN"] = "wrong"
	for _, path := range []string{"/api/engines/atlas/architectures", "/api/engines/atlas/architectures/c46e/newest"} {
		if r := w.vera.do("GET", path, ""); r.Code != http.StatusBadGateway || strings.Contains(r.Body.String(), "wrong") || !strings.Contains(r.Body.String(), "engine atlas: the engine refused the credential") {
			t.Errorf("%s with a refused credential: %d %s", path, r.Code, r.Body)
		}
	}

	// The description names what the routes answer.
	doc := w.vera.expect("GET", "/api/openapi.json", "", http.StatusOK)
	sketch, _ := field(doc, "paths", "/api/engines/{id}/architectures/{application}/{release}", "get").(map[string]any)["summary"].(string)
	for _, want := range []string{"image/svg+xml", "newest", "layers", "strategy, business, application, data, technology"} {
		if !strings.Contains(sketch, want) {
			t.Errorf("the description of the sketch lacks %q: %s", want, sketch)
		}
	}
}

// TestArchitectureBlocksThroughTheAPI: a body names an architecture sketch
// by its engine, application, release and layers; an empty list of layers
// is the engine's default, written as none (ADR-0145).
func TestArchitectureBlocksThroughTheAPI(t *testing.T) {
	w := newWorld(t)
	it := w.bert.expect("POST", "/api/items", `{"space":"PLAT","type":"task","title":"Ordering","properties":{"priority":"low"}}`, http.StatusCreated)
	path := "/api/items/" + field(it, "item", "key").(string) + "/body"
	got := field(w.bert.expect("PUT", path, `{"blocks":[
		{"kind":"architecture","engine":"atlas","application":"c46e","layers":[],"text":"Bestellwesen"},
		{"kind":"architecture","engine":"atlas","application":"c46e","release":3,"layers":["business","data"],"text":"As released"}]}`, http.StatusOK), "item", "body").([]any)
	newest, fixed := got[0].(map[string]any), got[1].(map[string]any)
	if newest["kind"] != "architecture" || newest["engine"] != "atlas" || newest["application"] != "c46e" || newest["text"] != "Bestellwesen" ||
		newest["release"] != nil || newest["layers"] != nil ||
		fixed["release"] != float64(3) || fmt.Sprint(fixed["layers"]) != "[business data]" {
		t.Fatalf("the body %v", got)
	}
	for _, bad := range []string{
		`{"kind":"architecture","engine":"atlas","application":"c46e","layers":["data","business"]}`,
		`{"kind":"architecture","engine":"atlas","application":"c46e","layers":["physics"]}`,
		`{"kind":"architecture","engine":"atlas","application":"c46e","release":-1}`,
		`{"kind":"architecture","engine":"atlas"}`,
		`{"kind":"paragraph","text":"x","application":"c46e"}`,
		`{"kind":"paragraph","text":"x","release":2}`,
		`{"kind":"process","engine":"atlas","process":"p","layers":["data"]}`,
	} {
		w.bert.expect("PUT", path, `{"blocks":[`+bad+`]}`, http.StatusBadRequest)
	}
}
