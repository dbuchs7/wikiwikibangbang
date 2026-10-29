package engine

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dbuchs7/wikiwikibangbang/internal/budget"
	"github.com/dbuchs7/wikiwikibangbang/internal/site"
)

const (
	model = `<?xml version="1.0" encoding="UTF-8"?><bpmn:definitions id="d"/>`
	dmn   = `<?xml version="1.0" encoding="UTF-8"?><definitions id="credit"/>`
	// sketch is an architecture sketch as atlas draws one: self-contained,
	// its only references fragments of its own. %s names the layers asked.
	sketch = `<?xml version="1.0" encoding="UTF-8"?>
<svg xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink" width="1140" height="420" viewBox="0 0 1140 420" role="img" aria-labelledby="sketch-title" font-family="sans-serif" font-size="13">
<title id="sketch-title">Bestellwesen v3, layers %s</title>
<defs><marker id="arrow" orient="auto"><path d="M0 0L10 5L0 10z"/></marker></defs>
<g><title>Worker &lt;script&gt; &amp; co</title><rect x="10" y="10" width="200" height="60" rx="6" style="fill:#fff;stroke:#333"/><use href="#arrow"/><use xlink:href="#arrow"/><line x1="0" y1="0" x2="5" y2="5" marker-end="url(#arrow)" opacity="0.5" offset="1"/></g>
</svg>`
)

// fakeAtlas answers the reads as atlas does, for the token "secret".
func fakeAtlas(t *testing.T) (*httptest.Server, *[]string) {
	t.Helper()
	var asked []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = append(asked, r.Method+" "+r.URL.EscapedPath())
		// A token whose reach names no application lists none (atlas
		// ADR-0493, as amended).
		if r.Header.Get("Authorization") == "Bearer unreached" && r.URL.Path == "/base/api/v1/architectures" {
			_, _ = w.Write([]byte(`{"applications":[]}`))
			return
		}
		if r.Header.Get("Authorization") != "Bearer secret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/base/api/v1/processes":
			_, _ = w.Write([]byte(`[
				{"key":2251799813685249,"processId":"order","name":"Order","version":1,"deployedAt":1761400000,"executable":true},
				{"key":2251799813685300,"processId":"order","name":"Order to cash","version":3,"deployedAt":1761480000,"executable":true},
				{"key":2251799813685260,"processId":"order","name":"Order","version":2,"deployedAt":1761450000},
				{"key":7,"processId":"approve","name":"","version":1,"deployedAt":1761300000},
				{"key":8,"processId":"","name":"broken","version":1}]`))
		case "/base/api/v1/processes/2251799813685300/xml":
			w.Header().Set("Content-Type", "application/xml")
			_, _ = w.Write([]byte(model))
		case "/base/api/v1/processes/2251799813685300/runtime":
			_, _ = w.Write([]byte(`{"instances":4,"finished":10,"tokens":5,"incidentTotal":1,"incidentCountsExact":true,
				"elements":[{"elementId":"check","type":"serviceTask","tokens":3,"visits":12,"incidents":1,"terminated":2},
					{"elementId":"start","type":"startEvent","tokens":0,"visits":14,"incidents":0,"terminated":0}],
				"incidents":[{"elementInstanceKey":99,"processInstanceKey":98,"elementId":"check","message":"customer 4711 has no address"}]}`))
		case "/base/api/v1/forms":
			_, _ = w.Write([]byte(`[{"id":"travel","name":"Travel request","projectId":"p","savedAt":1791795101},
				{"id":"a/b","name":"","savedAt":1791795000},{"id":"","name":"broken"}]`))
		case "/base/api/v1/forms/travel":
			_, _ = w.Write([]byte(`{"id":"travel","name":"Travel request","savedAt":1791795101,"schema":{"type":"default","components":[{"key":"to","type":"textfield"}]}}`))
		case "/base/api/v1/forms/a/b":
			_, _ = w.Write([]byte(`{"id":"a/b","schema":"not an object"}`))
		case "/base/api/v1/forms/hidden":
			_, _ = w.Write([]byte(`{"id":"hidden","schema":{"components":[]}}`))
		case "/base/api/v1/decision-deployments":
			_, _ = w.Write([]byte(`[
				{"key":561,"decisionId":"Bonität","version":10,"modelName":"Credit","deployedAt":1790367024,"current":true},
				{"key":561,"decisionId":"Decision 1","version":2,"modelName":"Credit","deployedAt":1790367024,"current":true},
				{"key":516,"decisionId":"Bonität","version":9,"modelName":"Credit","deployedAt":1790235601,"current":false},
				{"key":9,"decisionId":"","version":1}]`))
		case "/base/api/v1/decision-deployments/561/xml":
			w.Header().Set("Content-Type", "application/xml; charset=utf-8")
			_, _ = w.Write([]byte(dmn))
		case "/base/api/v1/architectures":
			_, _ = w.Write([]byte(`{"applications":[
				{"applicationId":"c46e","application":"bestellwesen","releases":[
					{"version":2,"releaseId":"r2","publishedAt":1793000000,"takenAt":1793000001,"recorded":["application","technology"]},
					{"version":3,"releaseId":"r3","publishedAt":1793094979,"takenAt":1793094980,"recorded":["strategy","business","application","data","technology"]},
					{"version":1,"releaseId":"r1","publishedAt":1792000000,"takenAt":1792000001,"recorded":["application","technology"]}]},
				{"applicationId":"a/b","application":"Abrechnung","releases":[{"version":1,"releaseId":"x","publishedAt":1,"takenAt":2}]},
				{"applicationId":"empty","application":"Leer","releases":[]},
				{"applicationId":"","application":"broken","releases":[{"version":1}]}]}`))
		case "/base/api/v1/applications/c46e/releases/3/architecture.embed.svg":
			if r.URL.Query().Get("layers") == "strategy" {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"error":"release 3 records none of the layers asked for"}`))
				return
			}
			w.Header().Set("Content-Type", "image/svg+xml; charset=utf-8")
			_, _ = fmt.Fprintf(w, sketch, r.URL.Query().Get("layers"))
		case "/base/api/v1/applications/c46e/releases/2/architecture.embed.svg":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(`<html><body>a sign-in page</body></html>`))
		case "/base/api/v1/applications/c46e/releases/1/architecture.embed.svg":
			w.Header().Set("Content-Type", "image/svg+xml")
			_, _ = w.Write([]byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(document.cookie)</script></svg>`))
		case "/base/api/v1/applications/a%2Fb/releases/1/architecture.embed.svg", "/base/api/v1/applications/a/b/releases/1/architecture.embed.svg":
			w.Header().Set("Content-Type", "image/svg+xml")
			_, _ = fmt.Fprintf(w, sketch, "")
		case "/base/api/v1/processes/7/xml":
			http.Redirect(w, r, "https://elsewhere.example.org/", http.StatusFound)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &asked
}

