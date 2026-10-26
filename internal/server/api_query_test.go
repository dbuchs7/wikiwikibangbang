package server

import (
	"encoding/json"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"
)

// itemKeys lists the keys of a listing's items.
func itemKeys(page map[string]any) []string {
	out := []string{}
	for _, it := range page["items"].([]any) {
		out = append(out, it.(map[string]any)["key"].(string))
	}
	return out
}

// The query language selects and orders items through the listing
// (ADR-0136).
func TestItemsAreSelectedByAQuery(t *testing.T) {
	w := newWorld(t)
	w.admin.expect("POST", "/api/types", `{"name":"page","body":true,"fields":[{"name":"owner","kind":"user"}]}`, http.StatusCreated)
	b := w.bert
	b.expect("POST", "/api/items", `{"space":"PLAT","type":"task","title":"Patch the kernel","properties":{"priority":"high","estimate":3,"due":"2026-12-01"}}`, http.StatusCreated)
	b.expect("POST", "/api/items", `{"space":"PLAT","type":"task","title":"Rotate logs","properties":{"priority":"low","estimate":1}}`, http.StatusCreated)
	b.expect("POST", "/api/items", `{"space":"PLAT","type":"task","title":"Replace disks","properties":{"priority":"high","estimate":8}}`, http.StatusCreated)
	b.expect("POST", "/api/items/PLAT-3/transitions", `{"name":"finish"}`, http.StatusOK)
	b.expect("POST", "/api/items", `{"space":"PLAT","type":"page","title":"Runbook","properties":{"owner":"`+w.ids["bert"]+`"}}`, http.StatusCreated)

	list := func(c *client, query, extra string) []string {
		t.Helper()
		return itemKeys(c.expect("GET", "/api/items?query="+url.QueryEscape(query)+extra, "", http.StatusOK))
	}
	for _, c := range []struct {
		who          *client
		query, extra string
		want         []string
	}{
		{b, "priority = high order by estimate desc", "", []string{"PLAT-3", "PLAT-1"}},
		{b, "priority = high", "&state=open", []string{"PLAT-1"}},
		{b, "due < 2027-01-01 or estimate <= 1", "", []string{"PLAT-1", "PLAT-2"}},
		{b, "type = task and state != done order by key desc", "", []string{"PLAT-2", "PLAT-1"}},
		{b, "", "&type=page", []string{"PLAT-4"}},
		// A name in a user condition is the account's, in any case; the
		// caller is currentUser().
		{b, "owner = BERT", "", []string{"PLAT-4"}},
		{b, "owner = currentUser()", "", []string{"PLAT-4"}},
		{w.vera, "owner = currentUser()", "", []string{}},
		{w.vera, "owner = bert", "", []string{"PLAT-4"}},
		{w.vera, "owner = nobody-here", "", []string{}},
		{w.vera, "title ~ kernel", "", []string{"PLAT-1"}},
		// Who holds no role finds nothing, as with every listing (D10).
		{w.carla, "", "", []string{}},
		{w.carla, "key = PLAT-1", "", []string{}},
	} {
		if got := list(c.who, c.query, c.extra); !slices.Equal(got, c.want) {
			t.Errorf("%q%s = %v, want %v", c.query, c.extra, got, c.want)
		}
	}

	// Paged like every listing.
	page := b.expect("GET", "/api/items?limit=1&query="+url.QueryEscape("order by estimate"), "", http.StatusOK)
	if !slices.Equal(itemKeys(page), []string{"PLAT-2"}) || field(page, "total", "value") != float64(4) || page["next"] == nil {
		t.Fatalf("first page: %v", page)
	}
	page = b.expect("GET", "/api/items?limit=1&query="+url.QueryEscape("order by estimate")+"&after="+page["next"].(string), "", http.StatusOK)
	if !slices.Equal(itemKeys(page), []string{"PLAT-1"}) {
		t.Fatalf("second page: %v", page)
	}

	// A mistake is answered with where it is.
	for query, want := range map[string]struct {
		msg string
		at  float64
	}{
		"priorty = high":     {"at character 1: no type has a field priorty", 0},
		"priority = urgent":  {`at character 12: "urgent" is not a value of priority`, 11},
		"state = open and (": {"at character 19: expected a field name", 18},
	} {
		rec := b.do("GET", "/api/items?query="+url.QueryEscape(query), "")
		var got map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &got)
		if rec.Code != http.StatusBadRequest || got["error"] != want.msg || got["at"] != want.at {
			t.Errorf("%q: %d %v, want %v", query, rec.Code, got, want)
		}
	}

	// The same query exports as CSV, in its order.
	rec := b.do("GET", "/api/items.csv?query="+url.QueryEscape("type = task order by estimate desc"), "")
	lines := strings.Split(strings.TrimSpace(strings.TrimPrefix(rec.Body.String(), "\xef\xbb\xbf")), "\r\n")
	if rec.Code != http.StatusOK || len(lines) != 4 || !strings.HasPrefix(lines[1], "PLAT-3,") || !strings.HasPrefix(lines[3], "PLAT-2,") {
		t.Fatalf("items.csv: %d %q", rec.Code, rec.Body)
	}
	if rec := w.carla.do("GET", "/api/items.csv?query="+url.QueryEscape("type = task"), ""); strings.Contains(rec.Body.String(), "kernel") {
		t.Fatalf("carla exports what she may not read: %q", rec.Body)
	}
	b.expect("GET", "/api/items.csv?query="+url.QueryEscape("priorty = high"), "", http.StatusBadRequest)

	// Agents ask the same through MCP.
	text, isErr := w.vera.tool("query_items", `{"query":"priority = high and state = open"}`)
	if isErr || !strings.Contains(text, "Patch the kernel") || strings.Contains(text, "Replace disks") {
		t.Fatalf("query_items: %v %s", isErr, text)
	}
	if text, isErr := w.vera.tool("query_items", `{"query":"priority ="}`); !isErr || !strings.Contains(text, "expected a value") {
		t.Fatalf("query_items with a mistake: %v %s", isErr, text)
	}
}
