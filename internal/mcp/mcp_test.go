package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// call is one API request the adapter made.
type call struct {
	Method, Path, Body string
}

// fakeAPI records the requests and answers them from a canned reply.
type fakeAPI struct {
	calls  []call
	status int
	body   string
	err    error
}

func (f *fakeAPI) Call(_ context.Context, method, path string, body []byte) (int, []byte, error) {
	f.calls = append(f.calls, call{method, path, string(body)})
	if f.err != nil {
		return 0, nil, f.err
	}
	status := f.status
	if status == 0 {
		status = 200
	}
	return status, []byte(f.body), nil
}

func handle(t *testing.T, api API, msg string) map[string]any {
	t.Helper()
	out, reply := Handle(context.Background(), api, []byte(msg))
	if !reply {
		t.Fatalf("no reply to %s", msg)
	}
	var m map[string]any
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatalf("reply is not JSON: %s", out)
	}
	if m["jsonrpc"] != "2.0" {
		t.Fatalf("reply is not JSON-RPC 2.0: %s", out)
	}
	return m
}

func errorCode(t *testing.T, m map[string]any) float64 {
	t.Helper()
	e, ok := m["error"].(map[string]any)
	if !ok {
		t.Fatalf("want an error, got %v", m)
	}
	return e["code"].(float64)
}

func result(t *testing.T, m map[string]any) map[string]any {
	t.Helper()
	r, ok := m["result"].(map[string]any)
	if !ok {
		t.Fatalf("want a result, got %v", m)
	}
	return r
}

func toolCall(name string, args string) string {
	return `{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"` + name + `","arguments":` + args + `}}`
}

