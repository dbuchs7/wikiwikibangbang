// Package markdown converts Markdown into the block document of an item
// (ADR-0131). goldmark parses the text; this package walks its syntax tree
// and creates every block and mark itself. No renderer of goldmark is ever
// called, so Markdown never reaches a browser as markup and the closed
// renderer of ADR-0014 stays the only one.
package markdown

import (
	"bytes"
	"path"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/yuin/goldmark/ast"
	east "github.com/yuin/goldmark/extension"
	extast "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"

	"github.com/dbuchs7/wikiwikibangbang/internal/item"
)

// Source says where a Markdown file lives: its name, the title when the
// file names none, and how the targets of its links and the sources of
// its pictures resolve to addresses.
type Source struct {
	Name string
	// Link resolves a link's target as written, e.g. "../install.md#linux"
	// or "https://example.org", to the address it leads to; "" drops the
	// link and keeps its text. Nil keeps http, https and mailto targets
	// and drops the others.
	Link func(target string) string
	// Picture resolves a picture's source as written to the address it is
	// read from; "" keeps only its alternative text. Nil keeps http and
	// https sources.
	Picture func(src string) string
}

// Picture is a picture a converted file shows. Until it is read, its block
// is a paragraph that links its alternative text to the picture's address;
// Show turns that block into the picture.
type Picture struct {
	// Block is the index of the block that shows it.
	Block int
	// Src is the address it is read from.
	Src string
	// Alt is its alternative text, which becomes its caption.
	Alt string
}

// Doc is a converted Markdown file.
type Doc struct {
	Title    string
	Blocks   []item.Block
	Pictures []Picture
}

// Show turns the block of picture p into an image of the attachment id,
// captioned with its alternative text.
func (d *Doc) Show(p Picture, id item.ID) {
	d.Blocks[p.Block] = item.Block{Kind: item.Image, Ref: id, Text: oneLine(p.Alt)}
}

// alerts map the markers that open a quote to the tones of a panel.
var alerts = map[string]string{"NOTE": "note", "TIP": "tip", "IMPORTANT": "info", "WARNING": "warning", "CAUTION": "error"}

// newParser is goldmark's CommonMark parser with the extensions for
// tables, strike-through, task lists and bare addresses, without any
// renderer.
func newParser() parser.Parser {
	return parser.NewParser(
		parser.WithBlockParsers(parser.DefaultBlockParsers()...),
		parser.WithInlineParsers(append(parser.DefaultInlineParsers(),
			util.Prioritized(east.NewStrikethroughParser(), 500),
			util.Prioritized(east.NewTaskCheckBoxParser(), 0),
			util.Prioritized(east.NewLinkifyParser(), 999))...),
		parser.WithParagraphTransformers(append(parser.DefaultParagraphTransformers(),
			util.Prioritized(east.NewTableParagraphTransformer(), 200))...),
		parser.WithASTTransformers(util.Prioritized(east.NewTableASTTransformer(), 0)),
	)
}

// Convert converts a Markdown file into blocks. The title is the front
// matter's title, else a leading heading of level 1, which then leaves the
// blocks, else the file's name without its extension.
func Convert(src []byte, s Source) Doc {
	if s.Link == nil {
		s.Link = keepSafe
	}
	if s.Picture == nil {
		s.Picture = keepWeb
	}
	src = bytes.ToValidUTF8(src, []byte("�"))
	body, title := frontMatter(src)
	c := converter{src: body, s: s}
	root := newParser().Parse(text.NewReader(body))
	c.blocks(root, item.Paragraph, "")
	d := Doc{Title: title, Blocks: c.out, Pictures: c.pictures}
	if d.Title == "" && len(d.Blocks) > 0 && d.Blocks[0].Kind == item.Heading && c.firstH1 {
		d.Title = d.Blocks[0].Text
		d.Blocks = d.Blocks[1:]
		for i := range d.Pictures {
			d.Pictures[i].Block--
		}
	}
	if d.Title == "" {
		d.Title = strings.TrimSuffix(path.Base(s.Name), path.Ext(s.Name))
	}
	d.Title = oneLine(d.Title)
	return d
}

// keepSafe keeps the link targets content may hold (ADR-0014).
func keepSafe(target string) string {
	if item.SafeHref(target) {
		return target
	}
	return ""
}

