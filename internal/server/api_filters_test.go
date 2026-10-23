package server

import (
	"net/http"
	"testing"
)

// Paged lists are filtered by the server, before paging (ADR-0098).
func TestPagedListsAreFilteredByTheServer(t *testing.T) {
	w := newWorld(t)
	it := w.bert.expect("POST", "/api/items", `{"space":"PLAT","type":"task","title":"Patch","properties":{"priority":"low"}}`, http.StatusCreated)
	key := field(it, "item", "key").(string)
	w.vera.expect("PUT", "/api/items/"+key+"/watch", "", http.StatusOK)
	w.bert.expect("PUT", "/api/items/"+key+"/title", `{"title":"Patch the kernel"}`, http.StatusOK)
	w.bert.expect("POST", "/api/items/"+key+"/comments", `{"text":"Ready, @vera?"}`, http.StatusCreated)
	w.bert.expect("POST", "/api/items/"+key+"/comments", `{"text":"Rebooted"}`, http.StatusCreated)

	count := func(c *client, path, list string) int {
		t.Helper()
		return len(c.expect("GET", path, "", http.StatusOK)[list].([]any))
	}
	for path, want := range map[string]int{
		"/api/notifications":                   3,
		"/api/notifications?kind=mentions":     1,
		"/api/notifications?kind=comments":     1,
		"/api/notifications?kind=changes":      1,
		"/api/notifications?q=kernel":          3,
		"/api/notifications?q=bert+KERNEL":     3,
		"/api/notifications?q=nothing":         0,
		"/api/notifications?unread=1&q=kernel": 3,
	} {
		if got := count(w.vera, path, "notifications"); got != want {
			t.Errorf("%s: %d, want %d", path, got, want)
		}
	}
	w.vera.expect("GET", "/api/notifications?kind=all", "", http.StatusBadRequest)
	page := w.vera.expect("GET", "/api/notifications?kind=comments&limit=1", "", http.StatusOK)
	if page["next"] != nil {
		t.Errorf("one comment fills no second page: %v", page)
	}

	for path, want := range map[string]int{
		"/api/comments":                 2,
		"/api/comments?q=reboot":        1,
		"/api/comments?q=patch+bert":    2,
		"/api/comments?space=PLAT":      2,
		"/api/comments?space=ELSEWHERE": 0,
	} {
		if got := count(w.bert, path, "comments"); got != want {
			t.Errorf("%s: %d, want %d", path, got, want)
		}
	}

	for path, want := range map[string]int{
		"/api/items?space=PLAT":                  1,
		"/api/items?space=PLAT&q=KERNEL+patch":   1,
		"/api/items?space=PLAT&q=kernel+nowhere": 0,
	} {
		if got := count(w.bert, path, "items"); got != want {
			t.Errorf("%s: %d, want %d", path, got, want)
		}
	}

	for path, want := range map[string]bool{
		"/api/audit?event=login":            true,
		"/api/audit?event=no.such.event":    false,
		"/api/audit?q=bert":                 true,
		"/api/audit?q=zzzz":                 false,
		"/api/audit?actor=" + w.ids["bert"]: true,
	} {
		if got := count(w.admin, path, "entries") > 0; got != want {
			t.Errorf("%s: entries %v, want %v", path, got, want)
		}
	}
	w.bert.expect("GET", "/api/audit?q=bert", "", http.StatusForbidden)
}
