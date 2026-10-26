package connector

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/dbuchs7/wikiwikibangbang/internal/site"
)

// changingSource answers its entries, or with ?changed= only those it
// was told changed; it fails while broken.
type changingSource struct {
	mu      sync.Mutex
	entries []string
	changed []string
	asked   []string
	broken  bool
}

func (c *changingSource) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.asked = append(c.asked, r.URL.Query().Get("changed"))
	if c.broken {
		http.Error(w, "down", http.StatusServiceUnavailable)
		return
	}
	list := c.entries
	if r.URL.Query().Has("changed") {
		list = c.changed
	}
	out := []any{}
	for _, id := range list {
		out = append(out, map[string]any{"id": id, "title": "Entry " + id})
	}
	json.NewEncoder(w).Encode(map[string]any{"entries": out})
}

func (c *changingSource) last() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.asked[len(c.asked)-1]
}

func sinceSetup(t *testing.T) (*fixture, *changingSource, site.Connector) {
	t.Helper()
	f := setup(t)
	src := &changingSource{entries: []string{"a", "b", "c"}}
	srv := httptest.NewServer(src)
	t.Cleanup(srv.Close)
	c := site.ConnectorTemplates()[1]
	c.ID, c.Name, c.Space = "changing", "Changing", "API"
	c.JSON.URL, c.JSON.Next = srv.URL+"/entries", ""
	c.JSON.Since = &site.Since{Param: "changed"}
	c, err := f.db.Site.SetConnector("admin", c)
	if err != nil {
		t.Fatal(err)
	}
	return f, src, c
}

func TestAConnectorReadsWhatChanged(t *testing.T) {
	f, src, c := sinceSetup(t)
	ctx := context.Background()
	if c.JSON.Since.Format != site.SinceRFC3339 || c.JSON.Since.FullDays != 7 {
		t.Fatalf("defaults %+v", c.JSON.Since)
	}
	first := f.clock
	res, err := f.r.Run(ctx, "changing", false, "")
	if err != nil || res.New != 3 || !res.Since.IsZero() || src.last() != "" {
		t.Fatalf("the first run reads in full: %+v %v, asked %q", res, err, src.last())
	}
	if st := f.r.Status("changing"); !st.LastFull.Equal(first) {
		t.Errorf("last full %v", st.LastFull)
	}

	// The next run asks for what changed since the first began, an hour
	// earlier; what it does not hear of stays as it is.
	f.clock = first.Add(24 * time.Hour)
	src.entries, src.changed = []string{"a"}, []string{"b"}
	res, err = f.r.Run(ctx, "changing", false, "")
	want := first.Add(-sinceOverlap).UTC()
	if err != nil || res.Unchanged != 1 || res.Withdrawn != 0 || !res.Since.Equal(want) || src.last() != want.Format(time.RFC3339) || len(res.Problems) != 0 {
		t.Fatalf("a run of what changed: %+v %v, asked %q", res, err, src.last())
	}
	// A dry run asks as the next run would, and moves nothing on.
	f.clock = first.Add(48 * time.Hour)
	if res, _ := f.r.Run(ctx, "changing", true, ""); !res.Since.Equal(first.Add(24*time.Hour - sinceOverlap)) {
		t.Errorf("a dry run asks from %v", res.Since)
	}
	if res, _ := f.r.Run(ctx, "changing", true, ""); !res.Since.Equal(first.Add(24*time.Hour - sinceOverlap)) {
		t.Errorf("a dry run moved the time on: %v", res.Since)
	}
	// A run that fails moves nothing on either.
	src.broken = true
	if _, err := f.r.Run(ctx, "changing", false, ""); err == nil {
		t.Fatal("a broken source")
	}
	src.broken = false
	if res, _ := f.r.Run(ctx, "changing", true, ""); !res.Since.Equal(first.Add(24*time.Hour - sinceOverlap)) {
		t.Errorf("a failed run moved the time on: %v", res.Since)
	}

	// A week after the last full run, a run reads in full and withdraws
	// what the source dropped.
	f.clock = first.Add(7 * 24 * time.Hour)
	src.entries = []string{"a", "b"}
	res, err = f.r.Run(ctx, "changing", false, "")
	if err != nil || !res.Since.IsZero() || res.Withdrawn != 1 || src.last() != "" {
		t.Fatalf("the weekly full run: %+v %v", res, err)
	}
	// So does a run after the settings changed, and one asked for in full.
	f.clock = first.Add(8 * 24 * time.Hour)
	if res, _ := f.r.Run(ctx, "changing", true, ""); res.Since.IsZero() {
		t.Error("no run of what changed after a full one")
	}
	c.JSON.Since.FullDays = 3
	f.db.Site.SetConnector("admin", c)
	if res, _ := f.r.Run(ctx, "changing", true, ""); !res.Since.IsZero() {
		t.Error("changed settings are not read in full")
	}
	f.r.Run(ctx, "changing", false, "")
	f.r.ReadInFull("changing")
	f.r.ReadInFull("nope")
	if res, _ := f.r.Run(ctx, "changing", true, ""); !res.Since.IsZero() {
		t.Error("a full run asked for is not full")
	}
}

func TestTimesAsSourcesAskForThem(t *testing.T) {
	at := time.Date(2026, 10, 26, 13, 4, 5, 0, time.FixedZone("CET", 3600))
	for format, want := range map[string]string{
		site.SinceRFC3339: "2026-10-26T12:04:05Z", site.SinceDate: "2026-10-26",
		site.SinceUnix: fmt.Sprint(at.Unix()), site.SinceUnixMS: fmt.Sprint(at.UnixMilli()),
	} {
		if got := sinceText(at, format); got != want {
			t.Errorf("%s: %s, want %s", format, got, want)
		}
	}
}