// keepWeb keeps the picture sources that are web addresses.
func keepWeb(src string) string {
	if item.SafeHref(src) && !strings.HasPrefix(strings.ToLower(src), "mailto:") {
		return src
	}
	return ""
}

// frontMatter cuts a front matter (a block between lines of "---" at the
// very start) off the text and returns its title, if it names one.
func frontMatter(src []byte) ([]byte, string) {
	rest, ok := bytes.CutPrefix(src, []byte("---\n"))
	if !ok {
		rest, ok = bytes.CutPrefix(src, []byte("---\r\n"))
	}
	if !ok {
		return src, ""
	}
	title := ""
	for n := 0; len(rest) > 0 && n < 200; n++ {
		line, after, _ := bytes.Cut(rest, []byte("\n"))
		rest = after
		l := strings.TrimRight(string(line), "\r ")
		if l == "---" || l == "..." {
			return rest, title
		}
		if v, found := strings.CutPrefix(l, "title:"); found && title == "" {
			title = unquote(strings.TrimSpace(v))
		}
	}
	return src, ""
}

// unquote takes a YAML scalar out of its quotes.
func unquote(v string) string {
	if len(v) >= 2 && (v[0] == '"' && v[len(v)-1] == '"') {
		if u, err := strconv.Unquote(v); err == nil {
			return u
		}
	}
	if len(v) >= 2 && v[0] == '\'' && v[len(v)-1] == '\'' {
		return strings.ReplaceAll(v[1:len(v)-1], "''", "'")
	}
	return v
}

type converter struct {
	src      []byte
	s        Source
	out      []item.Block
	pictures []Picture
	// firstH1 is whether the first block written is a heading of level 1.
	firstH1 bool
}

// blocks converts the block children of n. kind is the kind a paragraph
// becomes inside a container: a quote's paragraphs are quotes, a panel's
// paragraphs are panels of tone.
func (c *converter) blocks(n ast.Node, kind item.BlockKind, tone string) {
	for ch := n.FirstChild(); ch != nil; ch = ch.NextSibling() {
		c.block(ch, kind, tone)
	}
}

func (c *converter) block(n ast.Node, kind item.BlockKind, tone string) {
	switch n := n.(type) {
	case *ast.Heading:
		r := c.inline(n)
		if kind != item.Paragraph {
			c.text(r, kind, tone)
			return
		}
		if r.empty() {
			return
		}
		if len(c.out) == 0 && n.Level == 1 {
			c.firstH1 = true
		}
		b := r.block(item.Heading)
		b.Text = strings.ReplaceAll(b.Text, "\n", " ")
		b.Level = min(n.Level, 3)
		c.out = append(c.out, b)
	case *ast.Paragraph, *ast.TextBlock:
		c.paragraph(n, kind, tone)
	case *ast.ThematicBreak:
		c.out = append(c.out, item.Block{Kind: item.Divider})
	case *ast.FencedCodeBlock, *ast.CodeBlock:
		var buf bytes.Buffer
		lines := n.Lines()
		for i := 0; i < lines.Len(); i++ {
			seg := lines.At(i)
			buf.Write(seg.Value(c.src))
		}
		if code := strings.TrimRight(buf.String(), "\n"); code != "" {
			c.out = append(c.out, item.Block{Kind: item.CodeBlock, Text: code})
		}
	case *ast.Blockquote:
		if t, rest := c.alert(n); t != "" {
			c.blocks(rest, item.Panel, t)
			return
		}
		c.blocks(n, item.Quote, "")
	case *ast.List:
		c.list(n, kind, tone)
	case *ast.HTMLBlock:
		c.htmlBlock(n, kind, tone)
	case *extast.Table:
		c.table(n)
	default:
		// A container this walker does not know: its blocks still count.
		c.blocks(n, kind, tone)
	}
}

// list writes a list's items as flat blocks, as the body model holds
// lists (ADR-0032): an item's first paragraph is the item, its further
// blocks follow, and a list inside it continues the flat list.
func (c *converter) list(l *ast.List, kind item.BlockKind, tone string) {
	itemKind := item.BulletItem
	if l.IsOrdered() {
		itemKind = item.OrderedItem
	}
	if kind != item.Paragraph {
		itemKind = kind
	}
	for li := l.FirstChild(); li != nil; li = li.NextSibling() {
		first := true
		for ch := li.FirstChild(); ch != nil; ch = ch.NextSibling() {
			switch ch.(type) {
			case *ast.Paragraph, *ast.TextBlock:
				if !first {
					c.paragraph(ch, kind, tone)
					continue
				}
				first = false
				r := c.inline(ch)
				k := itemKind
				if r.task != nil && kind == item.Paragraph {
					k = item.TaskItem
				}
				b := r.block(k)
				if k == item.TaskItem {
					b.Checked = *r.task
				}
				if k == item.Panel {
					b.Tone = tone
				}
				if !r.empty() || k == item.TaskItem {
					c.out = append(c.out, b)
				}
			default:
				first = false
				c.block(ch, kind, tone)
			}
		}
	}
}

