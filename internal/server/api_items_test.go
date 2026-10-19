package server

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/dbuchs7/wikiwikibangbang/internal/budget"
	"github.com/dbuchs7/wikiwikibangbang/internal/item"
	"github.com/dbuchs7/wikiwikibangbang/internal/storage"
)

// world sets up an administrator, a task type, the space PLAT and the users
// bert (editor), vera (viewer) and carla (no role), each logged in.
type world struct {
	admin, bert, vera, carla *client
	ids                      map[string]string
	db                       *storage.DB
	deps                     Deps
}

func newWorld(t *testing.T) world {
	t.Helper()
	h, d := setupAuth(t)
	w := world{admin: &client{t: t, h: h}, ids: map[string]string{}, db: d.DB, deps: d}
	w.admin.login("admin", adminPassword)
	w.admin.expect("POST", "/api/types", `{"name":"task","body":true,"fields":[
		{"name":"priority","kind":"enum","required":true,"values":["low","high"]},
		{"name":"estimate","kind":"number"},{"name":"due","kind":"date"}],
		"workflow":{"initial":"open","states":["open","done"],"transitions":[{"name":"finish","from":["open"],"to":"done"}]}}`, http.StatusCreated)
	w.admin.expect("POST", "/api/spaces", `{"key":"PLAT","name":"Platform"}`, http.StatusCreated)
	for name, role := range map[string]string{"bert": "editor", "vera": "viewer", "carla": ""} {
		u := w.admin.expect("POST", "/api/users", `{"username":"`+name+`","password":"`+name+`'s password"}`, http.StatusCreated)
		id := u["user"].(map[string]any)["id"].(string)
		w.ids[name] = id
		if role != "" {
			w.admin.expect("PUT", "/api/spaces/PLAT/roles/"+id, `{"role":"`+role+`"}`, http.StatusNoContent)
		}
		c := &client{t: t, h: h}
		c.login(name, name+"'s password")
		switch name {
		case "bert":
			w.bert = c
		case "vera":
			w.vera = c
		case "carla":
			w.carla = c
		}
	}
	return w
}

func field(m map[string]any, path ...string) any {
	var v any = m
	for _, p := range path {
		v = v.(map[string]any)[p]
	}
	return v
}

