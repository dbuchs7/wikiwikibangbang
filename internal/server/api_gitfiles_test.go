package server

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// gitea answers as a Gitea instance holding o/r with one Markdown file.
func gitea(t *testing.T) *httptest.Server {
	t.Helper()
	commit := strings.Repeat("1", 40)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		const api = "/api/v1/repos/o/r"
		switch sha := r.URL.Query().Get("sha"); {
		case r.URL.Path == api+"/commits" && (sha == "main" || sha == commit):
			fmt.Fprintf(w, `[{"sha":%q}]`, commit)
		case r.URL.Path == api+"/commits":
			fmt.Fprint(w, `[]`)
		case r.URL.Path == api+"/raw/docs/a.md":
			fmt.Fprint(w, "# Page A\n\nFrom the *repository*.")
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// A Markdown file in a text becomes a page below the item, which its
// editors update or pin (ADR-0138).
func TestMarkdownFilesThroughTheAPI(t *testing.T) {
	t.Setenv("WWBB_CONNECTOR_ALLOW_PRIVATE", "on") // the fake host is on the loopback address
	t.Setenv("WWBB_GIT_FILES", "on")
	srv := gitea(t)
	w := newWorld(t)
	w.admin.expect("POST", "/api/types", `{"name":"page","body":true}`, http.StatusCreated)
	host := w.bert.expect("POST", "/api/items", `{"space":"PLAT","type":"page","title":"Host"}`, http.StatusCreated)
	key := field(host, "item", "key").(string)
	addr := srv.URL + "/o/r/src/branch/main/docs/a.md"
	path := "/api/items/" + key + "/markdown"

	w.vera.expect("POST", path, `{"url":"`+addr+`"}`, http.StatusForbidden)
	w.carla.expect("POST", path, `{"url":"`+addr+`"}`, http.StatusNotFound)
	w.bert.expect("POST", path, `{"url":"`+addr+`","version":"latest"}`, http.StatusBadRequest)
	w.bert.expect("POST", path, `{`, http.StatusBadRequest)
	w.bert.expect("POST", path, `{"url":"`+srv.URL+`/o/r/src/branch/main/docs"}`, http.StatusBadRequest)
	w.bert.expect("POST", path, `{"url":"`+srv.URL+`/o/r/src/branch/main/docs/b.md"}`, http.StatusBadRequest)
	w.bert.expect("POST", path, `{"url":"http://127.0.0.1:1/o/r/src/branch/main/a.md"}`, http.StatusBadGateway)

	made := w.bert.expect("POST", path, `{"url":"`+addr+`","version":"current"}`, http.StatusCreated)
	page := made["item"].(map[string]any)
	if page["title"] != "Page A" || field(page, "origin", "system") != "git:file" || field(page, "origin", "url") != addr ||
		field(page, "origin", "version") != strings.Repeat("1", 40) || page["origin"].(map[string]any)["pinned"] != nil {
		t.Fatalf("made %s", jsonOf(page))
	}
	if !strings.Contains(jsonOf(page["body"]), "From the repository.") || strings.Contains(jsonOf(page), "# Page A") {
		t.Fatalf("the page's text, without its raw Markdown: %s", jsonOf(page))
	}
	pageKey := page["key"].(string)
	updated := w.bert.expect("POST", "/api/items/"+pageKey+"/markdown/update", ``, http.StatusOK)
	if updated["changed"] != false {
		t.Errorf("an unchanged file %s", jsonOf(updated))
	}
	w.vera.expect("POST", "/api/items/"+pageKey+"/markdown/update", ``, http.StatusForbidden)
	pinned := w.bert.expect("PUT", "/api/items/"+pageKey+"/markdown", `{"version":"this"}`, http.StatusOK)
	if field(pinned, "item", "origin", "pinned") != true || !strings.Contains(field(pinned, "item", "origin", "url").(string), "/src/commit/") {
		t.Errorf("pinned %s", jsonOf(pinned))
	}
	w.bert.expect("PUT", "/api/items/"+key+"/markdown", `{"version":"this"}`, http.StatusBadRequest)
	w.bert.expect("PUT", "/api/items/NOPE-1/markdown", `{"version":"this"}`, http.StatusNotFound)
	w.bert.expect("POST", "/api/items/NOPE-1/markdown/update", ``, http.StatusNotFound)

	t.Setenv("WWBB_GIT_FILES", "")
	w.bert.expect("POST", path, `{"url":"`+addr+`"}`, http.StatusConflict)
}
