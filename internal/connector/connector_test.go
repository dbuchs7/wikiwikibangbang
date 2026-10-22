package connector

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/dbuchs7/wikiwikibangbang/internal/audit"
	"github.com/dbuchs7/wikiwikibangbang/internal/auth"
	"github.com/dbuchs7/wikiwikibangbang/internal/item"
	"github.com/dbuchs7/wikiwikibangbang/internal/site"
	"github.com/dbuchs7/wikiwikibangbang/internal/storage"
)

// The data sets as the portal might answer them, in the shapes its fields
// take: languages as an object or as text holding one, one or several
// interfaces per data set, formats as publishers write them.
var weather = `{"id":"d1","name":"wetterdaten","title":{"de":"Wetterdaten","fr":"Données météo"},
	"organization":{"name":"meteoschweiz","title":{"de":"MeteoSchweiz","fr":"MétéoSuisse"}},
	"contact_points":[{"name":"API-Team","email":"api@meteo.example"}],"modified":"2026-05-01T08:00:00",
	"resources":[{"id":"r1","format":"API","url":"https://api.meteo.example/v1","documentation":["https://api.meteo.example/docs"]},
		{"id":"r2","format":"CSV","url":"https://meteo.example/data.csv"}]}`

var geo = `{"id":"d2","name":"geodaten","title":"{\"de\": \"Geodaten\", \"en\": \"Geodata\"}",
	"organization":{"name":"swisstopo","title":"{\"fr\": \"swisstopo FR\"}"},
	"publisher":"{\"name\": {\"de\": \"Bundesamt für Landestopografie\"}, \"url\": \"https://swisstopo.example\"}",
	"documentation":"[\"https://geo.example/manual\"]","modified":"01.03.2026",
	"resources":[{"id":"r3","format":"WMS","title":{"de":"Karten"},"url":"https://wms.geo.example"},
		{"id":"r4","format":"wfs_srvc","url":"not a url","download_url":"https://wfs.geo.example"}]}`

var transport = `{"id":"d3","name":"fahrplan","title":"Fahrplan","organization":null,"publisher":{"name":"SBB"},
	"contact_points":{"name":"","email":"open@sbb.example"},
	"resources":[{"id":"r5","format":"rest","url":"https://transport.example/api"},{"format":"API","url":"https://x.example"}]}`

// fakeCKAN answers package_search in pages of size rows from datasets.
type fakeCKAN struct {
	mu       sync.Mutex
	datasets []string
	fail     bool
	queries  []string
}

func (f *fakeCKAN) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.URL.Path != "/api/3/action/package_search" {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	f.queries = append(f.queries, r.URL.Query().Get("fq"))
	if f.fail {
		w.WriteHeader(http.StatusConflict)
		fmt.Fprint(w, `{"success":false,"error":{"message":"Search error"}}`)
		return
	}
	// Like the portal's search, formats are compared exactly.
	fq := r.URL.Query().Get("fq")
	var matching []string
	facets := map[string]int{}
	for _, raw := range f.datasets {
		var d struct {
			Resources []struct {
				Format string `json:"format"`
			} `json:"resources"`
		}
		json.Unmarshal([]byte(raw), &d)
		seen, hit := map[string]bool{}, !strings.Contains(fq, "res_format")
		for _, res := range d.Resources {
			if res.Format != "" && !seen[res.Format] {
				seen[res.Format] = true
				facets[res.Format]++
			}
			hit = hit || strings.Contains(fq, strconv.Quote(res.Format))
		}
		if hit {
			matching = append(matching, raw)
		}
	}
	if r.URL.Query().Get("rows") == "0" {
		data, _ := json.Marshal(facets)
		fmt.Fprintf(w, `{"success":true,"result":{"count":%d,"results":[],"facets":{"res_format":%s}}}`, len(matching), data)
		return
	}
	start, _ := strconv.Atoi(r.URL.Query().Get("start"))
	rows, _ := strconv.Atoi(r.URL.Query().Get("rows"))
	end := min(start+rows, len(matching))
	page := []string{}
	if start < len(matching) {
		page = matching[start:end]
	}
	fmt.Fprintf(w, `{"success":true,"result":{"count":%d,"results":[%s]}}`, len(matching), strings.Join(page, ","))
}

