package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"slices"
	"testing"
)

// TestCopyPages: a page is copied with its files, body, labels and child
// pages; images show the copies; a copy into its own subtree stops at the
// subtree as it was (ADR-0067).
func TestCopyPages(t *testing.T) {
	w := newWorld(t)
	w.admin.expect("POST", "/api/types", `{"name":"page","body":true,"fields":[]}`, http.StatusCreated)
	w.admin.expect("POST", "/api/spaces", `{"key":"DOCS","name":"Docs"}`, http.StatusCreated)
	w.admin.expect("PUT", "/api/spaces/DOCS/roles/"+w.ids["bert"], `{"role":"editor"}`, http.StatusNoContent)
	create := func(title, parent string) (key, id string) {
		it := w.bert.expect("POST", "/api/items", `{"space":"PLAT","type":"page","title":"`+title+`","parent":"`+parent+`"}`, http.StatusCreated)
		return field(it, "item", "key").(string), field(it, "item", "id").(string)
	}
	guide, guideID := create("Guide", "")
	_, stepID := create("Step", guideID)
	create("Detail", stepID)
	png := append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{7}, 40)...)
	up := w.bert.upload("/api/items/"+guide+"/attachments?name=chart.png", "application/octet-stream", png)
	if up.Code != http.StatusCreated {
		t.Fatalf("upload %d %s", up.Code, up.Body)
	}
	fileID := field(decodeBody(t, up), "attachment", "id").(string)
	w.bert.expect("PUT", "/api/items/"+guide+"/body", `{"blocks":[{"kind":"paragraph","text":"Read me"},{"kind":"image","ref":"`+fileID+`","text":"Chart"}]}`, http.StatusOK)
	w.bert.expect("PUT", "/api/items/"+guide+"/labels", `{"labels":["howto"]}`, http.StatusOK)

	w.vera.expect("POST", "/api/items/"+guide+"/copy", `{"space":"PLAT","title":"x"}`, http.StatusForbidden)
	got := w.bert.expect("POST", "/api/items/"+guide+"/copy", `{"space":"DOCS","title":"Guide (copy)","children":true}`, http.StatusCreated)
	if got["copied"] != float64(3) {
		t.Fatalf("copy %v", got)
	}
	copyKey := got["item"].(map[string]any)["key"].(string)
	c := w.bert.expect("GET", "/api/items/"+copyKey, "", http.StatusOK)["item"].(map[string]any)
	if c["title"] != "Guide (copy)" || c["space"] != "DOCS" || !slices.Equal(toStrings(c["labels"]), []string{"howto"}) {
		t.Fatalf("copy %v", c)
	}
	blocks := c["body"].([]any)
	img := blocks[1].(map[string]any)
	if len(blocks) != 2 || img["ref"] == fileID || img["text"] != "Chart" {
		t.Fatalf("body %v", blocks)
	}
	files := w.bert.expect("GET", "/api/items/"+copyKey+"/attachments", "", http.StatusOK)["attachments"].([]any)
	if len(files) != 1 || files[0].(map[string]any)["id"] != img["ref"] || files[0].(map[string]any)["origin"] == nil {
		t.Fatalf("files %v", files)
	}
	if r := w.bert.do("GET", "/api/items/"+copyKey+"/attachments/"+img["ref"].(string), ""); r.Code != http.StatusOK || !bytes.Equal(r.Body.Bytes(), png) {
		t.Fatalf("file content %d", r.Code)
	}
	if got := treeTitles(t, w.bert, "/api/spaces/DOCS/tree?parent="+copyKey); !slices.Equal(got, []string{"Step"}) {
		t.Fatalf("copied children %v", got)
	}
	// Into its own subtree: Guide goes below Step; the copy holds Step and
	// Detail once, not itself again.
	again := w.bert.expect("POST", "/api/items/"+guide+"/copy", `{"space":"PLAT","parent":"`+stepID+`","title":"Guide again","children":true}`, http.StatusCreated)
	if again["copied"] != float64(3) {
		t.Fatalf("copy into own subtree %v", again)
	}
	// Without children, only the page.
	one := w.bert.expect("POST", "/api/items/"+guide+"/copy", `{"space":"PLAT","title":"Alone"}`, http.StatusCreated)
	if one["copied"] != float64(1) {
		t.Fatalf("single %v", one)
	}
	// The original is unchanged.
	if o := w.bert.expect("GET", "/api/items/"+guide, "", http.StatusOK)["item"].(map[string]any); o["title"] != "Guide" || o["space"] != "PLAT" {
		t.Fatalf("original %v", o)
	}
}

func toStrings(v any) []string {
	var out []string
	for _, x := range v.([]any) {
		out = append(out, x.(string))
	}
	return out
}