func TestItemLifecycleThroughTheAPI(t *testing.T) {
	w := newWorld(t)
	b := w.bert

	created := b.expect("POST", "/api/items", `{"space":"PLAT","type":"task","title":"Patch the kernel",
		"properties":{"priority":"high","estimate":3,"due":"2026-12-01"}}`, http.StatusCreated)
	id := field(created, "item", "id").(string)
	if key := field(created, "item", "key"); key != "PLAT-1" {
		t.Fatalf("key %v", key)
	}
	if st := field(created, "item", "state"); st != "open" {
		t.Fatalf("state %v", st)
	}
	if due := field(created, "item", "properties", "due"); due != "2026-12-01T00:00:00Z" {
		t.Fatalf("due %v", due)
	}

	byKey := b.expect("GET", "/api/items/PLAT-1", "", http.StatusOK)
	if field(byKey, "item", "id") != id {
		t.Fatal("key and ID resolve to different items")
	}
	b.expect("PUT", "/api/items/"+id+"/properties/estimate", `{"value":2.5}`, http.StatusOK)
	b.expect("PUT", "/api/items/"+id+"/properties/due", `{"value":null}`, http.StatusOK)
	b.expect("PUT", "/api/items/PLAT-1/title", `{"title":"Patch all kernels"}`, http.StatusOK)
	done := b.expect("POST", "/api/items/PLAT-1/transitions", `{"name":"finish"}`, http.StatusOK)
	if field(done, "item", "state") != "done" || field(done, "item", "properties", "estimate") != 2.5 {
		t.Fatalf("after changes: %v", done)
	}
	if _, has := field(done, "item", "properties").(map[string]any)["due"]; has {
		t.Fatal("due was not unset")
	}

	page := b.expect("POST", "/api/items", `{"space":"PLAT","type":"task","title":"Runbook","properties":{"priority":"low"}}`, http.StatusCreated)
	pid := field(page, "item", "id").(string)
	b.expect("PUT", "/api/items/"+pid+"/body", `{"blocks":[{"kind":"heading","text":"Steps"},{"kind":"paragraph","text":"see","mentions":["`+id+`"]}]}`, http.StatusOK)
	rels := b.expect("GET", "/api/items/"+id+"/relations?direction=incoming", "", http.StatusOK)
	if n := len(rels["relations"].([]any)); n != 1 {
		t.Fatalf("%d incoming relations, want the derived mention", n)
	}
	b.expect("POST", "/api/items/"+pid+"/embedded", `{"space":"PLAT","type":"task","title":"x"}`, http.StatusBadRequest)
	b.expect("POST", "/api/items/"+pid+"/embedded", `{"space":`, http.StatusBadRequest)
	b.expect("POST", "/api/items/"+pid+"/embedded", `{"space":"PLAT","type":"task","title":"x","properties":{"priority":"low","due":"x"}}`, http.StatusBadRequest)
	sub := b.expect("POST", "/api/items/"+pid+"/embedded", `{"space":"PLAT","type":"task","title":"Inline","properties":{"priority":"low"}}`, http.StatusCreated)
	emb := b.expect("GET", "/api/items/"+pid+"/embeds", "", http.StatusOK)
	if e := emb["embeds"].([]any); len(e) != 1 || e[0].(map[string]any)["ref"] != field(sub, "item", "id") {
		t.Fatalf("embeds %v", emb)
	}

	rel := b.expect("POST", "/api/relations", `{"type":"blocks","from":"`+id+`","to":"`+pid+`"}`, http.StatusCreated)
	b.expect("DELETE", "/api/relations/"+field(rel, "relation", "id").(string), "", http.StatusNoContent)

	hist := b.expect("GET", "/api/items/"+id+"/history", "", http.StatusOK)
	events := hist["events"].([]any)
	if len(events) < 5 || events[0].(map[string]any)["actor"] != w.ids["bert"] {
		t.Fatalf("history %v", events)
	}

	// The item is still referenced by the runbook's mention: no trash (ADR-0021).
	b.expect("POST", "/api/items/"+id+"/trash", "", http.StatusConflict)
	b.expect("PUT", "/api/items/"+pid+"/body", `{"blocks":[]}`, http.StatusOK)
	b.expect("POST", "/api/items/"+id+"/trash", "", http.StatusOK)
	b.expect("POST", "/api/items/"+id+"/restore", "", http.StatusOK)
	b.expect("POST", "/api/items/"+id+"/trash", "", http.StatusOK)
	b.expect("POST", "/api/items/"+id+"/purge", "", http.StatusForbidden) // purge is for space administrators
	gone := w.admin.expect("POST", "/api/items/"+id+"/purge", "", http.StatusOK)
	if field(gone, "item", "presence") != "purged" || field(gone, "item", "title") != "" {
		t.Fatalf("purged item %v", gone)
	}
	for _, e := range b.expect("GET", "/api/items/"+id+"/history", "", http.StatusOK)["events"].([]any) {
		if strings.Contains(fmt.Sprint(e), "kernel") {
			t.Fatal("the history shows purged content")
		}
	}
}

