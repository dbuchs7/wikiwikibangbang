package export

import (
	"fmt"
	"html"
	"io"
	"net/url"
	"strings"

	"github.com/dbuchs7/wikiwikibangbang/internal/item"
)

// HTML writes the document as a page to print, the source of a PDF
// (ADR-0071). It has no script and no inline style: its look comes from
// /assets/export.css, and its pictures from the items' attachments, so the
// server's content security policy holds. lang is the reader's language.
func HTML(w io.Writer, d Document, lang string) error {
	h := &htmlBuilder{d: d}
	title := ""
	if len(d.Pages) > 0 {
		title = d.Pages[0].Title
	}
	h.printf(`<!doctype html><html lang="%s"><head><meta charset="utf-8"><title>%s</title>`+
		`<link rel="stylesheet" href="/assets/export.css"></head><body><article class="export">`, html.EscapeString(lang), html.EscapeString(title))
	for i, p := range d.Pages {
		if i == 0 {
			h.printf(`<h1 class="doc-title">%s</h1>`, html.EscapeString(p.Title))
			if d.Contents != "" && len(d.Pages) > 1 {
				h.printf(`<nav class="contents"><h2>%s</h2><ol>`, html.EscapeString(d.Contents))
				for _, c := range d.Pages[1:] {
					h.printf(`<li class="depth-%d"><a href="#p-%s">%s</a></li>`, min(c.Depth, 6), html.EscapeString(string(c.ID)), html.EscapeString(c.Title))
				}
				h.out.WriteString(`</ol></nav>`)
			}
		} else {
			h.printf(`<h%d id="p-%s" class="page-title depth-%d">%s</h%d>`, min(p.Depth+1, 6), html.EscapeString(string(p.ID)), min(p.Depth, 6), html.EscapeString(p.Title), min(p.Depth+1, 6))
		}
		h.page(p, p.Depth+1, map[item.ID]bool{p.ID: true}, 0)
	}
	h.out.WriteString(`</article></body></html>`)
	_, err := io.WriteString(w, h.out.String())
	return err
}

type htmlBuilder struct {
	d   Document
	out strings.Builder
}

func (h *htmlBuilder) printf(format string, args ...any) { fmt.Fprintf(&h.out, format, args...) }

// page writes an item's facts and body; base is the heading level of its
// title, so its own headings come below it.
func (h *htmlBuilder) page(p Page, base int, path map[item.ID]bool, embedDepth int) {
	if len(p.Facts) > 0 {
		h.out.WriteString(`<table class="facts">`)
		for _, f := range p.Facts {
			h.printf(`<tr><th>%s</th><td>%s</td></tr>`, html.EscapeString(f[0]), html.EscapeString(f[1]))
		}
		h.out.WriteString(`</table>`)
	}
	list := ""
	for _, bl := range p.Body {
		want := map[item.BlockKind]string{item.BulletItem: "ul", item.OrderedItem: "ol", item.TaskItem: "ul class=\"tasks\""}[bl.Kind]
		if want != list {
			if list != "" {
				h.printf("</%s>", strings.Fields(list)[0])
			}
			if want != "" {
				h.printf("<%s>", want)
			}
			list = want
		}
		if bl.Kind == item.Embed {
			h.embed(bl.Ref, base, path, embedDepth)
			continue
		}
		h.block(bl, p, base)
	}
	if list != "" {
		h.printf("</%s>", strings.Fields(list)[0])
	}
}

func (h *htmlBuilder) embed(id item.ID, base int, path map[item.ID]bool, embedDepth int) {
	e, ok := h.d.Embeds[id]
	if !ok {
		h.printf(`<div class="embed restricted"><p class="embed-title">↳ %s</p></div>`, html.EscapeString(h.d.Restricted))
		return
	}
	h.printf(`<div class="embed"><p class="embed-title">↳ %s</p>`, html.EscapeString(e.name()))
	if !path[id] && embedDepth < maxEmbedDepth {
		path[id] = true
		h.page(e, base+1, path, embedDepth+1)
		delete(path, id)
	}
	h.out.WriteString(`</div>`)
}

