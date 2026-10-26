package export

import (
	"archive/zip"
	"bytes"
	"io"
	"slices"
	"strings"
	"testing"

	"github.com/dbuchs7/wikiwikibangbang/internal/item"
)

func markdownOf(t *testing.T, d Document) string {
	t.Helper()
	var b bytes.Buffer
	if err := Markdown(&b, d); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

// paragraph writes one paragraph of text with marks as Markdown, without
// the title of its page.
func paragraph(t *testing.T, text string, marks ...item.Mark) string {
	t.Helper()
	out := markdownOf(t, Document{Pages: []Page{{ID: "p", Body: []item.Block{{Kind: item.Paragraph, Text: text, Marks: marks}}}}})
	return strings.TrimSuffix(strings.TrimPrefix(out, "# \n\n"), "\n")
}

// TestMarkdownWritesEachBlockKind pins the Markdown of every block kind
// (ADR-0131): the title, the facts, then the body; list items of one kind
// form one list, numbered from 1.
func TestMarkdownWritesEachBlockKind(t *testing.T) {
	d := Document{Pages: []Page{{
		ID: "root", Key: "OPS-1", Title: "Runbook *Ops*",
		Facts: [][2]string{{"Key", "OPS-1"}, {"State", "open"}},
		Body: []item.Block{
			{Kind: item.Heading, Level: 2, Text: "Steps", Align: "center"},
			{Kind: item.Paragraph, Text: "Read the docs now", Marks: []item.Mark{
				{Type: item.MarkBold, Start: 0, End: 4}, {Type: item.MarkLink, Start: 9, End: 13, Href: "https://example.org/docs"},
				{Type: item.MarkLink, Start: 14, End: 17, Href: "javascript:alert(1)"}}},
			{Kind: item.Paragraph, Text: ""}, {Kind: item.Paragraph, Text: "  "}, {Kind: item.Quote, Text: " "}, {Kind: item.Panel, Tone: "info"},
			{Kind: item.BulletItem, Text: "one"}, {Kind: item.BulletItem, Text: "two"},
			{Kind: item.OrderedItem, Text: "first"}, {Kind: item.OrderedItem, Text: "second"},
			{Kind: item.Paragraph, Text: "between"}, {Kind: item.OrderedItem, Text: "again"},
			{Kind: item.TaskItem, Text: "done", Checked: true}, {Kind: item.TaskItem, Text: "open"},
			{Kind: item.Quote, Text: "said\nand more"},
			{Kind: item.CodeBlock, Text: "a := 1\n```\nb := 2"},
			{Kind: item.Panel, Tone: "warning", Text: "careful"},
			{Kind: item.Panel, Tone: "note", Text: "mind"},
			{Kind: item.Panel, Text: "no tone"},
			{Kind: item.Table, Text: ""},
			{Kind: item.Table, Text: "Name\tSize\nÄpfel | Birnen\t3"},
			{Kind: item.Divider},
			{Kind: item.TOC}, {Kind: item.Children},
			{Kind: item.Image, Ref: "pic", Text: "The plan", Width: 50},
			{Kind: item.Image, Ref: "missing", Text: "gone"},
			{Kind: item.Video, Ref: "vid", Text: "Demo"},
			{Kind: item.BPMN, Ref: "flow", Text: "Order flow"},
			{Kind: item.BPMN, Ref: "lost"},
			{Kind: item.Process, Engine: "atlas", Process: "order", Text: "Live"},
			{Kind: item.Figures},
			{Kind: item.TaskList, Text: "release"},
			{Kind: item.Unsupported, Text: "<ac:macro name=\"x\"/>"},
			{Kind: item.Heading, Level: 1, Text: "Item #"},
			{Kind: item.Heading, Level: 3, Text: "#"},
			{Kind: item.Heading, Level: 3, Text: `a\#`},
		},
		Images: map[item.ID]Image{"pic": {Name: "plan.png", Type: "image/png"}, "vid": {Name: "demo.mp4", Type: "video/mp4"},
			"flow": {Name: "order.bpmn", Type: "text/xml; charset=utf-8"}},
		Children: []string{"Child page"},
		Tasks:    map[string][]string{"release": {"OPS-3 Tag the build (open)"}},
	}}}
	want := strings.Join([]string{
		`# Runbook \*Ops\*`,
		"- **Key:** OPS-1\n- **State:** open",
		"### Steps",
		"**Read** the [docs](https://example.org/docs) now",
		"- one\n- two",
		"1. first\n2. second",
		"between",
		"1. again",
		"- [x] done\n- [ ] open",
		"> said\\\n> and more",
		"````\na := 1\n```\nb := 2\n````",
		"> [!WARNING]\n> careful",
		"> [!IMPORTANT]\n> mind",
		"> [!NOTE]\n> no tone",
		"| Name | Size |\n| --- | --- |\n| Äpfel \\| Birnen | 3 |",
		"---",
		"  - Steps\n- Item #\n    - \\#\n    - a\\\\#",
		"- Child page",
		"![The plan](/api/items/root/attachments/pic)",
		"*The plan*",
		"*gone*",
		"[▶ demo.mp4](/api/items/root/attachments/vid)",
		"*Demo*",
		"[BPMN: order.bpmn](/api/items/root/attachments/flow)",
		"*Order flow*",
		"BPMN: order (atlas)",
		"*Live*",
		"- Figure 1: The plan\n- Figure 2: gone",
		`\# release`,
		"- OPS-3 Tag the build (open)",
		"```\n<ac:macro name=\"x\"/>\n```",
		`## Item \#`,
		`#### \#`,
		`#### a\\\#`,
	}, "\n\n") + "\n"
	if got := markdownOf(t, d); got != want {
		t.Errorf("Markdown:\n%s\nwant:\n%s", got, want)
	}
}

// TestMarkdownEscapesWhatWouldBeMarkup: text the reader wrote stays text in
// any Markdown renderer; in particular, HTML in it never becomes markup
// (ADR-0014).
func TestMarkdownEscapesWhatWouldBeMarkup(t *testing.T) {
	for _, tc := range []struct{ text, want string }{
		{"a*b*c", `a\*b\*c`},
		{"snake_case and _x_", `snake_case and \_x\_`},
		{"use `x` here", "use \\`x\\` here"},
		{"[x](y) ![z](w)", `\[x\](y) !\[z\](w)`},
		{"<script>alert(1)</script>", `\<script>alert(1)\</script>`},
		{"a < b & c", "a < b & c"},
		{"&amp; &#169; &#x1F600; &x", `\&amp; \&#169; \&#x1F600; &x`},
		{"# not a heading", `\# not a heading`},
		{"> not a quote", `\> not a quote`},
		{"- not a list", `\- not a list`},
		{"+ not a list", `\+ not a list`},
		{"1. not a list", `1\. not a list`},
		{"2024) a year", `2024\) a year`},
		{"=== not a heading", `\=== not a heading`},
		{"a|b ~c~", `a\|b \~c\~`},
		{`back\slash`, `back\\slash`},
		{"    four spaces", "&#32;   four spaces"},
		{"\ttab", "&#9;tab"},
		{"line one\n# line two", "line one\\\n\\# line two"},
		{"C# and 3 > 2", "C# and 3 > 2"},
		{"Grüsse 😀 https://example.org", "Grüsse 😀 https://example.org"},
	} {
		if got := paragraph(t, tc.text); got != tc.want {
			t.Errorf("%q: %q, want %q", tc.text, got, tc.want)
		}
	}
}

// TestMarkdownMarksNestAndFlank: marks become delimiters that a CommonMark
// renderer reads back as the same formatting: properly nested, without
// whitespace or punctuation where a delimiter would not count.
func TestMarkdownMarksNestAndFlank(t *testing.T) {
	m := func(typ item.MarkType, start, end int) item.Mark { return item.Mark{Type: typ, Start: start, End: end} }
	link := func(start, end int, href string) item.Mark {
		return item.Mark{Type: item.MarkLink, Start: start, End: end, Href: href}
	}
	for _, tc := range []struct {
		name, text string
		marks      []item.Mark
		want       string
	}{
		{"nested", "bold with italic inside", []item.Mark{m(item.MarkBold, 0, 23), m(item.MarkItalic, 10, 16)}, "**bold with *italic* inside**"},
		{"both", "both", []item.Mark{m(item.MarkBold, 0, 4), m(item.MarkItalic, 0, 4)}, "***both***"},
		{"italic at the start of bold", "abcde", []item.Mark{m(item.MarkBold, 0, 5), m(item.MarkItalic, 0, 2)}, "***ab*cde**"},
		{"adjacent", "ab", []item.Mark{m(item.MarkBold, 0, 1), m(item.MarkItalic, 1, 2)}, "**a***b*"},
		{"crossing", "abcdefghij", []item.Mark{m(item.MarkBold, 0, 5), m(item.MarkItalic, 3, 8)}, "**abc*de***<em>fgh</em>ij"},
		{"spaces outside", "a bold b", []item.Mark{m(item.MarkBold, 1, 7)}, "a **bold** b"},
		{"punctuation outside", "a(b)c", []item.Mark{m(item.MarkBold, 1, 4)}, "a(**b**)c"},
		{"punctuation kept before a space", "Note: text", []item.Mark{m(item.MarkBold, 0, 5)}, "**Note:** text"},
		{"strike", "old new", []item.Mark{m(item.MarkStrike, 0, 3)}, "~~old~~ new"},
		{"strike across bold", "abcdefgh", []item.Mark{m(item.MarkBold, 0, 5), m(item.MarkStrike, 3, 8)}, "**abc~~de~~**~~fgh~~"},
		{"underline and highlight", "under high", []item.Mark{m(item.MarkUnderline, 0, 5), m(item.MarkHighlight, 6, 10)}, "<ins>under</ins> <mark>high</mark>"},
		{"code", "run go test now", []item.Mark{m(item.MarkCode, 4, 11)}, "run `go test` now"},
		{"code with backticks", "a `b` c", []item.Mark{m(item.MarkCode, 0, 7)}, "``a `b` c``"},
		{"code of spaces", "a  b", []item.Mark{m(item.MarkCode, 1, 3)}, "a`  `b"},
		{"code starting with a space", " x", []item.Mark{m(item.MarkCode, 0, 2)}, "`  x `"},
		{"code in bold", "x code y", []item.Mark{m(item.MarkBold, 0, 8), m(item.MarkCode, 2, 6)}, "**x `code` y**"},
		{"code starting a bold run", "codeX", []item.Mark{m(item.MarkCode, 0, 4), m(item.MarkBold, 2, 5)}, "`co`**`de`X**"},
		{"link with formatting", "see the guide", []item.Mark{link(4, 13, "https://x.ch/a (b)"), m(item.MarkBold, 8, 13)}, "see [the **guide**](https://x.ch/a%20%28b%29)"},
		{"link not allowed", "click", []item.Mark{link(0, 5, "javascript:alert(1)")}, "click"},
		{"mail", "write", []item.Mark{link(0, 5, "mailto:a@b.ch")}, "[write](mailto:a@b.ch)"},
		{"exclamation before a link", "Wow!x", []item.Mark{link(4, 5, "https://x.ch")}, `Wow\![x](https://x.ch)`},
		{"link that would read as a definition", "a]: b", []item.Mark{link(0, 3, "https://x.ch/?a=1&b=\"2\""), m(item.MarkCode, 1, 3)}, `<a href="https://x.ch/?a=1&amp;b=&#34;2&#34;">a` + "`]:`" + `</a> b`},
		{"mention", "@anna", []item.Mark{{Type: item.MarkMention, Start: 0, End: 5, Ref: "u1"}}, "@anna"},
		{"protected passage", "▒▒▒▒▒▒▒▒ left", []item.Mark{{Type: item.MarkSecret, Start: 0, End: 8, Ref: "s1"}}, "▒▒▒▒▒▒▒▒ left"},
		{"emoji", "😀 Grüsse", []item.Mark{m(item.MarkBold, 0, 8)}, "**😀 Grüsse**"},
		{"line break in bold", "one\ntwo", []item.Mark{m(item.MarkBold, 0, 7)}, "**one**\\\n**two**"},
		{"out of range", "abc", []item.Mark{m(item.MarkBold, 2, 9)}, "ab**c**"},
	} {
		if got := paragraph(t, tc.text, tc.marks...); got != tc.want {
			t.Errorf("%s: %q, want %q", tc.name, got, tc.want)
		}
	}
}

// TestMarkdownHandbookAndEmbeds: a handbook lists its pages, then each
// page under a heading by its depth; an embedded item is a quote with its
// name, facts and body, and a restricted one names nothing (D10).
func TestMarkdownHandbookAndEmbeds(t *testing.T) {
	d := Document{
		Contents:   "Table of contents",
		Restricted: "(restricted)",
		Pages: []Page{
			{ID: "r", Title: "Root", Body: []item.Block{{Kind: item.Heading, Level: 1, Text: "Intro"}, {Kind: item.Embed, Ref: "e"}, {Kind: item.Embed, Ref: "hidden"}}},
			{ID: "c", Title: "Child", Depth: 1, Body: []item.Block{{Kind: item.Heading, Level: 1, Text: "Setup"}}},
			{ID: "g", Title: "Grandchild", Depth: 2},
		},
		Embeds: map[item.ID]Page{
			"e": {ID: "e", Key: "OPS-9", Title: "Linked", Facts: [][2]string{{"State", "done"}},
				Body: []item.Block{{Kind: item.Paragraph, Text: "inside"}, {Kind: item.Embed, Ref: "e"}}},
		},
	}
	want := strings.Join([]string{
		"# Root",
		"## Table of contents",
		"- Child\n  - Grandchild",
		"## Intro",
		"> ↳ OPS-9 Linked\n>\n> - **State:** done\n>\n> inside\n>\n> > ↳ OPS-9 Linked",
		"> ↳ (restricted)",
		"## Child",
		"### Setup",
		"### Grandchild",
	}, "\n\n") + "\n"
	if got := markdownOf(t, d); got != want {
		t.Errorf("handbook:\n%s\nwant:\n%s", got, want)
	}
}

// TestMarkdownArchiveHoldsThePictures: the archive holds the Markdown and
// the pictures it shows, linked by relative paths; names cannot leave the
// archive's folder, and a picture without its file keeps its caption.
func TestMarkdownArchiveHoldsThePictures(t *testing.T) {
	png := pngOf(t, 4, 2)
	d := Document{Pages: []Page{{
		ID: "root", Title: "Plan",
		Body: []item.Block{
			{Kind: item.Image, Ref: "a", Text: "First"},
			{Kind: item.Image, Ref: "b"},
			{Kind: item.Image, Ref: "a", Text: "Again"},
			{Kind: item.Image, Ref: "nodata", Text: "No file"},
			{Kind: item.Video, Ref: "v", Text: "Clip"},
			{Kind: item.BPMN, Ref: "m"},
		},
		Images: map[item.ID]Image{
			"a":      {Name: "Übersicht plan.png", Type: "image/png", Data: png},
			"b":      {Name: "../../etc/passwd", Type: "image/jpeg", Data: []byte("jpeg")},
			"nodata": {Name: "x.png", Type: "image/png"},
			"v":      {Name: "clip.mp4", Type: "video/mp4"},
			"m":      {Name: "flow.bpmn", Type: "text/xml; charset=utf-8"},
		},
	}}}
	var buf bytes.Buffer
	if err := MarkdownArchive(&buf, d, "OPS-1 Plan.md"); err != nil {
		t.Fatal(err)
	}
	z, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{}
	var names []string
	for _, f := range z.File {
		r, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, _ := io.ReadAll(r)
		r.Close()
		files[f.Name] = data
		names = append(names, f.Name)
	}
	if want := []string{"OPS-1 Plan.md", "media/01-Übersicht-plan.png", "media/02-passwd.jpg"}; !slices.Equal(names, want) {
		t.Fatalf("archive holds %v, want %v", names, want)
	}
	if !bytes.Equal(files["media/01-Übersicht-plan.png"], png) {
		t.Error("the picture's file is not in the archive as it was")
	}
	want := strings.Join([]string{
		"# Plan",
		"![First](media/01-Übersicht-plan.png)",
		"*First*",
		"![passwd](media/02-passwd.jpg)",
		"![Again](media/01-Übersicht-plan.png)",
		"*Again*",
		"*No file*",
		"▶ clip.mp4",
		"*Clip*",
		"BPMN: flow.bpmn",
	}, "\n\n") + "\n"
	if got := string(files["OPS-1 Plan.md"]); got != want {
		t.Errorf("Markdown in the archive:\n%s\nwant:\n%s", got, want)
	}
}

// TestDefinitionLike: what a block starting like a link reference
// definition looks like, escapes in the label included.
func TestDefinitionLike(t *testing.T) {
	for s, want := range map[string]bool{"[a]: b": true, `[a\]]: b`: true, "[a] b": false, "[a": false, "a]: b": false, "[a]": false} {
		if got := definitionLike(s); got != want {
			t.Errorf("%q: %v, want %v", s, got, want)
		}
	}
}

// TestMediaName: a picture's name in an archive, from any file name.
func TestMediaName(t *testing.T) {
	long := strings.Repeat("a", 70) + ".png"
	for _, tc := range []struct{ name, typ, want string }{
		{"", "image/png", "image.png"},
		{"???", "image/gif", "image.gif"},
		{"plan", "application/octet-stream", "plan"},
		{"C:\\pics\\Plan (final).webp", "image/webp", "Plan-final.webp"},
		{long, "image/png", long[len(long)-60:]},
	} {
		if got := mediaName(Image{Name: tc.name, Type: tc.typ}); got != tc.want {
			t.Errorf("%q: %q, want %q", tc.name, got, tc.want)
		}
	}
}
