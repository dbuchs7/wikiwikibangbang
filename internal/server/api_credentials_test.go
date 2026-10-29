package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/dbuchs7/wikiwikibangbang/internal/audit"
	"github.com/dbuchs7/wikiwikibangbang/internal/engine"
	"github.com/dbuchs7/wikiwikibangbang/internal/site"
)

// TestACredentialStoredOnThePageTakesEffectAtOnce covers ADR-0151 through
// the API: an administrator stores an engine's token, which the next read
// sends; the value is never answered or audited; the environment wins and
// locks it; removing the engine removes its token.
func TestACredentialStoredOnThePageTakesEffectAtOnce(t *testing.T) {
	srv := fakeEngine(t)
	env := map[string]string{}
	w := newWorld(t, func(d *Deps) { d.Engines = engine.NewReader(func(k string) string { return env[k] }, srv.Client()) })
	w.admin.expect("PUT", "/api/engines/atlas", `{"name":"Atlas","kind":"atlas","baseURL":"`+srv.URL+`"}`, http.StatusCreated)
	if r := w.vera.do("GET", "/api/engines/atlas/processes", ""); r.Code != http.StatusBadGateway || !strings.Contains(r.Body.String(), "administration page") {
		t.Fatalf("no credential yet: %d %s", r.Code, r.Body)
	}

	const name = "/api/credentials/WWBB_ENGINE_ATLAS_TOKEN"
	w.bert.expect("PUT", name, `{"value":"secret"}`, http.StatusForbidden)
	w.admin.expect("PUT", "/api/credentials/WWBB_ENGINE_OTHER_TOKEN", `{"value":"secret"}`, http.StatusNotFound)
	w.admin.expect("PUT", "/api/credentials/WWBB_BACKUP_PASSPHRASE", `{"value":"secret"}`, http.StatusNotFound)
	w.admin.expect("PUT", name, `{"value":"two\nlines"}`, http.StatusBadRequest)
	w.admin.expect("PUT", name, `{"value":"  "}`, http.StatusBadRequest)
	stored := w.admin.expect("PUT", name, `{"value":"  secret\n"}`, http.StatusOK)
	if stored["source"] != "stored" || stored["name"] != "WWBB_ENGINE_ATLAS_TOKEN" || stored["stored"] == nil {
		t.Fatalf("stored %v", stored)
	}
	w.vera.expect("GET", "/api/engines/atlas/processes", "", http.StatusOK)

	list := w.admin.expect("GET", "/api/engines", "", http.StatusOK)["engines"].([]any)
	e := list[0].(map[string]any)
	state := e["credentialState"].(map[string]any)
	if e["credential"] != true || state["source"] != "stored" || state["stored"].(map[string]any)["by"] == "" {
		t.Fatalf("the engine as an administrator sees it: %v", e)
	}
	if reader := w.vera.expect("GET", "/api/engines", "", http.StatusOK)["engines"].([]any)[0].(map[string]any); reader["credentialState"] != nil {
		t.Errorf("a reader sees the credential's state: %v", reader)
	}

	// The environment wins over the stored value, and while it sets the
	// credential nothing is stored in its place.
	env["WWBB_ENGINE_ATLAS_TOKEN"] = "wrong"
	if r := w.vera.do("GET", "/api/engines/atlas/processes", ""); r.Code != http.StatusBadGateway {
		t.Fatalf("the variable is not sent first: %d %s", r.Code, r.Body)
	}
	if r := w.admin.do("PUT", name, `{"value":"secret"}`); r.Code != http.StatusConflict || !strings.Contains(r.Body.String(), "WWBB_ENGINE_ATLAS_TOKEN_FILE") {
		t.Fatalf("storing while the environment sets it: %d %s", r.Code, r.Body)
	}
	delete(env, "WWBB_ENGINE_ATLAS_TOKEN")

	// Replaced at once.
	w.admin.expect("PUT", name, `{"value":"wrong-again"}`, http.StatusOK)
	if r := w.vera.do("GET", "/api/engines/atlas/processes", ""); r.Code != http.StatusBadGateway {
		t.Fatalf("the replaced token is not sent: %d", r.Code)
	}
	w.bert.expect("DELETE", name, "", http.StatusForbidden)
	w.admin.expect("DELETE", name, "", http.StatusNoContent)
	w.admin.expect("DELETE", name, "", http.StatusNotFound)
	if _, ok := w.db.Credential("WWBB_ENGINE_ATLAS_TOKEN"); ok {
		t.Fatal("a removed credential is still stored")
	}

	// Removing the engine removes its stored credential.
	w.admin.expect("PUT", name, `{"value":"secret"}`, http.StatusOK)
	w.admin.expect("DELETE", "/api/engines/atlas", "", http.StatusNoContent)
	if _, ok := w.db.Credential("WWBB_ENGINE_ATLAS_TOKEN"); ok {
		t.Error("the credential of a removed engine is still stored")
	}

	set, removed := 0, 0
	for _, e := range w.db.Audit.List(0, 1000) {
		raw, _ := json.Marshal(e)
		for _, secret := range []string{"secret", "wrong-again"} {
			if strings.Contains(string(raw), `"`+secret+`"`) {
				t.Errorf("an audit entry holds a value: %s", raw)
			}
		}
		switch e.Event {
		case audit.CredentialSet:
			set++
			if e.Subject != "WWBB_ENGINE_ATLAS_TOKEN" {
				t.Errorf("audited %+v", e)
			}
		case audit.CredentialRemoved:
			removed++
		}
	}
	if set != 3 || removed != 2 {
		t.Errorf("%d stored and %d removed audited, want 3 and 2", set, removed)
	}
}