func TestTheNewestVersionAndItsOperations(t *testing.T) {
	srv, asked := fakeAtlas(t)
	env := map[string]string{"WWBB_ENGINE_ATLAS_PROD_TOKEN": "secret"}
	r := NewReader(func(k string) string { return env[k] }, srv.Client())
	e := site.Engine{ID: "atlas-prod", Name: "Atlas", Kind: site.EngineAtlas, BaseURL: srv.URL + "/base"}
	ctx := context.Background()

	if TokenVar("atlas-prod.v2") != "WWBB_ENGINE_ATLAS_PROD_V2_TOKEN" || !r.HasCredential("atlas-prod") || r.HasCredential("other") {
		t.Fatal("credential variables")
	}
	list, err := r.Processes(ctx, e)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].ID != "approve" || list[1].ID != "order" || list[1].Version != 3 || list[1].Name != "Order to cash" || list[1].DeployedAt.Unix() != 1761480000 {
		t.Fatalf("processes %+v", list)
	}
	d, err := r.Diagram(ctx, e, "order")
	if err != nil || d.XML != model || d.Version != 3 {
		t.Fatalf("diagram %+v, %v", d, err)
	}
	ops, err := r.Operations(ctx, e, "order")
	if err != nil {
		t.Fatal(err)
	}
	if ops.Version != 3 || ops.Instances != 4 || ops.Finished != 10 || ops.Tokens != 5 || ops.Incidents != 1 || !ops.Exact || len(ops.Elements) != 2 ||
		ops.Elements[0] != (Element{ID: "check", Tokens: 3, Incidents: 1, Passed: 7}) || ops.Elements[1].Passed != 14 {
		t.Fatalf("operations %+v", ops)
	}
	if want := "GET /base/api/v1/processes/2251799813685300/runtime"; (*asked)[len(*asked)-1] != want {
		t.Fatalf("asked %v", *asked)
	}

	if _, err := r.Diagram(ctx, e, "nothing"); !errors.Is(err, ErrNotFound) {
		t.Errorf("an unknown process: %v", err)
	}
	if _, err := r.Diagram(ctx, e, "approve"); !errors.Is(err, ErrUnavailable) {
		t.Errorf("a redirect is followed: %v", err)
	}
	for _, x := range *asked {
		if strings.Contains(x, "elsewhere") {
			t.Errorf("asked elsewhere: %v", *asked)
		}
	}
	env["WWBB_ENGINE_ATLAS_PROD_TOKEN"] = "wrong"
	if _, err := r.Processes(ctx, e); !errors.Is(err, ErrRefused) || strings.Contains(err.Error(), "wrong") {
		t.Errorf("a refused credential: %v", err)
	}
	delete(env, "WWBB_ENGINE_ATLAS_PROD_TOKEN")
	if _, err := r.Processes(ctx, e); !errors.Is(err, ErrNoCredential) || !strings.Contains(err.Error(), "WWBB_ENGINE_ATLAS_PROD_TOKEN") {
		t.Errorf("no credential: %v", err)
	}
	env["WWBB_ENGINE_ATLAS_PROD_TOKEN"] = "secret"
	if _, err := r.Processes(ctx, site.Engine{ID: "atlas-prod", Kind: "other", BaseURL: e.BaseURL}); !errors.Is(err, ErrUnavailable) {
		t.Errorf("an unknown kind: %v", err)
	}
	if _, err := r.Processes(ctx, site.Engine{ID: "atlas-prod", Kind: site.EngineAtlas, BaseURL: srv.URL + "/gone"}); !errors.Is(err, ErrNotFound) {
		t.Errorf("a wrong address: %v", err)
	}
	if _, err := r.Processes(ctx, site.Engine{ID: "atlas-prod", Kind: site.EngineAtlas, BaseURL: "http://127.0.0.1:1"}); !errors.Is(err, ErrUnavailable) {
		t.Errorf("an engine that does not answer: %v", err)
	}
}

