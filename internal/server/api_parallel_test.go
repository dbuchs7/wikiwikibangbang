package server

import (
	"fmt"
	"net/http"
	"sync"
	"testing"
)

// Reads run side by side (ADR-0081): every reading endpoint is asked at
// once from several clients while commands change the store. Run with
// -race, it fails when a read changes the store or a memo unguarded.
func TestReadsRunSideBySide(t *testing.T) {
	w := newWorld(t)
	w.admin.expect("POST", "/api/types", `{"name":"entry","body":true,"fields":[{"name":"owner","kind":"user","recommended":true},
		{"name":"reviewed","kind":"date"},{"name":"assignee","kind":"user"},{"name":"due","kind":"date"}],
		"workflow":{"initial":"active","states":["active","withdrawn"],"transitions":[{"name":"withdraw","from":["active"],"to":"withdrawn"}]},
		"review":{"everyDays":30,"field":"reviewed","owner":"owner"}}`, http.StatusCreated)
	var keys []string
	for i := range 30 {
		it := w.bert.expect("POST", "/api/items", fmt.Sprintf(`{"space":"PLAT","type":"entry","title":"Entry %d","properties":{"owner":%q,"due":"2020-01-0%d"},
			"body":[{"kind":"paragraph","text":"Text %d with IBAN CH93 0076 2011 6238 5295 7"}]}`, i, w.ids["bert"], i%9+1, i), http.StatusCreated)
		keys = append(keys, it["item"].(map[string]any)["key"].(string))
		w.bert.expect("POST", "/api/items/"+keys[i]+"/comments", `{"text":"a comment"}`, http.StatusCreated)
	}
	k := w.bert.expect("POST", "/api/spaces/PLAT/kanbans", `{"name":"Board","type":"entry"}`, http.StatusCreated)
	board := "/api/spaces/PLAT/kanbans/" + k["kanban"].(map[string]any)["id"].(string) + "/board"
	paths := []string{"/api/notifications?limit=1", "/api/notifications", "/api/items?limit=10", "/api/items?type=entry&limit=5",
		"/api/spaces/PLAT/tree", "/api/search?q=Entry&limit=20", "/api/spaces/PLAT/governance", "/api/album?limit=1", "/api/comments",
		"/api/kanbans", board, "/api/items/" + keys[0], "/api/items/" + keys[0] + "/history", "/api/items/" + keys[0] + "/relations",
		"/api/spaces", "/api/types", "/api/me"}
	var wg sync.WaitGroup
	for _, c := range []*client{w.admin, w.bert, w.vera} {
		for range 3 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for range 3 {
					for _, p := range paths {
						if rec := c.do("GET", p, ""); rec.Code >= 500 {
							t.Errorf("%s: %d", p, rec.Code)
						}
					}
				}
			}()
		}
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := range 20 {
			w.bert.do("PUT", "/api/items/"+keys[i]+"/properties/assignee", fmt.Sprintf(`{"value":%q}`, w.ids["bert"]))
			w.bert.do("POST", "/api/items/"+keys[i]+"/comments", `{"text":"IBAN CH93 0076 2011 6238 5295 7"}`)
		}
	}()
	wg.Wait()
}
