package server

import (
	"archive/zip"
	"bytes"
	"encoding/json"
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
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Key,Title,Room,Type,State,Labels,priority,estimate,due") ||
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
	if rec := w.bert.do("GET", "/api/items.csv?space=PLAT", ""); !strings.Contains(rec.Body.String(), "Schlüssel;Titel;Raum") {
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

// TestExportListsTheTasksOfALabel: a task list in a text exports the
// tasks that carry its label, as the exporting reader may read them
// (ADR-0114).
func TestExportListsTheTasksOfALabel(t *testing.T) {
	w := newWorld(t)
	w.admin.expect("POST", "/api/types", `{"name":"page","body":true,"fields":[]}`, http.StatusCreated)
	mk := func(typ, title, props string) string {
		return field(w.bert.expect("POST", "/api/items", `{"space":"PLAT","type":"`+typ+`","title":"`+title+`","properties":`+props+`}`, http.StatusCreated), "item", "key").(string)
	}
	open := mk("task", "Tag the build", `{"priority":"high"}`)
	hidden := mk("task", "Pay the vendor", `{"priority":"low"}`)
	note := mk("page", "Not a task", `{}`)
	for _, k := range []string{open, hidden, note} {
		w.bert.expect("PUT", "/api/items/"+k+"/labels", `{"labels":["release-2"]}`, http.StatusOK)
	}
	w.bert.expect("PUT", "/api/items/"+hidden+"/restriction", `{"read":["`+w.ids["bert"]+`"]}`, http.StatusOK)
	plan := mk("page", "Plan", `{}`)
	w.bert.expect("PUT", "/api/items/"+plan+"/body", `{"blocks":[{"kind":"tasklist","text":"release-2"}]}`, http.StatusOK)
	w.bert.expect("PUT", "/api/items/"+plan+"/body", `{"blocks":[{"kind":"tasklist","text":"Release 2"}]}`, http.StatusBadRequest)
	html := func(c *client) string { return c.do("GET", "/api/items/"+plan+"/export/html", "").Body.String() }
	if got := html(w.bert); !strings.Contains(got, open+" Tag the build · open") || !strings.Contains(got, hidden+" Pay the vendor") || strings.Contains(got, "Not a task") {
		t.Fatalf("bert's export: %s", got)
	}
	if got := html(w.vera); !strings.Contains(got, open+" Tag the build") || strings.Contains(got, "Pay the vendor") {
		t.Fatalf("vera's export shows a task she may not read: %s", got)
	}
	if r := w.bert.do("GET", "/api/items/"+plan+"/export/docx", ""); r.Code != http.StatusOK || !bytes.Contains(r.Body.Bytes(), []byte("PK")) {
		t.Fatalf("docx %d", r.Code)
	}
}

// TestMarkdownExport: a page as Markdown text, for the API and for agents,
// and as a ZIP archive with its pictures, for people (ADR-0131); read
// through the reader's session like every export (D10).
func TestMarkdownExport(t *testing.T) {
	w := newWorld(t)
	created := w.bert.expect("POST", "/api/items", `{"space":"PLAT","type":"task","title":"Plan Ä","properties":{"priority":"high"}}`, http.StatusCreated)
	key := field(created, "item", "key").(string)
	id := field(created, "item", "id").(string)
	png := append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{7}, 100)...)
	up := w.bert.upload("/api/items/"+key+"/attachments?name=Flow%20chart.png", "application/octet-stream", png)
	var att struct{ Attachment attachmentJSON }
	if err := json.Unmarshal(up.Body.Bytes(), &att); err != nil || up.Code != http.StatusCreated {
		t.Fatalf("upload: %d %s", up.Code, up.Body)
	}
	pic := string(att.Attachment.ID)
	w.bert.expect("PUT", "/api/items/"+key+"/body", `{"blocks":[{"kind":"heading","level":1,"text":"Overview"},`+
		`{"kind":"paragraph","text":"Pay Paul Muster on Friday.","marks":[{"type":"bold","start":0,"end":3}]},`+
		`{"kind":"table","text":"Name\tCost\nTea | Cake\t5"},{"kind":"image","ref":"`+pic+`","text":"Flow chart"}]}`, http.StatusOK)
	w.bert.expect("POST", "/api/items/"+key+"/secrets", `{"block":1,"start":4,"end":15}`, http.StatusOK)

	// The text, with its pictures linked to the server.
	rec := w.bert.do("GET", "/api/items/"+key+"/export/md", "")
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "text/markdown; charset=utf-8" ||
		!strings.Contains(rec.Header().Get("Content-Disposition"), ".md") {
		t.Fatalf("md: %d %v", rec.Code, rec.Header())
	}
	for _, want := range []string{"# Plan Ä\n", "- **Key:** " + key, "- **priority:** high", "## Overview",
		"**Pay** Paul Muster on Friday.", "| Name | Cost |\n| --- | --- |\n| Tea \\| Cake | 5 |",
		"![Flow chart](/api/items/" + id + "/attachments/" + pic + ")"} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Errorf("the Markdown lacks %q:\n%s", want, rec.Body)
		}
	}
	// A protected passage stays veiled for a reader who may not read it
	// (ADR-0088); someone who may not read the item gets nothing (D10).
	if rec := w.vera.do("GET", "/api/items/"+key+"/export/md", ""); rec.Code != http.StatusOK ||
		strings.Contains(rec.Body.String(), "Muster") || !strings.Contains(rec.Body.String(), "▒") {
		t.Errorf("vera reads %d: %s", rec.Code, rec.Body)
	}
	w.carla.expect("GET", "/api/items/"+key+"/export/md", "", http.StatusNotFound)
	w.carla.expect("GET", "/api/items/"+key+"/export/md.zip", "", http.StatusNotFound)

	// The archive, with the picture beside the text.
	rec = w.bert.do("GET", "/api/items/"+key+"/export/md.zip", "")
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "application/zip" ||
		!strings.Contains(rec.Header().Get("Content-Disposition"), ".zip") {
		t.Fatalf("md.zip: %d %v", rec.Code, rec.Header())
	}
	z, err := zip.NewReader(bytes.NewReader(rec.Body.Bytes()), int64(rec.Body.Len()))
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{}
	for _, f := range z.File {
		r, _ := f.Open()
		data, _ := io.ReadAll(r)
		r.Close()
		files[f.Name] = string(data)
	}
	if md := files[key+" Plan Ä.md"]; !strings.Contains(md, "![Flow chart](media/01-Flow-chart.png)") || files["media/01-Flow-chart.png"] != string(png) {
		t.Errorf("archive: %v", files)
	}

	// With children=true, a handbook with the pages below.
	w.bert.expect("POST", "/api/items", `{"space":"PLAT","type":"task","title":"Step one","parent":"`+id+`","properties":{"priority":"low"}}`, http.StatusCreated)
	book := w.bert.do("GET", "/api/items/"+key+"/export/md?children=true", "").Body.String()
	if !strings.Contains(book, "- Step one") || !strings.Contains(book, "\n## Step one\n") {
		t.Errorf("handbook:\n%s", book)
	}
}
