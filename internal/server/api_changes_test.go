package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dbuchs7/wikiwikibangbang/internal/budget"
)

// readToken gives a client of c's account that holds a token of scope read.
func readToken(c *client) *client {
	tok := c.expect("POST", "/api/tokens", `{"name":"catalogue","scope":"read","days":1}`, http.StatusCreated)
	return &client{t: c.t, h: c.h, bearer: tok["secret"].(string)}
}

func changesOf(t *testing.T, res map[string]any) ([]map[string]any, int) {
	t.Helper()
	list, ok := res["changes"].([]any)
	if !ok {
		t.Fatalf("no changes array: %v", res)
	}
	out := []map[string]any{}
	for _, c := range list {
		out = append(out, c.(map[string]any))
	}
	return out, int(res["last"].(float64))
}

func TestOwnRolesThroughTheAPI(t *testing.T) {
	w := newWorld(t)
	reader := readToken(w.vera)
	got := reader.expect("GET", "/api/me/roles", "", http.StatusOK)
	if fmt.Sprint(got) != "map[spaces:[map[role:viewer space:PLAT via:]] system:[]]" {
		t.Fatalf("vera's roles: %v", got)
	}
	// Through a group, one entry per grant, and the system roles.
	w.admin.expect("PUT", "/api/groups/readers/members/"+w.ids["vera"], "", http.StatusNoContent)
	w.admin.expect("PUT", "/api/spaces/PLAT/roles/@readers", `{"role":"editor"}`, http.StatusNoContent)
	w.admin.expect("PUT", "/api/system-roles/erasure/"+w.ids["vera"], "", http.StatusNoContent)
	got = reader.expect("GET", "/api/me/roles", "", http.StatusOK)
	if fmt.Sprint(got) != "map[spaces:[map[role:viewer space:PLAT via:] map[role:editor space:PLAT via:@readers]] system:[erasure]]" {
		t.Fatalf("vera's roles: %v", got)
	}
	// Nothing about anyone else; empty arrays, never null.
	rec := w.carla.do("GET", "/api/me/roles", "")
	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != `{"spaces":[],"system":[]}` {
		t.Fatalf("carla: %d %s", rec.Code, rec.Body)
	}
	admin := w.admin.expect("GET", "/api/me/roles", "", http.StatusOK)
	if fmt.Sprint(admin["system"]) != "[admin]" || strings.Contains(fmt.Sprint(admin), w.ids["vera"]) {
		t.Fatalf("admin: %v", admin)
	}
}

func TestReadTokensWatchButChangeNothingElse(t *testing.T) {
	w := newWorld(t)
	id := field(w.bert.expect("POST", "/api/items", `{"space":"PLAT","type":"task","title":"Backup service","properties":{"priority":"low"}}`, http.StatusCreated), "item", "id").(string)
	reader := readToken(w.vera)
	// The one exception to "read allows only GET and HEAD" (ADR-0121).
	reader.expect("PUT", "/api/items/"+id+"/watch", "", http.StatusOK)
	reader.expect("PUT", "/api/spaces/PLAT/watch", "", http.StatusOK)
	if n := len(reader.expect("GET", "/api/me/watches", "", http.StatusOK)["watches"].([]any)); n != 2 {
		t.Fatalf("%d watches", n)
	}
	reader.expect("DELETE", "/api/spaces/PLAT/watch", "", http.StatusOK)
	// Everything else stays refused.
	reader.expect("PUT", "/api/items/"+id+"/title", `{"title":"x"}`, http.StatusForbidden)
	reader.expect("POST", "/api/items/"+id+"/comments", `{"text":"x"}`, http.StatusForbidden)
	reader.expect("POST", "/api/notifications/seen", `{"seq":1}`, http.StatusForbidden)
	reader.expect("POST", "/api/tokens", `{"name":"y","scope":"full","days":1}`, http.StatusForbidden)
	reader.expect("PUT", "/api/me/language", `{"language":"de"}`, http.StatusForbidden)

	// The description says which routes read tokens reach although they
	// are no GET.
	doc := w.vera.expect("GET", "/api/openapi.json", "", http.StatusOK)
	for _, op := range []string{"put", "delete"} {
		for _, p := range []string{"/api/items/{ref}/watch", "/api/spaces/{space}/watch"} {
			if field(doc, "paths", p, op).(map[string]any)["x-read-scope"] != true {
				t.Errorf("%s %s is not marked for read tokens", op, p)
			}
		}
	}
	if _, marked := field(doc, "paths", "/api/items/{ref}/title", "put").(map[string]any)["x-read-scope"]; marked {
		t.Error("a change is marked for read tokens")
	}
	for _, p := range []string{"/api/changes", "/api/me/roles"} {
		if field(doc, "paths", p, "get") == nil {
			t.Errorf("%s is not described", p)
		}
	}
}

