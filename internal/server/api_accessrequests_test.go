package server

import (
	"net/http"
	"testing"
)

// The requests to read a protection wait in the task list of those who
// decide (ADR-0161): bert protects a name in PLAT, vera, who only views
// PLAT, asks; bert and the administrator find the request, vera and carla
// do not; an answer takes it off.
func TestAccessRequestsThroughTheAPI(t *testing.T) {
	w := newWorld(t)
	it := w.bert.expect("POST", "/api/items", `{"space":"PLAT","type":"task","title":"Review","properties":{"priority":"low"},
		"body":[{"kind":"paragraph","text":"Pay Paul Muster on Friday."}]}`, http.StatusCreated)
	key := field(it, "item", "key").(string)
	path := "/api/items/" + key
	got := w.bert.expect("POST", path+"/secrets", `{"block":0,"start":4,"end":15}`, http.StatusOK)
	id := field(got, "item", "secrets").([]any)[0].(map[string]any)["id"].(string)

	none := func(c *client, who string) {
		t.Helper()
		list := c.expect("GET", "/api/me/access-requests", "", http.StatusOK)
		if n := len(list["requests"].([]any)); n != 0 || list["total"] != float64(0) {
			t.Fatalf("%s is to decide %v", who, list)
		}
	}
	none(w.bert, "bert")
	asked := w.vera.expect("POST", path+"/secrets/"+id+"/request", "", http.StatusOK)
	if sec := field(asked, "item", "secrets").([]any)[0].(map[string]any); sec["requested"] != true || sec["requestedAt"] == nil {
		t.Fatalf("vera sees %v", sec)
	}
	for who, c := range map[string]*client{"bert": w.bert, "admin": w.admin} {
		list := c.expect("GET", "/api/me/access-requests", "", http.StatusOK)
		rs := list["requests"].([]any)
		if len(rs) != 1 || list["total"] != float64(1) || list["next"] != nil {
			t.Fatalf("%s is to decide %v", who, list)
		}
		r := rs[0].(map[string]any)
		if r["key"] != key || r["title"] != "Review" || r["space"] != "PLAT" || r["secret"] != id || r["number"] != float64(1) ||
			r["principal"] != w.ids["vera"] || r["at"] == "" || r["blocks"] != nil {
			t.Fatalf("%s sees %v", who, r)
		}
	}
	none(w.vera, "vera")
	none(w.carla, "carla")
	w.bert.expect("GET", "/api/me/access-requests?limit=0", "", http.StatusBadRequest)
	w.bert.expect("GET", "/api/me/access-requests?after=x", "", http.StatusBadRequest)
	w.admin.expect("POST", path+"/secrets/"+id+"/requests/"+w.ids["vera"], `{"grant":false}`, http.StatusOK)
	none(w.bert, "bert")
	none(w.admin, "admin")
}

// Requests are paged, oldest first, and the cursor continues the list.
func TestAccessRequestsArePaged(t *testing.T) {
	w := newWorld(t)
	var ids []string
	for _, title := range []string{"One", "Two"} {
		it := w.bert.expect("POST", "/api/items", `{"space":"PLAT","type":"task","title":"`+title+`","properties":{"priority":"low"},
			"body":[{"kind":"paragraph","text":"Secret text."}]}`, http.StatusCreated)
		path := "/api/items/" + field(it, "item", "key").(string)
		got := w.bert.expect("POST", path+"/secrets", `{"fromBlock":0,"toBlock":0}`, http.StatusOK)
		id := field(got, "item", "secrets").([]any)[0].(map[string]any)["id"].(string)
		w.vera.expect("POST", path+"/secrets/"+id+"/request", "", http.StatusOK)
		ids = append(ids, id)
	}
	first := w.bert.expect("GET", "/api/me/access-requests?limit=1", "", http.StatusOK)
	r := first["requests"].([]any)[0].(map[string]any)
	if r["title"] != "One" || r["blocks"] != true || first["total"] != float64(2) || first["next"] == nil {
		t.Fatalf("first page %v", first)
	}
	second := w.bert.expect("GET", "/api/me/access-requests?limit=1&after="+first["next"].(string), "", http.StatusOK)
	if r := second["requests"].([]any)[0].(map[string]any); r["title"] != "Two" || r["secret"] != ids[1] || second["next"] != nil {
		t.Fatalf("second page %v", second)
	}
}