// TestFormsAndDecisions: an engine's forms as they are saved, with their
// schema, and the newest deployed version of each decision, with the DMN
// model it is deployed in (ADR-0144).
func TestFormsAndDecisions(t *testing.T) {
	srv, asked := fakeAtlas(t)
	env := map[string]string{"WWBB_ENGINE_ATLAS_TOKEN": "secret"}
	r := NewReader(func(k string) string { return env[k] }, srv.Client())
	e := site.Engine{ID: "atlas", Name: "Atlas", Kind: site.EngineAtlas, BaseURL: srv.URL + "/base"}
	ctx := context.Background()

	forms, err := r.Forms(ctx, e)
	if err != nil {
		t.Fatal(err)
	}
	if len(forms) != 2 || forms[0].ID != "a/b" || forms[1].ID != "travel" || forms[1].Name != "Travel request" || forms[1].SavedAt.Unix() != 1791795101 {
		t.Fatalf("forms %+v", forms)
	}
	f, err := r.Form(ctx, e, "travel", "")
	if err != nil || f.Name != "Travel request" || string(f.Schema) != `{"type":"default","components":[{"key":"to","type":"textfield"}]}` {
		t.Fatalf("form %+v, %v", f, err)
	}
	if _, err := r.Form(ctx, e, "a/b", ""); !errors.Is(err, ErrUnavailable) || !strings.Contains(err.Error(), "no JSON object") {
		t.Errorf("a schema that is no object: %v", err)
	}
	if want := "GET /base/api/v1/forms/a%2Fb"; (*asked)[len(*asked)-1] != want {
		t.Errorf("an ID with a slash is one segment: %v", (*asked)[len(*asked)-1])
	}
	// atlas answers any form by its ID; the reader shows only those the
	// credential lists.
	n := len(*asked)
	if _, err := r.Form(ctx, e, "hidden", ""); !errors.Is(err, ErrNotFound) || !strings.Contains(err.Error(), "form hidden") {
		t.Errorf("a form the credential does not list: %v", err)
	}
	if len(*asked) != n+1 {
		t.Errorf("a form not listed was read: %v", (*asked)[n:])
	}

	decisions, err := r.Decisions(ctx, e)
	if err != nil {
		t.Fatal(err)
	}
	if len(decisions) != 2 || decisions[0].ID != "Bonität" || decisions[0].Version != 10 || decisions[0].Model != "Credit" || decisions[0].DeployedAt.Unix() != 1790367024 || decisions[1].ID != "Decision 1" {
		t.Fatalf("decisions %+v", decisions)
	}
	d, err := r.DecisionModel(ctx, e, "Bonität")
	if err != nil || d.XML != dmn || d.Version != 10 {
		t.Fatalf("decision %+v, %v", d, err)
	}
	if want := "GET /base/api/v1/decision-deployments/561/xml"; (*asked)[len(*asked)-1] != want {
		t.Fatalf("asked %v", (*asked)[len(*asked)-1])
	}
	if _, err := r.DecisionModel(ctx, e, "nothing"); !errors.Is(err, ErrNotFound) || !strings.Contains(err.Error(), "decision nothing") {
		t.Errorf("an unknown decision: %v", err)
	}
	delete(env, "WWBB_ENGINE_ATLAS_TOKEN")
	if _, err := r.Forms(ctx, e); !errors.Is(err, ErrNoCredential) {
		t.Errorf("forms without credential: %v", err)
	}
	if _, err := r.Decisions(ctx, e); !errors.Is(err, ErrNoCredential) {
		t.Errorf("decisions without credential: %v", err)
	}
	if _, err := r.Form(ctx, e, "travel", ""); !errors.Is(err, ErrNoCredential) {
		t.Errorf("a form without credential: %v", err)
	}
	if _, err := r.DecisionModel(ctx, e, "Bonität"); !errors.Is(err, ErrNoCredential) {
		t.Errorf("a decision without credential: %v", err)
	}
}