func TestChangeFeedThroughTheAPI(t *testing.T) {
	w := newWorld(t)
	page := w.bert.expect("POST", "/api/items", `{"space":"PLAT","type":"task","title":"Backup service","properties":{"priority":"low"}}`, http.StatusCreated)
	id, key := field(page, "item", "id").(string), field(page, "item", "key").(string)
	reader := readToken(w.vera)
	reader.expect("PUT", "/api/items/"+id+"/watch", "", http.StatusOK)

	// Without after: no changes, and the newest event to start from.
	changes, head := changesOf(t, reader.expect("GET", "/api/changes", "", http.StatusOK))
	if len(changes) != 0 || head == 0 {
		t.Fatalf("start: %v %d", changes, head)
	}
	w.bert.expect("PUT", "/api/items/"+id+"/title", `{"title":"Backup service v2"}`, http.StatusOK)
	w.bert.expect("PUT", "/api/items/"+id+"/labels", `{"labels":["shop"]}`, http.StatusOK)
	changes, last := changesOf(t, reader.expect("GET", fmt.Sprintf("/api/changes?after=%d", head), "", http.StatusOK))
	if len(changes) != 2 || changes[0]["item"] != id || changes[0]["seq"].(float64) <= float64(head) || last <= head {
		t.Fatalf("changes %v, last %d", changes, last)
	}
	at, err := time.Parse(time.RFC3339, changes[0]["at"].(string))
	if err != nil || at.Location() != time.UTC {
		t.Fatalf("at %v: %v", changes[0]["at"], err)
	}
	page1, cut := changesOf(t, reader.expect("GET", fmt.Sprintf("/api/changes?after=%d&limit=1", head), "", http.StatusOK))
	if len(page1) != 1 || cut != int(page1[0]["seq"].(float64)) {
		t.Fatalf("a page cut by limit: %v %d", page1, cut)
	}

	// A restriction hides the item: the feed tells that something changed,
	// never which item.
	w.bert.expect("PUT", "/api/items/"+id+"/restriction", `{"read":["`+w.ids["bert"]+`"]}`, http.StatusOK)
	w.bert.expect("PUT", "/api/items/"+id+"/title", `{"title":"Confidential name"}`, http.StatusOK)
	rec := reader.do("GET", fmt.Sprintf("/api/changes?after=%d", last), "")
	var res map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	changes, last = changesOf(t, res)
	if len(changes) != 2 || changes[0]["item"] != "" || changes[1]["item"] != "" {
		t.Fatalf("changes of a hidden item: %v", changes)
	}
	for _, leak := range []string{id, key, "Backup", "Confidential"} {
		if strings.Contains(rec.Body.String(), leak) {
			t.Fatalf("the feed reveals %q: %s", leak, rec.Body)
		}
	}
	// The watch of the hidden item ends by its ID; its key resolves to
	// nothing.
	reader.expect("DELETE", "/api/items/"+key+"/watch", "", http.StatusNotFound)
	reader.expect("DELETE", "/api/items/"+id+"/watch", "", http.StatusOK)
	reader.expect("PUT", "/api/items/"+id+"/watch", "", http.StatusNotFound)
	w.carla.expect("DELETE", "/api/items/"+id+"/watch", "", http.StatusNotFound)
	w.bert.expect("PUT", "/api/items/"+id+"/title", `{"title":"After the watch"}`, http.StatusOK)
	if changes, _ := changesOf(t, reader.expect("GET", fmt.Sprintf("/api/changes?after=%d", last), "", http.StatusOK)); len(changes) != 0 {
		t.Fatalf("changes after the watch ended: %v", changes)
	}

	for _, q := range []string{"after=-1", "after=x", "limit=0", fmt.Sprintf("limit=%d", budget.Get().ChangesPage+1),
		"wait=-1", fmt.Sprintf("wait=%d", budget.Get().ChangesWait+1)} {
		reader.expect("GET", "/api/changes?"+q, "", http.StatusBadRequest)
	}
	w.carla.expect("GET", "/api/changes?after=0", "", http.StatusOK) // no watches: nothing to tell
}

