package server

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// A protected passage through the API (ADR-0088): bert edits PLAT and
// protects a name for himself; vera views PLAT, carla has no role there,
// admin administers every space.
func TestProtectedPassagesThroughTheAPI(t *testing.T) {
	w := newWorld(t)
	it := w.bert.expect("POST", "/api/items", `{"space":"PLAT","type":"task","title":"Review","properties":{"priority":"low"},
		"body":[{"kind":"paragraph","text":"Pay Paul Muster on Friday."}]}`, http.StatusCreated)
	key := field(it, "item", "key").(string)
	path := "/api/items/" + key
	w.vera.expect("POST", path+"/secrets", `{"block":0,"start":4,"end":15}`, http.StatusForbidden)
	w.bert.expect("POST", path+"/secrets", `{"block":3,"start":4,"end":15}`, http.StatusBadRequest)
	got := w.bert.expect("POST", path+"/secrets", `{"block":0,"start":4,"end":15,"readers":["@hr"]}`, http.StatusOK)
	secrets := field(got, "item", "secrets").([]any)
	sec := secrets[0].(map[string]any)
	id := sec["id"].(string)
	if sec["readable"] != true || sec["decides"] != true || field(got, "item", "readsAllSecrets") != true {
		t.Fatalf("bert sees %v", got)
	}

	read := w.vera.expect("GET", path, "", http.StatusOK)
	text := field(read, "item", "body").([]any)[0].(map[string]any)["text"].(string)
	vsec := field(read, "item", "secrets").([]any)[0].(map[string]any)
	if strings.Contains(text, "Paul") || !strings.Contains(text, "▒") || vsec["readable"] != false || vsec["readers"] != nil || field(read, "item", "readsAllSecrets") != false {
		t.Fatalf("vera reads %q, %v", text, vsec)
	}
	// Nothing else gives the passage away to her.
	for _, p := range []string{"/api/search?q=Muster", "/api/items?space=PLAT", path + "/export/html"} {
		if body := w.vera.do("GET", p, "").Body.String(); strings.Contains(body, "Muster") {
			t.Errorf("%s gives the passage away: %s", p, body)
		}
	}
	var version string
	for _, e := range field(w.vera.expect("GET", path+"/history", "", http.StatusOK), "events").([]any) {
		if m := e.(map[string]any); m["kind"] == "BodyCommitted" {
			version = fmt.Sprint(int(m["seq"].(float64)))
		}
	}
	w.vera.expect("GET", path+"/versions/"+version, "", http.StatusForbidden)
	w.admin.expect("PUT", path+"/body", `{"blocks":[{"kind":"paragraph","text":"overwritten"}]}`, http.StatusForbidden)
	w.admin.expect("POST", path+"/copy", `{"space":"PLAT","title":"Copy"}`, http.StatusForbidden)
	if r := w.vera.do("GET", path+"/live", ""); r.Code == http.StatusOK {
		t.Errorf("one who may not read the passage joins the live session")
	}

	// Vera asks; bert and the administrator are told; the administrator
	// grants it.
	w.vera.expect("POST", path+"/secrets/"+id+"/request", "", http.StatusOK)
	note := field(w.bert.expect("GET", "/api/notifications", "", http.StatusOK), "notifications").([]any)[0].(map[string]any)
	if note["kind"] != "SecretRequested" || note["key"] != key {
		t.Fatalf("bert is told %v", note)
	}
	asked := field(w.admin.expect("GET", path, "", http.StatusOK), "item", "secrets").([]any)[0].(map[string]any)
	if asked["decides"] != true || asked["requests"].([]any)[0] != w.ids["vera"] {
		t.Fatalf("the administrator sees %v", asked)
	}
	w.vera.expect("POST", path+"/secrets/"+id+"/requests/"+w.ids["vera"], `{"grant":true}`, http.StatusForbidden)
	w.admin.expect("POST", path+"/secrets/"+id+"/requests/"+w.ids["vera"], `{"grant":true}`, http.StatusOK)
	read = w.vera.expect("GET", path, "", http.StatusOK)
	if text := field(read, "item", "body").([]any)[0].(map[string]any)["text"].(string); !strings.Contains(text, "Paul Muster") {
		t.Fatalf("granted, vera reads %q", text)
	}
	w.carla.expect("POST", path+"/secrets/"+id+"/request", "", http.StatusNotFound)

	// Readers set by hand, then the protection taken off.
	w.bert.expect("PUT", path+"/secrets/"+id, `{"readers":["@hr"]}`, http.StatusOK)
	w.bert.expect("DELETE", path+"/secrets/"+id, "", http.StatusOK)
	if got := field(w.admin.expect("GET", path, "", http.StatusOK), "item", "secrets"); got != nil {
		t.Fatalf("passages after unprotecting: %v", got)
	}
	w.admin.expect("POST", path+"/copy", `{"space":"PLAT","title":"Copy"}`, http.StatusCreated)
}