func (h *htmlBuilder) block(bl item.Block, p Page, base int) {
	cls := ""
	if bl.Align != "" {
		cls = ` class="align-` + html.EscapeString(bl.Align) + `"`
	}
	text := inline(bl.Text, bl.Marks)
	switch bl.Kind {
	case item.Heading:
		level := min(base+min(max(bl.Level, 1), 3), 6)
		h.printf("<h%d%s>%s</h%d>", level, cls, text, level)
	case item.BulletItem, item.OrderedItem:
		h.printf("<li>%s</li>", text)
	case item.TaskItem:
		box := "☐"
		if bl.Checked {
			box = "☑"
		}
		h.printf(`<li><span class="box">%s</span> %s</li>`, box, text)
	case item.Quote:
		h.printf("<blockquote%s>%s</blockquote>", cls, text)
	case item.CodeBlock:
		h.printf("<pre><code>%s</code></pre>", html.EscapeString(bl.Text))
	case item.Panel:
		tone := bl.Tone
		if panelShades[tone] == "" {
			tone = "info"
		}
		h.printf(`<div class="panel tone-%s"><p>%s</p></div>`, tone, text)
	case item.Table:
		rows := TableRows(bl.Text)
		if len(rows) == 0 {
			return
		}
		h.out.WriteString("<table><thead><tr>")
		for _, c := range rows[0] {
			h.printf("<th>%s</th>", html.EscapeString(c))
		}
		h.out.WriteString("</tr></thead><tbody>")
		for _, r := range rows[1:] {
			h.out.WriteString("<tr>")
			for _, c := range r {
				h.printf("<td>%s</td>", html.EscapeString(c))
			}
			h.out.WriteString("</tr>")
		}
		h.out.WriteString("</tbody></table>")
	case item.Divider:
		h.out.WriteString("<hr>")
	case item.Image:
		h.out.WriteString(`<figure class="` + imageClass(bl) + `">`)
		if img, ok := p.Images[bl.Ref]; ok {
			h.printf(`<img src="/api/items/%s/attachments/%s" alt="%s">`, url.PathEscape(string(p.ID)), url.PathEscape(string(bl.Ref)), html.EscapeString(img.Name))
		}
		if strings.TrimSpace(bl.Text) != "" {
			h.printf("<figcaption>%s</figcaption>", html.EscapeString(bl.Text))
		}
		h.out.WriteString("</figure>")
	case item.Video:
		h.out.WriteString(`<figure class="` + imageClass(bl) + `">`)
		if v, ok := p.Images[bl.Ref]; ok {
			h.printf(`<video controls preload="metadata" src="/api/items/%s/attachments/%s" title="%s"></video>`, url.PathEscape(string(p.ID)), url.PathEscape(string(bl.Ref)), html.EscapeString(v.Name))
		}
		if strings.TrimSpace(bl.Text) != "" {
			h.printf("<figcaption>%s</figcaption>", html.EscapeString(bl.Text))
		}
		h.out.WriteString("</figure>")
	case item.TOC:
		h.out.WriteString(`<ul class="toc">`)
		for _, x := range p.Body {
			if x.Kind == item.Heading && strings.TrimSpace(x.Text) != "" {
				h.printf(`<li class="level-%d">%s</li>`, min(max(x.Level, 1), 3), html.EscapeString(x.Text))
			}
		}
		h.out.WriteString("</ul>")
	case item.Children:
		h.out.WriteString(`<ul class="children">`)
		for _, c := range p.Children {
			h.printf("<li>%s</li>", html.EscapeString(c))
		}
		h.out.WriteString("</ul>")
	default:
		h.printf("<p%s>%s</p>", cls, text)
	}
}

// imageClass sizes and places a picture with classes, not inline style:
// its width in steps of ten percent, and its alignment.
func imageClass(bl item.Block) string {
	cls := "image"
	if bl.Width > 0 {
		cls += fmt.Sprintf(" w-%d", min(max((bl.Width+5)/10*10, 10), 100))
	}
	if bl.Align == "left" || bl.Align == "right" {
		cls += " align-" + bl.Align
	}
	return cls
}

// inline writes a text with its marks as HTML, escaped.
func inline(text string, marks []item.Mark) string {
	var out strings.Builder
	for _, sg := range segments(text, marks) {
		s := strings.ReplaceAll(html.EscapeString(sg.text), "\n", "<br>")
		wrap := func(tag string) { s = "<" + tag + ">" + s + "</" + tag + ">" }
		if sg.code {
			wrap("code")
		}
		if sg.bold {
			wrap("strong")
		}
		if sg.italic {
			wrap("em")
		}
		if sg.under {
			wrap("u")
		}
		if sg.strike {
			wrap("s")
		}
		if sg.high {
			wrap("mark")
		}
		if sg.link {
			s = `<a href="` + html.EscapeString(sg.href) + `">` + s + "</a>"
		}
		out.WriteString(s)
	}
	return out.String()
}