func TestEntriesReadTheShapesOfThePortal(t *testing.T) {
	var ds []Dataset
	for _, raw := range []string{weather, geo, transport} {
		var d Dataset
		if err := json.Unmarshal([]byte(raw), &d); err != nil {
			t.Fatal(err)
		}
		ds = append(ds, d)
	}
	entries, problems := Entries(ds, []string{"api", "WMS", "WFS"}, "https://opendata.swiss/de/dataset/", "de", "opendata.swiss")
	if len(entries) != 3 || len(problems) != 0 {
		t.Fatalf("entries %+v\nproblems %v", entries, problems)
	}
	want := []Entry{
		{SourceID: "d1", Dataset: "wetterdaten", Title: "Wetterdaten",
			Publisher: "MeteoSchweiz", Contact: "API-Team <api@meteo.example>", DatasetURL: "https://opendata.swiss/de/dataset/wetterdaten"},
		{SourceID: "d2", Dataset: "geodaten", Title: "Geodaten",
			Publisher: "swisstopo FR", DatasetURL: "https://opendata.swiss/de/dataset/geodaten"},
		{SourceID: "d3", Dataset: "fahrplan", Title: "Fahrplan",
			Publisher: "SBB", Contact: "open@sbb.example", DatasetURL: "https://opendata.swiss/de/dataset/fahrplan"},
	}
	for i := range want {
		got := entries[i]
		got.Body = nil
		if !reflect.DeepEqual(got, want[i]) {
			t.Errorf("entry %d\n got %+v\nwant %+v", i, got, want[i])
		}
		for j, b := range entries[i].Body {
			if err := item.CheckBlock(b, "x"); err != nil {
				t.Errorf("entry %d block %d %+v: %v", i, j, b, err)
			}
		}
	}
	text := pageText(entries[1].Body)
	for _, part := range []string{"wird bei jedem Abgleich aus opendata.swiss geschrieben", "Schnittstellen", "Karten", "Format\tWMS",
		"Download: https://wfs.geo.example", "Dokumentation: https://geo.example/manual", "Datensatz in der Quelle: https://opendata.swiss/de/dataset/geodaten"} {
		if !strings.Contains(text, part) {
			t.Errorf("the page of geodaten lacks %q:\n%s", part, text)
		}
	}
	if !strings.Contains(pageText(entries[0].Body), "Weitere Ressourcen") || !strings.Contains(pageText(entries[0].Body), "(CSV): https://meteo.example/data.csv") {
		t.Errorf("the other resources of wetterdaten:\n%s", pageText(entries[0].Body))
	}
	var pub Text
	json.Unmarshal([]byte(`{"name":"Only a name","url":"https://x"}`), &pub)
	if pub.Pick() != "Only a name" {
		t.Errorf("a publisher with a plain name: %v", pub)
	}
	var none Text
	json.Unmarshal([]byte(`42`), &none)
	if none.Pick() != "" {
		t.Errorf("a number as text: %v", none)
	}
	var links Links
	json.Unmarshal([]byte(`"https://one.example"`), &links)
	if len(links) != 1 {
		t.Errorf("one link as text: %v", links)
	}
	json.Unmarshal([]byte(`42`), &links)
	if len(links) != 0 {
		t.Errorf("a number as links: %v", links)
	}
	if !parseDate("garbage").IsZero() || parseDate("2026-01-02T03:04:05Z").Day() != 2 {
		t.Error("dates")
	}
}

// pageText is a page's blocks as text, one block per line.
func pageText(blocks []item.Block) string {
	var lines []string
	for _, b := range blocks {
		lines = append(lines, b.Text)
	}
	return strings.Join(lines, "\n")
}