func TestPermissionsThroughTheAPI(t *testing.T) {
	w := newWorld(t)
	it := w.bert.expect("POST", "/api/items", `{"space":"PLAT","type":"task","title":"secret","properties":{"priority":"low"}}`, http.StatusCreated)
	id := field(it, "item", "id").(string)

	w.vera.expect("GET", "/api/items/"+id, "", http.StatusOK)
	w.vera.expect("PUT", "/api/items/"+id+"/title", `{"title":"x"}`, http.StatusForbidden)
	w.vera.expect("POST", "/api/items", `{"space":"PLAT","type":"task","title":"x","properties":{"priority":"low"}}`, http.StatusForbidden)

	// Without a role the item does not exist, as far as carla can tell (D10).
	w.carla.expect("GET", "/api/items/"+id, "", http.StatusNotFound)
	w.carla.expect("GET", "/api/items/PLAT-1", "", http.StatusNotFound)
	w.carla.expect("PUT", "/api/items/"+id+"/title", `{"title":"x"}`, http.StatusNotFound)
	if got := w.carla.expect("GET", "/api/items?space=PLAT", "", http.StatusOK); len(got["items"].([]any)) != 0 {
		t.Fatal("carla lists an item she may not read")
	}
	if got := w.carla.expect("GET", "/api/spaces", "", http.StatusOK); len(got["spaces"].([]any)) != 0 {
		t.Fatal("carla lists a space without a role")
	}

	// A restriction narrows below the space role.
	w.bert.expect("PUT", "/api/items/"+id+"/restriction", `{"read":["`+w.ids["bert"]+`"],"edit":[]}`, http.StatusOK)
	w.vera.expect("GET", "/api/items/"+id, "", http.StatusNotFound)

	for _, c := range []*client{w.bert, w.vera} {
		c.expect("POST", "/api/types", `{"name":"x","fields":[]}`, http.StatusForbidden)
		c.expect("POST", "/api/spaces", `{"key":"X","name":"x"}`, http.StatusForbidden)
		c.expect("PUT", "/api/groups/ops/members/"+w.ids["carla"], "", http.StatusForbidden)
	}
	w.bert.expect("PUT", "/api/spaces/PLAT/roles/"+w.ids["carla"], `{"role":"viewer"}`, http.StatusForbidden)
	w.admin.expect("PUT", "/api/groups/ops/members/"+w.ids["carla"], "", http.StatusNoContent)
	w.admin.expect("PUT", "/api/spaces/PLAT/roles/@ops", `{"role":"viewer"}`, http.StatusNoContent)
	if got := w.carla.expect("GET", "/api/spaces", "", http.StatusOK); len(got["spaces"].([]any)) != 1 {
		t.Fatal("a group role did not reach its member")
	}

	// Redaction needs the erasure role, which only administrators grant.
	w.bert.expect("POST", "/api/items/"+id+"/redact", `{"reason":"personal data"}`, http.StatusForbidden)
	w.bert.expect("PUT", "/api/system-roles/erasure/"+w.ids["bert"], "", http.StatusForbidden)
	w.admin.expect("PUT", "/api/system-roles/owner/"+w.ids["bert"], "", http.StatusBadRequest)
	w.admin.expect("PUT", "/api/system-roles/erasure/"+w.ids["bert"], "", http.StatusNoContent)
	w.bert.expect("POST", "/api/items/"+id+"/redact", `{"reason":""}`, http.StatusBadRequest)
	w.bert.expect("POST", "/api/items/"+id+"/redact", `{"reason":`, http.StatusBadRequest)
	red := w.bert.expect("POST", "/api/items/"+id+"/redact", `{"reason":"personal data"}`, http.StatusOK)
	if field(red, "item", "presence") != "purged" || field(red, "item", "title") != "" {
		t.Fatalf("redacted: %v", red)
	}
}

func TestListingIsPaged(t *testing.T) {
	w := newWorld(t)
	for range 3 {
		w.bert.expect("POST", "/api/items", `{"space":"PLAT","type":"task","title":"t","properties":{"priority":"low"}}`, http.StatusCreated)
	}
	var keys []string
	after := ""
	for range 4 {
		page := w.bert.expect("GET", "/api/items?space=PLAT&limit=2"+after, "", http.StatusOK)
		if field(page, "total", "value") != 3.0 || field(page, "total", "exact") != true {
			t.Fatalf("total %v", page["total"])
		}
		for _, it := range page["items"].([]any) {
			keys = append(keys, it.(map[string]any)["key"].(string))
		}
		next, ok := page["next"].(string)
		if !ok {
			break
		}
		after = "&after=" + next
	}
	if strings.Join(keys, ",") != "PLAT-1,PLAT-2,PLAT-3" {
		t.Fatalf("pages gave %v", keys)
	}
	w.bert.expect("GET", "/api/items?limit=0", "", http.StatusBadRequest)
	w.bert.expect("GET", "/api/items?limit=501", "", http.StatusBadRequest)
	w.bert.expect("GET", "/api/items?after=nope", "", http.StatusBadRequest)
}

