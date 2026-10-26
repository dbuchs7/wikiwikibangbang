package markdown

import (
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/dbuchs7/wikiwikibangbang/internal/item"
)

// repo resolves as a file docs/guide.md of a repository at main does.
func repo() Source {
	base, _ := url.Parse("https://git.example.org/o/r/blob/main/docs/guide.md")
	raw, _ := url.Parse("https://raw.example.org/o/r/main/docs/guide.md")
	resolve := func(b *url.URL) func(string) string {
		return func(target string) string {
			if strings.HasPrefix(target, "#") {
				return ""
			}
			u, err := url.Parse(target)
			if err != nil {
				return ""
			}
			return b.ResolveReference(u).String()
		}
	}
	return Source{Name: "docs/guide.md", Link: resolve(base), Picture: resolve(raw)}
}

func conv(t *testing.T, src string, s Source) Doc {
	t.Helper()
	d := Convert([]byte(src), s)
	for i, b := range d.Blocks {
		if err := item.CheckBlock(b, "self"); err != nil {
			t.Errorf("block %d %+v is not valid: %v", i+1, b, err)
		}
	}
	return d
}

func TestTitle(t *testing.T) {
	for _, c := range []struct {
		name, src, title string
		blocks           int
	}{
		{"a leading heading of level 1 is the title", "# Install\n\nText.", "Install", 1},
		{"front matter names the title", "---\ntitle: \"Set up\"\nweight: 2\n---\n# Install\n\nText.", "Set up", 2},
		{"single quotes", "---\ntitle: 'It''s'\n---\nText.", "It's", 1},
		{"the file's name otherwise", "Text.\n\n# Later", "guide", 2},
		{"a heading of level 2 stays", "## Part\n\nText.", "guide", 2},
		{"an unclosed front matter is text", "---\ntitle: x\nText.", "guide", 0},
		{"an empty file", "", "guide", 0},
		{"formatting leaves the title", "# The *new* `tool`", "The new tool", 0},
	} {
		t.Run(c.name, func(t *testing.T) {
			d := conv(t, c.src, repo())
			if d.Title != c.title {
				t.Errorf("title %q, want %q", d.Title, c.title)
			}
			if c.blocks > 0 && len(d.Blocks) != c.blocks {
				t.Errorf("%d blocks, want %d: %+v", len(d.Blocks), c.blocks, d.Blocks)
			}
		})
	}
}

func TestBlocks(t *testing.T) {
	src := `# Title

## Two
### Three
#### Four

Para *one* **two** ~~gone~~ ` + "`code`" + ` and [a link](https://example.org/x).
Next line\
hard break.

- bullet
  - nested
1. first
2. second
- [ ] open
- [x] done

> quoted
> still

> [!WARNING]
> Careful
> now.

` + "```go\nfunc x() {}\n\n```" + `

    indented

---

| A | B |
|---|---|
| 1 | two \| three |
| x |
`
	d := conv(t, src, repo())
	want := []item.Block{
		{Kind: item.Heading, Level: 2, Text: "Two"},
		{Kind: item.Heading, Level: 3, Text: "Three"},
		{Kind: item.Heading, Level: 3, Text: "Four"},
		{Kind: item.Paragraph, Text: "Para one two gone code and a link. Next line\nhard break.", Marks: []item.Mark{
			{Type: item.MarkItalic, Start: 5, End: 8}, {Type: item.MarkBold, Start: 9, End: 12}, {Type: item.MarkStrike, Start: 13, End: 17},
			{Type: item.MarkCode, Start: 18, End: 22}, {Type: item.MarkLink, Start: 27, End: 33, Href: "https://example.org/x"}}},
		{Kind: item.BulletItem, Text: "bullet"},
		{Kind: item.BulletItem, Text: "nested"},
		{Kind: item.OrderedItem, Text: "first"},
		{Kind: item.OrderedItem, Text: "second"},
		{Kind: item.TaskItem, Text: "open"},
		{Kind: item.TaskItem, Text: "done", Checked: true},
		{Kind: item.Quote, Text: "quoted still"},
		{Kind: item.Panel, Tone: "warning", Text: "Careful now."},
		{Kind: item.CodeBlock, Text: "func x() {}"},
		{Kind: item.CodeBlock, Text: "indented"},
		{Kind: item.Divider},
		{Kind: item.Table, Text: "A\tB\n1\ttwo | three\nx\t"},
	}
	if d.Title != "Title" {
		t.Errorf("title %q", d.Title)
	}
	if !reflect.DeepEqual(d.Blocks, want) {
		for i := range max(len(d.Blocks), len(want)) {
			var g, w item.Block
			if i < len(d.Blocks) {
				g = d.Blocks[i]
			}
			if i < len(want) {
				w = want[i]
			}
			if !reflect.DeepEqual(g, w) {
				t.Errorf("block %d:\n got %+v\nwant %+v", i+1, g, w)
			}
		}
	}
}