// Everything the source says of a data set is on its page: the fields
// read by name, in the page's language, and every other field of the
// answer under further details.
func TestThePageShowsAllTheSourceSays(t *testing.T) {
	raw := `{"id":"d7","name":"luft","title":{"de":"Luft","fr":"Air"},"description":{"de":"Messwerte.\n\nStündlich.","fr":"Mesures."},
		"keywords":{"de":["Luft","Ozon"],"fr":["air"]},"groups":[{"name":"envi","title":{"de":"Umwelt"}}],"license_title":"Open use",
		"temporals":[{"start_date":"2020-01-01T00:00:00","end_date":"2025-12-31"}],"accrual_periodicity":"http://publications.europa.eu/resource/authority/frequency/HOURLY",
		"language":["de","fr"],"spatial":"Schweiz","identifier":"luft@bafu","url":"https://bafu.example/luft","issued":"2020-02-01",
		"relations":[{"label":{"de":"Methode"},"url":"https://bafu.example/methode"}],"tags":[{"name":"ozon"}],
		"coverage":"Nur Messnetz","see_alsos":[],"qualified":{"x":1},"count":3,"flag":true,"empty":"","nothing":null,
		"resources":[{"id":"r7","format":"API","title":{"de":"Messwerte-API"},"description":{"de":"JSON über HTTPS"},"media_type":"application/json",
			"url":"https://api.bafu.example","access_url":"https://api.bafu.example","download_url":"","byte_size":1024,"rights":{"de":"Frei"},
			"license":"CC BY","language":"de","issued":"2020-02-01","modified":"2026-01-05","documentation":["https://api.bafu.example/doc"],
			"access_services":"[\"https://bafu.example/service\"]"},
			{"id":"r8","format":"CSV","title":"Archiv","url":"https://bafu.example/archiv.csv"}]}`
	var d Dataset
	if err := json.Unmarshal([]byte(raw), &d); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(`[1,2]`), &d); err == nil {
		t.Error("a list as a data set")
	}
	for _, lang := range []string{"de", "fr", "en", "it"} {
		entries, _ := Entries([]Dataset{d}, []string{"API"}, "https://portal.example/", lang, "Portal")
		for i, b := range entries[0].Body {
			if err := item.CheckBlock(b, "x"); err != nil {
				t.Fatalf("%s block %d %+v: %v", lang, i, b, err)
			}
		}
	}
	entries, _ := Entries([]Dataset{d}, []string{"API"}, "https://portal.example/", "de", "Portal")
	text := pageText(entries[0].Body)
	for _, part := range []string{"Messwerte.", "Stündlich.", "Messwerte-API", "JSON über HTTPS", "Medientyp\tapplication/json", "Grösse in Bytes\t1024",
		"Nutzungsbedingungen\tFrei", "Lizenz\tCC BY", "Sprachen\tde", "Geändert\t2026-01-05", "Zugangsdienst: https://bafu.example/service",
		"Stichwörter\tLuft, Ozon, ozon", "Themen\tUmwelt", "Lizenz\tOpen use", "Zeitraum\t2020-01-01 – 2025-12-31", "Aktualisierung\tHOURLY",
		"Sprachen\tde, fr", "Raum\tSchweiz", "Kennung\tluft@bafu", "Einstiegsseite: https://bafu.example/luft", "Methode: https://bafu.example/methode",
		"Archiv (CSV): https://bafu.example/archiv.csv", "Weitere Angaben der Quelle", "coverage\tNur Messnetz", "qualified\t{\"x\":1}",
		"count\t3", "flag\ttrue"} {
		if !strings.Contains(text, part) {
			t.Errorf("the page lacks %q:\n%s", part, text)
		}
	}
	for _, absent := range []string{"see_alsos", "empty\t", "nothing", "Zugang: https://api.bafu.example"} {
		if strings.Contains(text, absent) {
			t.Errorf("the page shows %q:\n%s", absent, text)
		}
	}
	if fr := pageText(func() []item.Block {
		e, _ := Entries([]Dataset{d}, []string{"API"}, "", "fr", "Portal")
		return e[0].Body
	}()); !strings.Contains(fr, "Mesures.") || !strings.Contains(fr, "Interfaces") {
		t.Errorf("in French, the source's text and English words:\n%s", fr)
	}
	long := strings.Repeat("x", maxCell+10)
	if got := oneLine(long); utf8.RuneCountInString(got) != maxCell {
		t.Errorf("a long value is %d characters", utf8.RuneCountInString(got))
	}
	if lastSegment("plain") != "plain" || day("soon") != "soon" {
		t.Error("values that are no address or date")
	}
	var w Words
	json.Unmarshal([]byte(`42`), &w)
	var st Strings
	json.Unmarshal([]byte(`42`), &st)
	var sc Scalar
	json.Unmarshal([]byte(`{}`), &sc)
	if len(w) != 0 || st != nil || sc != "" {
		t.Errorf("values of no shape: %v %v %q", w, st, sc)
	}
}

// id is the connector the tests run.
const id = site.OpenDataSwiss

type fixture struct {
	db    *storage.DB
	cfg   site.Connector
	ckan  *fakeCKAN
	r     *Runner
	clock time.Time
}

