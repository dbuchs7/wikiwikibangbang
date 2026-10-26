package server

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestConnectorsThroughTheAPI(t *testing.T) {
	t.Setenv("WWBB_CONNECTOR_ALLOW_PRIVATE", "on") // the fake source is on the loopback address
	ckan := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"success":true,"result":{"count":1,"facets":{"res_format":{"API":1,"CSV":4}},"results":[{"id":"d1","name":"wetter","title":{"de":"Wetter"},
			"organization":{"name":"meteo","title":{"de":"Meteo"}},"resources":[{"id":"r1","format":"API","url":"https://api.example/v1"}]}]}}`)
	}))
	defer ckan.Close()
	w := newWorld(t)
	w.bert.expect("GET", "/api/connectors", "", http.StatusForbidden)
	w.bert.expect("POST", "/api/connectors", `{}`, http.StatusForbidden)
	w.bert.expect("PUT", "/api/connectors/opendata.swiss", `{}`, http.StatusForbidden)
	w.bert.expect("DELETE", "/api/connectors/opendata.swiss", "", http.StatusForbidden)
	w.bert.expect("POST", "/api/connectors/opendata.swiss/run", `{}`, http.StatusForbidden)
	w.bert.expect("GET", "/api/connectors/opendata.swiss/formats", "", http.StatusForbidden)

	got := w.admin.expect("GET", "/api/connectors", "", http.StatusOK)
	if len(got["connectors"].([]any)) != 0 || field(got["templates"].([]any)[0].(map[string]any), "ckan", "baseURL") != "https://ckan.opendata.swiss/api/3/action" ||
		field(got, "type") != "page" {
		t.Fatalf("before any %s", jsonOf(got))
	}
	connector := func(id, space, steward, formats string) string {
		return `{"id":"` + id + `","name":"Wetter","kind":"ckan","enabled":false,"space":"` + space + `","intervalHours":24,"steward":"` + steward +
			`","ckan":{"baseURL":"` + ckan.URL + `","portalURL":"https://opendata.swiss/de/dataset/","formats":[` + formats + `],"organizations":[]}}`
	}
	w.admin.expect("POST", "/api/connectors", connector("wetter", "NOPE", "", `"API"`), http.StatusBadRequest)
	w.admin.expect("POST", "/api/connectors", connector("wetter", "PLAT", "nobody", `"API"`), http.StatusBadRequest)
	w.admin.expect("POST", "/api/connectors", connector("wetter", "PLAT", "", ``), http.StatusBadRequest)
	w.admin.expect("POST", "/api/connectors", connector("Wetter Dienst", "PLAT", "", `"API"`), http.StatusBadRequest)
	w.admin.expect("POST", "/api/connectors", `{`, http.StatusBadRequest)
	w.admin.expect("POST", "/api/connectors", connector("wetter", "PLAT", w.ids["bert"], `"api"`), http.StatusCreated)
	w.admin.expect("POST", "/api/connectors", connector("wetter", "PLAT", "", `"API"`), http.StatusConflict)
	w.admin.expect("PUT", "/api/connectors/nope", connector("nope", "PLAT", "", `"API"`), http.StatusNotFound)
	w.admin.expect("PUT", "/api/connectors/wetter", `{`, http.StatusBadRequest)
	// The ID is the path's, whatever the body says.
	kept := w.admin.expect("PUT", "/api/connectors/wetter", connector("other", "PLAT", w.ids["bert"], `"api", "wms"`), http.StatusOK)
	if kept["id"] != "wetter" || jsonOf(field(kept, "ckan", "formats")) != `["API","WMS"]` {
		t.Fatalf("kept %s", jsonOf(kept))
	}

	formats := w.admin.expect("GET", "/api/connectors/wetter/formats", "", http.StatusOK)
	if jsonOf(formats) != `{"formats":[{"count":4,"format":"CSV"},{"count":1,"format":"API"}]}` {
		t.Fatalf("formats %s", jsonOf(formats))
	}
	w.admin.expect("GET", "/api/connectors/new-one/formats?baseURL="+ckan.URL+"&organizations=meteo,%20bafu", "", http.StatusOK)
	w.admin.expect("GET", "/api/connectors/wetter/formats?baseURL=ftp://x", "", http.StatusBadRequest)
	orgs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"success":true,"result":["meteo"]}`)
	}))
	defer orgs.Close()
	if got := w.admin.expect("GET", "/api/connectors/wetter/formats?baseURL="+orgs.URL+"&organizations=schweizerische", "", http.StatusBadRequest); !strings.Contains(jsonOf(got), "no organization schweizerische") {
		t.Errorf("unknown publisher %s", jsonOf(got))
	}
	w.admin.expect("GET", "/api/connectors/wetter/formats?baseURL=http://127.0.0.1:1", "", http.StatusBadGateway)
	w.admin.expect("POST", "/api/connectors/wetter/run", `{`, http.StatusBadRequest)
	w.admin.expect("POST", "/api/connectors/nope/run", `{}`, http.StatusNotFound)
	w.admin.expect("POST", "/api/connectors/wetter/run", `{"dryRun":false}`, http.StatusAccepted)
	var status map[string]any
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		got = w.admin.expect("GET", "/api/connectors", "", http.StatusOK)
		status = got["connectors"].([]any)[0].(map[string]any)["status"].(map[string]any)
		if h := status["history"].([]any); len(h) > 0 && status["running"] == false {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if h := status["history"].([]any)[0].(map[string]any); h["new"].(float64) != 1 || h["error"] != nil || h["connector"] != "wetter" || h["problems"] == nil {
		t.Fatalf("run %s", jsonOf(h))
	}
	if status["credentialVar"] != "WWBB_CONNECTOR_WETTER_TOKEN" || status["credential"] != false {
		t.Errorf("credential %s", jsonOf(status))
	}
	// The data set is an ordinary page (ADR-0093), which its steward
	// watches.
	found := w.bert.expect("GET", "/api/items?space=PLAT&type=page", "", http.StatusOK)
	items := found["items"].([]any)
	if len(items) != 1 || !strings.Contains(jsonOf(items[0]), `"url":"https://opendata.swiss/de/dataset/wetter"`) ||
		!strings.Contains(jsonOf(items[0]), `"system":"wetter"`) || !strings.Contains(jsonOf(items[0]), `"externalId":"page/d1"`) {
		t.Fatalf("entry %s", jsonOf(found))
	}
	watched := jsonOf(w.bert.expect("GET", "/api/me/watches", "", http.StatusOK))
	if !strings.Contains(watched, field(items[0].(map[string]any), "key").(string)) {
		t.Errorf("the steward does not watch the page: %s", watched)
	}
	audit := w.admin.expect("GET", "/api/audit", "", http.StatusOK)
	if !strings.Contains(jsonOf(audit), "catalog.sync.change") || !strings.Contains(jsonOf(audit), `"catalog.sync"`) {
		t.Fatalf("not audited")
	}

	// A JSON connector writes items of a type that exists and takes
	// what it maps (ADR-0127); the list names the types and their fields.
	jsonConnector := func(typ, fields string) string {
		return `{"id":"feed","name":"Feed","kind":"json","space":"PLAT","intervalHours":24,"type":"` + typ + `","fields":{` + fields + `},` +
			`"json":{"url":"https://feed.example/entries","items":"entries","id":"id","title":"title","values":{"owner":"owner"}}}`
	}
	w.admin.expect("POST", "/api/connectors", jsonConnector("nope", ""), http.StatusBadRequest)
	w.admin.expect("POST", "/api/connectors", jsonConnector("", `"owner":"owner"`), http.StatusBadRequest)
	w.admin.expect("POST", "/api/connectors", jsonConnector("", ""), http.StatusCreated)
	listed := w.admin.expect("GET", "/api/connectors", "", http.StatusOK)
	if !strings.Contains(jsonOf(listed["types"]), `{"fields":[],"name":"page"}`) || jsonOf(listed["ckanValues"]) != `["dataset","publisher","contact","url","formats"]` {
		t.Errorf("types %s, values %s", jsonOf(listed["types"]), jsonOf(listed["ckanValues"]))
	}
	w.admin.expect("DELETE", "/api/connectors/feed", "", http.StatusNoContent)

	// A Git connector names a folder of a repository (ADR-0131); its
	// address is checked, and the list names the values it gives.
	gitConnector := func(url string) string {
		return `{"id":"docs","name":"Docs","kind":"git","space":"PLAT","intervalHours":1,"git":{"url":"` + url + `"}}`
	}
	w.admin.expect("POST", "/api/connectors", gitConnector("https://example.org/docs"), http.StatusBadRequest)
	w.admin.expect("POST", "/api/connectors", gitConnector("https://github.com/o/r/tree/main/docs"), http.StatusCreated)
	if listed := w.admin.expect("GET", "/api/connectors", "", http.StatusOK); jsonOf(listed["gitValues"]) != `["commit","path","url"]` || !strings.Contains(jsonOf(listed["kinds"]), `"git"`) {
		t.Errorf("git values %s, kinds %s", jsonOf(listed["gitValues"]), jsonOf(listed["kinds"]))
	}
	w.admin.expect("DELETE", "/api/connectors/docs", "", http.StatusNoContent)

	// An API description offers its lists as JSON sources (ADR-0130).
	spec := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "openapi: 3.0.0\ninfo: {title: Registry}\npaths:\n  /services:\n    get:\n      responses:\n        200:\n"+
			"          content: {application/json: {schema: {type: array, items: {properties: {id: {type: string}, name: {type: string}}}}}}\n")
	}))
	defer spec.Close()
	described := w.admin.expect("GET", "/api/connectors/new/openapi?url="+spec.URL+"/openapi.yaml", "", http.StatusOK)
	if described["title"] != "Registry" || field(described["lists"].([]any)[0].(map[string]any), "source", "url") != spec.URL+"/services" {
		t.Errorf("described %s", jsonOf(described))
	}
	w.bert.expect("GET", "/api/connectors/new/openapi?url="+spec.URL, "", http.StatusForbidden)
	w.admin.expect("GET", "/api/connectors/new/openapi?url=ftp://x", "", http.StatusBadRequest)
	w.admin.expect("GET", "/api/connectors/new/openapi?url=http://127.0.0.1:1/spec", "", http.StatusBadGateway)

	// An Atlassian import names its spaces itself (ADR-0128); the accounts
	// it names must exist, and the cut-over turns its schedule off.
	atlassian := func(user string) string {
		return `{"id":"acme","name":"Acme","kind":"atlassian","enabled":true,"intervalHours":24,` +
			`"atlassian":{"site":"https://acme.atlassian.net","spaces":["ENG"],"projects":[],"users":{"acc-1":"` + user + `"},"grants":["@eng=viewer"]}}`
	}
	w.admin.expect("POST", "/api/connectors", atlassian("nobody"), http.StatusBadRequest)
	w.admin.expect("POST", "/api/connectors", atlassian("bert"), http.StatusCreated)
	listed = w.admin.expect("GET", "/api/connectors", "", http.StatusOK)
	if !strings.Contains(jsonOf(listed["connectors"]), `"emailVar":"WWBB_CONNECTOR_ACME_EMAIL"`) {
		t.Errorf("the e-mail address's variable %s", jsonOf(listed["connectors"]))
	}
	w.bert.expect("POST", "/api/connectors/acme/release", "", http.StatusForbidden)
	w.admin.expect("POST", "/api/connectors/nope/release", "", http.StatusNotFound)
	w.admin.expect("POST", "/api/connectors/wetter/release", "", http.StatusConflict)
	if got := w.admin.expect("POST", "/api/connectors/acme/release", "", http.StatusOK); jsonOf(got) != `{"released":[]}` {
		t.Errorf("released %s", jsonOf(got))
	}
	listed = w.admin.expect("GET", "/api/connectors", "", http.StatusOK)
	if acme := listed["connectors"].([]any)[0].(map[string]any); acme["id"] != "acme" || acme["enabled"] != false {
		t.Errorf("the schedule is on after the cut-over: %s", jsonOf(listed["connectors"]))
	}
	w.admin.expect("POST", "/api/connectors/acme/run", `{"dryRun":true}`, http.StatusAccepted)
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if !strings.Contains(jsonOf(w.admin.expect("GET", "/api/connectors", "", http.StatusOK)), `"running":true`) {
			break
		}
	}
	w.admin.expect("DELETE", "/api/connectors/acme", "", http.StatusNoContent)

	// A connector without a space does not run; removing one keeps its
	// entries.
	w.admin.expect("POST", "/api/connectors", connector("empty", "", "", `"API"`), http.StatusCreated)
	w.admin.expect("POST", "/api/connectors/empty/run", `{}`, http.StatusConflict)
	w.admin.expect("DELETE", "/api/connectors/nope", "", http.StatusNotFound)
	w.admin.expect("DELETE", "/api/connectors/wetter", "", http.StatusNoContent)
	if got := w.admin.expect("GET", "/api/connectors", "", http.StatusOK); len(got["connectors"].([]any)) != 1 {
		t.Errorf("after removing %s", jsonOf(got))
	}
	if found := w.bert.expect("GET", "/api/items?space=PLAT&type=page", "", http.StatusOK); len(found["items"].([]any)) != 1 {
		t.Errorf("the entries went with the connector %s", jsonOf(found))
	}
}