func TestTheAPIRefusesBadInput(t *testing.T) {
	w := newWorld(t)
	b := w.bert
	cases := []struct {
		method, path, body string
		status             int
	}{
		{"POST", "/api/items", `{"space":"PLAT","type":"task","title":"x"}`, http.StatusBadRequest},                                            // required priority
		{"POST", "/api/items", `{"space":"PLAT","type":"task","title":"x","properties":{"priority":"mid"}}`, http.StatusBadRequest},            // enum
		{"POST", "/api/items", `{"space":"PLAT","type":"task","title":"x","properties":{"priority":"low","colour":1}}`, http.StatusBadRequest}, // unknown field
		{"POST", "/api/items", `{"space":"PLAT","type":"task","title":"x","properties":{"priority":"low","estimate":"3"}}`, http.StatusBadRequest},
		{"POST", "/api/items", `{"space":"PLAT","type":"task","title":"x","properties":{"priority":"low","due":"tomorrow"}}`, http.StatusBadRequest},
		{"POST", "/api/items", `{"space":"PLAT","type":"task","title":"x","properties":{"priority":"low","due":5}}`, http.StatusBadRequest},
		{"POST", "/api/items", `{"space":"PLAT","type":"task","title":"x","properties":{"priority":"low","estimate":1e999}}`, http.StatusBadRequest},
		{"POST", "/api/items", `{"space":"PLAT","type":"nope","title":"x"}`, http.StatusNotFound},
		{"POST", "/api/items", `{"space":"NOPE","type":"task","title":"x"}`, http.StatusForbidden}, // no role there, existing or not
		{"POST", "/api/items", `{"space":"PLAT"`, http.StatusBadRequest},
		{"GET", "/api/items/nope", "", http.StatusNotFound},
		{"PUT", "/api/items/nope/title", `{"title":"x"}`, http.StatusNotFound},
		{"PUT", "/api/items/nope/title", `{"title":`, http.StatusBadRequest},
		{"PUT", "/api/items/nope/body", `{"blocks":[{"kind":"unsupported","text":"<script>"}]}`, http.StatusNotFound}, // the item first
		{"PUT", "/api/items/nope/body", `{"blocks":[{"kind":"paragraph","marks":[{"type":"bold","start":"x"}]}]}`, http.StatusBadRequest},
		{"PUT", "/api/spaces/PLAT/roles/x", `{"role":"owner"}`, http.StatusBadRequest},
		{"GET", "/api/items/nope/relations?direction=sideways", "", http.StatusBadRequest},
		{"GET", "/api/items/nope/relations", "", http.StatusNotFound},
		{"GET", "/api/items/nope/history", "", http.StatusNotFound},
		{"GET", "/api/items/nope/embeds", "", http.StatusNotFound},
		{"POST", "/api/items/nope/embedded", `{"space":"PLAT","type":"task","title":"x"}`, http.StatusNotFound},
		{"POST", "/api/relations", `{"type":"blocks","from":"a","to":"b"}`, http.StatusNotFound},
		{"DELETE", "/api/relations/nope", "", http.StatusNotFound},
	}
	for _, c := range cases {
		b.expect(c.method, c.path, c.body, c.status)
	}
	it := b.expect("POST", "/api/items", `{"space":"PLAT","type":"task","title":"x","properties":{"priority":"low"}}`, http.StatusCreated)
	id := field(it, "item", "id").(string)
	b.expect("PUT", "/api/items/"+id+"/properties/estimate", `{"value":"three"}`, http.StatusBadRequest)
	b.expect("PUT", "/api/items/"+id+"/properties/estimate", `{"value":}`, http.StatusBadRequest)
	b.expect("POST", "/api/items/"+id+"/transitions", `{"name":"fly"}`, http.StatusBadRequest)
	b.expect("POST", "/api/items/"+id+"/move", `{"space":"NOPE"}`, http.StatusForbidden)
	w.admin.expect("POST", "/api/items/"+id+"/purge", "", http.StatusBadRequest) // only from the trash
	w.admin.expect("POST", "/api/types", `{"name":"","fields":[]}`, http.StatusBadRequest)
	w.admin.expect("POST", "/api/spaces", `{"key":"lower","name":"x"}`, http.StatusBadRequest)
	w.admin.expect("POST", "/api/types", `{"name":`, http.StatusBadRequest)
	w.admin.expect("POST", "/api/spaces", `{"key":`, http.StatusBadRequest)
}

func TestTypesAndOpenAPI(t *testing.T) {
	w := newWorld(t)
	types := w.vera.expect("GET", "/api/types", "", http.StatusOK)["types"].([]any)
	if len(types) != 1 || types[0].(map[string]any)["workflow"] == nil {
		t.Fatalf("types %v", types)
	}
	doc := w.vera.expect("GET", "/api/openapi.json", "", http.StatusOK)
	paths := doc["paths"].(map[string]any)
	served := 0
	for _, rt := range Routes(testDeps(t)) {
		if !strings.HasPrefix(rt.Pattern, "/api/") {
			continue
		}
		served++
		ops, ok := paths[rt.Pattern].(map[string]any)
		if !ok || ops[strings.ToLower(rt.Method)] == nil {
			t.Errorf("%s %s is served but not described", rt.Method, rt.Pattern)
		}
	}
	described := 0
	for _, ops := range paths {
		described += len(ops.(map[string]any))
	}
	if described != served {
		t.Errorf("%d operations described, %d served", described, served)
	}
	if login := field(doc, "paths", "/api/login", "post").(map[string]any); login["security"] != nil {
		t.Error("the public login route is described as needing a credential")
	}
}

