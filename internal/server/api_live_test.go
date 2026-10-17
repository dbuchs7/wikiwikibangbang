package server

import (
	"bufio"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// stream reads server-sent events from a live session.
type stream struct {
	t      *testing.T
	res    *http.Response
	events chan sse
}

type sse struct {
	kind string
	data map[string]any
}

func openStream(t *testing.T, srv *httptest.Server, c *client, ref string) *stream {
	t.Helper()
	req, _ := http.NewRequest("GET", srv.URL+"/api/items/"+ref+"/live", nil)
	req.AddCookie(c.cookie)
	res, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusOK {
		t.Fatalf("stream %s: status %d", ref, res.StatusCode)
	}
	if ct := res.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("content type %q", ct)
	}
	s := &stream{t: t, res: res, events: make(chan sse, 64)}
	go func() {
		defer close(s.events)
		r := bufio.NewReader(res.Body)
		var cur sse
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			line = strings.TrimRight(line, "\n")
			switch {
			case strings.HasPrefix(line, "event: "):
				cur.kind = line[len("event: "):]
			case strings.HasPrefix(line, "data: "):
				_ = json.Unmarshal([]byte(line[len("data: "):]), &cur.data)
			case line == "" && cur.kind != "":
				s.events <- cur
				cur = sse{}
			}
		}
	}()
	t.Cleanup(func() { res.Body.Close() })
	return s
}

// next returns the next event of a kind, skipping others.
func (s *stream) next(kind string) map[string]any {
	s.t.Helper()
	timeout := time.After(10 * time.Second)
	for {
		select {
		case e, ok := <-s.events:
			if !ok {
				s.t.Fatalf("stream ended while waiting for %s", kind)
			}
			if e.kind == kind {
				return e.data
			}
		case <-timeout:
			s.t.Fatalf("no %s event", kind)
		}
	}
}

func (s *stream) ends() {
	s.t.Helper()
	timeout := time.After(10 * time.Second)
	for {
		select {
		case _, ok := <-s.events:
			if !ok {
				return
			}
		case <-timeout:
			s.t.Fatal("the stream did not end")
		}
	}
}

func TestLiveEditingThroughTheAPI(t *testing.T) {
	w := newWorld(t)
	srv := httptest.NewServer(w.bert.h)
	defer srv.Close()
	w.bert.expect("POST", "/api/items", `{"space":"PLAT","type":"task","title":"Runbook","properties":{"priority":"low"}}`, http.StatusCreated)
	w.bert.expect("PUT", "/api/items/PLAT-1/body", `{"blocks":[{"kind":"paragraph","text":"one"},{"kind":"paragraph","text":"two"}]}`, http.StatusOK)

	bert := openStream(t, srv, w.bert, "PLAT-1")
	snap := bert.next("snapshot")
	bertClient := snap["client"].(string)
	blocks := snap["blocks"].([]any)
	first := blocks[0].(map[string]any)["id"].(string)
	if len(blocks) != 2 || field(blocks[1].(map[string]any), "block", "text") != "two" {
		t.Fatalf("snapshot %v", snap)
	}
	vera := openStream(t, srv, w.vera, "PLAT-1")
	veraClient := vera.next("snapshot")["client"].(string)
	if p := bert.next("presence")["participants"].([]any); len(p) != 1 {
		// the first presence event lists bert alone; vera's join follows
		if p = bert.next("presence")["participants"].([]any); len(p) != 2 {
			t.Fatalf("presence %v", p)
		}
	}

	ops := func(c *client, clientID, body string, status int) {
		t.Helper()
		c.expect("POST", "/api/items/PLAT-1/live", `{"client":"`+clientID+`","ops":`+body+`}`, status)
	}
	ops(w.bert, bertClient, `[{"op":"set","id":"`+first+`","block":{"kind":"heading","level":2,"text":"Steps"}}]`, http.StatusNoContent)
	if e := vera.next("lock"); e["id"] != first || field(e, "holder", "name") != "bert" {
		t.Fatalf("lock %v", e)
	}
	if e := vera.next("block"); field(e, "block", "text") != "Steps" || field(e, "block", "level") != 2.0 {
		t.Fatalf("block %v", e)
	}

	// A viewer may watch, not write; a stranger's client ID is refused.
	ops(w.vera, veraClient, `[{"op":"lock","id":"`+first+`"}]`, http.StatusForbidden)
	ops(w.vera, "nope", `[{"op":"lock","id":"`+first+`"}]`, http.StatusConflict)
	ops(w.vera, bertClient, `[{"op":"lock","id":"`+first+`"}]`, http.StatusConflict) // bert's client is not vera's
	ops(w.bert, bertClient, `[{"op":"set","id":"`+first+`","block":{"kind":"paragraph","text":"x","level":3}}]`, http.StatusBadRequest)
	ops(w.bert, bertClient, `[{"op":"set","id":"`+first+`","block":{"kind":"unsupported","text":"<x/>"}}]`, http.StatusBadRequest)

	// The body cannot be overwritten from outside while it is edited live.
	w.admin.expect("PUT", "/api/items/PLAT-1/body", `{"blocks":[]}`, http.StatusConflict)

	ops(w.bert, bertClient, `[{"op":"release","id":"`+first+`"}]`, http.StatusNoContent)
	if e := vera.next("committed"); e["name"] != "bert" || e["by"] != w.ids["bert"] {
		t.Fatalf("committed %v", e)
	}
	got := w.vera.expect("GET", "/api/items/PLAT-1", "", http.StatusOK)
	body := field(got, "item", "body").([]any)
	if field(body[0].(map[string]any), "text") != "Steps" || len(body) != 2 {
		t.Fatalf("stored body %v", body)
	}

	// Strangers cannot join; trashing the item ends every stream.
	if rec := w.carla.do("GET", "/api/items/PLAT-1/live", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("carla joins: %d", rec.Code)
	}
	w.bert.expect("POST", "/api/items/PLAT-1/trash", "", http.StatusOK)
	if e := vera.next("closed"); e["reason"] == "" {
		t.Fatalf("closed %v", e)
	}
	vera.ends()
	bert.ends()
}
