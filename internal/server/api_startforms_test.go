package server

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dbuchs7/wikiwikibangbang/internal/audit"
	"github.com/dbuchs7/wikiwikibangbang/internal/engine"
)

const startLink = "8f14e45fceea167a5a36dedd4bea2543a4c1b5e2f0d6c7b8a9e0f1d2c3b4a596"

// fakeStartEngine answers as an atlas engine does: its API for the
// credential "secret", and a start link's public routes for anyone,
// refusing a credential there. It records what reaches the public routes.
func fakeStartEngine(t *testing.T) (*httptest.Server, *[]string) {
	t.Helper()
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/public/") {
			if r.Header.Get("Authorization") != "" {
				t.Errorf("a public route was sent a credential: %s", r.URL.Path)
			}
			body, _ := io.ReadAll(r.Body)
			seen = append(seen, r.Method+" "+r.URL.Path+" "+r.Header.Get("X-Forwarded-For")+" "+string(body))
			switch {
			case r.URL.Path == "/public/forms/"+startLink+"/schema":
				_, _ = w.Write([]byte(`{"processName":"Incident","schema":{"components":[{"key":"reporter","type":"textfield"}]}}`))
			case r.URL.Path == "/public/forms/"+startLink+"/start" && strings.Contains(string(body), `"other"`):
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error":"not fields of this form: other"}`))
			case r.URL.Path == "/public/forms/"+startLink+"/start" && strings.Contains(string(body), `"flood"`):
				w.WriteHeader(http.StatusTooManyRequests)
			case r.URL.Path == "/public/forms/"+startLink+"/start":
				_, _ = w.Write([]byte(`{"started":true}`))
			default:
				w.WriteHeader(http.StatusNotFound)
			}
			return
		}
		if r.Header.Get("Authorization") != "Bearer secret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/api/v1/processes":
			_, _ = w.Write([]byte(`[{"key":31,"processId":"incident","name":"Incident","version":4,"deployedAt":1761480000,"startFormId":"incident-report","executable":true},
				{"key":40,"processId":"leave","name":"Leave","version":1,"deployedAt":1761480000,"startFormId":"leave-request","executable":true}]`))
		case "/api/v1/processes/31/xml":
			_, _ = w.Write([]byte(`<definitions><process id="incident"><startEvent id="start" name="Reported"><extensionElements><formDefinition formId="incident-report"/></extensionElements></startEvent>
				<userTask id="triage" name="Triage"><extensionElements><formDefinition formId="triage-form"/></extensionElements></userTask></process></definitions>`))
		case "/api/v1/processes/40/xml":
			_, _ = w.Write([]byte(`<definitions><process id="leave"><subProcess id="w" wizard="public"/></process></definitions>`))
		case "/api/v1/forms/incident-report":
			_, _ = w.Write([]byte(`{"id":"incident-report","name":"Incident report","savedAt":1791795101,"schema":{"components":[]}}`))
		case "/api/v1/public-links":
			_, _ = w.Write([]byte(`[{"token":"` + startLink + `","processId":"incident","formId":"incident-report"}]`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &seen
}

// TestStartFormsThroughTheAPI covers ADR-0148 through the API: every
// signed-in person reads the forms a process's steps name and a start
// link's form, and starts its process through the engine's public route,
// without the credential and with the person's address; only those who
// may edit in a space list the links; what is no link never reaches the
// engine; the start is audited, the link only by its first digits.
func TestStartFormsThroughTheAPI(t *testing.T) {
	srv, seen := fakeStartEngine(t)
	env := map[string]string{"WWBB_ENGINE_ATLAS_TOKEN": "secret"}
	w := newWorld(t, func(d *Deps) { d.Engines = engine.NewReader(func(k string) string { return env[k] }, srv.Client()) })
	w.admin.expect("PUT", "/api/engines/atlas", `{"name":"Atlas","kind":"atlas","baseURL":"`+srv.URL+`"}`, http.StatusCreated)

	pf := field(w.vera.expect("GET", "/api/engines/atlas/processes/incident/forms", "", http.StatusOK), "process").(map[string]any)
	if steps := pf["steps"].([]any); pf["process"] != "incident" || pf["startForm"] != "incident-report" || pf["publicWizard"] != false || len(steps) != 2 || steps[1].(map[string]any)["form"] != "triage-form" {
		t.Fatalf("process forms %v", pf)
	}
	w.vera.expect("GET", "/api/engines/atlas/processes/nothing/forms", "", http.StatusNotFound)
	f := field(w.carla.expect("GET", "/api/engines/atlas/forms/incident-report?process=incident", "", http.StatusOK), "form").(map[string]any)
	if f["name"] != "Incident report" || len(f["steps"].([]any)) != 1 {
		t.Fatalf("a form of a step %v", f)
	}
	w.carla.expect("GET", "/api/engines/atlas/forms/triage?process=incident", "", http.StatusNotFound)

	w.vera.expect("GET", "/api/engines/atlas/public-links", "", http.StatusForbidden)
	w.bert.expect("GET", "/api/engines/gone/public-links", "", http.StatusNotFound)
	links := w.bert.expect("GET", "/api/engines/atlas/public-links", "", http.StatusOK)["links"].([]any)
	if len(links) != 1 || links[0].(map[string]any)["link"] != startLink || links[0].(map[string]any)["name"] != "Incident" {
		t.Fatalf("links %v", links)
	}

	rec := w.vera.do("GET", "/api/engines/atlas/public-links/"+startLink+"?process=incident", "")
	if rec.Code != http.StatusOK || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("public form: %d %s", rec.Code, rec.Body)
	}
	pub := field(decodeBody(t, rec), "form").(map[string]any)
	if pub["processName"] != "Incident" || pub["url"] != srv.URL+"/public/forms/"+startLink || pub["wizard"] != false {
		t.Fatalf("public form %v", pub)
	}
	if want := "GET /public/forms/" + startLink + "/schema 192.0.2.1 "; (*seen)[len(*seen)-1] != want {
		t.Errorf("asked %q", (*seen)[len(*seen)-1])
	}
	n := len(*seen)
	for _, bad := range []string{strings.ToUpper(startLink), startLink[:31], "..%2F..%2Fapi%2Fv1%2Fprocesses"} {
		w.vera.expect("GET", "/api/engines/atlas/public-links/"+bad, "", http.StatusBadRequest)
		w.vera.expect("POST", "/api/engines/atlas/public-links/"+bad+"/start", `{"variables":{}}`, http.StatusBadRequest)
	}
	if len(*seen) != n {
		t.Errorf("what is no link reached the engine: %v", (*seen)[n:])
	}
	w.vera.expect("GET", "/api/engines/atlas/public-links/"+strings.Repeat("0", 64), "", http.StatusNotFound)

	start := "/api/engines/atlas/public-links/" + startLink + "/start"
	if got := w.vera.expect("POST", start, `{"variables":{"reporter":"vera"},"process":"incident"}`, http.StatusOK); got["started"] != true || got["wizard"] != false {
		t.Fatalf("start %v", got)
	}
	if want := "POST /public/forms/" + startLink + "/start 192.0.2.1 " + `{"variables":{"reporter":"vera"}}`; (*seen)[len(*seen)-1] != want {
		t.Errorf("sent %q", (*seen)[len(*seen)-1])
	}
	if r := w.vera.do("POST", start, `{"variables":{"other":1}}`); r.Code != http.StatusUnprocessableEntity || !strings.Contains(r.Body.String(), "not fields of this form: other") {
		t.Errorf("a refused submission: %d %s", r.Code, r.Body)
	}
	if r := w.vera.do("POST", start, `{"variables":{"flood":1}}`); r.Code != http.StatusServiceUnavailable || r.Header().Get("Retry-After") == "" {
		t.Errorf("a busy engine: %d %v", r.Code, r.Header())
	}
	w.vera.expect("POST", start, `{"variables":{},"process":"leave"}`, http.StatusUnprocessableEntity)
	w.vera.expect("POST", start, `{"variables":[1]}`, http.StatusBadRequest)
	w.vera.expect("POST", start, `{}`, http.StatusBadRequest)
	w.vera.expect("POST", start, `{"variables":{},"more":1}`, http.StatusBadRequest)
	w.vera.expect("POST", "/api/engines/gone/public-links/"+startLink+"/start", `{"variables":{}}`, http.StatusNotFound)

	// The public routes need no credential.
	delete(env, "WWBB_ENGINE_ATLAS_TOKEN")
	if pub := field(w.vera.expect("GET", "/api/engines/atlas/public-links/"+startLink+"?process=incident", "", http.StatusOK), "form").(map[string]any); pub["wizard"] != nil {
		t.Errorf("a public form without credential: %v", pub)
	}
	w.vera.expect("POST", start, `{"variables":{"reporter":"vera"}}`, http.StatusOK)

	var started []audit.Entry
	for _, e := range w.db.Audit.List(0, 1000) {
		if e.Event == audit.EngineStarted {
			started = append(started, e)
		}
	}
	if len(started) != 2 || started[0].Actor != w.ids["vera"] || started[0].Subject != "atlas" || started[0].Detail["link"] != startLink[:8] || started[0].Addr != "192.0.2.1" {
		t.Fatalf("starts audited: %+v", started)
	}
}