func TestRichTextThroughTheAPI(t *testing.T) {
	w := newWorld(t)
	b := w.bert
	task := field(b.expect("POST", "/api/items", `{"space":"PLAT","type":"task","title":"Patch","properties":{"priority":"low"}}`, http.StatusCreated), "item", "id").(string)
	page := field(b.expect("POST", "/api/items", `{"space":"PLAT","type":"task","title":"Runbook","properties":{"priority":"low"}}`, http.StatusCreated), "item", "id").(string)
	body := `{"blocks":[
		{"kind":"heading","level":2,"text":"Steps"},
		{"kind":"paragraph","text":"Ask @PLAT-1 or see the guide","marks":[
			{"type":"bold","start":0,"end":3},
			{"type":"mention","start":4,"end":11,"ref":"` + task + `"},
			{"type":"link","start":23,"end":28,"href":"https://example.org/guide"}]},
		{"kind":"bullet","text":"one"},{"kind":"ordered","text":"two"},
		{"kind":"quote","text":"q"},{"kind":"code","text":"go test"}]}`
	got := b.expect("PUT", "/api/items/"+page+"/body", body, http.StatusOK)
	blocks := field(got, "item", "body").([]any)
	p := blocks[1].(map[string]any)
	if len(p["marks"].([]any)) != 3 || p["mentions"].([]any)[0] != task || blocks[0].(map[string]any)["level"] != 2.0 {
		t.Fatalf("body %v", blocks[:2])
	}
	// The mention mark alone relates the page to the task.
	inc := b.expect("GET", "/api/items/"+task+"/relations?direction=incoming", "", http.StatusOK)
	if len(inc["relations"].([]any)) != 1 {
		t.Fatalf("incoming %v", inc)
	}
	b.expect("PUT", "/api/items/"+page+"/body", `{"blocks":[{"kind":"paragraph","text":"x","marks":[{"type":"link","start":0,"end":1,"href":"javascript:alert(1)"}]}]}`, http.StatusBadRequest)
}

// TestImportedUnsupportedBlocksCanBeKeptNotAdded: the editor saves a body
// with the unsupported blocks it was given (ADR-0023, ADR-0032).
func TestImportedUnsupportedBlocksCanBeKeptNotAdded(t *testing.T) {
	w := newWorld(t)
	id := field(w.bert.expect("POST", "/api/items", `{"space":"PLAT","type":"task","title":"x","properties":{"priority":"low"}}`, http.StatusCreated), "item", "id").(string)
	raw := "<table><tr><td>a</td></tr></table>"
	var itemID item.ID = item.ID(id)
	if err := w.db.Write(func(s *item.Store) error {
		return s.CommitBody("import", itemID, item.Doc{Blocks: []item.Block{{Kind: item.Unsupported, Text: raw}}})
	}); err != nil {
		t.Fatal(err)
	}
	keep := `{"blocks":[{"kind":"paragraph","text":"intro"},{"kind":"unsupported","text":` + strconv.Quote(raw) + `}]}`
	w.bert.expect("PUT", "/api/items/"+id+"/body", keep, http.StatusOK)
	w.bert.expect("PUT", "/api/items/"+id+"/body", `{"blocks":[{"kind":"unsupported","text":"<b>new</b>"}]}`, http.StatusBadRequest)
}

// The page budget is read when a request arrives, so a value set from the
// environment at startup bounds every listing (ADR-0015).
func TestPageBudgetBoundsListings(t *testing.T) {
	w := newWorld(t)
	small := budget.Default()
	small.PageItems = 3
	budget.Set(small)
	t.Cleanup(func() { budget.Set(budget.Default()) })
	w.bert.expect("GET", "/api/items?space=PLAT&limit=3", "", http.StatusOK)
	w.bert.expect("GET", "/api/items?space=PLAT&limit=4", "", http.StatusBadRequest)
	w.admin.expect("GET", "/api/audit?limit=4", "", http.StatusBadRequest)
}
