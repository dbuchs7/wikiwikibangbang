package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dbuchs7/wikiwikibangbang/internal/budget"
)

// rpc posts one JSON-RPC message to /mcp and decodes the answer.
func (c *client) rpc(msg string) map[string]any {
	c.t.Helper()
	rec := c.do("POST", MCPPath, msg)
	if rec.Code != http.StatusOK {
		c.t.Fatalf("POST %s: status %d; body %s", MCPPath, rec.Code, rec.Body)
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		c.t.Fatalf("answer is not JSON: %s", rec.Body)
	}
	return out
}

// tool calls an MCP tool and returns its text and whether it is an error.
func (c *client) tool(name, args string) (string, bool) {
	c.t.Helper()
	out := c.rpc(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"` + name + `","arguments":` + args + `}}`)
	res, ok := out["result"].(map[string]any)
	if !ok {
		c.t.Fatalf("%s: no result: %v", name, out)
	}
	return res["content"].([]any)[0].(map[string]any)["text"].(string), res["isError"].(bool)
}

func TestMCPToolsActAsTheCaller(t *testing.T) {
	w := newWorld(t)
	if out := w.bert.rpc(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`); len(out["result"].(map[string]any)["tools"].([]any)) == 0 {
		t.Fatalf("no tools: %v", out)
	}
	text, isErr := w.bert.tool("create_item", `{"space":"PLAT","type":"task","title":"From an agent","properties":{"priority":"low"}}`)
	if isErr {
		t.Fatalf("create_item: %s", text)
	}
	var created map[string]any
	_ = json.Unmarshal([]byte(text), &created)
	key := field(created, "item", "key").(string)

	// The change is bert's: the API attributed it to the session's user.
	got := w.bert.expect("GET", "/api/items/"+key+"/history", "", http.StatusOK)
	if !strings.Contains(mustJSON(t, got), w.ids["bert"]) {
		t.Fatalf("history does not name bert: %v", got)
	}
	if text, isErr = w.bert.tool("transition", `{"ref":"`+key+`","name":"finish"}`); isErr {
		t.Fatalf("transition: %s", text)
	}
	if text, _ = w.bert.tool("get_item", `{"ref":"`+key+`"}`); !strings.Contains(text, `"done"`) {
		t.Fatalf("get_item after the transition: %s", text)
	}
	if text, isErr = w.bert.tool("add_comment", `{"ref":"`+key+`","text":"Finished by an agent"}`); isErr {
		t.Fatalf("add_comment: %s", text)
	}
	if text, _ = w.vera.tool("list_comments", `{"ref":"`+key+`"}`); !strings.Contains(text, "Finished by an agent") || !strings.Contains(text, w.ids["bert"]) {
		t.Fatalf("list_comments: %s", text)
	}

	// A viewer's agent may read and is refused the write, with the reason.
	if text, isErr = w.vera.tool("get_item", `{"ref":"`+key+`"}`); isErr {
		t.Fatalf("vera reads: %s", text)
	}
	if text, isErr = w.vera.tool("set_title", `{"ref":"`+key+`","title":"x"}`); !isErr || !strings.Contains(text, "403") {
		t.Fatalf("vera writes: %v %s", isErr, text)
	}
	// Someone without a role finds nothing: unreadable is non-existent (D10).
	if text, isErr = w.carla.tool("get_item", `{"ref":"`+key+`"}`); !isErr || !strings.Contains(text, "404") {
		t.Fatalf("carla reads: %v %s", isErr, text)
	}
}

func TestMCPWithAReadToken(t *testing.T) {
	w := newWorld(t)
	tok := w.bert.expect("POST", "/api/tokens", `{"name":"agent","scope":"read","days":1}`, http.StatusCreated)
	agent := &client{t: t, h: w.bert.h, bearer: tok["secret"].(string)}
	if text, isErr := agent.tool("list_spaces", `{}`); isErr || !strings.Contains(text, "PLAT") {
		t.Fatalf("a read token reads through MCP: %v %s", isErr, text)
	}
	text, isErr := agent.tool("create_item", `{"space":"PLAT","type":"task","title":"x","properties":{"priority":"low"}}`)
	if !isErr || !strings.Contains(text, "may only read") {
		t.Fatalf("a read token wrote through MCP: %v %s", isErr, text)
	}
	if n := len(w.bert.expect("GET", "/api/items?space=PLAT", "", http.StatusOK)["items"].([]any)); n != 0 {
		t.Fatalf("%d items exist after the refused write", n)
	}
}

func TestMCPIsAuthenticatedAndBounded(t *testing.T) {
	w := newWorld(t)
	anon := &client{t: t, h: w.bert.h}
	if rec := anon.do("POST", MCPPath, `{"jsonrpc":"2.0","id":1,"method":"ping"}`); rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous: %d", rec.Code)
	}
	tok := w.admin.expect("POST", "/api/tokens", `{"name":"scraper","scope":"metrics","days":1}`, http.StatusCreated)
	scraper := &client{t: t, h: w.bert.h, bearer: tok["secret"].(string)}
	if rec := scraper.do("POST", MCPPath, `{"jsonrpc":"2.0","id":1,"method":"ping"}`); rec.Code != http.StatusForbidden {
		t.Fatalf("metrics token: %d", rec.Code)
	}
	if rec := w.bert.do("GET", MCPPath, ""); rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET: %d", rec.Code)
	}
	// Without application/json a cross-site form could post with the cookie.
	req := httptest.NewRequest("POST", MCPPath, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`))
	req.Header.Set("Content-Type", "text/plain")
	req.AddCookie(w.bert.cookie)
	rec := httptest.NewRecorder()
	w.bert.h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("text/plain: %d", rec.Code)
	}
	if rec := w.bert.do("POST", MCPPath, `{"jsonrpc":"2.0","method":"notifications/initialized"}`); rec.Code != http.StatusAccepted || rec.Body.Len() != 0 {
		t.Fatalf("notification: %d %q", rec.Code, rec.Body)
	}
	small := budget.Default()
	small.RequestBody = 64
	budget.Set(small)
	t.Cleanup(func() { budget.Set(budget.Default()) })
	if rec := w.bert.do("POST", MCPPath, `{"jsonrpc":"2.0","id":1,"method":"ping","params":{"pad":"`+strings.Repeat("x", 100)+`"}}`); rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized message: %d", rec.Code)
	}
}

// Even an administrator's agent cannot reach what is not offered as a tool.
func TestMCPOffersNoDestructiveTool(t *testing.T) {
	w := newWorld(t)
	for _, name := range []string{"purge_item", "trash_item", "redact_item", "set_restriction", "grant_role", "create_token", "edit_comment", "delete_comment"} {
		out := w.admin.rpc(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"` + name + `","arguments":{}}}`)
		if out["error"] == nil {
			t.Errorf("%s is offered: %v", name, out)
		}
	}
}

// The inner request takes its principal from the forwarded credential
// alone, never from the context of the request that carried the tool call.
func TestInnerRequestsCarryNoContextValues(t *testing.T) {
	ctx := context.WithValue(context.Background(), principalKey{}, "outer")
	if v := (withoutValues{ctx}).Value(principalKey{}); v != nil {
		t.Fatalf("value %v leaked", v)
	}
	serveMCP(httptest.NewRecorder(), httptest.NewRequest("POST", MCPPath, nil)) // no media type: refused before anything else
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", MCPPath, strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	serveMCP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("outside a handler: %d", rec.Code)
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
