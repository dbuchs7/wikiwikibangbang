package server

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/dbuchs7/wikiwikibangbang/internal/budget"
	"github.com/dbuchs7/wikiwikibangbang/internal/linkcheck"
)

// linkTarget answers 200 on /ok and 404 elsewhere.
func linkTarget(t *testing.T) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ok" {
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func withLinkCheck(d *Deps) {
	d.LinkCheck = NewLinkChecks(linkcheck.New(linkcheck.Options{
		Allowed: func(a netip.Addr) bool { return a.IsLoopback() },
		Timeout: 2 * time.Second,
	}))
}

func linkBody(hrefs ...string) string {
	var blocks []string
	for _, h := range hrefs {
		blocks = append(blocks, fmt.Sprintf(`{"kind":"paragraph","text":"link","marks":[{"type":"link","start":0,"end":4,"href":%q}]}`, h))
	}
	return `{"blocks":[` + strings.Join(blocks, ",") + `]}`
}

// awaitReport polls the report until the run has finished.
func awaitReport(t *testing.T, c *client) map[string]any {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for {
		r := c.expect("GET", "/api/spaces/PLAT/linkcheck", "", http.StatusOK)
		if r["running"] == false && r["finished"] != nil {
			return r
		}
		if time.Now().After(deadline) {
			t.Fatalf("the run did not finish: %v", r)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestLinkCheckThroughTheAPI(t *testing.T) {
	w := newWorld(t, withLinkCheck)
	srv := linkTarget(t)
	it := w.bert.expect("POST", "/api/items", `{"space":"PLAT","type":"task","title":"Runbook","properties":{"priority":"low"}}`, http.StatusCreated)
	id := field(it, "item", "id").(string)
	w.bert.expect("PUT", "/api/items/"+id+"/body", linkBody(srv.URL+"/ok", srv.URL+"/gone"), http.StatusOK)

	// Only a space administrator starts a run; a reader sees the report.
	w.bert.expect("POST", "/api/spaces/PLAT/linkcheck", "", http.StatusForbidden)
	w.carla.expect("GET", "/api/spaces/PLAT/linkcheck", "", http.StatusNotFound)
	w.admin.expect("POST", "/api/spaces/PLAT/linkcheck", "", http.StatusAccepted)
	r := awaitReport(t, w.vera)
	if r["enabled"] != true || r["checked"] != 2.0 || r["by"] == "" || r["by"] == nil {
		t.Fatalf("report %v", r)
	}
	broken := r["broken"].([]any)
	if len(broken) != 1 {
		t.Fatalf("broken %v, want the one link that answers 404", broken)
	}
	b := broken[0].(map[string]any)
	if b["href"] != srv.URL+"/gone" || b["status"] != 404.0 || b["responsible"] != w.ids["bert"] || b["key"] != field(it, "item", "key") {
		t.Fatalf("broken link %v", b)
	}

	// A link that is no longer in the item is no longer reported once the
	// item becomes unreadable; trashed items drop out of the report.
	w.bert.expect("POST", "/api/items/"+id+"/trash", "", http.StatusOK)
	if r := w.vera.expect("GET", "/api/spaces/PLAT/linkcheck", "", http.StatusOK); len(r["broken"].([]any)) != 0 {
		t.Fatalf("a trashed item's link is still reported: %v", r)
	}
}

func TestLinkCheckIsOffByDefaultAndBounded(t *testing.T) {
	w := newWorld(t)
	if r := w.admin.expect("GET", "/api/spaces/PLAT/linkcheck", "", http.StatusOK); r["enabled"] != false {
		t.Fatalf("report %v", r)
	}
	w.admin.expect("POST", "/api/spaces/PLAT/linkcheck", "", http.StatusConflict)

	w = newWorld(t, withLinkCheck)
	srv := linkTarget(t)
	it := w.bert.expect("POST", "/api/items", `{"space":"PLAT","type":"task","title":"Many","properties":{"priority":"low"}}`, http.StatusCreated)
	w.bert.expect("PUT", "/api/items/"+field(it, "item", "id").(string)+"/body", linkBody(srv.URL+"/a", srv.URL+"/b", srv.URL+"/c"), http.StatusOK)
	small := budget.Default()
	small.LinkCheckLinks = 2
	budget.Set(small)
	t.Cleanup(func() { budget.Set(budget.Default()) })
	w.admin.expect("POST", "/api/spaces/PLAT/linkcheck", "", http.StatusAccepted)
	r := awaitReport(t, w.admin)
	if r["checked"] != 2.0 || r["truncated"] != true {
		t.Fatalf("report %v, want 2 checked and truncated", r)
	}
}
