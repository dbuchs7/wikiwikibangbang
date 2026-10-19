package server

import (
	"net/http"
	"slices"
	"testing"
)

func treeTitles(t *testing.T, c *client, path string) []string {
	t.Helper()
	var out []string
	for _, n := range c.expect("GET", path, "", http.StatusOK)["items"].([]any) {
		out = append(out, n.(map[string]any)["title"].(string))
	}
	return out
}

func TestPageTreeThroughTheAPI(t *testing.T) {
	w := newWorld(t)
	w.admin.expect("POST", "/api/types", `{"name":"page","body":true,"fields":[]}`, http.StatusCreated)
	create := func(title, parent string) string {
		it := w.bert.expect("POST", "/api/items", `{"space":"PLAT","type":"page","title":"`+title+`","parent":"`+parent+`"}`, http.StatusCreated)
		return field(it, "item", "key").(string)
	}
	home := create("Home", "")
	homeID := field(w.bert.expect("GET", "/api/items/"+home, "", http.StatusOK), "item", "id").(string)
	a, b := create("A", homeID), create("B", homeID)

	top := w.vera.expect("GET", "/api/spaces/PLAT/tree", "", http.StatusOK)["items"].([]any)
	if len(top) != 1 || top[0].(map[string]any)["key"] != home || top[0].(map[string]any)["hasChildren"] != true {
		t.Fatalf("top %v", top)
	}
	if got := treeTitles(t, w.vera, "/api/spaces/PLAT/tree?parent="+home); !slices.Equal(got, []string{"A", "B"}) {
		t.Fatalf("children %v", got)
	}
	// Place B before A, by key; a viewer may not.
	w.vera.expect("POST", "/api/items/"+b+"/move", `{"space":"PLAT","parent":"`+homeID+`","before":"`+a+`"}`, http.StatusForbidden)
	w.bert.expect("POST", "/api/items/"+b+"/move", `{"space":"PLAT","parent":"`+homeID+`","before":"`+a+`"}`, http.StatusOK)
	if got := treeTitles(t, w.vera, "/api/spaces/PLAT/tree?parent="+home); !slices.Equal(got, []string{"B", "A"}) {
		t.Fatalf("after placing %v", got)
	}
	// "before": "" places last; a move without it keeps the order.
	w.bert.expect("POST", "/api/items/"+b+"/move", `{"space":"PLAT","parent":"`+homeID+`","before":""}`, http.StatusOK)
	w.bert.expect("POST", "/api/items/"+a+"/move", `{"space":"PLAT","parent":"`+homeID+`"}`, http.StatusOK)
	if got := treeTitles(t, w.vera, "/api/spaces/PLAT/tree?parent="+home); !slices.Equal(got, []string{"A", "B"}) {
		t.Fatalf("after placing last %v", got)
	}
	w.bert.expect("POST", "/api/items/"+b+"/move", `{"space":"PLAT","parent":"`+homeID+`","before":"`+home+`"}`, http.StatusBadRequest)

	// Paged, and nothing for someone without a role.
	page := w.vera.expect("GET", "/api/spaces/PLAT/tree?parent="+home+"&limit=1", "", http.StatusOK)
	if len(page["items"].([]any)) != 1 || page["next"] == nil {
		t.Fatalf("page %v", page)
	}
	if got := w.carla.expect("GET", "/api/spaces/PLAT/tree", "", http.StatusOK)["items"].([]any); len(got) != 0 {
		t.Fatalf("outsider sees %v", got)
	}
	w.carla.expect("GET", "/api/spaces/PLAT/tree?parent="+home, "", http.StatusNotFound)
	w.vera.expect("GET", "/api/spaces/NOPE/tree", "", http.StatusNotFound)
}
