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