func setup(t *testing.T) *fixture {
	t.Helper()
	db, _, err := storage.OpenWith(t.TempDir(), storage.Options{Auth: []auth.Option{auth.WithIterations(1000)}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.Write(func(s *item.Store) error {
		if err := s.CreateSpace("admin", "API", "Interfaces"); err != nil {
			return err
		}
		// The steward watches new pages, so may read the space.
		return s.GrantRole("admin", "API", "anna", item.Viewer)
	}); err != nil {
		t.Fatal(err)
	}
	f := &fixture{db: db, ckan: &fakeCKAN{datasets: []string{weather, geo, transport}}, clock: time.Date(2026, 10, 21, 8, 0, 0, 0, time.UTC)}
	srv := httptest.NewServer(f.ckan)
	t.Cleanup(srv.Close)
	cfg := site.ConnectorTemplates()[0]
	cfg.Enabled, cfg.Space, cfg.CKAN.BaseURL, cfg.Steward = true, "API", srv.URL+"/api/3/action", "anna"
	if f.cfg, err = db.Site.SetConnector("admin", cfg); err != nil {
		t.Fatal(err)
	}
	f.r = New(db, nil)
	f.r.now = func() time.Time { return f.clock }
	t.Cleanup(f.r.Close)
	return f
}

func (f *fixture) items(t *testing.T) map[string]item.Item {
	t.Helper()
	out := map[string]item.Item{}
	f.db.Read(func(s *item.Store) {
		for _, it := range s.Find(item.Query{Space: "API", Type: PageType}) {
			out[strings.TrimPrefix(it.Origin.ExternalID, "page/")] = it
		}
	})
	return out
}

// A run writes a page per data set; the next one changes only what
// changed, keeps comments and labels added here, and marks the pages the
// source no longer publishes. A dry run writes nothing (ADR-0093).
func TestTheCatalogFollowsTheSource(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	dry, err := f.r.Run(ctx, id, true, "admin")
	if err != nil || dry.New != 3 || dry.Datasets != 3 || len(f.items(t)) != 0 || len(dry.Examples) != 3 {
		t.Fatalf("dry run %+v %v", dry, err)
	}
	if f.ckan.queries[0] != "" || !strings.Contains(f.ckan.queries[1], `"wfs_srvc"`) || !strings.Contains(f.ckan.queries[1], `"rest"`) {
		t.Errorf("queries %q", f.ckan.queries)
	}
	if !slices.Equal(dry.Formats, []string{"API", "WMS", "rest", "wfs_srvc"}) {
		t.Errorf("formats read %v", dry.Formats)
	}
	res, err := f.r.Run(ctx, id, false, "admin")
	if err != nil || res.New != 3 || res.Changed != 0 || len(res.Problems) != 0 {
		t.Fatalf("first run %+v %v", res, err)
	}
	if data, _ := json.Marshal(res); !strings.Contains(string(data), `"problems":[`) || !strings.Contains(string(data), `"examples":[`) {
		t.Errorf("lists as null: %s", data)
	}
	got := f.items(t)
	d1 := got["d1"]
	if d1.Title != "Wetterdaten" || d1.Type != PageType || d1.State != "" || len(d1.Properties) != 0 ||
		d1.Origin == nil || d1.Origin.URL != "https://opendata.swiss/de/dataset/wetterdaten" || d1.Origin.System != id || d1.Origin.ExternalID != "page/d1" {
		t.Fatalf("page d1 %+v %+v", d1, d1.Origin)
	}
	if text := pageText(d1.Body.Blocks); !strings.Contains(text, "https://api.meteo.example/v1") {
		t.Fatalf("the page of d1:\n%s", text)
	}
	f.db.Read(func(s *item.Store) {
		if !s.As("anna").Watching(d1.ID) {
			t.Error("the steward does not watch the new page")
		}
	})

	// Here someone comments on d1 and writes into its page, and labels
	// d2; the source changes d1's address and a resource of fahrplan.
	f.db.Write(func(s *item.Store) error {
		s.AddComment("bert", d1.ID, "Checked with the team")
		s.CommitBody("bert", d1.ID, item.Doc{Blocks: []item.Block{{Kind: item.Paragraph, Text: "my notes"}}})
		return s.SetLabels("bert", got["d2"].ID, []string{"important"})
	})
	f.ckan.datasets = []string{strings.Replace(weather, "api.meteo.example/v1", "api.meteo.example/v2", 1), geo,
		strings.Replace(transport, `"id":"r5"`, `"id":"r6"`, 1)}
	res, err = f.r.Run(ctx, id, false, "")
	if err != nil || res.New != 0 || res.Changed != 2 || res.Unchanged != 1 || res.Withdrawn != 0 {
		t.Fatalf("second run %+v %v", res, err)
	}
	got = f.items(t)
	if text := pageText(got["d1"].Body.Blocks); strings.Contains(text, "my notes") || !strings.Contains(text, "api.meteo.example/v2") {
		t.Errorf("the page of d1 is not the source's:\n%s", text)
	}
	if len(got["d1"].Comments) != 1 {
		t.Errorf("the comment on d1 went: %+v", got["d1"].Comments)
	}
	if res, _ := f.r.Run(ctx, id, false, ""); res.New+res.Changed+res.Withdrawn != 0 || res.Unchanged != 3 {
		t.Errorf("a run without changes %+v", res)
	}
	// A data set without interfaces any more is marked withdrawn, with
	// its labels kept, and comes back.
	f.ckan.datasets = []string{weather, strings.ReplaceAll(geo, "wfs_srvc", "zip"), transport}
	f.ckan.datasets[1] = strings.Replace(f.ckan.datasets[1], `"format":"WMS"`, `"format":"PDF"`, 1)
	res, _ = f.r.Run(ctx, id, false, "")
	d2 := f.items(t)["d2"]
	if res.Withdrawn != 1 || !slices.Equal(d2.Labels, []string{"important", WithdrawnLabel}) ||
		d2.Body.Blocks[0].Kind != item.Panel || !strings.Contains(d2.Body.Blocks[0].Text, "publiziert diesen Datensatz nicht mehr") {
		t.Errorf("geodaten without interfaces %+v %v %+v", res, d2.Labels, d2.Body.Blocks[0])
	}
	if res, _ := f.r.Run(ctx, id, false, ""); res.Withdrawn != 0 || res.Unchanged != 2 {
		t.Errorf("a withdrawn page is marked once %+v", res)
	}
	f.ckan.datasets = []string{weather, geo, transport}
	res, _ = f.r.Run(ctx, id, false, "")
	d2 = f.items(t)["d2"]
	if res.Changed != 1 || res.Unchanged != 2 || !slices.Equal(d2.Labels, []string{"important"}) || strings.Contains(pageText(d2.Body.Blocks), "nicht mehr") {
		t.Errorf("geodaten back %+v %v", res, d2.Labels)
	}

	// A source that answers far fewer entries withdraws nothing.
	f.ckan.datasets = []string{transport}
	res, _ = f.r.Run(ctx, id, false, "")
	if res.Withdrawn != 0 || !slices.ContainsFunc(res.Problems, func(p string) bool { return strings.Contains(p, "nothing is withdrawn") }) {
		t.Errorf("a short answer %+v", res)
	}
	synced := 0
	for _, e := range f.db.Audit.List(0, 100) {
		if e.Event == audit.CatalogSynced {
			synced++
		}
	}
	if synced != 7 {
		t.Errorf("%d runs audited", synced)
	}
	if st := f.r.Status(id); len(st.History) != 8 || !st.History[7].DryRun || st.Running {
		t.Errorf("status %+v", st)
	}
}

// The entries of before ADR-0093, with fields and a workflow, are erased
// and pages written in their place; one that something points to stays,
// and the run says so. A dry run only counts them.
func TestOldEntriesGiveWayToPages(t *testing.T) {
	f := setup(t)
	old := func(external, title string) item.Upsert {
		return item.Upsert{Origin: item.Origin{System: id, Instance: f.cfg.CKAN.BaseURL, ExternalID: external}, Space: "API", Type: LegacyType, Title: title}
	}
	var kept item.Item
	f.db.Write(func(s *item.Store) error {
		if err := s.DefineType("admin", item.TypeDef{Name: LegacyType, Body: true}); err != nil {
			return err
		}
		for _, u := range []item.Upsert{old("d1", "Wetterdaten"), old("r3", "Geodaten – Karten"), old("r9", "Gone")} {
			if _, _, err := s.Upsert("sync:"+id, u); err != nil {
				return err
			}
		}
		var err error
		kept, _, err = s.Upsert("sync:"+id, old("d3", "Fahrplan"))
		if err != nil {
			return err
		}
		note, err := s.Create("bert", item.NewItem{Space: "API", Type: LegacyType, Title: "Notes"})
		if err != nil {
			return err
		}
		return s.AddRelation("bert", "relates-to", note.ID, kept.ID)
	})
	dry, err := f.r.Run(context.Background(), id, true, "")
	if err != nil || dry.Removed != 4 || dry.New != 3 {
		t.Fatalf("dry run %+v %v", dry, err)
	}
	res, err := f.r.Run(context.Background(), id, false, "")
	if err != nil || res.Removed != 3 || res.New != 3 || len(f.items(t)) != 3 {
		t.Fatalf("the change to pages %+v %v", res, err)
	}
	if !slices.ContainsFunc(res.Problems, func(p string) bool { return strings.Contains(p, kept.Key+" is not removed") }) {
		t.Errorf("problems %v", res.Problems)
	}
	f.db.Read(func(s *item.Store) {
		left := s.Find(item.Query{Space: "API", Type: LegacyType})
		if len(left) != 2 {
			t.Errorf("old entries left %d", len(left))
		}
	})
}

func TestRunsThatCannotWork(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	f.ckan.fail = true
	if res, err := f.r.Run(ctx, id, false, ""); err == nil || !strings.Contains(res.Error, "Search error") {
		t.Errorf("a failing source: %+v %v", res, err)
	}
	f.ckan.fail = false
	cfg := f.cfg
	cfg.Space = "NOPE"
	f.db.Site.SetConnector("admin", cfg)
	if _, err := f.r.Run(ctx, id, false, ""); !errors.Is(err, ErrNoSpace) {
		t.Errorf("no space: %v", err)
	}
	cfg.Space = "API"
	f.db.Site.SetConnector("admin", cfg)
	f.db.Write(func(s *item.Store) error {
		return s.DefineType("admin", item.TypeDef{Name: PageType})
	})
	if _, err := f.r.Run(ctx, id, false, ""); err == nil || !strings.Contains(err.Error(), "no text") {
		t.Errorf("a type that does not fit: %v", err)
	}
	if err := fits(item.TypeDef{Name: PageType, Body: true, Fields: []item.FieldDef{{Name: "owner", Kind: item.KindUser, Required: true}}}); err == nil {
		t.Error("fits a type with a field to fill")
	}
	if err := fits(PageTypeDef()); err != nil {
		t.Errorf("does not fit its own type: %v", err)
	}
	if NewClient("http://127.0.0.1:1", "").HTTP == nil {
		t.Error("no client")
	}
	if _, err := NewClient("http://127.0.0.1:1", "").Datasets(ctx, []string{"API"}, []string{"bfs"}); err == nil {
		t.Error("an unreachable source")
	}
	if q := query([]string{"API"}, []string{"bfs", "bafu"}); q != `res_format:("API") AND organization:("bfs" OR "bafu")` {
		t.Errorf("query %s", q)
	}
	if ds, err := NewClient("http://127.0.0.1:1", "").Datasets(ctx, nil, nil); ds != nil || err != nil {
		t.Errorf("no formats: %v %v", ds, err)
	}

	// No format of the list in the source: the run says which there are.
	cfg.CKAN.Formats = []string{"SOAP"}
	f.db.Site.SetConnector("admin", cfg)
	res, err := f.r.Run(ctx, id, true, "")
	if err != nil || res.Entries != 0 || len(res.Problems) != 1 || !strings.Contains(res.Problems[0], "API (2)") {
		t.Errorf("no matching format: %+v %v", res, err)
	}
	formats, err := f.r.SourceFormats(ctx, id, cfg.CKAN.BaseURL, nil)
	if err != nil || len(formats) != 5 || formats[0] != (FormatCount{"API", 2}) {
		t.Errorf("source formats %v %v", formats, err)
	}
	if got := describe(formats, 2); !strings.Contains(got, "and 3 more") {
		t.Errorf("describe %s", got)
	}
	if describe(nil, 3) != "none" {
		t.Error("describe of nothing")
	}
}

// The schedule runs when due; one run at a time; Close waits.
func TestTheSchedule(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	f.r.Tick(ctx)
	if len(f.items(t)) != 3 {
		t.Fatal("the first tick did not run")
	}
	f.ckan.datasets = append(f.ckan.datasets, strings.ReplaceAll(weather, `"r1"`, `"r9"`))
	f.ckan.datasets[3] = strings.ReplaceAll(f.ckan.datasets[3], `"r2"`, `"r10"`)
	f.ckan.datasets[3] = strings.ReplaceAll(f.ckan.datasets[3], `"d1"`, `"d9"`)
	f.clock = f.clock.Add(time.Hour)
	f.r.Tick(ctx)
	if len(f.items(t)) != 3 {
		t.Fatal("ran before it was due")
	}
	if st := f.r.Status(id); !st.Next.Equal(f.clock.Add(23 * time.Hour)) {
		t.Errorf("next %v", st.Next)
	}
	f.clock = f.clock.Add(23 * time.Hour)
	f.r.Tick(ctx)
	if len(f.items(t)) != 4 {
		t.Fatal("did not run when due")
	}
	if err := f.r.Start(id, true, "admin"); err != nil {
		t.Fatal(err)
	}
	f.r.Close()
	if st := f.r.Status(id); st.Running || !st.History[0].DryRun {
		t.Errorf("after Close %+v", st)
	}
	cfg := f.cfg
	cfg.Enabled = false
	f.db.Site.SetConnector("admin", cfg)
	if st := f.r.Status(id); !st.Next.IsZero() {
		t.Errorf("next when off %v", st.Next)
	}
	ctx2, cancel := context.WithCancel(ctx)
	cancel()
	f.r.Loop(ctx2)
}

// Formats reads facets in either shape CKAN answers them, and Match
// takes the source's spelling for the wanted formats.
func TestFormatsAndMatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("fq") == `organization:("fail")` {
			fmt.Fprint(w, `{"success":false}`)
			return
		}
		fmt.Fprint(w, `{"success":true,"result":{"search_facets":{"res_format":{"items":[{"name":"WMS-Dienst","count":7},{"name":"api","count":3},{"name":"","count":9}]}}}}`)
	}))
	defer srv.Close()
	c := NewClient(srv.URL, "")
	got, err := c.Formats(context.Background(), nil)
	if err != nil || len(got) != 2 || got[0] != (FormatCount{"WMS-Dienst", 7}) {
		t.Fatalf("formats %v %v", got, err)
	}
	if m := Match(got, []string{"API", "WMS-DIENST", "WFS"}); !slices.Equal(m, []string{"WMS-Dienst", "api"}) {
		t.Errorf("match %v", m)
	}
	if _, err := c.Formats(context.Background(), []string{"fail"}); err == nil {
		t.Error("a failing source")
	}
}