func TestInitializeNegotiatesTheVersion(t *testing.T) {
	r := result(t, handle(t, &fakeAPI{}, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26"}}`))
	if r["protocolVersion"] != "2025-03-26" {
		t.Errorf("a supported version is echoed, got %v", r["protocolVersion"])
	}
	if r["serverInfo"].(map[string]any)["name"] != "wikiwikibangbang" {
		t.Errorf("serverInfo: %v", r["serverInfo"])
	}
	if _, ok := r["capabilities"].(map[string]any)["tools"]; !ok {
		t.Errorf("tools capability missing: %v", r["capabilities"])
	}
	r = result(t, handle(t, &fakeAPI{}, `{"jsonrpc":"2.0","id":2,"method":"initialize","params":{"protocolVersion":"1999-01-01"}}`))
	if r["protocolVersion"] != latestVersion {
		t.Errorf("an unknown version gets the latest, got %v", r["protocolVersion"])
	}
}

func TestPingAndNotifications(t *testing.T) {
	m := handle(t, &fakeAPI{}, `{"jsonrpc":"2.0","id":"a","method":"ping"}`)
	if m["id"] != "a" || m["result"] == nil {
		t.Fatalf("ping: %v", m)
	}
	if out, reply := Handle(context.Background(), &fakeAPI{}, []byte(`{"jsonrpc":"2.0","method":"notifications/initialized"}`)); reply {
		t.Fatalf("a notification got a reply: %s", out)
	}
}

func TestProtocolErrors(t *testing.T) {
	for msg, code := range map[string]float64{
		`{"jsonrpc":"2.0","id":1,"method":`:              codeParse,
		`[{"jsonrpc":"2.0","id":1,"method":"ping"}]`:     codeInvalidRequest, // no batches
		`{"jsonrpc":"1.0","id":1,"method":"ping"}`:       codeInvalidRequest,
		`{"jsonrpc":"2.0","id":1,"method":"nope"}`:       codeMethodNotFound,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call"}`: codeInvalidParams,
		toolCall("no_such_tool", `{}`):                   codeInvalidParams,
	} {
		if got := errorCode(t, handle(t, &fakeAPI{}, msg)); got != code {
			t.Errorf("%s: code %v, want %v", msg, got, code)
		}
	}
}

func TestToolsListDescribesEveryTool(t *testing.T) {
	r := result(t, handle(t, &fakeAPI{}, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	list := r["tools"].([]any)
	if len(list) != len(tools) {
		t.Fatalf("%d tools listed, %d defined", len(list), len(tools))
	}
	for _, x := range list {
		tool := x.(map[string]any)
		schema := tool["inputSchema"].(map[string]any)
		if tool["name"] == "" || tool["description"] == "" || schema["type"] != "object" {
			t.Errorf("incomplete tool: %v", tool)
		}
	}
}

// Every tool is exactly one request to the API (ADR-0012); this pins the
// request each one makes.
func TestEachToolIsOneAPIRequest(t *testing.T) {
	for _, tc := range []struct {
		tool, args string
		want       call
	}{
		{"list_spaces", `{}`, call{"GET", "/api/spaces", ""}},
		{"list_types", `{}`, call{"GET", "/api/types", ""}},
		{"find_items", `{"space":"PLAT","type":"task","limit":20,"after":"x1"}`, call{"GET", "/api/items?after=x1&limit=20&space=PLAT&type=task", ""}},
		{"search_items", `{"q":"kernel patch"}`, call{"GET", "/api/search?q=kernel+patch", ""}},
		{"get_item", `{"ref":"PLAT-1"}`, call{"GET", "/api/items/PLAT-1", ""}},
		{"get_history", `{"ref":"PLAT-1"}`, call{"GET", "/api/items/PLAT-1/history", ""}},
		{"get_relations", `{"ref":"PLAT-1","direction":"incoming"}`, call{"GET", "/api/items/PLAT-1/relations?direction=incoming", ""}},
		{"get_embeds", `{"ref":"PLAT-1"}`, call{"GET", "/api/items/PLAT-1/embeds", ""}},
		{"create_item", `{"space":"PLAT","type":"task","title":"x","properties":{"priority":"low"}}`,
			call{"POST", "/api/items", `{"properties":{"priority":"low"},"space":"PLAT","title":"x","type":"task"}`}},
		{"set_title", `{"ref":"PLAT-1","title":"New"}`, call{"PUT", "/api/items/PLAT-1/title", `{"title":"New"}`}},
		{"set_property", `{"ref":"PLAT-1","field":"priority","value":"high"}`, call{"PUT", "/api/items/PLAT-1/properties/priority", `{"value":"high"}`}},
		{"set_property", `{"ref":"PLAT-1","field":"estimate","value":null}`, call{"PUT", "/api/items/PLAT-1/properties/estimate", `{"value":null}`}},
		{"transition", `{"ref":"PLAT-1","name":"finish"}`, call{"POST", "/api/items/PLAT-1/transitions", `{"name":"finish"}`}},
		{"commit_body", `{"ref":"PLAT-1","blocks":[{"kind":"paragraph","text":"hi"}]}`, call{"PUT", "/api/items/PLAT-1/body", `{"blocks":[{"kind":"paragraph","text":"hi"}]}`}},
		{"add_relation", `{"type":"blocks","from":"a","to":"b"}`, call{"POST", "/api/relations", `{"from":"a","to":"b","type":"blocks"}`}},
	} {
		api := &fakeAPI{body: `{"ok":true}`}
		r := result(t, handle(t, api, toolCall(tc.tool, tc.args)))
		if len(api.calls) != 1 || api.calls[0] != tc.want {
			t.Errorf("%s %s: requests %+v, want %+v", tc.tool, tc.args, api.calls, tc.want)
			continue
		}
		content := r["content"].([]any)[0].(map[string]any)
		if content["type"] != "text" || content["text"] != `{"ok":true}` || r["isError"] != false {
			t.Errorf("%s: result %v", tc.tool, r)
		}
	}
}

func TestPathArgumentsAreEscaped(t *testing.T) {
	api := &fakeAPI{}
	handle(t, api, toolCall("get_item", `{"ref":"../admin?x=1"}`))
	if api.calls[0].Path != "/api/items/..%2Fadmin%3Fx=1" {
		t.Fatalf("path %q", api.calls[0].Path)
	}
}

func TestBadArgumentsReachNoAPI(t *testing.T) {
	for _, args := range []string{
		`{}`,                       // ref missing
		`{"ref":5}`,                // not a string
		`{"ref":""}`,               // empty
		`{"ref":"x","extra":true}`, // unknown
		`"PLAT-1"`,                 // not an object
	} {
		api := &fakeAPI{}
		if code := errorCode(t, handle(t, api, toolCall("get_item", args))); code != codeInvalidParams {
			t.Errorf("%s: code %v", args, code)
		}
		if len(api.calls) != 0 {
			t.Errorf("%s reached the API: %+v", args, api.calls)
		}
	}
	api := &fakeAPI{}
	for _, args := range []string{`{"limit":1.5}`, `{"limit":"3"}`, `{"limit":0}`} {
		if code := errorCode(t, handle(t, api, toolCall("find_items", args))); code != codeInvalidParams {
			t.Errorf("find_items %s: code %v", args, code)
		}
	}
	if code := errorCode(t, handle(t, api, toolCall("commit_body", `{"ref":"x","blocks":"text"}`))); code != codeInvalidParams {
		t.Errorf("blocks must be an array, code %v", code)
	}
	if code := errorCode(t, handle(t, api, toolCall("create_item", `{"space":"P","type":"t","title":"x","properties":[1]}`))); code != codeInvalidParams {
		t.Errorf("properties must be an object, code %v", code)
	}
	if len(api.calls) != 0 {
		t.Errorf("bad arguments reached the API: %+v", api.calls)
	}
}

// An answer of the API that refuses is a tool result marked as an error, so
// the agent reads the reason; it is not a protocol failure.
func TestAPIRefusalIsAToolError(t *testing.T) {
	r := result(t, handle(t, &fakeAPI{status: 403, body: `{"error":"this token may only read"}`}, toolCall("set_title", `{"ref":"x","title":"y"}`)))
	text := r["content"].([]any)[0].(map[string]any)["text"].(string)
	if r["isError"] != true || !strings.Contains(text, "403") || !strings.Contains(text, "may only read") {
		t.Fatalf("result %v", r)
	}
	r = result(t, handle(t, &fakeAPI{err: errors.New("connection refused")}, toolCall("list_spaces", `{}`)))
	if r["isError"] != true || !strings.Contains(r["content"].([]any)[0].(map[string]any)["text"].(string), "connection refused") {
		t.Fatalf("result %v", r)
	}
}

func TestServeReadsOneMessagePerLine(t *testing.T) {
	in := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}` + "\n\n" +
		`{"jsonrpc":"2.0","method":"notifications/initialized"}` + "\n" +
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}` + "\n")
	var out bytes.Buffer
	if err := Serve(context.Background(), &fakeAPI{}, in, &out); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 2 || !strings.Contains(lines[0], `"id":1`) || !strings.Contains(lines[1], `"tools"`) {
		t.Fatalf("output:\n%s", out.String())
	}
}

func TestServeRefusesAnOversizedLine(t *testing.T) {
	in := strings.NewReader(strings.Repeat("x", 1<<20) + "\n")
	if err := Serve(context.Background(), &fakeAPI{}, in, &bytes.Buffer{}); err == nil {
		t.Fatal("a line beyond the request budget was read")
	}
}

func TestHTTPAPISendsTheCallersCredentialOnly(t *testing.T) {
	var got *http.Request
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"item":{}}`))
	}))
	defer srv.Close()
	api := HTTPAPI{Base: srv.URL + "/", Header: http.Header{"Authorization": {"Bearer wwbb_secret"}}}
	status, answer, err := api.Call(context.Background(), "POST", "/api/items", []byte(`{"title":"x"}`))
	if err != nil || status != http.StatusCreated || string(answer) != `{"item":{}}` {
		t.Fatalf("status %d, answer %s, err %v", status, answer, err)
	}
	if got.URL.Path != "/api/items" || got.Header.Get("Authorization") != "Bearer wwbb_secret" ||
		got.Header.Get("Content-Type") != "application/json" || gotBody != `{"title":"x"}` {
		t.Fatalf("request %s %s, headers %v, body %q", got.Method, got.URL, got.Header, gotBody)
	}
	if _, _, err := (HTTPAPI{Base: "http://127.0.0.1:1"}).Call(context.Background(), "GET", "/api/me", nil); err == nil {
		t.Fatal("an unreachable server is no error")
	}
	if _, _, err := (HTTPAPI{Base: "http://bad host"}).Call(context.Background(), "GET", "/", nil); err == nil {
		t.Fatal("a malformed URL is no error")
	}
}