func TestAlerts(t *testing.T) {
	// As pasted Markdown maps them (ADR-0133).
	for marker, tone := range map[string]string{"NOTE": "info", "TIP": "tip", "IMPORTANT": "note", "WARNING": "warning", "CAUTION": "error", "note": "info"} {
		d := conv(t, "> [!"+marker+"]\n> Mind *this*.\n>\n> - and that", repo())
		want := []item.Block{
			{Kind: item.Panel, Tone: tone, Text: "Mind this.", Marks: []item.Mark{{Type: item.MarkItalic, Start: 5, End: 9}}},
			{Kind: item.Panel, Tone: tone, Text: "• and that"},
		}
		if !reflect.DeepEqual(d.Blocks, want) {
			t.Errorf("%s: %+v", marker, d.Blocks)
		}
	}
	// A marker alone on its paragraph, and a marker that is none.
	d := conv(t, "> [!TIP]\n\n> [!OTHER]\n> text", repo())
	if len(d.Blocks) != 1 || d.Blocks[0].Kind != item.Quote || d.Blocks[0].Text != "[!OTHER] text" {
		t.Errorf("%+v", d.Blocks)
	}
}

func TestLinks(t *testing.T) {
	src := `[rel](install.md#linux) [up](../README.md) [abs](https://other.org) [mail](mailto:a@b.ch)
[js](javascript:alert(1)) [anchor](#part) <https://auto.org> <x@y.ch> www.bare.ch https://plain.org
[ref][r]

[r]: ./ref.md`
	d := conv(t, src, repo())
	if len(d.Blocks) != 1 {
		t.Fatalf("%+v", d.Blocks)
	}
	got := map[string]string{}
	runes := []rune(d.Blocks[0].Text)
	for _, m := range d.Blocks[0].Marks {
		got[string(runes[m.Start:m.End])] = m.Href
	}
	want := map[string]string{
		"rel": "https://git.example.org/o/r/blob/main/docs/install.md#linux", "up": "https://git.example.org/o/r/blob/main/README.md",
		"abs": "https://other.org", "mail": "mailto:a@b.ch", "https://auto.org": "https://auto.org", "x@y.ch": "mailto:x@y.ch",
		"www.bare.ch": "https://www.bare.ch", "https://plain.org": "https://plain.org", "ref": "https://git.example.org/o/r/blob/main/docs/ref.md",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("links %v,\nwant %v", got, want)
	}
	if !strings.Contains(d.Blocks[0].Text, "js anchor") {
		t.Errorf("a link that is dropped keeps its text: %q", d.Blocks[0].Text)
	}
	// Without resolvers only safe absolute targets stay.
	d = conv(t, "[rel](a.md) [abs](https://x.org) ![p](a.png) ![q](https://x.org/q.png)", Source{Name: "a.md"})
	if len(d.Blocks) != 1 || len(d.Blocks[0].Marks) != 2 || d.Blocks[0].Marks[0].Href != "https://x.org" || d.Blocks[0].Marks[1].Href != "https://x.org/q.png" {
		t.Errorf("%+v", d.Blocks)
	}
}

func TestEscapes(t *testing.T) {
	d := conv(t, `\*not em\* &amp; &#x41;&#66; &copy; `+"`a\\*b`"+` <b>bold</b> a<br>b`, repo())
	if len(d.Blocks) != 1 || d.Blocks[0].Text != "*not em* & AB © a\\*b bold a\nb" {
		t.Errorf("%q", d.Blocks[0].Text)
	}
}

func TestPictures(t *testing.T) {
	src := `![Logo](img/logo.png "The logo")

[![Badge](https://shields.example/b.svg)](https://ci.example) ![](pics/x.jpg)

Text with ![inline](i.png) picture.

<p align="center">
  <img src="banner.png" alt="Banner &amp; more" width="300">
  <br>Tagline
</p>

<!-- a comment -->

<script>alert(1)</script>

<details><summary>More</summary>

Hidden *text*.

</details>`
	d := conv(t, src, repo())
	pics := []Picture{
		{Block: 0, Src: "https://raw.example.org/o/r/main/docs/img/logo.png", Alt: "Logo"},
		{Block: 1, Src: "https://shields.example/b.svg", Alt: "Badge"},
		{Block: 2, Src: "https://raw.example.org/o/r/main/docs/pics/x.jpg", Alt: ""},
		{Block: 4, Src: "https://raw.example.org/o/r/main/docs/banner.png", Alt: "Banner & more"},
	}
	if !reflect.DeepEqual(d.Pictures, pics) {
		t.Errorf("pictures %+v,\nwant %+v", d.Pictures, pics)
	}
	texts := []string{}
	for _, b := range d.Blocks {
		texts = append(texts, b.Text)
	}
	want := []string{"Logo", "Badge", "x.jpg", "Text with inline picture.", "Banner & more", "Tagline", "More", "Hidden text."}
	if !reflect.DeepEqual(texts, want) {
		t.Errorf("texts %q,\nwant %q", texts, want)
	}
	if m := d.Blocks[0].Marks; len(m) != 1 || m[0].Href != pics[0].Src {
		t.Errorf("a picture links to itself until it is read: %+v", d.Blocks[0])
	}
	if m := d.Blocks[3].Marks; len(m) != 1 || m[0].Href != "https://raw.example.org/o/r/main/docs/i.png" {
		t.Errorf("a picture within text links its alternative text: %+v", d.Blocks[3])
	}
	d.Show(d.Pictures[0], "0123456789abcdef0123456789abcdef")
	if b := d.Blocks[0]; b.Kind != item.Image || b.Ref != "0123456789abcdef0123456789abcdef" || b.Text != "Logo" || len(b.Marks) != 0 {
		t.Errorf("shown: %+v", b)
	}
	if err := item.CheckBlock(d.Blocks[0], "self"); err != nil {
		t.Error(err)
	}
}