// TestArchitectureSketches: the applications whose releases carry an
// architecture sketch, newest release first, and a release's sketch, read
// after the application and the release are found in that list; release 0
// is the newest (ADR-0146).
func TestArchitectureSketches(t *testing.T) {
	srv, asked := fakeAtlas(t)
	env := map[string]string{"WWBB_ENGINE_ATLAS_TOKEN": "secret"}
	r := NewReader(func(k string) string { return env[k] }, srv.Client())
	e := site.Engine{ID: "atlas", Name: "Atlas", Kind: site.EngineAtlas, BaseURL: srv.URL + "/base"}
	ctx := context.Background()

	apps, err := r.Architectures(ctx, e)
	if err != nil {
		t.Fatal(err)
	}
	if len(apps) != 2 || apps[0].ID != "a/b" || apps[0].Name != "Abrechnung" || apps[1].ID != "c46e" || apps[1].Name != "bestellwesen" {
		t.Fatalf("applications %+v", apps)
	}
	rels := apps[1].Releases
	if len(rels) != 3 || rels[0].Version != 3 || rels[1].Version != 2 || rels[2].Version != 1 ||
		rels[0].PublishedAt.Unix() != 1793094979 || rels[0].TakenAt.Unix() != 1793094980 || strings.Join(rels[0].Recorded, ",") != "strategy,business,application,data,technology" {
		t.Fatalf("releases %+v", rels)
	}
	if apps[0].Releases[0].Recorded == nil {
		t.Error("a release that records no layer lists none, not null")
	}

	svg, rel, err := r.ArchitectureSketch(ctx, e, "c46e", 0, []string{"business", "application"})
	if err != nil {
		t.Fatal(err)
	}
	if rel.Application != "c46e" || rel.Name != "bestellwesen" || rel.Version != 3 || rel.TakenAt.Unix() != 1793094980 || !strings.Contains(string(svg), "layers business,application") {
		t.Fatalf("the newest sketch: %+v %s", rel, svg)
	}
	if want := "GET /base/api/v1/applications/c46e/releases/3/architecture.embed.svg"; (*asked)[len(*asked)-1] != want || (*asked)[len(*asked)-2] != "GET /base/api/v1/architectures" {
		t.Fatalf("asked %v", (*asked)[len(*asked)-2:])
	}
	if svg, rel, err := r.ArchitectureSketch(ctx, e, "c46e", 3, nil); err != nil || rel.Version != 3 || !strings.Contains(string(svg), "layers </title>") {
		t.Fatalf("a fixed release with the engine's layers: %+v %v %s", rel, err, svg)
	}
	if _, rel, err := r.ArchitectureSketch(ctx, e, "a/b", 0, nil); err != nil || rel.Version != 1 || (*asked)[len(*asked)-1] != "GET /base/api/v1/applications/a%2Fb/releases/1/architecture.embed.svg" {
		t.Fatalf("an ID with a slash is one segment: %+v %v %v", rel, err, (*asked)[len(*asked)-1])
	}

	// Only what the list offers is read.
	n := len(*asked)
	for name, c := range map[string]struct {
		app     string
		release int32
		want    string
	}{
		"an application not listed":  {"hidden", 0, "application hidden"},
		"an application without one": {"empty", 0, "application empty"},
		"a release without a sketch": {"c46e", 9, "release 9 of bestellwesen"},
		"a release no number":        {"c46e", -1, "release -1 of bestellwesen"},
		"an empty application":       {"", 0, "application "},
	} {
		if _, _, err := r.ArchitectureSketch(ctx, e, c.app, c.release, nil); !errors.Is(err, ErrNotFound) || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v", name, err)
		}
	}
	for _, x := range (*asked)[n:] {
		if x != "GET /base/api/v1/architectures" {
			t.Errorf("read what the list does not offer: %v", (*asked)[n:])
			break
		}
	}

	if _, _, err := r.ArchitectureSketch(ctx, e, "c46e", 3, []string{"strategy"}); !errors.Is(err, ErrNotFound) || !strings.Contains(err.Error(), "release 3 of bestellwesen records none of the layers strategy") {
		t.Errorf("layers the release does not record: %v", err)
	}
	if _, _, err := r.ArchitectureSketch(ctx, e, "c46e", 2, nil); !errors.Is(err, ErrUnavailable) || !strings.Contains(err.Error(), "text/html") {
		t.Errorf("an answer that is no SVG picture: %v", err)
	}
	if _, _, err := r.ArchitectureSketch(ctx, e, "c46e", 1, nil); !errors.Is(err, ErrUnavailable) || !strings.Contains(err.Error(), "<script>") || !strings.Contains(err.Error(), "bestellwesen v1") {
		t.Errorf("a sketch with a script: %v", err)
	}

	// A token whose reach names no application lists none; a sketch then
	// points the administrator at the token's reach.
	env["WWBB_ENGINE_ATLAS_TOKEN"] = "unreached"
	if apps, err := r.Architectures(ctx, e); err != nil || apps == nil || len(apps) != 0 {
		t.Errorf("a token that reaches no application: %v %v", apps, err)
	}
	if _, _, err := r.ArchitectureSketch(ctx, e, "c46e", 0, nil); !errors.Is(err, ErrNotFound) || !strings.Contains(err.Error(), "reach") || strings.Contains(err.Error(), "unreached") {
		t.Errorf("a sketch with a token that reaches no application: %v", err)
	}
	delete(env, "WWBB_ENGINE_ATLAS_TOKEN")
	if _, err := r.Architectures(ctx, e); !errors.Is(err, ErrNoCredential) {
		t.Errorf("architectures without credential: %v", err)
	}
	if _, _, err := r.ArchitectureSketch(ctx, e, "c46e", 0, nil); !errors.Is(err, ErrNoCredential) {
		t.Errorf("a sketch without credential: %v", err)
	}
}