// alert reads the marker that makes a quote a panel, e.g. "[!NOTE]" on
// its first line, and returns the panel's tone and the quote without the
// marker.
func (c *converter) alert(q *ast.Blockquote) (string, ast.Node) {
	p, ok := q.FirstChild().(*ast.Paragraph)
	if !ok {
		return "", nil
	}
	first, ok := p.FirstChild().(*ast.Text)
	if !ok {
		return "", nil
	}
	// The marker is parsed as a link label of its own: "[" "!NOTE" "]",
	// or as one text; reading the line from the source covers both.
	lines := p.Lines()
	if lines.Len() == 0 {
		return "", nil
	}
	seg := lines.At(0)
	line := strings.TrimSpace(string(seg.Value(c.src)))
	marker, ok := strings.CutPrefix(line, "[!")
	if !ok {
		return "", nil
	}
	name, ok := strings.CutSuffix(marker, "]")
	tone := alerts[strings.ToUpper(name)]
	if !ok || tone == "" {
		return "", nil
	}
	// Drop the inline nodes of the marker's line.
	for n := ast.Node(first); n != nil; {
		next := n.NextSibling()
		t, isText := n.(*ast.Text)
		p.RemoveChild(p, n)
		if isText && (t.SoftLineBreak() || t.HardLineBreak()) {
			break
		}
		n = next
	}
	return tone, q
}

// paragraph writes a paragraph: as pictures when it holds only pictures,
// else as one block of text.
func (c *converter) paragraph(n ast.Node, kind item.BlockKind, tone string) {
	if pics, ok := c.onlyPictures(n); ok && len(pics) > 0 {
		for _, p := range pics {
			c.picture(p.src, p.alt, kind, tone)
		}
		return
	}
	c.text(c.inline(n), kind, tone)
}

// text appends a block of text of kind, if it holds any.
func (c *converter) text(r *run, kind item.BlockKind, tone string) {
	if r.empty() {
		return
	}
	b := r.block(kind)
	if kind == item.Panel {
		b.Tone = tone
	}
	c.out = append(c.out, b)
}

// picture appends the block of a picture: a paragraph that links its
// alternative text to its address, until Show makes it the picture.
func (c *converter) picture(src, alt string, kind item.BlockKind, tone string) {
	addr := c.s.Picture(src)
	label := oneLine(alt)
	if label == "" {
		label = path.Base(src)
	}
	r := &run{}
	r.add(label, nil)
	if addr != "" && item.SafeHref(addr) {
		r.marks = append(r.marks, item.Mark{Type: item.MarkLink, Start: 0, End: r.n, Href: addr})
	}
	if r.empty() {
		return
	}
	if kind != item.Paragraph && kind != item.Quote && kind != item.Panel {
		kind = item.Paragraph
	}
	b := r.block(kind)
	if kind == item.Panel {
		b.Tone = tone
	}
	c.out = append(c.out, b)
	if addr != "" {
		c.pictures = append(c.pictures, Picture{Block: len(c.out) - 1, Src: addr, Alt: alt})
	}
}

type pic struct{ src, alt string }

// onlyPictures reports the pictures of a paragraph that holds nothing
// else: pictures, links around them, line breaks and spaces.
func (c *converter) onlyPictures(n ast.Node) ([]pic, bool) {
	var out []pic
	ok := true
	_ = ast.Walk(n, func(m ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering || m == n {
			return ast.WalkContinue, nil
		}
		switch m := m.(type) {
		case *ast.Image:
			out = append(out, pic{src: c.dest(m.Destination), alt: c.plain(m)})
			return ast.WalkSkipChildren, nil
		case *ast.Link:
			return ast.WalkContinue, nil
		case *ast.RawHTML:
			// A tag shows no text of its own; the text between tags is
			// a text node.
			out = append(out, imgTags(c.raw(m))...)
		case *ast.Text:
			if strings.TrimSpace(string(m.Value(c.src))) != "" {
				ok = false
			}
		default:
			ok = false
		}
		if !ok {
			return ast.WalkStop, nil
		}
		return ast.WalkContinue, nil
	})
	return out, ok
}