// TestStartFormBlocksThroughTheAPI: a body holds a form of a process's
// step with its view, and a start form with its link and the fields it
// fills in beforehand, through the API as through the store (ADR-0148).
func TestStartFormBlocksThroughTheAPI(t *testing.T) {
	w := newWorld(t)
	it := w.bert.expect("POST", "/api/items", `{"space":"PLAT","type":"task","title":"Incidents","properties":{"priority":"low"}}`, http.StatusCreated)
	path := "/api/items/" + field(it, "item", "key").(string) + "/body"
	got := field(w.bert.expect("PUT", path, `{"blocks":[
		{"kind":"form","engine":"atlas","form":"incident-report","process":"incident","view":"both"},
		{"kind":"startform","engine":"atlas","link":"`+startLink+`","process":"incident","prefill":[{"field":"reporter","from":"username"},{"field":"mail","from":"email"}],"text":"Report"},
		{"kind":"startform","engine":"atlas","link":"`+startLink+`","prefill":[]}]}`, http.StatusOK), "item", "body").([]any)
	form, start, bare := got[0].(map[string]any), got[1].(map[string]any), got[2].(map[string]any)
	if form["view"] != "both" || form["process"] != "incident" || start["kind"] != "startform" || start["link"] != startLink || start["text"] != "Report" ||
		fmt.Sprint(start["prefill"]) != "[map[field:reporter from:username] map[field:mail from:email]]" || bare["prefill"] != nil || bare["process"] != nil {
		t.Fatalf("the body %v", got)
	}
	for _, bad := range []string{
		`{"kind":"startform","engine":"atlas"}`,
		`{"kind":"startform","engine":"atlas","link":"` + startLink + `","prefill":[{"field":"x","from":"password"}]}`,
		`{"kind":"startform","engine":"atlas","link":"` + startLink + `","form":"f"}`,
		`{"kind":"form","engine":"atlas","form":"f","link":"` + startLink + `"}`,
		`{"kind":"paragraph","text":"x","prefill":[{"field":"x","from":"email"}]}`,
	} {
		w.bert.expect("PUT", path, `{"blocks":[`+bad+`]}`, http.StatusBadRequest)
	}
}