// When the source's data do not fit its own facets, the run says what it
// saw instead of reporting nothing.
func TestRunsReportWhatTheSourceAnswered(t *testing.T) {
	f := setup(t)
	answer := `{"success":true,"result":{"count":1,"results":[{"id":"d1","name":"karten","resources":[{"id":"r1","format":"Dienst"},{"id":"r2"}]}]}}`
	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		if r.URL.Query().Get("rows") == "0" {
			fmt.Fprint(w, `{"success":true,"result":{"facets":{"res_format":{"WMS":3}}}}`)
			return
		}
		fmt.Fprint(w, answer)
	}))
	defer srv.Close()
	cfg := f.cfg
	cfg.CKAN.BaseURL = srv.URL
	f.db.Site.SetConnector("admin", cfg)
	f.r.getenv = func(k string) string {
		if k == "WWBB_CONNECTOR_OPENDATA_SWISS_TOKEN" {
			return "secret-token"
		}
		return ""
	}
	res, err := f.r.Run(context.Background(), id, true, "")
	if err != nil || res.Datasets != 1 || res.Entries != 0 || len(res.Problems) != 1 ||
		!strings.Contains(res.Problems[0], "Dienst (1)") || !strings.Contains(res.Problems[0], "(no format) (1)") {
		t.Fatalf("resources without the format: %+v %v", res, err)
	}
	if auth != "secret-token" {
		t.Errorf("the credential was not sent: %q", auth)
	}
	if st := f.r.Status(id); !st.Credential || st.CredentialVar != "WWBB_CONNECTOR_OPENDATA_SWISS_TOKEN" {
		t.Errorf("status of the credential %+v", st)
	}
	answer = `{"success":true,"result":{"count":0,"results":[]}}`
	res, err = f.r.Run(context.Background(), id, true, "")
	if err != nil || len(res.Problems) != 1 || !strings.Contains(res.Problems[0], "counts 3 data sets") {
		t.Errorf("a search that answers none: %+v %v", res, err)
	}
}