// table writes a table's cells as plain text, rows by lines and cells by
// tabs, as the body model holds a table (ADR-0042).
func (c *converter) table(t *extast.Table) {
	var rows [][]string
	width := 0
	for row := t.FirstChild(); row != nil; row = row.NextSibling() {
		var cells []string
		for cell := row.FirstChild(); cell != nil; cell = cell.NextSibling() {
			r := c.inline(cell)
			cells = append(cells, strings.Join(strings.Fields(strings.ReplaceAll(r.text.String(), "\t", " ")), " "))
		}
		width = max(width, len(cells))
		rows = append(rows, cells)
	}
	if len(rows) == 0 || width == 0 {
		return
	}
	lines := make([]string, len(rows))
	for i, cells := range rows {
		for len(cells) < width {
			cells = append(cells, "")
		}
		lines[i] = strings.Join(cells, "\t")
	}
	c.out = append(c.out, item.Block{Kind: item.Table, Text: strings.Join(lines, "\n")})
}

// dest is a link's or picture's target with its escapes and character
// references resolved, as CommonMark reads it.
func (c *converter) dest(d []byte) string {
	d = util.UnescapePunctuations(d)
	d = util.ResolveNumericReferences(d)
	d = util.ResolveEntityNames(d)
	return string(d)
}

// raw is the source of a piece of inline HTML.
func (c *converter) raw(h *ast.RawHTML) string {
	var b strings.Builder
	for i := 0; i < h.Segments.Len(); i++ {
		seg := h.Segments.At(i)
		b.Write(seg.Value(c.src))
	}
	return b.String()
}

// plain is the text of a node's inline children, without formatting.
func (c *converter) plain(n ast.Node) string {
	r := &run{}
	c.walk(n, r, nil)
	return r.text.String()
}

// run collects the text of one block and its marks, counted in code
// points as the body model counts them (ADR-0032).
type run struct {
	text  strings.Builder
	n     int
	marks []item.Mark
	// task is set when the text opens with a task's check box.
	task *bool
}

func (r *run) add(s string, open []item.Mark) {
	if s == "" {
		return
	}
	start := r.n
	r.text.WriteString(s)
	r.n += utf8.RuneCountInString(s)
	for _, m := range open {
		m.Start, m.End = start, r.n
		r.marks = append(r.marks, m)
	}
}

func (r *run) empty() bool { return strings.TrimSpace(r.text.String()) == "" }

// block makes the run a block of kind: its text trimmed and its marks
// merged where they touch.
func (r *run) block(kind item.BlockKind) item.Block {
	full := r.text.String()
	trimmed := strings.TrimRightFunc(strings.TrimLeft(full, " \n\t"), func(c rune) bool { return c == ' ' || c == '\n' || c == '\t' })
	lead := utf8.RuneCountInString(full[:len(full)-len(strings.TrimLeft(full, " \n\t"))])
	n := utf8.RuneCountInString(trimmed)
	b := item.Block{Kind: kind, Text: trimmed}
	for _, m := range mergeMarks(r.marks) {
		m.Start, m.End = max(m.Start-lead, 0), min(m.End-lead, n)
		if m.End > m.Start {
			b.Marks = append(b.Marks, m)
		}
	}
	if kind == item.CodeBlock || kind == item.Table || kind == item.Unsupported {
		b.Marks = nil
	}
	return b
}

// mergeMarks joins marks of one kind and target that touch or overlap.
func mergeMarks(marks []item.Mark) []item.Mark {
	slices.SortStableFunc(marks, func(a, b item.Mark) int {
		if a.Type != b.Type {
			return strings.Compare(string(a.Type), string(b.Type))
		}
		if a.Href != b.Href {
			return strings.Compare(a.Href, b.Href)
		}
		return a.Start - b.Start
	})
	var out []item.Mark
	for _, m := range marks {
		if k := len(out) - 1; k >= 0 && out[k].Type == m.Type && out[k].Href == m.Href && m.Start <= out[k].End {
			out[k].End = max(out[k].End, m.End)
			continue
		}
		out = append(out, m)
	}
	slices.SortStableFunc(out, func(a, b item.Mark) int { return a.Start - b.Start })
	return out
}