func TestPictureInLinkUsesTheLink(t *testing.T) {
	d := conv(t, "See [![shot](s.png) here](https://site.org).", repo())
	if len(d.Blocks) != 1 || len(d.Blocks[0].Marks) != 1 || d.Blocks[0].Marks[0].Href != "https://site.org" || d.Blocks[0].Text != "See shot here." {
		t.Errorf("%+v", d.Blocks)
	}
}

func TestPicturesInQuotesAndLists(t *testing.T) {
	d := conv(t, "> ![q](q.png)\n\n- ![l](l.png)\n- [ ]\n", repo())
	if len(d.Blocks) != 3 || d.Blocks[0].Kind != item.Quote || d.Blocks[1].Kind != item.BulletItem || d.Blocks[2].Kind != item.TaskItem {
		t.Errorf("%+v", d.Blocks)
	}
	if len(d.Pictures) != 1 || d.Pictures[0].Block != 0 {
		t.Errorf("only a paragraph of pictures shows them: %+v", d.Pictures)
	}
}

func TestPictureWithoutAddress(t *testing.T) {
	s := repo()
	s.Picture = func(string) string { return "" }
	d := conv(t, "![alt](x.png)\n\n![](y.png)", s)
	if len(d.Pictures) != 0 || len(d.Blocks) != 2 || d.Blocks[0].Text != "alt" || d.Blocks[1].Text != "y.png" || len(d.Blocks[0].Marks) != 0 {
		t.Errorf("%+v %+v", d.Blocks, d.Pictures)
	}
}

func TestLeadingPictureBeforeTitleShiftsWithIt(t *testing.T) {
	d := conv(t, "# T\n\n![a](a.png)", repo())
	if d.Title != "T" || len(d.Pictures) != 1 || d.Pictures[0].Block != 0 || d.Blocks[0].Text != "a" {
		t.Errorf("%+v %+v", d.Blocks, d.Pictures)
	}
}

func TestInvalidUTF8AndNoise(t *testing.T) {
	d := conv(t, "caf\xe9 *\n\n  \n\n<div></div>\n\n- \n\n## \n", repo())
	if len(d.Blocks) != 1 || d.Blocks[0].Text != "caf\uFFFD *" {
		t.Errorf("%+v", d.Blocks)
	}
}

func TestHeadingInQuote(t *testing.T) {
	d := conv(t, "> ## Inside\n> text", repo())
	if len(d.Blocks) != 2 || d.Blocks[0].Kind != item.Quote || d.Blocks[0].Text != "Inside" {
		t.Errorf("%+v", d.Blocks)
	}
}

func TestListItemsKeepTheirFurtherBlocks(t *testing.T) {
	d := conv(t, "1. first\n\n   more\n\n   ```\n   code\n   ```\n2. second", repo())
	kinds := []item.BlockKind{}
	for _, b := range d.Blocks {
		kinds = append(kinds, b.Kind)
	}
	if !reflect.DeepEqual(kinds, []item.BlockKind{item.OrderedItem, item.Paragraph, item.CodeBlock, item.OrderedItem}) {
		t.Errorf("%v", kinds)
	}
}

func TestMergeMarks(t *testing.T) {
	d := conv(t, "**a** **b** ***c***", repo())
	want := []item.Mark{{Type: item.MarkBold, Start: 0, End: 1}, {Type: item.MarkBold, Start: 2, End: 3}, {Type: item.MarkBold, Start: 4, End: 5}, {Type: item.MarkItalic, Start: 4, End: 5}}
	if !reflect.DeepEqual(d.Blocks[0].Marks, want) {
		t.Errorf("%+v", d.Blocks[0].Marks)
	}
}

// Inside a quote, list items keep their marker as text, and a table's
// cell keeps a link's address, as pasted Markdown does (ADR-0133).
func TestQuotedListsAndTableLinks(t *testing.T) {
	d := conv(t, "> - one\n> 3. three\n> 4. four\n> - [ ] open\n> - [x] done\n\n| A | B |\n|---|---|\n| [site](https://x.org) | <https://y.org> |", repo())
	var texts []string
	for _, b := range d.Blocks {
		texts = append(texts, b.Text)
	}
	want := []string{"• one", "3. three", "4. four", "☐ open", "☑ done", "A\tB\nsite (https://x.org)\thttps://y.org"}
	if !reflect.DeepEqual(texts, want) {
		t.Errorf("%q,\nwant %q", texts, want)
	}
	if d.Blocks[0].Kind != item.Quote || d.Blocks[3].Kind != item.Quote {
		t.Errorf("%+v", d.Blocks)
	}
}
