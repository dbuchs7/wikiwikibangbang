package engine

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dbuchs7/wikiwikibangbang/internal/site"
)

const (
	// link and wizardLink are public start links as atlas makes them: 32
	// random bytes in hexadecimal.
	link       = "8f14e45fceea167a5a36dedd4bea2543a4c1b5e2f0d6c7b8a9e0f1d2c3b4a596"
	wizardLink = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	// incidentModel names forms as atlas writes them: a start form on the
	// start event, a work form on a user task, a repair form on a service
	// task, and a user task in a sub-process. formDefinition elements
	// outside a step, or without a formId, name nothing.
	incidentModel = `<?xml version="1.0" encoding="UTF-8"?>
<bpmn:definitions xmlns:bpmn="http://www.omg.org/spec/BPMN/20100524/MODEL" xmlns:zeebe="http://camunda.org/schema/zeebe/1.0" id="d">
<bpmn:process id="incident" isExecutable="true">
  <bpmn:extensionElements><zeebe:formDefinition formId="nowhere"/></bpmn:extensionElements>
  <bpmn:startEvent id="start" name="Incident reported"><bpmn:extensionElements><zeebe:formDefinition formId="incident-report"/></bpmn:extensionElements></bpmn:startEvent>
  <bpmn:userTask id="triage" name="Triage"><bpmn:extensionElements><zeebe:formDefinition formId="triage-form"/></bpmn:extensionElements></bpmn:userTask>
  <bpmn:serviceTask id="notify" name="Notify"><bpmn:extensionElements><zeebe:taskDefinition type="mail"/><zeebe:formDefinition formId="repair-mail"/></bpmn:extensionElements></bpmn:serviceTask>
  <bpmn:userTask id="unbound" name="Unbound"><bpmn:extensionElements><zeebe:formDefinition/></bpmn:extensionElements></bpmn:userTask>
  <bpmn:subProcess id="fix" name="Fix it"><bpmn:userTask id="close" name="Close"><bpmn:extensionElements><zeebe:formDefinition formId="incident-report"/></bpmn:extensionElements></bpmn:userTask></bpmn:subProcess>
</bpmn:process></bpmn:definitions>`
	// wizardModel holds a public wizard (atlas ADR-0449).
	wizardModel = `<definitions xmlns:atlas="https://atlas.example/bpmn"><process id="leave"><startEvent id="s"><extensionElements><formDefinition formId="leave-request"/></extensionElements></startEvent>
<subProcess id="w" atlas:wizard="public"><userTask id="dates"/></subProcess></process></definitions>`
)

