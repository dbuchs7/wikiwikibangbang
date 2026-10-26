package connector

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dbuchs7/wikiwikibangbang/internal/item"
	"github.com/dbuchs7/wikiwikibangbang/internal/site"
)

// feed is a source in the connectors' own feed format, in two pages.
type feed struct {
	mu      sync.Mutex
	pages   []string
	endless bool // the last page names a next one, for ever
	auth    string
	answers int
}

func (f *feed) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.auth = r.Header.Get("Authorization")
	f.answers++
	n := 0
	fmt.Sscanf(r.URL.Query().Get("page"), "%d", &n)
	i := n
	if f.endless && i >= len(f.pages) {
		i = len(f.pages) - 1
	}
	if i >= len(f.pages) {
		http.NotFound(w, r)
		return
	}
	// NEXT is the next page's address, PAGE the page's number.
	answer := strings.ReplaceAll(f.pages[i], "NEXT", "http://"+r.Host+"/entries?page="+fmt.Sprint(n+1))
	fmt.Fprint(w, strings.ReplaceAll(answer, "PAGE", fmt.Sprint(n)))
}

const (
	feedPage0 = `{"entries":[
		{"id":"a1","title":"Adressen","url":"https://source.example/a1","text":"Alle Adressen.\n\nAuch alte.","owner":"Amt A","since":"2024-03-01","count":"12","open":"true","level":"high","home":"https://a.example/"},
		{"id":"a2","title":"","owner":"Amt B","since":"gestern","count":"viele","level":"none","home":"javascript:alert(1)"},
		{"title":"no ID"},
		"not an object",
		{"id":"a1","title":"twice"}
	],"next":"NEXT"}`
	feedPage1 = `{"entries":[{"id":3,"title":"Drei","tags":["x","y"]}],"next":""}`
)

func jsonSetup(t *testing.T) (*fixture, *feed, site.Connector) {
	t.Helper()
	f := setup(t)
	src := &feed{pages: []string{feedPage0, feedPage1}}
	srv := httptest.NewServer(src)
	t.Cleanup(srv.Close)
	c := site.ConnectorTemplates()[1]
	c.ID, c.Name, c.Enabled, c.Space = "feed", "Feed", true, "API"
	c.JSON.URL = srv.URL + "/entries?page=0"
	c.JSON.Values = map[string]string{"owner": "owner", "since": "since", "count": "count", "open": "open", "level": "level", "home": "home", "tags": "tags"}
	cfg, err := f.db.Site.SetConnector("admin", c)
	if err != nil {
		t.Fatal(err)
	}
	f.r.getenv = allowPrivate(func(k string) string {
		if k == TokenVar("feed") {
			return "feed-token"
		}
		return ""
	})
	return f, src, cfg
}

func TestAJSONSourceBecomesPages(t *testing.T) {
	f, src, _ := jsonSetup(t)
	res, err := f.r.Run(context.Background(), "feed", false, "")
	if err != nil || res.Datasets != 6 || res.Entries != 3 || res.New != 3 || len(res.Problems) != 3 {
		t.Fatalf("%+v %v", res, err)
	}
	if src.auth != "Bearer feed-token" || src.answers != 2 {
		t.Errorf("the credential %q, %d answers", src.auth, src.answers)
	}
	var pages map[string]item.Item
	f.db.Read(func(s *item.Store) {
		pages = map[string]item.Item{}
		for _, it := range s.Find(item.Query{Space: "API", Type: PageType}) {
			pages[it.Origin.ExternalID] = it
		}
	})
	a1, a2, three := pages["entry/a1"], pages["entry/a2"], pages["entry/3"]
	if a1.Title != "Adressen" || a1.Origin.URL != "https://source.example/a1" || a1.Origin.Instance != src0(f) ||
		a2.Title != "a2" || a2.Origin.URL != "" || three.Title != "Drei" {
		t.Fatalf("pages %+v", pages)
	}
	var kinds []item.BlockKind
	for _, b := range a1.Body.Blocks {
		kinds = append(kinds, b.Kind)
		if err := item.CheckBlock(b, "x"); err != nil {
			t.Errorf("block %+v: %v", b, err)
		}
	}
	if len(kinds) < 4 || kinds[0] != item.Panel || kinds[1] != item.Paragraph || kinds[2] != item.Paragraph {
		t.Errorf("the page's blocks %v", kinds)
	}
	if !strings.Contains(fmt.Sprint(three.Body.Blocks), "x, y") {
		t.Errorf("a list value %+v", three.Body.Blocks)
	}
	again, err := f.r.Run(context.Background(), "feed", false, "")
	if err != nil || again.Unchanged != 3 || again.New+again.Changed != 0 {
		t.Errorf("a second run %+v %v", again, err)
	}
}

