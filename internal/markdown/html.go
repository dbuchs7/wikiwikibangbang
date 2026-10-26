package markdown

import (
	"html"
	"regexp"
	"strings"

	"github.com/yuin/goldmark/ast"

	"github.com/dbuchs7/wikiwikibangbang/internal/item"
)

// HTML in Markdown never becomes markup (ADR-0014, ADR-0138): comments,
// scripts and styles go, <br> and the end of a block of HTML break the
// line, <img> is a picture, and of every other tag only the text between
// tags stays. The patterns only find where text and pictures are; what
// they find is inserted as text.
var (
	brTag      = regexp.MustCompile(`(?i)^<br\s*/?>$`)
	imgTag     = regexp.MustCompile(`(?is)<img\b[^>]*>`)
	srcAttr    = regexp.MustCompile(`(?is)\ssrc\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s"'>]+))`)
	altAttr    = regexp.MustCompile(`(?is)\salt\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s"'>]+))`)
	comment    = regexp.MustCompile(`(?s)<!--.*?(-->|$)`)
	hidden     = regexp.MustCompile(`(?is)<(script|style|textarea)\b.*?(</(script|style|textarea)\s*>|$)`)
	lineBreaks = regexp.MustCompile(`(?i)<br\s*/?>|</(p|div|h[1-6]|li|tr|summary|blockquote|pre|table|ul|ol|dt|dd)\s*>`)
	anyTag     = regexp.MustCompile(`(?s)<[^>]*>`)
)

// attr is the value of an attribute matched by re in a tag, with its
// character references resolved.
func attr(re *regexp.Regexp, tag string) string {
	m := re.FindStringSubmatch(tag)
	if m == nil {
		return ""
	}
	return html.UnescapeString(m[1] + m[2] + m[3])
}

// imgTags are the pictures of a piece of HTML, in order.
func imgTags(raw string) []pic {
	var out []pic
	for _, tag := range imgTag.FindAllString(raw, -1) {
		if src := attr(srcAttr, tag); src != "" {
			out = append(out, pic{src: src, alt: attr(altAttr, tag)})
		}
	}
	return out
}

// htmlText is the text of a piece of HTML: its tags removed, its lines
// broken where the HTML breaks them, its character references resolved.
func htmlText(raw string) []string {
	raw = strings.Join(strings.Fields(raw), " ")
	raw = lineBreaks.ReplaceAllString(raw, "\n")
	raw = html.UnescapeString(anyTag.ReplaceAllString(raw, ""))
	var lines []string
	for line := range strings.SplitSeq(raw, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

// htmlBlock writes a block of HTML as its text and pictures, in order.
func (c *converter) htmlBlock(n *ast.HTMLBlock, kind item.BlockKind, tone string) {
	var b strings.Builder
	lines := n.Lines()
	for i := 0; i < lines.Len(); i++ {
		seg := lines.At(i)
		b.Write(seg.Value(c.src))
	}
	if n.HasClosure() {
		b.Write(n.ClosureLine.Value(c.src))
	}
	raw := hidden.ReplaceAllString(comment.ReplaceAllString(b.String(), ""), "")
	for raw != "" {
		loc := imgTag.FindStringIndex(raw)
		before := raw
		if loc != nil {
			before = raw[:loc[0]]
		}
		for _, line := range htmlText(before) {
			r := &run{}
			r.add(line, nil)
			c.text(r, kind, tone)
		}
		if loc == nil {
			return
		}
		for _, p := range imgTags(raw[loc[0]:loc[1]]) {
			c.picture(p.src, p.alt, kind, tone)
		}
		raw = raw[loc[1]:]
	}
}