// TestUnsafeSketchesAreRefused: as defence in depth, a sketch is shown only
// when it holds no script, no embedded document, no event attribute and no
// reference outside itself (ADR-0146); the engine's own content security
// policy forbids them too.
func TestUnsafeSketchesAreRefused(t *testing.T) {
	if what := unsafeSVG([]byte(fmt.Sprintf(sketch, "application,technology"))); what != "" {
		t.Fatalf("atlas's sketch is refused: %s", what)
	}
	for svg, want := range map[string]string{
		`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`:                                                  "<script>",
		`<svg xmlns="http://www.w3.org/2000/svg"><SCRIPT/></svg>`:                                                                  "<script>",
		`<svg xmlns="http://www.w3.org/2000/svg" xmlns:s="http://www.w3.org/2000/svg"><s:script/></svg>`:                           "<script>",
		`<svg xmlns="http://www.w3.org/2000/svg"><foreignObject><div xmlns="http://www.w3.org/1999/xhtml"/></foreignObject></svg>`: "<foreignobject>",
		`<svg xmlns="http://www.w3.org/2000/svg" onload="x()"/>`:                                                                   "an event attribute (onload)",
		`<svg xmlns="http://www.w3.org/2000/svg"><rect ONCLICK="x()"/></svg>`:                                                      "an event attribute (onclick)",
		`<svg xmlns="http://www.w3.org/2000/svg"><image href="https://elsewhere.example.org/x.png"/></svg>`:                        "a reference outside the sketch",
		`<svg xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink"><use xlink:href="other.svg#a"/></svg>`: "a reference outside the sketch",
		`<svg xmlns="http://www.w3.org/2000/svg"><a href="javascript:alert(1)"><text>x</text></a></svg>`:                           "a reference outside the sketch",
		`<svg xmlns="http://www.w3.org/2000/svg"><use href=" #a"/></svg>`:                                                          "a reference outside the sketch",
		`<svg xmlns="http://www.w3.org/2000/svg"><set attributeName="onmouseover" to="alert(1)"/></svg>`:                           "an animation of onmouseover",
		`<svg xmlns="http://www.w3.org/2000/svg"><animate attributeName="xlink:href" values="javascript:alert(1)"/></svg>`:         "an animation of xlink:href",
		`<?xml-stylesheet href="https://elsewhere.example.org/s.css"?><svg xmlns="http://www.w3.org/2000/svg"/>`:                   "a stylesheet outside the sketch",
		`<svg xmlns="http://www.w3.org/2000/svg"><![CDATA[<script>alert(1)</script>]]></svg>`:                                      "<script>",
		`<svg xmlns="http://www.w3.org/2000/svg"><rect></svg>`:                                                                     "no well-formed XML",
		`<svg xmlns="http://www.w3.org/2000/svg">&unknown;</svg>`:                                                                  "no well-formed XML",
		`<html><body/></html>`: "no SVG picture",
		``:                     "no SVG picture",
	} {
		if got := unsafeSVG([]byte(svg)); got != want {
			t.Errorf("%s: %q, want %q", svg, got, want)
		}
	}
}