// Connectors run independently, each one at a time; a removed one is
// forgotten.
func TestSeveralConnectors(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	if _, err := f.r.Run(ctx, "nope", false, ""); !errors.Is(err, ErrUnknown) {
		t.Errorf("an unknown connector: %v", err)
	}
	if err := f.r.Start("nope", false, ""); !errors.Is(err, ErrUnknown) {
		t.Errorf("starting an unknown connector: %v", err)
	}
	f.db.Write(func(s *item.Store) error { return s.CreateSpace("admin", "API2", "More interfaces") })
	other := f.cfg
	other.ID, other.Name, other.Enabled, other.Space = "second", "Second", false, "API2"
	if _, err := f.db.Site.SetConnector("admin", other); err != nil {
		t.Fatal(err)
	}
	f.r.Tick(ctx)
	if st := f.r.Status("second"); len(st.History) != 0 || !st.Next.IsZero() {
		t.Errorf("a connector switched off ran: %+v", st)
	}
	res, err := f.r.Run(ctx, "second", false, "admin")
	if err != nil || res.New != 3 || res.Connector != "second" {
		t.Fatalf("the second connector %+v %v", res, err)
	}
	// The first connector (which the tick ran) leaves the second's
	// entries alone.
	if res, err := f.r.Run(ctx, id, false, ""); err != nil || res.Unchanged != 3 || res.Withdrawn != 0 {
		t.Errorf("the first connector %+v %v", res, err)
	}
	n := 0
	f.db.Read(func(s *item.Store) { n = len(s.Find(item.Query{Space: "API2", Type: PageType})) })
	if n != 3 || len(f.items(t)) != 3 {
		t.Errorf("%d and %d entries", n, len(f.items(t)))
	}
	f.r.mu.Lock()
	f.r.of("second").running = true
	f.r.mu.Unlock()
	if _, err := f.r.Run(ctx, "second", false, ""); !errors.Is(err, ErrBusy) {
		t.Errorf("a second run at once: %v", err)
	}
	f.r.Forget("second")
	if st := f.r.Status("second"); !st.Running {
		t.Error("a running connector was forgotten")
	}
	f.r.mu.Lock()
	f.r.of("second").running = false
	f.r.mu.Unlock()
	f.r.Forget("second")
	if st := f.r.Status("second"); len(st.History) != 0 {
		t.Errorf("not forgotten: %+v", st)
	}
	if TokenVar("geo.admin-ch") != "WWBB_CONNECTOR_GEO_ADMIN_CH_TOKEN" {
		t.Errorf("token variable %s", TokenVar("geo.admin-ch"))
	}
	f.db.Site.SetConnector("admin", site.Connector{ID: "odd", Name: "Odd", Kind: site.KindCKAN, IntervalHours: 1, CKAN: other.CKAN})
	f.r.mu.Lock()
	f.r.state["odd"] = &state{}
	f.r.mu.Unlock()
	odd, _ := f.db.Site.Connector("odd")
	odd.Kind = "other"
	if res, err := f.r.run(ctx, odd, true, ""); err == nil || res.Error == "" {
		t.Errorf("an unknown kind: %+v %v", res, err)
	}
}

