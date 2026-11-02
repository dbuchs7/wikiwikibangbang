package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// A request to read protected content becomes a task for the page's owner
// (ADR-0162): bert protects a sentence of a page the administrator owns;
// vera, who only views PLAT, asks; the task is made in PLAT below the
// page, in the administrator's language, readable only by those it
// concerns; the administrator grants it from the task, which is done.
func TestARequestBecomesATaskThroughTheAPI(t *testing.T) {
	w := newWorld(t)
	tmpl := w.admin.expect("GET", "/api/types/templates", "", http.StatusOK)["templates"].([]any)
	if len(tmpl) != 3 || tmpl[1].(map[string]any)["name"] != "task" {
		t.Fatalf("templates %v", tmpl)
	}
	page, _ := json.Marshal(tmpl[0])
	w.admin.expect("POST", "/api/types", string(page), http.StatusCreated)
	// The installation's task type has a required priority, so it cannot
	// hold the task; one that can is chosen by its definition.
	w.admin.expect("POST", "/api/types", `{"name":"todo","body":true,"fields":[{"name":"assignee","kind":"user"}],
		"workflow":{"initial":"open","states":["open","done"],"transitions":[{"name":"finish","from":["open"],"to":"done"}]}}`, http.StatusCreated)
	w.admin.expect("PUT", "/api/me/language", `{"language":"de"}`, http.StatusNoContent)
	w.ids["admin"] = w.admin.expect("GET", "/api/me", "", http.StatusOK)["user"].(map[string]any)["id"].(string)
	u := w.admin.expect("POST", "/api/users", `{"username":"ulla","password":"ulla's password"}`, http.StatusCreated)
	ulla := u["user"].(map[string]any)["id"].(string)
	w.admin.expect("PUT", "/api/spaces/PLAT/roles/"+ulla, `{"role":"viewer"}`, http.StatusNoContent)
	uc := &client{t: t, h: w.bert.h}
	uc.login("ulla", "ulla's password")

	it := w.bert.expect("POST", "/api/items", `{"space":"PLAT","type":"page","title":"Gehälter","properties":{"owner":"`+w.ids["admin"]+`"},
		"body":[{"kind":"paragraph","text":"Paul Muster verdient 9000."}]}`, http.StatusCreated)
	key := field(it, "item", "key").(string)
	path := "/api/items/" + key
	got := w.bert.expect("POST", path+"/secrets", `{"fromBlock":0,"toBlock":0}`, http.StatusOK)
	id := field(got, "item", "secrets").([]any)[0].(map[string]any)["id"].(string)

	asked := w.vera.expect("POST", path+"/secrets/"+id+"/request", "", http.StatusOK)
	own := field(asked, "item", "secrets").([]any)[0].(map[string]any)["own"].(map[string]any)
	taskKey, _ := own["taskKey"].(string)
	if taskKey == "" || own["assignee"] != w.ids["admin"] {
		t.Fatalf("vera sees her request as %v", own)
	}
	task := w.admin.expect("GET", "/api/items/"+taskKey, "", http.StatusOK)["item"].(map[string]any)
	title := task["title"].(string)
	body, _ := json.Marshal(task["body"])
	if title != "Freigabe-Anfrage: vera für «Gehälter»" || task["type"] != "todo" || task["parent"] != field(it, "item", "id") ||
		!strings.Contains(string(body), "vera hat am") || strings.Contains(string(body), "9000") || strings.Contains(string(body), "Paul") {
		t.Fatalf("task %q %s", title, body)
	}
	if l := task["labels"].([]any); len(l) != 1 || l[0] != "access-request" {
		t.Fatalf("labels %v", l)
	}
	req := task["accessRequest"].(map[string]any)
	if req["key"] != key || req["secret"] != id || req["principal"] != w.ids["vera"] || req["waiting"] != true || req["answers"] != true || req["blocks"] != true {
		t.Fatalf("the task's request %v", req)
	}
	// Vera reads her task but does not answer it; ulla, who views the
	// room too, does not learn of it.
	if r := w.vera.expect("GET", "/api/items/"+taskKey, "", http.StatusOK)["item"].(map[string]any)["accessRequest"].(map[string]any); r["answers"] != false {
		t.Fatalf("vera answers %v", r)
	}
	uc.expect("GET", "/api/items/"+taskKey, "", http.StatusNotFound)
	// The page's card names the task to the owner.
	card := w.admin.expect("GET", path, "", http.StatusOK)
	sec := field(card, "item", "secrets").([]any)[0].(map[string]any)
	if asks := sec["asks"].([]any); sec["answers"] != true || len(asks) != 1 || asks[0].(map[string]any)["taskKey"] != taskKey {
		t.Fatalf("the owner sees %v", sec)
	}
	// The owner grants; the task records it, in German, and is done.
	w.admin.expect("POST", path+"/secrets/"+id+"/requests/"+w.ids["vera"], `{"grant":true}`, http.StatusOK)
	done := w.admin.expect("GET", "/api/items/"+taskKey, "", http.StatusOK)["item"].(map[string]any)
	if done["state"] != "done" || done["accessRequest"].(map[string]any)["waiting"] != false {
		t.Fatalf("granted, the task is %v", done["state"])
	}
	comments := w.admin.expect("GET", "/api/items/"+taskKey+"/comments", "", http.StatusOK)
	if b, _ := json.Marshal(comments); !strings.Contains(string(b), "Freigegeben von admin.") {
		t.Fatalf("comments %s", b)
	}
	if b := w.vera.expect("GET", path, "", http.StatusOK); !strings.Contains(field(b, "item", "body").([]any)[0].(map[string]any)["text"].(string), "9000") {
		t.Fatal("granted, vera does not read it")
	}
	// Asked again by ulla and closed by the owner: declined, in English
	// for ulla, whose task is worded in the owner's language.
	uc.expect("POST", path+"/secrets/"+id+"/request", "", http.StatusOK)
	second := uc.expect("GET", path, "", http.StatusOK)
	k := field(second, "item", "secrets").([]any)[0].(map[string]any)["own"].(map[string]any)["taskKey"].(string)
	w.admin.expect("POST", "/api/items/"+k+"/transitions", `{"name":"finish"}`, http.StatusOK)
	w.admin.expect("PUT", "/api/me/language", `{"language":""}`, http.StatusNoContent)
	if s := field(uc.expect("GET", path, "", http.StatusOK), "item", "secrets").([]any)[0].(map[string]any); s["requested"] == true {
		t.Fatalf("closed, ulla still waits: %v", s)
	}
}

// The texts of a request's task fall back to English and to IDs.
func TestAccessTextsInEnglish(t *testing.T) {
	w := newWorld(t)
	a := accessTexts{w.db.Accounts}
	if a.name("nobody") != "nobody" || a.lang("nobody") != "en" {
		t.Fatal("unknown people")
	}
}