func TestAnAnswerBeyondTheBudgetIsRefused(t *testing.T) {
	srv, _ := fakeAtlas(t)
	b := budget.Get()
	b.EngineResponseBytes = 100
	budget.Set(b)
	t.Cleanup(func() { budget.Set(budget.Default()) })
	r := NewReader(func(k string) string {
		switch k {
		case AllowPrivateVar:
			return "on" // the fake engine is on the loopback address
		case TokenVar("a"):
			return "secret"
		}
		return ""
	}, nil)
	_, err := r.Processes(context.Background(), site.Engine{ID: "a", Kind: site.EngineAtlas, BaseURL: srv.URL + "/base"})
	if !errors.Is(err, ErrUnavailable) || !strings.Contains(err.Error(), "larger than 100 bytes") {
		t.Fatalf("a large answer: %v", err)
	}
}

// TestEnginesAreKeptToPublicAddresses: as connectors (ADR-0127), a reader
// reaches only addresses on the public internet unless the operator sets
// WWBB_ENGINE_ALLOW_PRIVATE=on, which an engine in the organisation's own
// network needs (ADR-0129).
func TestEnginesAreKeptToPublicAddresses(t *testing.T) {
	srv, _ := fakeAtlas(t)
	e := site.Engine{ID: "atlas", Kind: site.EngineAtlas, BaseURL: srv.URL + "/base"}
	env := map[string]string{"WWBB_ENGINE_ATLAS_TOKEN": "secret"}
	_, err := NewReader(func(k string) string { return env[k] }, nil).Processes(context.Background(), e)
	if !errors.Is(err, ErrUnavailable) || !strings.Contains(err.Error(), AllowPrivateVar+"=on") || strings.Contains(err.Error(), "connector") {
		t.Fatalf("an engine on the loopback address: %v", err)
	}
	env[AllowPrivateVar] = "on"
	if _, err := NewReader(func(k string) string { return env[k] }, nil).Processes(context.Background(), e); err != nil {
		t.Fatalf("with private addresses allowed: %v", err)
	}
}