// src0 is the address the JSON connector's items name as their instance.
func src0(f *fixture) string {
	c, _ := f.db.Site.Connector("feed")
	return c.JSON.URL
}

func TestAJSONSourceFillsTheFieldsOfAType(t *testing.T) {
	f, _, cfg := jsonSetup(t)
	api := item.TypeDef{Name: "api", Body: true, Fields: []item.FieldDef{
		{Name: "owner", Kind: item.KindText, Required: true},
		{Name: "since", Kind: item.KindDate},
		{Name: "count", Kind: item.KindNumber},
		{Name: "open", Kind: item.KindBool},
		{Name: "level", Kind: item.KindEnum, Values: []string{"low", "high"}},
		{Name: "home", Kind: item.KindURL},
		{Name: "note", Kind: item.KindText},
		{Name: "steward", Kind: item.KindUser},
	}}
	if err := f.db.Write(func(s *item.Store) error { return s.DefineType("admin", api) }); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	cfg.Type = "nope"
	f.db.Site.SetConnector("admin", cfg)
	if _, err := f.r.Run(ctx, "feed", true, ""); err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Errorf("a type that does not exist: %v", err)
	}
	cfg.Type = "api"
	f.db.Site.SetConnector("admin", cfg)
	if _, err := f.r.Run(ctx, "feed", true, ""); err == nil || !strings.Contains(err.Error(), `requires the field "owner"`) {
		t.Errorf("a required field not mapped: %v", err)
	}
	cfg.Fields = map[string]string{"owner": "owner", "steward": "owner"}
	f.db.Site.SetConnector("admin", cfg)
	if _, err := f.r.Run(ctx, "feed", true, ""); err == nil || !strings.Contains(err.Error(), "holds a user") {
		t.Errorf("a user field mapped: %v", err)
	}
	cfg.Fields = map[string]string{"owner": "owner", "since": "since", "count": "count", "open": "open", "level": "level", "home": "home"}
	f.db.Site.SetConnector("admin", cfg)
	res, err := f.r.Run(ctx, "feed", false, "")
	// a2 has the owner but nothing else that fits; "Drei" has no owner, which
	// the type requires.
	if err != nil || res.New != 2 || !strings.Contains(strings.Join(res.Problems, "\n"), `"gestern" is not a date`) ||
		!strings.Contains(strings.Join(res.Problems, "\n"), `field "owner" is required`) {
		t.Fatalf("%+v %v", res, err)
	}
	var a1, a2 item.Item
	f.db.Read(func(s *item.Store) {
		for _, it := range s.Find(item.Query{Space: "API", Type: "api"}) {
			switch it.Origin.ExternalID {
			case "entry/a1":
				a1 = it
			case "entry/a2":
				a2 = it
			}
		}
	})
	want := item.Props{"owner": "Amt A", "since": time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC), "count": 12.0, "open": true, "level": "high", "home": "https://a.example/"}
	if fmt.Sprint(a1.Properties) != fmt.Sprint(want) || fmt.Sprint(a2.Properties) != fmt.Sprint(item.Props{"owner": "Amt B"}) {
		t.Fatalf("a1 %v\na2 %v", a1.Properties, a2.Properties)
	}
	// A field people fill is kept; one the source fills is the source's.
	if err := f.db.Write(func(s *item.Store) error { return s.SetProperty("admin", a1.ID, "note", "kept") }); err != nil {
		t.Fatal(err)
	}
	if again, err := f.r.Run(ctx, "feed", false, ""); err != nil || again.Changed != 0 || again.Unchanged != 2 {
		t.Errorf("a second run %+v %v", again, err)
	}
	cfg.Type = ""
	f.db.Site.SetConnector("admin", cfg)
	if _, err := f.r.Run(ctx, "feed", true, ""); !errors.Is(err, ErrTypeChanged) {
		t.Errorf("another type: %v", err)
	}
}

