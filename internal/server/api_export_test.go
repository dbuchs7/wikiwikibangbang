package server

import (
	"archive/zip"
	"bytes"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/dbuchs7/wikiwikibangbang/internal/budget"
)

func TestExportsOfPagesAndLists(t *testing.T) {
	w := newWorld(t)
	created := w.bert.expect("POST", "/api/items", `{"space":"PLAT","type":"task","title":"Plan Ä","properties":{"priority":"high","estimate":3}}`, http.StatusCreated)
	key := field(created, "item", "key").(string)
	w.bert.expect("PUT", "/api/items/"+key+"/body", `{"blocks":[{"kind":"heading","level":1,"text":"Overview"},`+
		`{"kind":"table","text":"Name\tCost\n=SUM(A1)\t5"},{"kind":"paragraph","text":"after"},{"kind":"table","text":"x\ty"}]}`, http.StatusOK)

	// The page as a Word document.
	rec := w.bert.do("GET", "/api/items/"+key+"/export/docx", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Header().Get("Content-Disposition"), "attachment") ||
		!strings.Contains(rec.Header().Get("Content-Type"), "wordprocessingml") {
		t.Fatalf("docx: %d %v", rec.Code, rec.Header())
	}
	z, err := zip.NewReader(bytes.NewReader(rec.Body.Bytes()), int64(rec.Body.Len()))
	if err != nil {
		t.Fatal(err)
	}
	var doc string
	for _, f := range z.File {
		if f.Name == "word/document.xml" {
			r, _ := f.Open()
			data, _ := io.ReadAll(r)
			doc = string(data)
		}
	}
	for _, want := range []string{"Plan Ä", "Overview", "priority", "high", key} {
		if !strings.Contains(doc, want) {
			t.Errorf("the document lacks %q", want)
		}
	}

	// A table of the page as CSV, safe for spreadsheets.
	rec = w.bert.do("GET", "/api/items/"+key+"/tables/1", "")
	if rec.Code != http.StatusOK || rec.Body.String() != "\xef\xbb\xbfName,Cost\r\n'=SUM(A1),5\r\n" {
		t.Fatalf("table 1: %d %q", rec.Code, rec.Body)
	}
	w.bert.expect("GET", "/api/items/"+key+"/tables/3", "", http.StatusNotFound)
	w.bert.expect("GET", "/api/items/"+key+"/tables/x", "", http.StatusBadRequest)
	// Someone who may not read the item gets nothing (D10).
	w.carla.expect("GET", "/api/items/"+key+"/export/docx", "", http.StatusNotFound)
	w.carla.expect("GET", "/api/items/"+key+"/tables/1", "", http.StatusNotFound)

	// A list and a search as CSV, with every property.
	rec = w.bert.do("GET", "/api/items.csv?space=PLAT&type=task", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Key,Title,Space,Type,State,Labels,priority,estimate,due") ||
		!strings.Contains(rec.Body.String(), key+",Plan Ä,PLAT,task,open,,high,3,") {
		t.Fatalf("items.csv: %d %q", rec.Code, rec.Body)
	}
	if rec := w.carla.do("GET", "/api/items.csv?space=PLAT", ""); strings.Contains(rec.Body.String(), "Plan") {
		t.Fatalf("carla exports what she may not read: %q", rec.Body)
	}
	rec = w.bert.do("GET", "/api/search.csv?q=Plan", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), key) {
		t.Fatalf("search.csv: %d %q", rec.Code, rec.Body)
	}
	// German readers get German headers and semicolons.
	w.bert.expect("PUT", "/api/me/language", `{"language":"de"}`, http.StatusNoContent)
	if rec := w.bert.do("GET", "/api/items.csv?space=PLAT", ""); !strings.Contains(rec.Body.String(), "Schlüssel;Titel;Bereich") {
		t.Fatalf("German export: %q", rec.Body)
	}
	b := budget.Default()
	b.ExportRows = 0
	budget.Set(b)
	defer budget.Set(budget.Default())
	w.bert.expect("GET", "/api/items.csv?space=PLAT", "", http.StatusRequestEntityTooLarge)
}

// TestAHandbookHoldsTheSubpagesAndWhatTheyEmbed: a page with children=true
// brings every readable page below it, and embedded items come with their
// content, as far as the reader may read them (D10).
func TestAHandbookHoldsTheSubpagesAndWhatTheyEmbed(t *testing.T) {
	w := newWorld(t)
	mk := func(title, parent, body string) string {
		t.Helper()
		p := ""
		if parent != "" {
			p = `,"parent":"` + parent + `"`
		}
		out := w.bert.expect("POST", "/api/items", `{"space":"PLAT","type":"page","title":"`+title+`"`+p+`,"body":`+body+`}`, http.StatusCreated)
		return field(out, "item", "id").(string)
	}
	w.admin.expect("POST", "/api/types", `{"name":"page","body":true}`, http.StatusCreated)
	secret := mk("Secret", "", `[{"kind":"paragraph","text":"only for some"}]`)
	w.admin.expect("PUT", "/api/items/"+secret+"/restriction", `{"read":["`+w.ids["bert"]+`"]}`, http.StatusOK)
	shared := mk("Shared part", "", `[{"kind":"paragraph","text":"embedded words"}]`)
	root := mk("Manual", "", `[{"kind":"paragraph","text":"welcome"}]`)
	chapter := mk("Chapter one", root, `[{"kind":"embed","ref":"`+shared+`"},{"kind":"embed","ref":"`+secret+`"}]`)
	mk("Section", chapter, `[{"kind":"paragraph","text":"deep text"}]`)

	page := w.bert.do("GET", "/api/items/"+root+"/export/html?children=true", "").Body.String()
	for _, want := range []string{"Manual", "Chapter one", "Section", "deep text", "embedded words", "only for some", `<nav class="contents">`} {
		if !strings.Contains(page, want) {
			t.Errorf("bert's handbook lacks %q", want)
		}
	}
	vera := w.vera.do("GET", "/api/items/"+root+"/export/html?children=true", "")
	if vera.Code != http.StatusOK || !strings.Contains(vera.Body.String(), "embedded words") || strings.Contains(vera.Body.String(), "only for some") ||
		!strings.Contains(vera.Body.String(), "(restricted)") {
		t.Fatalf("vera's handbook shows what she may not read, or lacks the rest: %s", vera.Body)
	}
	if csp := vera.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "script-src 'none'") || !strings.Contains(csp, "frame-ancestors 'self'") {
		t.Fatalf("policy: %q", csp)
	}
	// Without children only the page itself.
	if single := w.bert.do("GET", "/api/items/"+root+"/export/html", "").Body.String(); strings.Contains(single, "Chapter one</h2>") {
		t.Fatal("a single page brought its subpages")
	}
	rec := w.bert.do("GET", "/api/items/"+root+"/export/docx?children=true", "")
	z, err := zip.NewReader(bytes.NewReader(rec.Body.Bytes()), int64(rec.Body.Len()))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range z.File {
		if f.Name == "word/document.xml" {
			r, _ := f.Open()
			data, _ := io.ReadAll(r)
			if !strings.Contains(string(data), "deep text") || !strings.Contains(string(data), "embedded words") {
				t.Fatal("the Word handbook lacks the section or the embed")
			}
		}
	}
	b := budget.Default()
	b.PageItems = 2
	budget.Set(b)
	defer budget.Set(budget.Default())
	w.bert.expect("GET", "/api/items/"+root+"/export/docx?children=true", "", http.StatusRequestEntityTooLarge)
}
