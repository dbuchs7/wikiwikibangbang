package connector

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/dbuchs7/wikiwikibangbang/internal/site"
)

// pagedSource answers a list of n entries a page at a time, by a page
// number counted from 1 or by an offset, as its parameters ask; it holds
// the total with every page unless total is false, and answers the first
// page for any page if ignore is set.
type pagedSource struct {
	n             int
	total         bool
	ignore        bool
	maxSize       int
	asked         []string
	defaultSize   int
	answerWrapper bool
}

func (p *pagedSource) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p.asked = append(p.asked, r.URL.RawQuery)
	q := r.URL.Query()
	size, _ := strconv.Atoi(q.Get("size"))
	if size == 0 {
		size = p.defaultSize
	}
	size = min(size, p.maxSize)
	from := 0
	if q.Has("page") {
		page, _ := strconv.Atoi(q.Get("page"))
		from = (page - 1) * size
	}
	if q.Has("offset") {
		from, _ = strconv.Atoi(q.Get("offset"))
	}
	if p.ignore {
		from = 0
	}
	entries := []any{}
	for i := from; i < min(from+size, p.n); i++ {
		entries = append(entries, map[string]any{"id": "e" + strconv.Itoa(i), "title": "Entry " + strconv.Itoa(i)})
	}
	answer := map[string]any{"entries": entries}
	if p.total {
		answer["meta"] = map[string]any{"total": p.n}
	}
	json.NewEncoder(w).Encode(answer)
}

func pagedRun(t *testing.T, src *pagedSource, paging *site.Paging) Result {
	t.Helper()
	srv := httptest.NewServer(src)
	t.Cleanup(srv.Close)
	f := setup(t)
	c := site.ConnectorTemplates()[1]
	c.ID, c.Name, c.Space = "paged", "Paged", "API"
	c.JSON.URL, c.JSON.Next, c.JSON.Paging = srv.URL+"/list?page=7&keep=yes", "", paging
	if _, err := f.db.Site.SetConnector("admin", c); err != nil {
		t.Fatal(err)
	}
	res, err := f.r.Run(context.Background(), "paged", true, "")
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func TestAListIsReadByPageNumbers(t *testing.T) {
	// Five full pages and a shorter one: the shorter one ends the list.
	src := &pagedSource{n: 23, maxSize: 100, defaultSize: 50}
	res := pagedRun(t, src, &site.Paging{Kind: site.PagingPage, Param: "page", Start: 1, SizeParam: "size", Size: 4})
	if res.Entries != 23 || len(src.asked) != 6 || src.asked[0] != "keep=yes&page=1&size=4" || src.asked[5] != "keep=yes&page=6&size=4" {
		t.Errorf("%d entries, asked %v", res.Entries, src.asked)
	}
	// Without a size, the source's own; an empty page ends a list whose
	// pages are all full.
	src = &pagedSource{n: 5, maxSize: 100, defaultSize: 2}
	res = pagedRun(t, src, &site.Paging{Kind: site.PagingPage, Param: "page", Start: 1})
	if res.Entries != 5 || len(src.asked) != 3 {
		t.Errorf("%d entries, asked %v", res.Entries, src.asked)
	}
	src = &pagedSource{n: 4, maxSize: 100, defaultSize: 2}
	res = pagedRun(t, src, &site.Paging{Kind: site.PagingPage, Param: "page", Start: 1})
	if res.Entries != 4 || len(src.asked) != 3 {
		t.Errorf("%d entries, asked %v", res.Entries, src.asked)
	}
	// A source that answers fewer than a page's size asks: without a
	// total, the list ends at a page shorter than the others, or empty.
	src = &pagedSource{n: 7, maxSize: 3, defaultSize: 3}
	res = pagedRun(t, src, &site.Paging{Kind: site.PagingOffset, Param: "offset", SizeParam: "size", Size: 10})
	if res.Entries != 7 || len(src.asked) != 3 {
		t.Errorf("capped by offset: %d entries, asked %v", res.Entries, src.asked)
	}
	src = &pagedSource{n: 6, maxSize: 3, defaultSize: 3}
	res = pagedRun(t, src, &site.Paging{Kind: site.PagingOffset, Param: "offset", SizeParam: "size", Size: 10})
	if res.Entries != 6 || len(src.asked) != 3 {
		t.Errorf("capped, ending on a full page: %d entries, asked %v", res.Entries, src.asked)
	}
	// With the total it names.
	src = &pagedSource{n: 7, maxSize: 3, defaultSize: 3, total: true}
	res = pagedRun(t, src, &site.Paging{Kind: site.PagingOffset, Param: "offset", SizeParam: "size", Size: 10, Total: "meta.total"})
	if res.Entries != 7 || len(src.asked) != 3 || src.asked[1] != "keep=yes&offset=3&page=7&size=10" {
		t.Errorf("by offset: %d entries, asked %v", res.Entries, src.asked)
	}
	// A total of the whole list ends it on a full page.
	src = &pagedSource{n: 8, maxSize: 100, defaultSize: 4, total: true}
	res = pagedRun(t, src, &site.Paging{Kind: site.PagingPage, Param: "page", Start: 1, Total: "meta.total"})
	if res.Entries != 8 || len(src.asked) != 2 {
		t.Errorf("with a total: %d entries, asked %v", res.Entries, src.asked)
	}
}

func TestASourceThatIgnoresItsPageIsReadOnce(t *testing.T) {
	src := &pagedSource{n: 50, maxSize: 10, defaultSize: 10, ignore: true}
	res := pagedRun(t, src, &site.Paging{Kind: site.PagingPage, Param: "page", Start: 1})
	if res.Entries != 10 || len(src.asked) != 2 || !strings.Contains(strings.Join(res.Problems, "\n"), "only entries already read") {
		t.Errorf("%d entries, asked %v, problems %v", res.Entries, src.asked, res.Problems)
	}
}

func TestPageAddresses(t *testing.T) {
	j := &site.JSONSource{URL: "https://x.example/l?a=1", Paging: &site.Paging{Kind: site.PagingOffset, Param: "$skip", Start: 5, SizeParam: "$top", Size: 20}}
	if got := pageAddress(j, 3, 60); got != "https://x.example/l?%24skip=65&%24top=20&a=1" {
		t.Errorf("offset %s", got)
	}
	j.URL = "%zz"
	if pageAddress(j, 0, 0) != "" {
		t.Error("a bad address")
	}
	if total(map[string]any{"n": "many"}, "n") != -1 || total(map[string]any{"n": json.Number("12")}, "n") != 12 {
		t.Error("totals")
	}
}
