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
		field(got, "type") != "interface" {
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
	found := w.bert.expect("GET", "/api/items?space=PLAT&type=interface&where=source-id%3Dr1", "", http.StatusOK)
	items := found["items"].([]any)
	if len(items) != 1 || !strings.Contains(jsonOf(items[0]), `"url":"https://opendata.swiss/de/dataset/wetter"`) ||
		!strings.Contains(jsonOf(items[0]), `"system":"wetter"`) || field(items[0].(map[string]any), "properties", "owner") != w.ids["bert"] {
		t.Fatalf("entry %s", jsonOf(found))
	}
	audit := w.admin.expect("GET", "/api/audit", "", http.StatusOK)
	if !strings.Contains(jsonOf(audit), "catalog.sync.change") || !strings.Contains(jsonOf(audit), `"catalog.sync"`) {
		t.Fatalf("not audited")
	}

	// A connector without a space does not run; removing one keeps its
	// entries.
	w.admin.expect("POST", "/api/connectors", connector("empty", "", "", `"API"`), http.StatusCreated)
	w.admin.expect("POST", "/api/connectors/empty/run", `{}`, http.StatusConflict)
	w.admin.expect("DELETE", "/api/connectors/nope", "", http.StatusNotFound)
	w.admin.expect("DELETE", "/api/connectors/wetter", "", http.StatusNoContent)
	if got := w.admin.expect("GET", "/api/connectors", "", http.StatusOK); len(got["connectors"].([]any)) != 1 {
		t.Errorf("after removing %s", jsonOf(got))
	}
	if found := w.bert.expect("GET", "/api/items?space=PLAT&type=interface", "", http.StatusOK); len(found["items"].([]any)) != 1 {
		t.Errorf("the entries went with the connector %s", jsonOf(found))
	}
}
