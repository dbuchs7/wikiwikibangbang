package server

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestCatalogSyncThroughTheAPI(t *testing.T) {
	ckan := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"success":true,"result":{"count":1,"facets":{"res_format":{"API":1,"CSV":4}},"results":[{"id":"d1","name":"wetter","title":{"de":"Wetter"},
			"organization":{"name":"meteo","title":{"de":"Meteo"}},"resources":[{"id":"r1","format":"API","url":"https://api.example/v1"}]}]}}`)
	}))
	defer ckan.Close()
	w := newWorld(t)
	w.bert.expect("GET", "/api/catalog/opendata", "", http.StatusForbidden)
	w.bert.expect("PUT", "/api/catalog/opendata/settings", `{}`, http.StatusForbidden)
	w.bert.expect("POST", "/api/catalog/opendata/run", `{}`, http.StatusForbidden)

	got := w.admin.expect("GET", "/api/catalog/opendata", "", http.StatusOK)
	if field(got, "settings", "baseURL") != "https://ckan.opendata.swiss/api/3/action" || field(got, "type") != "interface" {
		t.Fatalf("defaults %s", jsonOf(got))
	}
	w.admin.expect("POST", "/api/catalog/opendata/run", `{"dryRun":true}`, http.StatusConflict)
	settings := func(space, steward, formats string) string {
		return `{"enabled":false,"space":"` + space + `","baseURL":"` + ckan.URL + `","portalURL":"https://opendata.swiss/de/dataset/","formats":[` + formats + `],"organizations":[],"intervalHours":24,"steward":"` + steward + `"}`
	}
	w.admin.expect("PUT", "/api/catalog/opendata/settings", settings("NOPE", "", `"API"`), http.StatusBadRequest)
	w.admin.expect("PUT", "/api/catalog/opendata/settings", settings("PLAT", "nobody", `"API"`), http.StatusBadRequest)
	w.admin.expect("PUT", "/api/catalog/opendata/settings", settings("PLAT", "", ``), http.StatusBadRequest)
	w.admin.expect("PUT", "/api/catalog/opendata/settings", `{`, http.StatusBadRequest)
	w.admin.expect("PUT", "/api/catalog/opendata/settings", settings("PLAT", w.ids["bert"], `"api"`), http.StatusOK)

	formats := w.admin.expect("GET", "/api/catalog/opendata/formats", "", http.StatusOK)
	if jsonOf(formats) != `{"formats":[{"count":4,"format":"CSV"},{"count":1,"format":"API"}]}` {
		t.Fatalf("formats %s", jsonOf(formats))
	}
	w.bert.expect("GET", "/api/catalog/opendata/formats", "", http.StatusForbidden)
	w.admin.expect("GET", "/api/catalog/opendata/formats?baseURL="+ckan.URL+"&organizations=meteo,%20bafu", "", http.StatusOK)
	w.admin.expect("GET", "/api/catalog/opendata/formats?baseURL=ftp://x", "", http.StatusBadRequest)
	w.admin.expect("GET", "/api/catalog/opendata/formats?baseURL=http://127.0.0.1:1", "", http.StatusBadGateway)
	w.admin.expect("POST", "/api/catalog/opendata/run", `{`, http.StatusBadRequest)
	w.admin.expect("POST", "/api/catalog/opendata/run", `{"dryRun":false}`, http.StatusAccepted)
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		got = w.admin.expect("GET", "/api/catalog/opendata", "", http.StatusOK)
		if h := field(got, "status", "history").([]any); len(h) > 0 && field(got, "status", "running") == false {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if h := field(got, "status", "history").([]any)[0].(map[string]any); h["new"].(float64) != 1 || h["error"] != nil {
		t.Fatalf("run %s", jsonOf(h))
	}
	found := w.bert.expect("GET", "/api/items?space=PLAT&type=interface&where=source-id%3Dr1", "", http.StatusOK)
	items := found["items"].([]any)
	if len(items) != 1 || !strings.Contains(jsonOf(items[0]), `"url":"https://opendata.swiss/de/dataset/wetter"`) ||
		field(items[0].(map[string]any), "properties", "owner") != w.ids["bert"] {
		t.Fatalf("entry %s", jsonOf(found))
	}
	audit := w.admin.expect("GET", "/api/audit", "", http.StatusOK)
	if !strings.Contains(jsonOf(audit), "catalog.sync.change") || !strings.Contains(jsonOf(audit), `"catalog.sync"`) {
		t.Fatalf("not audited")
	}
}
