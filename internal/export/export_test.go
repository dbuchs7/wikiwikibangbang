package export

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"image"
	"image/color"
	"image/png"
	"io"
	"strings"
	"testing"

	"github.com/dbuchs7/wikiwikibangbang/internal/item"
)

func pngOf(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	img.Set(0, 0, color.Black)
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// TestADocumentIsAWellFormedWordFile writes every block kind and checks
// that each part of the package is well-formed XML and holds the text, the
// picture and the link.
func TestADocumentIsAWellFormedWordFile(t *testing.T) {
	d := Document{Pages: []Page{{
		ID:    "root",
		Title: "Runbook <Ops> & more",
		Facts: [][2]string{{"Key", "OPS-1"}, {"State", "open"}},
		Body: []item.Block{
			{Kind: item.Heading, Level: 2, Text: "Steps"},
			{Kind: item.Paragraph, Text: "Read the docs now", Align: "justify", Marks: []item.Mark{
				{Type: item.MarkBold, Start: 0, End: 4}, {Type: item.MarkLink, Start: 9, End: 13, Href: "https://example.org/docs"},
				{Type: item.MarkLink, Start: 14, End: 17, Href: "javascript:alert(1)"}}},
			{Kind: item.BulletItem, Text: "one"}, {Kind: item.BulletItem, Text: "two"},
			{Kind: item.OrderedItem, Text: "first"}, {Kind: item.Paragraph, Text: "between"}, {Kind: item.OrderedItem, Text: "again"},
			{Kind: item.TaskItem, Text: "done", Checked: true},
			{Kind: item.Quote, Text: "said"}, {Kind: item.CodeBlock, Text: "a := 1\nb := 2"},
			{Kind: item.Panel, Tone: "warning", Text: "careful"},
			{Kind: item.Table, Text: "Name\tSize\nÄpfel\t3"},
			{Kind: item.Divider}, {Kind: item.TOC}, {Kind: item.Children},
			{Kind: item.Image, Ref: "pic", Text: "The plan", Width: 50},
			{Kind: item.Image, Ref: "missing", Text: "gone"},
			{Kind: item.Embed, Ref: "x"}, {Kind: item.Embed, Ref: "secret"},
			{Kind: item.Paragraph, Text: "bad \x01 char"},
		},
		Images:   map[item.ID]Image{"pic": {Name: "plan.png", Type: "image/png", Data: pngOf(t, 400, 200)}},
		Children: []string{"Child page"},
	}},
		Embeds:     map[item.ID]Page{"x": {ID: "x", Key: "OPS-9", Title: "Linked", Body: []item.Block{{Kind: item.Paragraph, Text: "inside the embed"}}}},
		Restricted: "(restricted)",
	}
	var buf bytes.Buffer
	if err := DOCX(&buf, d); err != nil {
		t.Fatal(err)
	}
	z, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	parts := map[string]string{}
	for _, f := range z.File {
		r, _ := f.Open()
		data, _ := io.ReadAll(r)
		parts[f.Name] = string(data)
		if strings.HasSuffix(f.Name, ".xml") || strings.HasSuffix(f.Name, ".rels") {
			dec := xml.NewDecoder(bytes.NewReader(data))
			for {
				if _, err := dec.Token(); err == io.EOF {
					break
				} else if err != nil {
					t.Fatalf("%s is not well-formed: %v", f.Name, err)
				}
			}
		}
	}
	doc := parts["word/document.xml"]
	for _, want := range []string{"Runbook &lt;Ops&gt; &amp; more", "Heading2", "<w:b/>", "☑ ", "Äpfel", "OPS-9 Linked", "inside the embed", "(restricted)", "Child page", "The plan", "w:numId w:val=\"3\"", "<w:drawing>"} {
		if !strings.Contains(doc, want) {
			t.Errorf("the document lacks %q", want)
		}
	}
	if !strings.Contains(parts["word/_rels/document.xml.rels"], "https://example.org/docs") || strings.Contains(parts["word/_rels/document.xml.rels"], "javascript") {
		t.Errorf("links: %s", parts["word/_rels/document.xml.rels"])
	}
	if _, ok := parts["word/media/image1.png"]; !ok {
		t.Error("the picture is not in the package")
	}
	if !strings.Contains(doc, `cx="2880000"`) {
		t.Error("the picture is not half the text width")
	}
}

func TestCSVIsSafeForSpreadsheets(t *testing.T) {
	var b bytes.Buffer
	if err := CSV(&b, [][]string{{"Name", "Formula"}, {"Müller; AG", "=1+1"}, {"x", "-2"}}, ';'); err != nil {
		t.Fatal(err)
	}
	want := "\xef\xbb\xbfName;Formula\r\n\"Müller; AG\";'=1+1\r\nx;'-2\r\n"
	if b.String() != want {
		t.Fatalf("got %q, want %q", b.String(), want)
	}
	if Separator("de") != ';' || Separator("en") != ',' {
		t.Fatal("separators")
	}
	if rows := TableRows("a\tb\nc\td"); len(rows) != 2 || rows[1][1] != "d" {
		t.Fatalf("rows %v", rows)
	}
	if TableRows("  ") != nil {
		t.Fatal("an empty table has rows")
	}
}

// handbook is a root with a chapter, a section below it, and an item
// embedded in itself.
func handbook() Document {
	return Document{
		Pages: []Page{
			{ID: "r", Title: "Handbook", Body: []item.Block{{Kind: item.Heading, Level: 1, Text: "Intro"}}},
			{ID: "c", Title: "Chapter", Depth: 1, Body: []item.Block{{Kind: item.Heading, Level: 1, Text: "Part"}, {Kind: item.Embed, Ref: "loop"}}},
			{ID: "s", Title: "Section", Depth: 2, Body: []item.Block{{Kind: item.Paragraph, Text: "deep"}}},
		},
		Embeds:   map[item.ID]Page{"loop": {ID: "loop", Key: "K-1", Title: "Loop", Body: []item.Block{{Kind: item.Paragraph, Text: "looping"}, {Kind: item.Embed, Ref: "loop"}}}},
		Contents: "Contents",
	}
}

func TestAHandbookHasChaptersAndEmbedsEndInCycles(t *testing.T) {
	var buf bytes.Buffer
	if err := DOCX(&buf, handbook()); err != nil {
		t.Fatal(err)
	}
	z, _ := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	var doc string
	for _, f := range z.File {
		if f.Name == "word/document.xml" {
			r, _ := f.Open()
			data, _ := io.ReadAll(r)
			doc = string(data)
		}
	}
	for _, want := range []string{"TOCHeading", `<w:pStyle w:val="Heading1"/><w:pageBreakBefore/>`, "Chapter", `<w:pStyle w:val="Heading2"/></w:pPr><w:r><w:t xml:space="preserve">Part`,
		`<w:pStyle w:val="Heading2"/></w:pPr><w:r><w:t xml:space="preserve">Section`} {
		if !strings.Contains(doc, want) {
			t.Errorf("the handbook lacks %q", want)
		}
	}
	if n := strings.Count(doc, "looping"); n != 1 {
		t.Errorf("an item embedded in itself is written %d times, want once", n)
	}
	var page bytes.Buffer
	if err := HTML(&page, handbook(), "en"); err != nil {
		t.Fatal(err)
	}
	h := page.String()
	for _, want := range []string{`<link rel="stylesheet" href="/assets/export.css">`, `<nav class="contents">`, `<h2 id="p-c" class="page-title depth-1">Chapter</h2>`,
		`<h3>Part</h3>`, `<h3 id="p-s" class="page-title depth-2">Section</h3>`, "↳ K-1 Loop"} {
		if !strings.Contains(h, want) {
			t.Errorf("the HTML lacks %q:\n%s", want, h)
		}
	}
	if strings.Contains(h, "<script") || strings.Contains(h, "style=") || strings.Count(h, "looping") != 1 {
		t.Errorf("script, inline style or a repeated embed:\n%s", h)
	}
}

func TestTheHTMLEscapesAndKeepsSafeLinksOnly(t *testing.T) {
	d := Document{Pages: []Page{{ID: "p", Title: "<b>x</b>", Body: []item.Block{
		{Kind: item.Paragraph, Text: "<script>a</script> link bad", Marks: []item.Mark{
			{Type: item.MarkLink, Start: 19, End: 23, Href: "https://e.org/?a=1&b=\"2\""}, {Type: item.MarkLink, Start: 24, End: 27, Href: "javascript:x"}}},
		{Kind: item.BulletItem, Text: "one"}, {Kind: item.TaskItem, Text: "two", Checked: true}, {Kind: item.OrderedItem, Text: "three"},
		{Kind: item.Image, Ref: "i", Text: "pic", Width: 47, Align: "right"},
		{Kind: item.Table, Text: "a\tb\n<c>\td"}, {Kind: item.CodeBlock, Text: "<x>"}, {Kind: item.Panel, Tone: "bogus", Text: "p"},
		{Kind: item.Quote, Text: "q", Align: "center"}, {Kind: item.Divider}, {Kind: item.TOC}, {Kind: item.Children},
	}, Images: map[item.ID]Image{"i": {Name: "i.png"}}, Children: []string{"kid"}}}}
	var b bytes.Buffer
	if err := HTML(&b, d, "de"); err != nil {
		t.Fatal(err)
	}
	h := b.String()
	for _, want := range []string{"&lt;b&gt;x&lt;/b&gt;", "&lt;script&gt;", `<a href="https://e.org/?a=1&amp;b=&#34;2&#34;">link</a>`, "<ul><li>one</li></ul>",
		`<ul class="tasks"><li><span class="box">☑</span> two</li></ul><ol><li>three</li></ol>`, `<figure class="image w-50 align-right"><img src="/api/items/p/attachments/i"`,
		"<td>&lt;c&gt;</td>", "<code>&lt;x&gt;</code>", "panel tone-info", `<blockquote class="align-center">`, "<hr>", "<li>kid</li>"} {
		if !strings.Contains(h, want) {
			t.Errorf("the HTML lacks %q:\n%s", want, h)
		}
	}
	if strings.Contains(h, "javascript") {
		t.Error("an unsafe link was written")
	}
}

// TestListsOfFiguresAndTables: a list of figures numbers the pictures of
// the body with their captions, a list of tables its tables with their
// header rows, in the export's language; English by default (ADR-0119).
func TestListsOfFiguresAndTables(t *testing.T) {
	page := Page{ID: "p", Title: "Report", Body: []item.Block{
		{Kind: item.Figures}, {Kind: item.Tables},
		{Kind: item.Image, Ref: "a", Text: "The <plan>"}, {Kind: item.Video, Ref: "v", Text: "not a figure"},
		{Kind: item.Table, Text: "Item\tPrice\nTea\t3"}, {Kind: item.Image, Ref: "b"}, {Kind: item.Table, Text: "\t\nx\ty"},
	}, Images: map[item.ID]Image{}}
	var b bytes.Buffer
	if err := HTML(&b, Document{Pages: []Page{page}, Figure: "Abbildung", Table: "Tabelle"}, "de"); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`<ul class="figures"><li>Abbildung 1: The &lt;plan&gt;</li><li>Abbildung 2</li></ul>`,
		`<ul class="tables"><li>Tabelle 1: Item, Price</li><li>Tabelle 2</li></ul>`} {
		if !strings.Contains(b.String(), want) {
			t.Errorf("the HTML lacks %q:\n%s", want, b.String())
		}
	}
	var w bytes.Buffer
	if err := DOCX(&w, Document{Pages: []Page{page}}); err != nil {
		t.Fatal(err)
	}
	doc := documentXML(t, w.Bytes())
	for _, want := range []string{"Figure 1: The &lt;plan&gt;", "Figure 2", "Table 1: Item, Price", "Table 2"} {
		if !strings.Contains(doc, want) {
			t.Errorf("the Word file lacks %q", want)
		}
	}
}