func TestChangeFeedWaitsForAChange(t *testing.T) {
	w := newWorld(t)
	id := field(w.bert.expect("POST", "/api/items", `{"space":"PLAT","type":"task","title":"Backup service","properties":{"priority":"low"}}`, http.StatusCreated), "item", "id").(string)
	reader := readToken(w.vera)
	reader.expect("PUT", "/api/items/"+id+"/watch", "", http.StatusOK)
	_, head := changesOf(t, reader.expect("GET", "/api/changes", "", http.StatusOK))

	// Held until the change arrives, not until the wait ends.
	done := make(chan map[string]any)
	begun := time.Now()
	go func() {
		done <- reader.expect("GET", fmt.Sprintf("/api/changes?after=%d&wait=20", head), "", http.StatusOK)
	}()
	w.bert.expect("PUT", "/api/items/PLAT-1/properties/priority", `{"value":"high"}`, http.StatusOK)
	changes, _ := changesOf(t, <-done)
	if len(changes) != 1 || changes[0]["item"] != id || time.Since(begun) > 15*time.Second {
		t.Fatalf("long poll: %v after %v", changes, time.Since(begun))
	}

	// A change of something else wakes the wait, which goes on; the wait
	// ends empty, with the cursor moved on.
	_, head = changesOf(t, reader.expect("GET", "/api/changes", "", http.StatusOK))
	go func() {
		done <- reader.expect("GET", fmt.Sprintf("/api/changes?after=%d&wait=1", head), "", http.StatusOK)
	}()
	w.admin.expect("POST", "/api/spaces", `{"key":"OTHER","name":"Other"}`, http.StatusCreated)
	changes, last := changesOf(t, <-done)
	if len(changes) != 0 || last <= head {
		t.Fatalf("after the wait: %v %d (head %d)", changes, last, head)
	}

	// A cancelled request ends at once.
	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequestWithContext(ctx, "GET", fmt.Sprintf("/api/changes?after=%d&wait=20", last), nil)
	req.Header.Set("Authorization", "Bearer "+reader.bearer)
	rec := httptest.NewRecorder()
	ended := make(chan struct{})
	go func() { reader.h.ServeHTTP(rec, req); close(ended) }()
	cancel()
	select {
	case <-ended:
	case <-time.After(10 * time.Second):
		t.Fatal("a cancelled request goes on waiting")
	}

	// Beyond the budget of waiting requests, a request is answered at once.
	b := budget.Default()
	b.ChangesWaiting = 0
	budget.Set(b)
	t.Cleanup(func() { budget.Set(budget.Default()) })
	begun = time.Now()
	reader.expect("GET", fmt.Sprintf("/api/changes?after=%d&wait=20", last), "", http.StatusOK)
	if time.Since(begun) > 10*time.Second {
		t.Fatal("waited beyond the budget")
	}
}