// Publishers the source does not know are named in the report: all of
// them unknown, the run reads nothing; some, it reads the others.
func TestUnknownPublishersAreReported(t *testing.T) {
	f := setup(t)
	var queries []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/organization_list") {
			fmt.Fprint(w, `{"success":true,"result":["meteoschweiz","swisstopo"]}`)
			return
		}
		queries = append(queries, r.URL.Query().Get("fq"))
		f.ckan.ServeHTTP(w, r)
	}))
	defer srv.Close()
	cfg := f.cfg
	cfg.CKAN.BaseURL = srv.URL + "/api/3/action"
	cfg.CKAN.Organizations = []string{"schweizerische", "eidgenossenschaft"}
	f.db.Site.SetConnector("admin", cfg)
	ctx := context.Background()
	res, err := f.r.Run(ctx, id, true, "")
	if err != nil || res.Datasets != 0 || len(queries) != 0 || len(res.Problems) != 1 ||
		!strings.Contains(res.Problems[0], "no organization schweizerische, eidgenossenschaft") {
		t.Fatalf("all unknown: %+v %v %q", res, err, queries)
	}
	if _, err := f.r.SourceFormats(ctx, id, cfg.CKAN.BaseURL, cfg.CKAN.Organizations); !errors.Is(err, ErrUnknownOrganization) {
		t.Errorf("formats of unknown publishers: %v", err)
	}
	cfg.CKAN.Organizations = []string{"meteoschweiz", "bfs"}
	f.db.Site.SetConnector("admin", cfg)
	res, err = f.r.Run(ctx, id, true, "")
	if err != nil || len(queries) == 0 || !slices.ContainsFunc(res.Problems, func(p string) bool { return strings.Contains(p, "no organization bfs") }) {
		t.Errorf("one unknown: %+v %v", res, err)
	}
	if got, err := NewClient(srv.URL+"/api/3/action", "").UnknownOrganizations(ctx, []string{"swisstopo"}); err != nil || got != nil {
		t.Errorf("a known one: %v %v", got, err)
	}
	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `{"success":false}`) }))
	defer failing.Close()
	if _, err := NewClient(failing.URL, "").UnknownOrganizations(ctx, []string{"x"}); err == nil {
		t.Error("a source that refuses the list")
	}
}
