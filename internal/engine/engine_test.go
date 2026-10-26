package engine

import (
	"context"
	"errors"
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
)

// fakeAtlas answers the reads as atlas does, for the token "secret".
func fakeAtlas(t *testing.T) (*httptest.Server, *[]string) {
	t.Helper()
	var asked []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = append(asked, r.Method+" "+r.URL.EscapedPath())
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
	f, err := r.Form(ctx, e, "travel")
	if err != nil || f.Name != "Travel request" || string(f.Schema) != `{"type":"default","components":[{"key":"to","type":"textfield"}]}` {
		t.Fatalf("form %+v, %v", f, err)
	}
	if _, err := r.Form(ctx, e, "a/b"); !errors.Is(err, ErrUnavailable) || !strings.Contains(err.Error(), "no JSON object") {
		t.Errorf("a schema that is no object: %v", err)
	}
	if want := "GET /base/api/v1/forms/a%2Fb"; (*asked)[len(*asked)-1] != want {
		t.Errorf("an ID with a slash is one segment: %v", (*asked)[len(*asked)-1])
	}
	// atlas answers any form by its ID; the reader shows only those the
	// credential lists.
	n := len(*asked)
	if _, err := r.Form(ctx, e, "hidden"); !errors.Is(err, ErrNotFound) || !strings.Contains(err.Error(), "form hidden") {
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
	if _, err := r.Form(ctx, e, "travel"); !errors.Is(err, ErrNoCredential) {
		t.Errorf("a form without credential: %v", err)
	}
	if _, err := r.DecisionModel(ctx, e, "Bonität"); !errors.Is(err, ErrNoCredential) {
		t.Errorf("a decision without credential: %v", err)
	}
}

func TestAnAnswerBeyondTheBudgetIsRefused(t *testing.T) {
	srv, _ := fakeAtlas(t)
	b := budget.Get()
	b.EngineResponseBytes = 100
	budget.Set(b)
	t.Cleanup(func() { budget.Set(budget.Default()) })
	r := NewReader(func(k string) string {
		if k == AllowPrivateVar {
			return "on" // the fake engine is on the loopback address
		}
		return "secret"
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