// A connector's token, and the e-mail address of an Atlassian import, are
// stored as an engine's are; another connector's e-mail is no credential.
func TestAConnectorsCredentialsAreStoredOnThePage(t *testing.T) {
	w := newWorld(t)
	if _, err := w.db.Site.SetConnector("admin", site.Connector{ID: "jira", Name: "Jira", Kind: site.KindAtlassian, IntervalHours: 24,
		Atlassian: &site.Atlassian{Site: "https://example.atlassian.net", Spaces: []string{"DOC"}}}); err != nil {
		t.Fatal(err)
	}
	w.admin.expect("PUT", "/api/credentials/WWBB_CONNECTOR_JIRA_EMAIL", `{"value":"bot@example.org"}`, http.StatusOK)
	w.admin.expect("PUT", "/api/credentials/WWBB_CONNECTOR_JIRA_TOKEN", `{"value":"api-token"}`, http.StatusOK)

	got := w.admin.expect("GET", "/api/connectors", "", http.StatusOK)
	raw, _ := json.Marshal(got)
	if strings.Contains(string(raw), "api-token") || strings.Contains(string(raw), "bot@example.org") {
		t.Fatalf("the connectors answer a stored value: %s", raw)
	}
	status := got["connectors"].([]any)[0].(map[string]any)["status"].(map[string]any)
	creds := status["credentials"].([]any)
	if status["credential"] != true || status["email"] != true || len(creds) != 2 || creds[1].(map[string]any)["source"] != "stored" {
		t.Fatalf("status %v", status)
	}

	w.admin.expect("DELETE", "/api/connectors/jira", "", http.StatusNoContent)
	for _, n := range []string{"WWBB_CONNECTOR_JIRA_EMAIL", "WWBB_CONNECTOR_JIRA_TOKEN"} {
		if _, ok := w.db.Credential(n); ok {
			t.Errorf("%s outlived its connector", n)
		}
	}
	w.admin.expect("PUT", "/api/credentials/WWBB_CONNECTOR_JIRA_TOKEN", `{"value":"api-token"}`, http.StatusNotFound)
}
