//go:build scale

// The scale test measures the server with a catalog of the size an
// installation reached (ADR-0081): 10 690 entries with a generated page
// each. It is not part of the usual run, as the import alone takes
// minutes:
//
//	go test -tags scale -run TestScale -timeout 40m -v ./internal/server/
//
// With PROF=<file> it writes a CPU profile of searching and listing.

package server

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"runtime/pprof"
	"sort"
	"testing"
	"time"

	"github.com/dbuchs7/wikiwikibangbang/internal/connector"
	"github.com/dbuchs7/wikiwikibangbang/internal/item"
	"github.com/dbuchs7/wikiwikibangbang/internal/site"
)

func body(i int) []item.Block {
	var b []item.Block
	for j := range 30 {
		b = append(b, item.Block{Kind: item.Paragraph, Text: fmt.Sprintf("Angabe %d des Datensatzes %d: Anzahl Haushalte im Kanton Zürich nach Gemeinde, Stichtag 31.12.2024, Quelle Statistik", j, i)})
	}
	return b
}

func TestScale(t *testing.T) {
	w := newWorld(t)
	w.admin.expect("POST", "/api/spaces", `{"key":"API","name":"Interfaces"}`, http.StatusCreated)
	w.admin.expect("PUT", "/api/spaces/API/roles/"+w.ids["bert"], `{"role":"editor"}`, http.StatusNoContent)
	c := site.ConnectorTemplates()[0]
	c.Space = "API"
	c.Steward = w.admin.expect("GET", "/api/me", "", http.StatusOK)["user"].(map[string]any)["id"].(string)
	n := 10690
	var entries []connector.Entry
	for i := range n {
		entries = append(entries, connector.Entry{SourceID: fmt.Sprintf("ds-%05d", i), Dataset: fmt.Sprintf("data-%d", i),
			Title: fmt.Sprintf("Datensatz %d Zürich Statistik", i), Format: "CSV", Endpoint: "https://example.org/x.csv",
			Publisher: "Amt für Statistik", Contact: "Data Shop <a@b.ch>", DatasetURL: fmt.Sprintf("https://opendata.swiss/de/dataset/data-%d", i),
			Modified: time.Date(2025, 9, 30, 0, 0, 0, 0, time.UTC), Body: body(i)})
	}
	start := time.Now()
	res, err := connector.Apply(w.db, c, entries, false)
	t.Logf("import of %d: %v", n, time.Since(start))
	start = time.Now()
	w.db.WarmUp(context.Background())
	t.Logf("warm-up: %v", time.Since(start))
	if err != nil || res.New != n {
		t.Fatalf("import %+v %v", res, err)
	}
	var key, id string
	w.db.Read(func(s *item.Store) {
		its := s.Find(item.Query{Space: "API", Type: connector.TypeName})
		key, id = its[0].Key, string(its[0].ID)
	})
	if os.Getenv("PROF") != "" {
		w.admin.do("GET", "/api/notifications?limit=1", "")
		f, _ := os.Create(os.Getenv("PROF"))
		pprof.StartCPUProfile(f)
		for range 5 {
			w.admin.do("GET", "/api/search?q=API-1&limit=50", "")
			w.admin.do("GET", "/api/spaces/API/tree", "")
		}
		pprof.StopCPUProfile()
		f.Close()
	}
	type m struct {
		name string
		d    time.Duration
	}
	var out []m
	time1 := func(who *client, name, path string) {
		start := time.Now()
		rec := who.do("GET", path, "")
		d := time.Since(start)
		if rec.Code >= 400 {
			t.Logf("%s: %d", path, rec.Code)
		}
		out = append(out, m{name + " " + path, d})
	}
	for _, who := range []struct {
		n string
		c *client
	}{{"admin", w.admin}, {"bert", w.bert}} {
		for _, p := range []string{"/api/me", "/api/spaces", "/api/types", "/api/notifications?limit=1", "/api/notifications", "/api/kanbans",
			"/api/items?limit=100", "/api/items?space=API&limit=100", "/api/items?type=interface&limit=1", "/api/items?type=task&limit=1",
			"/api/spaces/API/tree", "/api/items/" + id, "/api/items/" + id + "/history", "/api/items/" + id + "/embeds", "/api/items/" + id + "/relations",
			"/api/items/" + id + "/comments", "/api/search?q=Zürich&limit=50", "/api/search?q=" + key + "&limit=50", "/api/spaces/API/governance",
			"/api/album?limit=1", "/api/comments", "/api/connectors"} {
			time1(who.c, who.n, p)
		}
	}
	// The task list reads every item, 100 at a time.
	start = time.Now()
	after, pages := "", 0
	for {
		r := w.admin.expect("GET", "/api/items?limit=100"+func() string {
			if after == "" {
				return ""
			}
			return "&after=" + after
		}(), "", http.StatusOK)
		pages++
		next, _ := r["next"].(string)
		if next == "" || pages > 200 {
			break
		}
		after = next
	}
	out = append(out, m{fmt.Sprintf("admin all items in %d pages", pages), time.Since(start)})
	sort.Slice(out, func(i, j int) bool { return out[i].d > out[j].d })
	for _, x := range out {
		t.Logf("%10v  %s", x.d.Round(time.Microsecond), x.name)
	}
}