// documentXML is the main part of a Word file.
func documentXML(t *testing.T, file []byte) string {
	t.Helper()
	z, err := zip.NewReader(bytes.NewReader(file), int64(len(file)))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range z.File {
		if f.Name == "word/document.xml" {
			r, _ := f.Open()
			data, _ := io.ReadAll(r)
			return string(data)
		}
	}
	t.Fatal("no word/document.xml")
	return ""
}

// TestDiagramsAreNamedInDocuments: a BPMN diagram and a process are named
// with their captions in both exports, not drawn (ADR-0129).
func TestDiagramsAreNamedInDocuments(t *testing.T) {
	page := Page{ID: "p", Title: "Order to cash", Body: []item.Block{
		{Kind: item.BPMN, Ref: "m", Text: "As <drawn>"},
		{Kind: item.Process, Engine: "atlas", Process: "order-to-cash", Text: "As it runs"},
		{Kind: item.Form, Engine: "atlas", Form: "reise-antrag", Text: "Filled <in>"},
		{Kind: item.Decision, Engine: "atlas", Decision: "Decision 1", Text: "Who needs a visa"},
	}, Images: map[item.ID]Image{"m": {Name: "order&co.bpmn", Type: "text/xml; charset=utf-8"}}}
	var h bytes.Buffer
	if err := HTML(&h, Document{Pages: []Page{page}}, "en"); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"[BPMN: order&amp;co.bpmn]", "<figcaption>As &lt;drawn&gt;</figcaption>", "[BPMN: order-to-cash (atlas)]", "<figcaption>As it runs</figcaption>",
		"[Form: reise-antrag (atlas)]", "<figcaption>Filled &lt;in&gt;</figcaption>", "[Decision: Decision 1 (atlas)]", "<figcaption>Who needs a visa</figcaption>"} {
		if !strings.Contains(h.String(), want) {
			t.Errorf("the HTML lacks %q:\n%s", want, h.String())
		}
	}
	var w bytes.Buffer
	if err := DOCX(&w, Document{Pages: []Page{page}}); err != nil {
		t.Fatal(err)
	}
	z, err := zip.NewReader(bytes.NewReader(w.Bytes()), int64(w.Len()))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range z.File {
		if f.Name != "word/document.xml" {
			continue
		}
		r, _ := f.Open()
		data, _ := io.ReadAll(r)
		for _, want := range []string{"[BPMN: order&amp;co.bpmn]", "As &lt;drawn&gt;", "[BPMN: order-to-cash (atlas)]", "As it runs", "[Form: reise-antrag (atlas)]", "Filled &lt;in&gt;", "[Decision: Decision 1 (atlas)]"} {
			if !strings.Contains(string(data), want) {
				t.Errorf("the Word document lacks %q", want)
			}
		}
	}
}