// fakePublicAtlas answers as atlas does: its API for the token "secret",
// and the public routes of a start link for anyone, refusing a credential
// there so that a test notices one sent. It records the start bodies and
// the forwarded addresses.
func fakePublicAtlas(t *testing.T) (*httptest.Server, *[]string) {
	t.Helper()
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/base/public/") {
			if r.Header.Get("Authorization") != "" {
				t.Errorf("a public route was sent a credential: %s", r.URL.Path)
			}
			body, _ := io.ReadAll(r.Body)
			seen = append(seen, r.Method+" "+r.URL.EscapedPath()+" "+r.Header.Get("X-Forwarded-For")+" "+string(body))
			switch r.URL.Path {
			case "/base/public/forms/" + link + "/schema", "/base/public/forms/" + wizardLink + "/schema":
				_, _ = w.Write([]byte(`{"processName":"Incident","schema":{"type":"default","components":[{"key":"reporter","type":"textfield"}]}}`))
			case "/base/public/forms/" + link + "/start":
				switch {
				case strings.Contains(string(body), `"unknown"`):
					w.WriteHeader(http.StatusBadRequest)
					_, _ = w.Write([]byte(`{"error":"not fields of this form:   unknown"}`))
				case strings.Contains(string(body), `"busy"`):
					w.WriteHeader(http.StatusTooManyRequests)
				case strings.Contains(string(body), `"stopped"`):
					w.WriteHeader(http.StatusConflict)
					_, _ = w.Write([]byte(`{"error":"process is not executable and cannot be started"}`))
				case strings.Contains(string(body), `"silent"`):
					_, _ = w.Write([]byte(`{}`))
				case strings.Contains(string(body), `"broken"`):
					w.WriteHeader(http.StatusInternalServerError)
				default:
					_, _ = w.Write([]byte(`{"started":true}`))
				}
			case "/base/public/forms/" + wizardLink + "/start":
				_, _ = w.Write([]byte(`{"started":true,"wizard":{"pass":"p","state":"pending"}}`))
			case "/base/public/forms/" + strings.Repeat("e", 64) + "/schema":
				_, _ = w.Write([]byte(`{"processName":"x","schema":"none"}`))
			default:
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"error":"unknown or revoked link"}`))
			}
			return
		}
		if r.Header.Get("Authorization") == "Bearer viewer" && r.URL.Path == "/base/api/v1/public-links" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		if r.Header.Get("Authorization") != "Bearer secret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/base/api/v1/processes":
			_, _ = w.Write([]byte(`[{"key":31,"processId":"incident","name":"Incident","version":4,"deployedAt":1761480000,"startFormId":"incident-report","executable":true},
				{"key":30,"processId":"incident","name":"Incident","version":3,"deployedAt":1761400000,"startFormId":"old-report","executable":true},
				{"key":40,"processId":"leave","name":"Leave","version":1,"deployedAt":1761480000,"startFormId":"leave-request","executable":true},
				{"key":50,"processId":"broken","name":"Broken","version":1,"deployedAt":1761480000}]`))
		case "/base/api/v1/processes/31/xml":
			_, _ = w.Write([]byte(incidentModel))
		case "/base/api/v1/processes/40/xml":
			_, _ = w.Write([]byte(wizardModel))
		case "/base/api/v1/processes/50/xml":
			_, _ = w.Write([]byte(`<definitions><process id="broken">`))
		case "/base/api/v1/forms/incident-report":
			_, _ = w.Write([]byte(`{"id":"incident-report","name":"Incident report","savedAt":1791795101,"schema":{"components":[{"key":"reporter","type":"textfield","label":"Reporter"}]}}`))
		case "/base/api/v1/forms/triage-form":
			_, _ = w.Write([]byte(`{"id":"triage-form","name":"","savedAt":1791795000,"schema":{"components":[]}}`))
		case "/base/api/v1/forms":
			_, _ = w.Write([]byte(`[]`))
		case "/base/api/v1/public-links":
			_, _ = w.Write([]byte(`[{"token":"` + wizardLink + `","processId":"leave","formId":"leave-request","createdAt":1,"url":"/public/forms/` + wizardLink + `"},
				{"token":"` + link + `","processId":"incident","formId":"incident-report","createdAt":2},
				{"token":"` + strings.Repeat("c", 64) + `","processId":"gone","formId":"f"},
				{"token":"","processId":"incident"}]`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &seen
}

// TestTheFormsOfAProcess: the steps of a process's newest version that
// name a form, in the order of its model, whether it holds a public
// wizard, and a form read as the form of a step (ADR-0148).
func TestTheFormsOfAProcess(t *testing.T) {
	srv, _ := fakePublicAtlas(t)
	env := map[string]string{"WWBB_ENGINE_ATLAS_TOKEN": "secret"}
	r := NewReader(func(k string) string { return env[k] }, srv.Client())
	e := site.Engine{ID: "atlas", Name: "Atlas", Kind: site.EngineAtlas, BaseURL: srv.URL + "/base"}
	ctx := context.Background()

	procs, err := r.Processes(ctx, e)
	if err != nil || len(procs) != 3 || procs[1].ID != "incident" || procs[1].StartForm != "incident-report" || !procs[1].Executable || procs[0].Executable {
		t.Fatalf("processes %+v, %v", procs, err)
	}
	pf, err := r.ProcessForms(ctx, e, "incident")
	if err != nil {
		t.Fatal(err)
	}
	want := []Step{
		{Element: "start", Kind: "startEvent", Name: "Incident reported", Form: "incident-report"},
		{Element: "triage", Kind: "userTask", Name: "Triage", Form: "triage-form"},
		{Element: "notify", Kind: "serviceTask", Name: "Notify", Form: "repair-mail"},
		{Element: "close", Kind: "userTask", Name: "Close", Form: "incident-report"},
	}
	if pf.Version != 4 || pf.PublicWizard || len(pf.Steps) != len(want) {
		t.Fatalf("process forms %+v", pf)
	}
	for i, st := range want {
		if pf.Steps[i] != st {
			t.Errorf("step %d: %+v, want %+v", i, pf.Steps[i], st)
		}
	}
	if pf, err := r.ProcessForms(ctx, e, "leave"); err != nil || !pf.PublicWizard || len(pf.Steps) != 1 {
		t.Errorf("a process with a public wizard: %+v, %v", pf, err)
	}
	if _, err := r.ProcessForms(ctx, e, "broken"); !errors.Is(err, ErrUnavailable) || !strings.Contains(err.Error(), "cannot be read") {
		t.Errorf("a model that is no XML: %v", err)
	}
	if _, err := r.ProcessForms(ctx, e, "nothing"); !errors.Is(err, ErrNotFound) {
		t.Errorf("an unknown process: %v", err)
	}

	// A form of a step is read although the credential lists no form; it
	// carries the steps that name it.
	f, err := r.Form(ctx, e, "incident-report", "incident")
	if err != nil || f.Name != "Incident report" || f.SavedAt.Unix() != 1791795101 || len(f.Steps) != 2 || f.Steps[1].Element != "close" || !strings.Contains(string(f.Schema), "Reporter") {
		t.Fatalf("form of a step %+v, %v", f, err)
	}
	if f, err := r.Form(ctx, e, "triage-form", "incident"); err != nil || f.ID != "triage-form" || f.Name != "" {
		t.Errorf("a form without a name: %+v, %v", f, err)
	}
	if _, err := r.Form(ctx, e, "hidden", "incident"); !errors.Is(err, ErrNotFound) || !strings.Contains(err.Error(), "named by no step of process incident") {
		t.Errorf("a form no step names: %v", err)
	}
	if _, err := r.Form(ctx, e, "incident-report", ""); !errors.Is(err, ErrNotFound) {
		t.Errorf("a form outside the listing, without its process: %v", err)
	}
	if _, err := r.Form(ctx, e, "incident-report", "nothing"); !errors.Is(err, ErrNotFound) {
		t.Errorf("a form of an unknown process: %v", err)
	}
}

// TestPublicStartLinks: the start links listed with the credential, a
// link's form read and its process started through the public routes,
// without the credential and with the person's address, and how the
// engine's refusals are told (ADR-0148).
func TestPublicStartLinks(t *testing.T) {
	srv, seen := fakePublicAtlas(t)
	env := map[string]string{"WWBB_ENGINE_ATLAS_TOKEN": "secret"}
	r := NewReader(func(k string) string { return env[k] }, srv.Client())
	e := site.Engine{ID: "atlas", Name: "Atlas", Kind: site.EngineAtlas, BaseURL: srv.URL + "/base"}
	ctx := context.Background()

	links, err := r.PublicLinks(ctx, e)
	if err != nil {
		t.Fatal(err)
	}
	if len(links) != 3 || links[0].Process != "gone" || links[0].Name != "" || links[1] != (PublicLink{Token: link, Process: "incident", Name: "Incident", Form: "incident-report"}) || links[2].Name != "Leave" {
		t.Fatalf("links %+v", links)
	}

	f, err := r.PublicForm(ctx, e, link, "", "203.0.113.7")
	if err != nil || f.ProcessName != "Incident" || f.URL != srv.URL+"/base/public/forms/"+link || f.Wizard != nil || !strings.Contains(string(f.Schema), "reporter") {
		t.Fatalf("public form %+v, %v", f, err)
	}
	if want := "GET /base/public/forms/" + link + "/schema 203.0.113.7 "; (*seen)[len(*seen)-1] != want {
		t.Errorf("asked %q", (*seen)[len(*seen)-1])
	}
	if f, err := r.PublicForm(ctx, e, link, "incident", ""); err != nil || f.Wizard == nil || *f.Wizard {
		t.Errorf("a link of a process without a wizard: %+v, %v", f, err)
	}
	if f, err := r.PublicForm(ctx, e, wizardLink, "leave", ""); err != nil || f.Wizard == nil || !*f.Wizard {
		t.Errorf("a link of a process with a wizard: %+v, %v", f, err)
	}
	if f, err := r.PublicForm(ctx, e, link, "nothing", ""); err != nil || f.Wizard != nil {
		t.Errorf("a link of a process the credential cannot read: %+v, %v", f, err)
	}
	if _, err := r.PublicForm(ctx, e, strings.Repeat("d", 64), "", ""); !errors.Is(err, ErrNotFound) || !strings.Contains(err.Error(), "unknown or was revoked") {
		t.Errorf("a revoked link: %v", err)
	}
	if _, err := r.PublicForm(ctx, e, strings.Repeat("e", 64), "", ""); !errors.Is(err, ErrUnavailable) {
		t.Errorf("a schema that is no object: %v", err)
	}

	started, err := r.StartPublic(ctx, e, link, "incident", json.RawMessage(`{"reporter":"anna"}`), "203.0.113.7")
	if err != nil || started.Wizard {
		t.Fatalf("start %+v, %v", started, err)
	}
	if want := "POST /base/public/forms/" + link + "/start 203.0.113.7 " + `{"variables":{"reporter":"anna"}}`; (*seen)[len(*seen)-1] != want {
		t.Errorf("sent %q", (*seen)[len(*seen)-1])
	}
	n := len(*seen)
	if _, err := r.StartPublic(ctx, e, wizardLink, "leave", json.RawMessage(`{}`), ""); !errors.Is(err, ErrRejected) || !strings.Contains(err.Error(), "open the link there") {
		t.Errorf("a process with a wizard: %v", err)
	}
	if len(*seen) != n {
		t.Errorf("a process with a wizard was started: %v", (*seen)[n:])
	}
	// Without its process, a wizard is learnt only from the answer.
	if started, err := r.StartPublic(ctx, e, wizardLink, "", json.RawMessage(`{}`), ""); err != nil || !started.Wizard {
		t.Errorf("a start into a wizard: %+v, %v", started, err)
	}
	for vars, want := range map[string]error{
		`{"unknown":1}`:    ErrRejected,
		`{"stopped":1}`:    ErrRejected,
		`{"busy":1}`:       ErrBusy,
		`{"silent":1}`:     ErrUnavailable,
		`{"broken":1}`:     ErrUnavailable,
		`["not","object"]`: ErrRejected,
		` `:                ErrRejected,
	} {
		if _, err := r.StartPublic(ctx, e, link, "", json.RawMessage(vars), ""); !errors.Is(err, want) {
			t.Errorf("start with %s: %v, want %v", vars, err, want)
		}
	}
	if _, err := r.StartPublic(ctx, e, link, "", json.RawMessage(`{"unknown":1}`), ""); err == nil || !strings.HasSuffix(err.Error(), ": not fields of this form: unknown") {
		t.Errorf("the engine's reason, in one line: %v", err)
	}
	if _, err := r.StartPublic(ctx, e, strings.Repeat("d", 64), "", json.RawMessage(`{}`), ""); !errors.Is(err, ErrNotFound) {
		t.Errorf("a start through a revoked link: %v", err)
	}

	// The public routes need no credential; listing the links does.
	delete(env, "WWBB_ENGINE_ATLAS_TOKEN")
	if f, err := r.PublicForm(ctx, e, link, "incident", ""); err != nil || f.Wizard != nil {
		t.Errorf("a public form without credential: %+v, %v", f, err)
	}
	if _, err := r.StartPublic(ctx, e, link, "incident", json.RawMessage(`{}`), ""); err != nil {
		t.Errorf("a start without credential: %v", err)
	}
	if _, err := r.PublicLinks(ctx, e); !errors.Is(err, ErrNoCredential) {
		t.Errorf("links without credential: %v", err)
	}
	env["WWBB_ENGINE_ATLAS_TOKEN"] = "viewer"
	if _, err := r.PublicLinks(ctx, e); !errors.Is(err, ErrRefused) {
		t.Errorf("links for a credential without the role: %v", err)
	}
}

// TestEngineMessage: the engine's reason is one line of at most 300
// characters, or says that none was given.
func TestEngineMessage(t *testing.T) {
	if got := engineMessage(strings.NewReader(`{"error":"` + strings.Repeat("ä", 301) + `"}`)); got != strings.Repeat("ä", 300)+"…" {
		t.Errorf("long reason %q", got)
	}
	if got := engineMessage(strings.NewReader(`not json`)); got != "no reason given" {
		t.Errorf("no reason %q", got)
	}
}
