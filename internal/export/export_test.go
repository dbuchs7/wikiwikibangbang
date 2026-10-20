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
	d := Document{
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
			{Kind: item.Embed, Ref: "x"},
			{Kind: item.Paragraph, Text: "bad \x01 char"},
		},
		Images:   map[item.ID]Image{"pic": {Name: "plan.png", Type: "image/png", Data: pngOf(t, 400, 200)}},
		Ref:      func(id item.ID) string { return "OPS-9 Linked" },
		Children: []string{"Child page"},
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
	for _, want := range []string{"Runbook &lt;Ops&gt; &amp; more", "Heading2", "<w:b/>", "☑ ", "Äpfel", "OPS-9 Linked", "Child page", "The plan", "w:numId w:val=\"3\"", "<w:drawing>"} {
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