// inline converts the inline children of a block.
func (c *converter) inline(n ast.Node) *run {
	r := &run{}
	c.walk(n, r, nil)
	return r
}

// walk adds the text of n's inline children to r, formatted by the marks
// open around them.
func (c *converter) walk(n ast.Node, r *run, open []item.Mark) {
	for ch := n.FirstChild(); ch != nil; ch = ch.NextSibling() {
		c.inlineNode(ch, r, open)
	}
}

func (c *converter) inlineNode(n ast.Node, r *run, open []item.Mark) {
	with := func(m item.Mark) []item.Mark { return append(slices.Clone(open), m) }
	switch n := n.(type) {
	case *ast.Text:
		v := n.Value(c.src)
		if !n.IsRaw() {
			v = c.unescape(v)
		}
		r.add(string(v), open)
		switch {
		case n.HardLineBreak():
			r.add("\n", nil)
		case n.SoftLineBreak():
			r.add(" ", nil)
		}
	case *ast.String:
		v := n.Value
		if !n.IsCode() && !n.IsRaw() {
			v = c.unescape(v)
		}
		r.add(string(v), open)
	case *ast.CodeSpan:
		var b strings.Builder
		for ch := n.FirstChild(); ch != nil; ch = ch.NextSibling() {
			switch t := ch.(type) {
			case *ast.Text:
				b.Write(t.Value(c.src))
			case *ast.String:
				b.Write(t.Value)
			}
		}
		r.add(b.String(), with(item.Mark{Type: item.MarkCode}))
	case *ast.Emphasis:
		kind := item.MarkItalic
		if n.Level >= 2 {
			kind = item.MarkBold
		}
		c.walk(n, r, with(item.Mark{Type: kind}))
	case *extast.Strikethrough:
		c.walk(n, r, with(item.Mark{Type: item.MarkStrike}))
	case *ast.Link:
		if href := c.s.Link(c.dest(n.Destination)); href != "" && item.SafeHref(href) {
			c.walk(n, r, with(item.Mark{Type: item.MarkLink, Href: href}))
			return
		}
		c.walk(n, r, open)
	case *ast.AutoLink:
		label := string(n.Label(c.src))
		href := string(n.URL(c.src))
		switch {
		case n.AutoLinkType == ast.AutoLinkEmail && !strings.HasPrefix(strings.ToLower(href), "mailto:"):
			href = "mailto:" + href
		case strings.HasPrefix(strings.ToLower(label), "www."):
			// A bare www. address is taken as https (ADR-0096).
			href = "https://" + label
		}
		if item.SafeHref(href) {
			r.add(label, with(item.Mark{Type: item.MarkLink, Href: href}))
			return
		}
		r.add(label, open)
	case *ast.Image:
		// A picture within text shows as its alternative text, linked to
		// the picture; a paragraph of pictures alone shows them (paragraph).
		alt := c.plain(n)
		if alt == "" {
			alt = path.Base(c.dest(n.Destination))
		}
		c.inlinePicture(r, open, c.dest(n.Destination), alt)
	case *ast.RawHTML:
		raw := c.raw(n)
		if brTag.MatchString(raw) {
			r.add("\n", nil)
			return
		}
		for _, p := range imgTags(raw) {
			c.inlinePicture(r, open, p.src, p.alt)
		}
	case *extast.TaskCheckBox:
		checked := n.IsChecked
		r.task = &checked
	default:
		c.walk(n, r, open)
	}
}

// inlinePicture adds a picture within text as its alternative text,
// linked to the picture unless a link around it leads elsewhere.
func (c *converter) inlinePicture(r *run, open []item.Mark, src, alt string) {
	if alt == "" {
		return
	}
	inLink := slices.ContainsFunc(open, func(m item.Mark) bool { return m.Type == item.MarkLink })
	if addr := c.s.Picture(src); addr != "" && item.SafeHref(addr) && !inLink {
		r.add(alt, append(slices.Clone(open), item.Mark{Type: item.MarkLink, Href: addr}))
		return
	}
	r.add(alt, open)
}

// unescape resolves backslash escapes and character references of text,
// as CommonMark reads them.
func (c *converter) unescape(v []byte) []byte {
	v = util.UnescapePunctuations(v)
	v = util.ResolveNumericReferences(v)
	return util.ResolveEntityNames(v)
}

// oneLine makes a text one line.
func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }
