package export

import (
	"archive/zip"
	"fmt"
	"html"
	"io"
	"net/url"
	"slices"
	"strings"
	"unicode"

	"github.com/dbuchs7/wikiwikibangbang/internal/item"
)

// Markdown writes the document as Markdown text (ADR-0131), for people
// and for agents: the title, the facts as a list, then the body. Pictures
// and videos link to their attachments on this server. Text the reader
// wrote is escaped, so no Markdown renderer reads it as markup or HTML
// (ADR-0014); the only HTML written is the server's own <ins>, <mark>, and
// <em>, <strong> or <del> where Markdown cannot nest formatting.
func Markdown(w io.Writer, d Document) error {
	m := &mdBuilder{d: d, picture: func(p Page, ref item.ID, _ bool) string {
		if _, ok := p.Images[ref]; !ok {
			return ""
		}
		return "/api/items/" + url.PathEscape(string(p.ID)) + "/attachments/" + url.PathEscape(string(ref))
	}}
	_, err := io.WriteString(w, m.document())
	return err
}

// MarkdownArchive writes the document as a ZIP archive: the Markdown as
// name, and the pictures it shows in media/, linked by relative paths, so
// the archive reads anywhere without the server. A video is named, not
// copied (ADR-0097), and so is a BPMN diagram; a picture without its file
// keeps its caption.
func MarkdownArchive(w io.Writer, d Document, name string) error {
	type file struct {
		path string
		data []byte
	}
	var files []file
	seen := map[[2]item.ID]string{}
	m := &mdBuilder{d: d, picture: func(p Page, ref item.ID, named bool) string {
		img, ok := p.Images[ref]
		if named || !ok || len(img.Data) == 0 {
			return ""
		}
		if path, ok := seen[[2]item.ID{p.ID, ref}]; ok {
			return path
		}
		path := fmt.Sprintf("media/%02d-%s", len(files)+1, mediaName(img))
		seen[[2]item.ID{p.ID, ref}] = path
		files = append(files, file{path, img.Data})
		return path
	}}
	text := m.document()
	z := zip.NewWriter(w)
	all := append([]file{{strings.NewReplacer("/", "-", `\`, "-").Replace(name), []byte(text)}}, files...)
	for _, f := range all {
		fw, err := z.Create(f.path)
		if err != nil {
			return err
		}
		if _, err := fw.Write(f.data); err != nil {
			return err
		}
	}
	return z.Close()
}

// mediaName is a picture's file name in an archive: letters, digits, dots
// and dashes only, so it cannot leave the media folder, with an extension
// by its type.
func mediaName(img Image) string {
	base := baseName(img.Name)
	var b strings.Builder
	for _, r := range base {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '.' || r == '_' {
			b.WriteRune(r)
		} else if s := b.String(); s != "" && !strings.HasSuffix(s, "-") {
			b.WriteRune('-')
		}
	}
	name := strings.Trim(strings.ReplaceAll(b.String(), "-.", "."), "-.")
	if r := []rune(name); len(r) > 60 {
		name = string(r[len(r)-60:])
	}
	ext := map[string]string{"image/png": ".png", "image/jpeg": ".jpg", "image/gif": ".gif", "image/webp": ".webp", "image/svg+xml": ".svg"}[img.Type]
	if name == "" {
		return "image" + ext
	}
	if !strings.Contains(name, ".") {
		name += ext
	}
	return name
}

// baseName is a file name without any folder.
func baseName(name string) string {
	if i := strings.LastIndexAny(name, `/\`); i >= 0 {
		return name[i+1:]
	}
	return name
}

type mdBuilder struct {
	d Document
	// picture names the target of a picture, or of a file that is named
	// only (a video, a diagram), or "" where there is none.
	picture func(p Page, ref item.ID, named bool) string
}

// alertOf is the GitHub alert a panel's tone writes.
var alertOf = map[string]string{"info": "NOTE", "tip": "TIP", "note": "IMPORTANT", "warning": "WARNING", "error": "CAUTION"}

func (m *mdBuilder) document() string {
	var parts []string
	for i, p := range m.d.Pages {
		if i == 0 {
			parts = append(parts, "# "+headingText(p.Title, nil))
			if m.d.Contents != "" && len(m.d.Pages) > 1 {
				var lines []string
				for _, c := range m.d.Pages[1:] {
					lines = append(lines, strings.Repeat("  ", max(c.Depth-1, 0))+"- "+inlineMarkdown(c.Title, nil))
				}
				parts = append(parts, "## "+headingText(m.d.Contents, nil), strings.Join(lines, "\n"))
			}
		} else {
			parts = append(parts, strings.Repeat("#", min(p.Depth+1, 6))+" "+headingText(p.Title, nil))
		}
		parts = append(parts, m.page(p, p.Depth+1, map[item.ID]bool{p.ID: true}, 0)...)
	}
	return strings.Join(parts, "\n\n") + "\n"
}

// page writes an item's facts and body as blocks of Markdown; base is the
// heading level of its title, so its own headings come below it. Items of
// one list follow each other line by line.
func (m *mdBuilder) page(p Page, base int, path map[item.ID]bool, embedDepth int) []string {
	var parts []string
	if len(p.Facts) > 0 {
		var lines []string
		for _, f := range p.Facts {
			lines = append(lines, "- **"+inlineMarkdown(f[0]+":", nil)+"** "+inlineMarkdown(f[1], nil))
		}
		parts = append(parts, strings.Join(lines, "\n"))
	}
	var list item.BlockKind // the kind of list the last part holds, if any
	n := 0                  // the number of the last item of a numbered list
	for _, bl := range p.Body {
		marker := map[item.BlockKind]string{item.BulletItem: "- ", item.TaskItem: "- [ ] "}[bl.Kind]
		switch bl.Kind {
		case item.OrderedItem:
			if list != item.OrderedItem {
				n = 0
			}
			n++
			marker = fmt.Sprintf("%d. ", n)
		case item.TaskItem:
			if bl.Checked {
				marker = "- [x] "
			}
		}
		if marker != "" {
			line := marker + indent(inlineMarkdown(bl.Text, bl.Marks), len(marker))
			if list == bl.Kind {
				parts[len(parts)-1] += "\n" + line
			} else {
				parts = append(parts, line)
			}
			list = bl.Kind
			continue
		}
		list = ""
		if bl.Kind == item.Embed {
			parts = append(parts, m.embed(bl.Ref, base, path, embedDepth))
			continue
		}
		parts = append(parts, m.block(bl, p, base)...)
	}
	return parts
}

// embed writes an embedded item as a quote: its name, facts and body, or
// its name alone when it is too deep or already open around it; an item
// the reader may not read is named as restricted (D10).
func (m *mdBuilder) embed(id item.ID, base int, path map[item.ID]bool, embedDepth int) string {
	e, ok := m.d.Embeds[id]
	if !ok {
		return quote("↳ " + inlineMarkdown(m.d.Restricted, nil))
	}
	parts := []string{"↳ " + inlineMarkdown(e.name(), nil)}
	if !path[id] && embedDepth < maxEmbedDepth {
		path[id] = true
		parts = append(parts, m.page(e, base+1, path, embedDepth+1)...)
		delete(path, id)
	}
	return quote(strings.Join(parts, "\n\n"))
}

func (m *mdBuilder) block(bl item.Block, p Page, base int) []string {
	list := func(entries []string) []string {
		if len(entries) == 0 {
			return nil
		}
		return []string{strings.Join(entries, "\n")}
	}
	caption := strings.TrimSpace(bl.Text)
	// A paragraph, quote or panel without text has no Markdown.
	if caption == "" && (bl.Kind == item.Paragraph || bl.Kind == item.Quote || bl.Kind == item.Panel) {
		return nil
	}
	captionPart := func() []string {
		if caption == "" {
			return nil
		}
		return []string{"*" + inlineMarkdown(caption, nil) + "*"}
	}
	switch bl.Kind {
	case item.Heading:
		level := min(base+min(max(bl.Level, 1), 3), 6)
		return []string{strings.Repeat("#", level) + " " + headingText(bl.Text, bl.Marks)}
	case item.Quote:
		return []string{quote(inlineMarkdown(bl.Text, bl.Marks))}
	case item.CodeBlock, item.Unsupported:
		fence := strings.Repeat("`", max(3, longestRun(bl.Text, '`')+1))
		return []string{fence + "\n" + bl.Text + "\n" + fence}
	case item.Panel:
		alert := alertOf[bl.Tone]
		if alert == "" {
			alert = "NOTE"
		}
		return []string{quote("[!" + alert + "]\n" + inlineMarkdown(bl.Text, bl.Marks))}
	case item.Table:
		return markdownTable(TableRows(bl.Text))
	case item.Divider:
		return []string{"---"}
	case item.Image:
		img, ok := p.Images[bl.Ref]
		target := m.picture(p, bl.Ref, false)
		if !ok || target == "" {
			return captionPart()
		}
		alt := caption
		if alt == "" {
			alt = baseName(img.Name)
		}
		return append([]string{"![" + inlineMarkdown(alt, nil) + "](" + target + ")"}, captionPart()...)
	case item.Video:
		var out []string
		if v, ok := p.Images[bl.Ref]; ok {
			label := inlineMarkdown("▶ "+baseName(v.Name), nil)
			if target := m.picture(p, bl.Ref, true); target != "" {
				label = "[" + label + "](" + target + ")"
			}
			out = append(out, label)
		}
		return append(out, captionPart()...)
	case item.BPMN, item.Process:
		// A diagram is named as in Word and PDF (ADR-0071): its file, linked
		// where it is reachable, or its process and engine.
		var out []string
		if bl.Kind == item.Process {
			out = append(out, inlineMarkdown("BPMN: "+bl.Process+" ("+bl.Engine+")", nil))
		} else if f, ok := p.Images[bl.Ref]; ok {
			label := inlineMarkdown("BPMN: "+baseName(f.Name), nil)
			if target := m.picture(p, bl.Ref, true); target != "" {
				label = "[" + label + "](" + target + ")"
			}
			out = append(out, label)
		}
		return append(out, captionPart()...)
	case item.TOC:
		var entries []string
		for _, h := range p.Body {
			if h.Kind == item.Heading && strings.TrimSpace(h.Text) != "" {
				entries = append(entries, strings.Repeat("  ", min(max(h.Level, 1), 3)-1)+"- "+inlineMarkdown(h.Text, nil))
			}
		}
		return list(entries)
	case item.Figures, item.Tables:
		var entries []string
		for _, e := range listing(m.d, p.Body, bl.Kind) {
			entries = append(entries, "- "+inlineMarkdown(e, nil))
		}
		return list(entries)
	case item.Children:
		var entries []string
		for _, c := range p.Children {
			entries = append(entries, "- "+inlineMarkdown(c, nil))
		}
		return list(entries)
	case item.TaskList:
		var entries []string
		for _, c := range p.Tasks[bl.Text] {
			entries = append(entries, "- "+inlineMarkdown(c, nil))
		}
		return append([]string{inlineMarkdown("# "+bl.Text, nil)}, list(entries)...)
	default: // a paragraph
		return []string{inlineMarkdown(bl.Text, bl.Marks)}
	}
}

// markdownTable writes a table with its first row as the header; every row has
// as many cells as the widest.
func markdownTable(rows [][]string) []string {
	if len(rows) == 0 {
		return nil
	}
	width := 0
	for _, r := range rows {
		width = max(width, len(r))
	}
	line := func(cells []string) string {
		out := make([]string, width)
		for i := range out {
			if i < len(cells) {
				out[i] = inlineMarkdown(strings.TrimSpace(cells[i]), nil)
			}
		}
		return "| " + strings.Join(out, " | ") + " |"
	}
	lines := []string{line(rows[0]), "|" + strings.Repeat(" --- |", width)}
	for _, r := range rows[1:] {
		lines = append(lines, line(r))
	}
	return []string{strings.Join(lines, "\n")}
}

// quote prefixes every line with "> ".
func quote(s string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		if l == "" {
			lines[i] = ">"
		} else {
			lines[i] = "> " + l
		}
	}
	return strings.Join(lines, "\n")
}

// indent indents the lines after the first by n spaces, so they stay in
// their list item.
func indent(s string, n int) string {
	return strings.ReplaceAll(s, "\n", "\n"+strings.Repeat(" ", n))
}

func longestRun(s string, c rune) int {
	longest, run := 0, 0
	for _, r := range s {
		if r == c {
			run++
			longest = max(longest, run)
		} else {
			run = 0
		}
	}
	return longest
}

// headingText is a heading's text, its closing hashes escaped: a heading
// ending in " #" would lose them.
func headingText(text string, marks []item.Mark) string {
	s := inlineMarkdown(text, marks)
	trimmed := strings.TrimRight(s, "#")
	hashes := len(s) - len(trimmed)
	if hashes == 0 {
		return s
	}
	// The first of them is escaped already after an odd number of backslashes.
	slashes := len(trimmed) - len(strings.TrimRight(trimmed, `\`))
	if slashes%2 == 1 {
		return trimmed + "#" + strings.Repeat(`\#`, hashes-1)
	}
	return trimmed + strings.Repeat(`\#`, hashes)
}

// --- inline text ------------------------------------------------------------

// The formats a run of text can carry, in the order they open when they
// start together: a link outermost.
const (
	fmtLink = 1 << iota
	fmtStrike
	fmtBold
	fmtItalic
	fmtIns
	fmtMark
	fmtCode
)

var opening = []int{fmtLink, fmtStrike, fmtBold, fmtItalic, fmtIns, fmtMark}

// delimited are the formats written with delimiters that CommonMark
// matches by the characters around them.
const delimited = fmtStrike | fmtBold | fmtItalic

func isPunct(r rune) bool { return unicode.IsPunct(r) || unicode.IsSymbol(r) }
func isAlnum(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }

// inlineMarkdown writes a block's text with its marks: formats as
// delimiters, properly nested, and every character of the text that
// Markdown would read as syntax escaped. Marks count Unicode code points
// (ADR-0032); mentions and protected passages are their text. A line
// break at the end is left out: it cannot end a paragraph in Markdown.
func inlineMarkdown(text string, marks []item.Mark) string {
	runes := []rune(strings.TrimRight(text, "\n"))
	n := len(runes)
	formats := make([]int, n)
	hrefs := make([]string, n)
	for _, mk := range marks {
		start, end := min(max(mk.Start, 0), n), min(max(mk.End, 0), n)
		bit := map[item.MarkType]int{item.MarkBold: fmtBold, item.MarkItalic: fmtItalic, item.MarkStrike: fmtStrike,
			item.MarkUnderline: fmtIns, item.MarkHighlight: fmtMark, item.MarkCode: fmtCode}[mk.Type]
		if mk.Type == item.MarkLink && safeLink(mk.Href) {
			bit = fmtLink
		}
		for i := start; i < end && bit != 0; i++ {
			formats[i] |= bit
			if bit == fmtLink {
				hrefs[i] = mk.Href
			}
		}
	}
	for i, r := range runes {
		if r == '\n' {
			formats[i], hrefs[i] = 0, ""
		}
	}
	for _, bit := range []int{fmtStrike, fmtBold, fmtItalic} {
		flank(runes, formats, bit)
	}
	esc := escapes(runes, formats, hrefs)

	// The output is first a list of tokens: text, and the openers and
	// closers of formats. The opener and closer of one occurrence of a
	// format are written as delimiters or, where these would not count,
	// as HTML.
	type occurrence struct {
		bit  int
		href string
		html bool
	}
	type token struct {
		text string // the text written, for a token that is no format's
		occ  int    // the occurrence of a format, or -1
		open bool
	}
	var occs []occurrence
	var toks []token
	render := func(t token) string {
		if t.occ < 0 {
			return t.text
		}
		o := occs[t.occ]
		switch {
		case o.bit == fmtLink && o.html && t.open:
			return `<a href="` + html.EscapeString(o.href) + `">`
		case o.bit == fmtLink && o.html:
			return "</a>"
		case o.bit == fmtLink && t.open:
			return "["
		case o.bit == fmtLink:
			return "](" + linkTarget(o.href) + ")"
		case o.html || o.bit&delimited == 0:
			tag := map[int]string{fmtStrike: "del", fmtBold: "strong", fmtItalic: "em", fmtIns: "ins", fmtMark: "mark"}[o.bit]
			if t.open {
				return "<" + tag + ">"
			}
			return "</" + tag + ">"
		}
		return delimiterOf(o.bit)
	}
	lastRune := func() rune {
		for k := len(toks) - 1; k >= 0; k-- {
			if s := []rune(render(toks[k])); len(s) > 0 {
				return s[len(s)-1]
			}
		}
		return 0
	}
	var stack []int // the open occurrences, outermost first
	push := func(o occurrence) {
		occs = append(occs, o)
		stack = append(stack, len(occs)-1)
		toks = append(toks, token{occ: len(occs) - 1, open: true})
	}
	// moveTo closes what no longer applies and opens what newly does.
	// Formats still open stay open; one closed only because a format
	// around it ended is opened again, as HTML where its delimiter would
	// run into the one just written (CommonMark cannot read "****").
	moveTo := func(want int, href string) {
		wanted := func(k int) bool { return want&occs[k].bit != 0 && (occs[k].bit != fmtLink || occs[k].href == href) }
		keep := 0
		for keep < len(stack) && wanted(stack[keep]) {
			keep++
		}
		var again []occurrence
		for i := len(stack) - 1; i >= keep; i-- {
			toks = append(toks, token{occ: stack[i]})
			if wanted(stack[i]) {
				again = append([]occurrence{occs[stack[i]]}, again...)
			}
		}
		stack = stack[:keep]
		for _, o := range again {
			if o.bit&delimited != 0 && lastRune() == rune(delimiterOf(o.bit)[0]) {
				o.html = true
			}
			push(o)
		}
		for _, bit := range opening {
			if want&bit != 0 && !slices.ContainsFunc(stack, func(k int) bool { return occs[k].bit == bit }) {
				push(occurrence{bit: bit, href: href})
			}
		}
	}
	for i := 0; i < n; {
		j := i + 1
		for j < n && runes[i] != '\n' && runes[j] != '\n' && formats[j] == formats[i] && hrefs[j] == hrefs[i] {
			j++
		}
		if runes[i] == '\n' {
			moveTo(0, "")
			toks = append(toks, token{text: "\\\n", occ: -1})
		} else {
			moveTo(formats[i]&^fmtCode, hrefs[i])
			if formats[i]&fmtCode != 0 {
				toks = append(toks, token{text: codeSpan(string(runes[i:j])), occ: -1})
			} else {
				toks = append(toks, token{text: strings.Join(esc[i:j], ""), occ: -1})
			}
		}
		i = j
	}
	moveTo(0, "")

	// A delimiter counts only where CommonMark's flanking rules let it:
	// judged by the characters beside its run as written, which may be
	// other formats' tokens. One that would not count is written as HTML.
	beside := func(k, dir int, ch rune) rune {
		for k += dir; k >= 0 && k < len(toks); k += dir {
			t := toks[k]
			if t.occ >= 0 && occs[t.occ].bit&delimited != 0 && !occs[t.occ].html && rune(delimiterOf(occs[t.occ].bit)[0]) == ch {
				continue // the same run of delimiters
			}
			if s := []rune(render(t)); len(s) > 0 {
				if dir < 0 {
					return s[len(s)-1]
				}
				return s[0]
			}
		}
		return 0
	}
	space := func(r rune) bool { return r == 0 || unicode.IsSpace(r) }
	for changed := true; changed; {
		changed = false
		for k, t := range toks {
			if t.occ < 0 || occs[t.occ].bit&delimited == 0 || occs[t.occ].html {
				continue
			}
			ch := rune(delimiterOf(occs[t.occ].bit)[0])
			prev, next := beside(k, -1, ch), beside(k, 1, ch)
			counts := !space(next) && (!isPunct(next) || space(prev) || isPunct(prev))
			if !t.open {
				counts = !space(prev) && (!isPunct(prev) || space(next) || isPunct(next))
			}
			if !counts {
				occs[t.occ].html = true
				changed = true
			}
		}
	}
	var out strings.Builder
	for _, t := range toks {
		out.WriteString(render(t))
	}
	// A block starting with "[label]:" is a link reference definition, and
	// its text would vanish: a link there whose text reads so is HTML.
	if s := out.String(); len(toks) > 0 && toks[0].occ >= 0 && occs[toks[0].occ].bit == fmtLink && definitionLike(s) {
		occs[toks[0].occ].html = true
		out.Reset()
		for _, t := range toks {
			out.WriteString(render(t))
		}
	}
	return out.String()
}

// definitionLike says whether Markdown starts like a link reference
// definition: "[", a label up to the first unescaped "]", and ":".
func definitionLike(s string) bool {
	if !strings.HasPrefix(s, "[") {
		return false
	}
	for i := 1; i < len(s); i++ {
		switch s[i] {
		case '\\':
			i++
		case ']':
			return i+1 < len(s) && s[i+1] == ':'
		}
	}
	return false
}

func delimiterOf(bit int) string {
	return map[int]string{fmtStrike: "~~", fmtBold: "**", fmtItalic: "*"}[bit]
}

// flank keeps a delimited format off the characters where its delimiter
// would not count (CommonMark's flanking rules): whitespace at either end,
// and punctuation at an end that touches a letter or digit outside.
func flank(runes []rune, formats []int, bit int) {
	n := len(runes)
	for i := 0; i < n; {
		if formats[i]&bit == 0 {
			i++
			continue
		}
		start, end := i, i
		for end < n && formats[end]&bit != 0 {
			end++
		}
		i = end
		for changed := true; changed && start < end; {
			changed = false
			switch {
			case unicode.IsSpace(runes[start]), isPunct(runes[start]) && start > 0 && isAlnum(runes[start-1]):
				formats[start] &^= bit
				start++
				changed = true
			case unicode.IsSpace(runes[end-1]), isPunct(runes[end-1]) && end < n && isAlnum(runes[end]):
				formats[end-1] &^= bit
				end--
				changed = true
			}
		}
	}
}

// escapes says how each character of a text is written: characters
// Markdown reads as syntax get a backslash, whitespace starting a line an
// entity. Inline syntax is escaped wherever it is; block syntax (#, >, -,
// +, =, "1.") where a line starts.
func escapes(runes []rune, formats []int, hrefs []string) []string {
	n := len(runes)
	out := make([]string, n)
	boundary := func(i int) bool {
		return i <= 0 || i >= n || formats[i] != formats[i-1] || hrefs[i] != hrefs[i-1]
	}
	for i, r := range runes {
		out[i] = string(r)
	}
	for i, r := range runes {
		lineStart := i == 0 || runes[i-1] == '\n'
		switch {
		case strings.ContainsRune("\\`*[]~|", r):
			out[i] = `\` + string(r)
		case r == '_':
			// Inside a word an underscore is text, unless a format begins
			// or ends beside it.
			if i == 0 || i == n-1 || !isAlnum(runes[i-1]) || !isAlnum(runes[i+1]) || boundary(i) || boundary(i+1) {
				out[i] = `\_`
			}
		case r == '<':
			if i+1 < n && (unicode.IsLetter(runes[i+1]) || strings.ContainsRune("/!?", runes[i+1])) {
				out[i] = `\<`
			}
		case r == '!':
			// Before a link's "[", it would make the link a picture.
			if i+1 < n && formats[i+1]&fmtLink != 0 && (formats[i]&fmtLink == 0 || hrefs[i] != hrefs[i+1]) {
				out[i] = `\!`
			}
		case r == '&':
			if entityAt(runes[i+1:]) {
				out[i] = `\&`
			}
		case lineStart && strings.ContainsRune("#>-+=", r):
			out[i] = `\` + string(r)
		case lineStart && r == ' ':
			out[i] = "&#32;"
		case lineStart && r == '\t':
			out[i] = "&#9;"
		case lineStart && r >= '0' && r <= '9':
			k := i
			for k < n && k-i < 9 && runes[k] >= '0' && runes[k] <= '9' {
				k++
			}
			if k < n && (runes[k] == '.' || runes[k] == ')') {
				out[k] = `\` + string(runes[k])
			}
		}
	}
	return out
}

// entityAt says whether text starts like the rest of an HTML entity
// (after its "&"): a name or number and a semicolon.
func entityAt(rest []rune) bool {
	i := 0
	if i < len(rest) && rest[i] == '#' {
		i++
		if i < len(rest) && (rest[i] == 'x' || rest[i] == 'X') {
			i++
		}
	}
	start := i
	for i < len(rest) && i-start < 32 && (rest[i] < 128 && (unicode.IsLetter(rest[i]) || unicode.IsDigit(rest[i]))) {
		i++
	}
	return i > start && i < len(rest) && rest[i] == ';'
}

// codeSpan writes code as a code span: fenced by more backticks than any
// run in it, padded with a space where it starts or ends with a backtick
// or a space.
func codeSpan(s string) string {
	fence := strings.Repeat("`", longestRun(s, '`')+1)
	if strings.Trim(s, " ") != "" && (strings.HasPrefix(s, "`") || strings.HasSuffix(s, "`") || strings.HasPrefix(s, " ") || strings.HasSuffix(s, " ")) {
		s = " " + s + " "
	}
	return fence + s + fence
}

// linkTarget writes a link's address so that Markdown reads it whole:
// spaces, parentheses, angle brackets and backslashes percent-encoded.
func linkTarget(href string) string {
	return strings.NewReplacer(" ", "%20", "(", "%28", ")", "%29", "<", "%3C", ">", "%3E", `\`, "%5C").Replace(href)
}