func TestJSONSourcesThatDoNotAnswerWell(t *testing.T) {
	f, src, _ := jsonSetup(t)
	ctx := context.Background()
	for name, answer := range map[string]string{
		"no JSON":         `<html>`,
		"no list":         `{"entries":{}}`,
		"an error status": "",
	} {
		src.pages = []string{answer}
		if answer == "" {
			src.pages = nil
		}
		res, err := f.r.Run(ctx, "feed", true, "")
		if err == nil || res.Error == "" {
			t.Errorf("%s: %+v", name, res)
		}
	}
	src.pages = []string{`{"entries":[{"id":"x","title":"X"}],"next":"ftp://elsewhere/"}`}
	res, err := f.r.Run(ctx, "feed", true, "")
	if err != nil || res.New != 1 || len(res.Problems) != 1 || !strings.Contains(res.Problems[0], "not an http") {
		t.Errorf("a next page elsewhere %+v %v", res, err)
	}
	src.pages, src.endless = []string{`{"entries":[{"id":"x-PAGE","title":"X"}],"next":"NEXT"}`}, true
	src.answers = 0
	res, err = f.r.Run(ctx, "feed", true, "")
	if err != nil || res.Entries != maxPages || src.answers != maxPages || !strings.Contains(strings.Join(res.Problems, "\n"), "more than") {
		t.Errorf("an endless list: %d entries, %d answers, %v %v", res.Entries, src.answers, res.Problems, err)
	}
	src.pages = []string{`{"entries":[{"id":"x","title":"X"}],"next":"NEXT"}`}
	src.answers = 0
	res, err = f.r.Run(ctx, "feed", true, "")
	if err != nil || res.Entries != 1 || src.answers != 2 || !strings.Contains(strings.Join(res.Problems, "\n"), "only entries already read") {
		t.Errorf("a list that names the same page again: %d answers %+v %v", src.answers, res, err)
	}
	src.endless = false
	src.pages = []string{`{"entries":[{"id":"` + strings.Repeat("x", 201) + `","title":"X"}]}`}
	if res, _ := f.r.Run(ctx, "feed", true, ""); len(res.Problems) != 1 || !strings.Contains(res.Problems[0], "longer") {
		t.Errorf("a long ID %+v", res)
	}
	if _, err := getJSON(ctx, Env{HTTP: http.DefaultClient}, "http://[::1]:0/%zz"); err == nil {
		t.Error("a bad address")
	}
}

func TestPathsIntoAnAnswer(t *testing.T) {
	v := map[string]any{"a": []any{map[string]any{"b": "deep"}}, "n": nil}
	for path, want := range map[string]any{"a.0.b": "deep", "a.1.b": nil, "a.x": nil, "a.0.b.c": nil, "": v} {
		if got := at(v, path); fmt.Sprint(got) != fmt.Sprint(want) {
			t.Errorf("%q: %v", path, got)
		}
	}
	if text(map[string]any{}) != "" || text(true) != "true" || text([]any{" a ", "", 1.5}) != "a" {
		t.Errorf("texts %q %q %q", text(map[string]any{}), text(true), text([]any{" a ", "", 1.5}))
	}
}