func decodeBody(t *testing.T, r interface{ Result() *http.Response }) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.NewDecoder(r.Result().Body).Decode(&m); err != nil {
		t.Fatal(err)
	}
	return m
}

// TestCopyNeverLeavesAnOpenCopy: a copy whose restriction would lock its
// maker out is refused before anything is made, and a page protected
// through a page above it is not copied out from under it.
func TestCopyNeverLeavesAnOpenCopy(t *testing.T) {
	w := newWorld(t)
	w.admin.expect("POST", "/api/types", `{"name":"page","body":true,"fields":[]}`, http.StatusCreated)
	w.admin.expect("PUT", "/api/spaces/PLAT/roles/"+w.ids["carla"], `{"role":"editor"}`, http.StatusNoContent)
	page := w.bert.expect("POST", "/api/items", `{"space":"PLAT","type":"page","title":"Salaries 2027"}`, http.StatusCreated)
	key := field(page, "item", "key").(string)
	w.bert.expect("PUT", "/api/items/"+key+"/restriction", `{"read":["`+w.ids["bert"]+`","`+w.ids["carla"]+`"],"edit":["`+w.ids["bert"]+`"]}`, http.StatusOK)
	before := treeTitles(t, w.admin, "/api/spaces/PLAT/tree")
	w.carla.expect("POST", "/api/items/"+key+"/copy", `{"space":"PLAT","title":"Salaries (copy)"}`, http.StatusBadRequest)
	if after := treeTitles(t, w.admin, "/api/spaces/PLAT/tree"); !slices.Equal(before, after) {
		t.Fatalf("a refused copy left %v behind", after)
	}

	parent := w.bert.expect("POST", "/api/items", `{"space":"PLAT","type":"page","title":"HR"}`, http.StatusCreated)
	parentKey, parentID := field(parent, "item", "key").(string), field(parent, "item", "id").(string)
	child := w.bert.expect("POST", "/api/items", `{"space":"PLAT","type":"page","title":"Reviews","parent":"`+parentID+`"}`, http.StatusCreated)
	childKey := field(child, "item", "key").(string)
	w.bert.expect("PUT", "/api/items/"+parentKey+"/restriction", `{"read":["`+w.ids["bert"]+`"]}`, http.StatusOK)
	w.bert.expect("POST", "/api/items/"+childKey+"/copy", `{"space":"PLAT","title":"Reviews (copy)"}`, http.StatusBadRequest)
	w.bert.expect("POST", "/api/items/"+childKey+"/copy", `{"space":"PLAT","parent":"`+parentID+`","title":"Reviews (copy)"}`, http.StatusCreated)
	if got := treeTitles(t, w.vera, "/api/spaces/PLAT/tree"); slices.Contains(got, "Reviews (copy)") {
		t.Fatalf("vera reads the copy: %v", got)
	}
}

// TestItemTellsWhichRemovalsAreAllowed: the item names the removals the
// server allows, so the UI offers no button it refuses.
func TestItemTellsWhichRemovalsAreAllowed(t *testing.T) {
	w := newWorld(t)
	w.admin.expect("POST", "/api/types", `{"name":"page","body":true,"fields":[]}`, http.StatusCreated)
	w.admin.expect("PUT", "/api/spaces/PLAT/roles/"+w.ids["carla"], `{"role":"editor"}`, http.StatusNoContent)
	page := w.bert.expect("POST", "/api/items", `{"space":"PLAT","type":"page","title":"Handbook"}`, http.StatusCreated)
	key, id := field(page, "item", "key").(string), field(page, "item", "id").(string)
	may := func(c *client, what string) any {
		return field(c.expect("GET", "/api/items/"+key, "", http.StatusOK), "may", what)
	}
	if may(w.bert, "trash") != true {
		t.Fatal("bert may trash his page")
	}
	hidden := w.carla.expect("POST", "/api/items", `{"space":"PLAT","type":"page","title":"Private","parent":"`+id+`"}`, http.StatusCreated)
	w.carla.expect("PUT", "/api/items/"+field(hidden, "item", "key").(string)+"/restriction", `{"read":["`+w.ids["carla"]+`"]}`, http.StatusOK)
	if may(w.bert, "trash") != false {
		t.Fatal("bert is offered to trash a page holding one he cannot see")
	}
	w.admin.expect("POST", "/api/items/"+key+"/trash", "", http.StatusOK)
	if may(w.bert, "restore") != false || may(w.bert, "purge") != false || may(w.admin, "restore") != true || may(w.admin, "purge") != true {
		t.Fatal("restore and purge are offered to the wrong people")
	}
}