func TestFieldValues(t *testing.T) {
	for _, c := range []struct {
		f    item.FieldDef
		in   string
		want any
		bad  bool
	}{
		{item.FieldDef{Kind: item.KindText}, " a ", "a", false},
		{item.FieldDef{Kind: item.KindText}, " ", nil, false},
		{item.FieldDef{Kind: item.KindNumber}, "1e3", 1000.0, false},
		{item.FieldDef{Kind: item.KindNumber}, "NaN", nil, true},
		{item.FieldDef{Kind: item.KindBool}, "false", false, false},
		{item.FieldDef{Kind: item.KindBool}, "ja", nil, true},
		{item.FieldDef{Kind: item.KindDate}, "01.03.2024", time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC), false},
		{item.FieldDef{Kind: item.KindEnum, Values: []string{"a"}}, "b", nil, true},
		{item.FieldDef{Kind: item.KindURL}, "https://x.example/", "https://x.example/", false},
		{item.FieldDef{Kind: item.KindURL}, "mailto:a@b", nil, true},
		{item.FieldDef{Kind: item.KindUser}, "anna", nil, true},
	} {
		got, err := fieldValue(c.f, c.in)
		if (err != nil) != c.bad || fmt.Sprint(got) != fmt.Sprint(c.want) {
			t.Errorf("%s %q: %v %v", c.f.Kind, c.in, got, err)
		}
	}
	if err := Fits(item.TypeDef{Name: "t", Body: true}, site.Connector{Fields: map[string]string{"x": "y"}}); err == nil {
		t.Error("a field the type does not have")
	}
}

// answering is a round tripper that answers every request it is given.
type answering struct{ asked int }

func (a *answering) RoundTrip(*http.Request) (*http.Response, error) {
	a.asked++
	return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody}, nil
}

func TestTheGuardKeepsToPublicAddresses(t *testing.T) {
	next := &answering{}
	resolve := func(addrs ...string) func(context.Context, string) ([]netip.Addr, error) {
		return func(context.Context, string) ([]netip.Addr, error) {
			if len(addrs) == 0 {
				return nil, errors.New("no such host")
			}
			var out []netip.Addr
			for _, a := range addrs {
				out = append(out, netip.MustParseAddr(a))
			}
			return out, nil
		}
	}
	never := func(*http.Request) bool { return false }
	always := func(*http.Request) bool { return true }
	for _, c := range []struct {
		name  string
		url   string
		g     guard
		allow bool
	}{
		{"a public name", "https://source.example/", guard{next, resolve("203.0.114.7"), never}, true},
		{"a name of a private address", "https://intranet.example/", guard{next, resolve("203.0.114.7", "10.0.0.3"), never}, false},
		{"a loopback address", "http://127.0.0.1:8080/", guard{next, resolve(), never}, false},
		{"the metadata address", "http://169.254.169.254/", guard{next, resolve(), never}, false},
		{"a public address", "http://[2001:4860::1]/", guard{next, resolve(), never}, true},
		{"a name only the proxy knows", "https://source.example/", guard{next, resolve(), always}, true},
		{"a name nobody knows", "https://source.example/", guard{next, resolve(), never}, false},
	} {
		req := httptest.NewRequest(http.MethodGet, c.url, nil)
		before := next.asked
		_, err := c.g.RoundTrip(req)
		if c.allow != (err == nil) || c.allow != (next.asked > before) {
			t.Errorf("%s: %v", c.name, err)
		}
		if !c.allow && strings.Contains(c.name, "address") && !errors.Is(err, ErrPrivateAddress) {
			t.Errorf("%s: %v is not ErrPrivateAddress", c.name, err)
		}
	}
	r := New(nil, nil)
	defer r.Close()
	r.getenv = func(string) string { return "" }
	if _, ok := r.httpClient().Transport.(guard); !ok {
		t.Error("the client is not guarded")
	}
	r.getenv = allowPrivate(os.Getenv)
	if _, ok := r.httpClient().Transport.(guard); ok {
		t.Error("the operator's permission is not heeded")
	}
	viaProxy(httptest.NewRequest(http.MethodGet, "https://source.example/", nil))
}

func TestUnknownKinds(t *testing.T) {
	if _, err := reader("ftp"); err == nil {
		t.Error("a kind no reader reads")
	}
}
