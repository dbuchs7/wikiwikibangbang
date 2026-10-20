package export

import (
	"archive/zip"
	"bytes"
	"fmt"
	"image"
	_ "image/gif" // the formats image.DecodeConfig reads for picture sizes
	_ "image/jpeg"
	_ "image/png"
	"io"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/dbuchs7/wikiwikibangbang/internal/item"
)

// Document is a page to write as a Word document.
type Document struct {
	Title string
	// Facts are label and value pairs shown under the title: the key, the
	// type, the state and the properties.
	Facts [][2]string
	Body  []item.Block
	// Images are the item's pictures by attachment ID; a picture block
	// whose file is missing or in a format Word cannot show keeps only its
	// caption.
	Images map[item.ID]Image
	// Ref names an embedded or mentioned item, or says it is restricted.
	Ref func(item.ID) string
	// Children are the titles of the child pages, for a list of them.
	Children []string
}

// Image is a picture file.
type Image struct {
	Name, Type string
	Data       []byte
}

// textWidth is the width of the text column of an A4 page with margins of
// 2.5 cm, in EMU (English Metric Units, 914400 to the inch).
const textWidth = 5760000

// emuPerPixel converts pixels at 96 dpi.
const emuPerPixel = 9525

// panelShades are the background colours of the panel tones.
var panelShades = map[string]string{"info": "E8F0FE", "note": "F3EEFC", "warning": "FFF6E0", "tip": "E9F6EC", "error": "FDECEA"}

// DOCX writes the document as an Office Open XML word-processing file.
func DOCX(w io.Writer, d Document) error {
	b := &docxBuilder{d: d}
	body := b.body()
	z := zip.NewWriter(w)
	files := []struct{ name, data string }{
		{"[Content_Types].xml", contentTypes},
		{"_rels/.rels", rootRels},
		{"docProps/core.xml", fmt.Sprintf(coreProps, esc(d.Title))},
		{"word/document.xml", documentHead + body + sectPr + "</w:body></w:document>"},
		{"word/styles.xml", styles},
		{"word/numbering.xml", b.numbering()},
		{"word/_rels/document.xml.rels", b.rels()},
	}
	for _, f := range files {
		fw, err := z.Create(f.name)
		if err != nil {
			return err
		}
		if _, err := io.WriteString(fw, f.data); err != nil {
			return err
		}
	}
	for _, m := range b.media {
		fw, err := z.Create("word/media/" + m.file)
		if err != nil {
			return err
		}
		if _, err := fw.Write(m.data); err != nil {
			return err
		}
	}
	return z.Close()
}

type media struct {
	id, file string
	data     []byte
}

type docxBuilder struct {
	d       Document
	links   []string // targets of hyperlinks; rId is "rL" plus the index
	media   []media
	ordered int // ordered lists so far; each gets its own numbering
	shapes  int
}

func (b *docxBuilder) body() string {
	var out strings.Builder
	out.WriteString(para("Title", "", runs(b, b.d.Title, nil)))
	if len(b.d.Facts) > 0 {
		var rows [][]string
		for _, f := range b.d.Facts {
			rows = append(rows, []string{f[0], f[1]})
		}
		out.WriteString(table(rows, false))
		out.WriteString(para("", "", ""))
	}
	listNum := 0
	for i, bl := range b.d.Body {
		if bl.Kind == item.OrderedItem && (i == 0 || b.d.Body[i-1].Kind != item.OrderedItem) {
			b.ordered++
			listNum = 1 + b.ordered
		}
		out.WriteString(b.block(bl, listNum))
	}
	return out.String()
}

func (b *docxBuilder) block(bl item.Block, listNum int) string {
	jc := ""
	switch bl.Align {
	case "center", "right":
		jc = bl.Align
	case "justify":
		jc = "both"
	}
	text := runs(b, bl.Text, bl.Marks)
	switch bl.Kind {
	case item.Heading:
		level := min(max(bl.Level, 1), 3)
		return paraPr(fmt.Sprintf(`<w:pStyle w:val="Heading%d"/>`, level)+align(jc), text)
	case item.BulletItem:
		return paraPr(`<w:pStyle w:val="ListParagraph"/><w:numPr><w:ilvl w:val="0"/><w:numId w:val="1"/></w:numPr>`, text)
	case item.OrderedItem:
		return paraPr(fmt.Sprintf(`<w:pStyle w:val="ListParagraph"/><w:numPr><w:ilvl w:val="0"/><w:numId w:val="%d"/></w:numPr>`, listNum), text)
	case item.TaskItem:
		box := "☐ "
		if bl.Checked {
			box = "☑ "
		}
		return para("ListParagraph", "", run(box, "")+text)
	case item.Quote:
		return paraPr(`<w:pStyle w:val="Quote"/>`+align(jc), text)
	case item.CodeBlock:
		return para("Code", "", run(bl.Text, ""))
	case item.Panel:
		shade := panelShades[bl.Tone]
		if shade == "" {
			shade = panelShades["info"]
		}
		return paraPr(`<w:pStyle w:val="Panel"/><w:shd w:val="clear" w:color="auto" w:fill="`+shade+`"/>`+align(jc), text)
	case item.Table:
		return table(TableRows(bl.Text), true)
	case item.Divider:
		return paraPr(`<w:pBdr><w:bottom w:val="single" w:sz="6" w:space="1" w:color="BFBFBF"/></w:pBdr>`, "")
	case item.Image:
		return b.picture(bl)
	case item.Embed:
		return para("", "", run("↳ "+b.ref(bl.Ref), ""))
	case item.TOC:
		var out strings.Builder
		for _, h := range b.d.Body {
			if h.Kind == item.Heading && strings.TrimSpace(h.Text) != "" {
				out.WriteString(paraPr(fmt.Sprintf(`<w:ind w:left="%d"/>`, (max(h.Level, 1)-1)*360), run(h.Text, "")))
			}
		}
		return out.String()
	case item.Children:
		var out strings.Builder
		for _, c := range b.d.Children {
			out.WriteString(paraPr(`<w:pStyle w:val="ListParagraph"/><w:numPr><w:ilvl w:val="0"/><w:numId w:val="1"/></w:numPr>`, run(c, "")))
		}
		return out.String()
	default: // paragraph, and content an import could not convert
		return paraPr(align(jc), text)
	}
}

func (b *docxBuilder) ref(id item.ID) string {
	if b.d.Ref == nil {
		return string(id)
	}
	return b.d.Ref(id)
}

// picture places an image of the item, sized as in the page.
func (b *docxBuilder) picture(bl item.Block) string {
	caption := ""
	if strings.TrimSpace(bl.Text) != "" {
		caption = para("Caption", "", run(bl.Text, ""))
	}
	img, ok := b.d.Images[bl.Ref]
	if !ok {
		return caption
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(img.Data))
	if err != nil || cfg.Width == 0 || cfg.Height == 0 {
		return para("", "", run("["+img.Name+"]", "")) + caption
	}
	ext := map[string]string{"png": "png", "jpeg": "jpeg", "gif": "gif"}[format]
	cx := int64(cfg.Width) * emuPerPixel
	if bl.Width > 0 {
		cx = int64(textWidth) * int64(bl.Width) / 100
	}
	cx = min(cx, textWidth)
	cy := cx * int64(cfg.Height) / int64(cfg.Width)
	b.shapes++
	id := fmt.Sprintf("rI%d", b.shapes)
	b.media = append(b.media, media{id: id, file: fmt.Sprintf("image%d.%s", b.shapes, ext), data: img.Data})
	jc := `<w:jc w:val="center"/>`
	switch bl.Align {
	case "left", "right":
		jc = `<w:jc w:val="` + bl.Align + `"/>`
	}
	drawing := fmt.Sprintf(`<w:r><w:drawing><wp:inline distT="0" distB="0" distL="0" distR="0"><wp:extent cx="%d" cy="%d"/>`+
		`<wp:docPr id="%d" name="Picture %d" descr="%s"/><a:graphic xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main">`+
		`<a:graphicData uri="http://schemas.openxmlformats.org/drawingml/2006/picture"><pic:pic xmlns:pic="http://schemas.openxmlformats.org/drawingml/2006/picture">`+
		`<pic:nvPicPr><pic:cNvPr id="%d" name="%s"/><pic:cNvPicPr/></pic:nvPicPr><pic:blipFill><a:blip r:embed="%s"/><a:stretch><a:fillRect/></a:stretch></pic:blipFill>`+
		`<pic:spPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="%d" cy="%d"/></a:xfrm><a:prstGeom prst="rect"><a:avLst/></a:prstGeom></pic:spPr></pic:pic></a:graphicData></a:graphic></wp:inline></w:drawing></w:r>`,
		cx, cy, b.shapes, b.shapes, esc(bl.Text), b.shapes, esc(img.Name), id, cx, cy)
	return paraPr(jc, drawing) + caption
}

// runs writes a text with its marks as runs of equal formatting.
func runs(b *docxBuilder, text string, marks []item.Mark) string {
	if text == "" {
		return ""
	}
	n := utf8.RuneCountInString(text)
	cuts := []int{0, n}
	for _, m := range marks {
		cuts = append(cuts, min(max(m.Start, 0), n), min(max(m.End, 0), n))
	}
	slices.Sort(cuts)
	cuts = slices.Compact(cuts)
	chars := []rune(text)
	var out strings.Builder
	for i := 0; i+1 < len(cuts); i++ {
		from, to := cuts[i], cuts[i+1]
		if from >= to {
			continue
		}
		var props, href string
		var bold, italic, under, strike, code, high, link bool
		for _, m := range marks {
			if m.Start > from || m.End < to {
				continue
			}
			switch m.Type {
			case item.MarkBold:
				bold = true
			case item.MarkItalic:
				italic = true
			case item.MarkUnderline:
				under = true
			case item.MarkStrike:
				strike = true
			case item.MarkCode:
				code = true
			case item.MarkHighlight:
				high = true
			case item.MarkLink:
				link, href = true, m.Href
			}
		}
		// The order of the run properties is the schema's.
		if link {
			props += `<w:rStyle w:val="Hyperlink"/>`
		} else if code {
			props += `<w:rStyle w:val="CodeChar"/>`
		}
		if bold {
			props += "<w:b/>"
		}
		if italic {
			props += "<w:i/>"
		}
		if strike {
			props += "<w:strike/>"
		}
		if high {
			props += `<w:highlight w:val="yellow"/>`
		}
		if under {
			props += `<w:u w:val="single"/>`
		}
		r := run(string(chars[from:to]), props)
		if link && safeLink(href) {
			b.links = append(b.links, href)
			r = fmt.Sprintf(`<w:hyperlink r:id="rL%d">%s</w:hyperlink>`, len(b.links), r)
		}
		out.WriteString(r)
	}
	return out.String()
}

// safeLink admits the link targets a page admits (ADR-0014).
func safeLink(href string) bool {
	h := strings.ToLower(href)
	return strings.HasPrefix(h, "https://") || strings.HasPrefix(h, "http://") || strings.HasPrefix(h, "mailto:")
}

// run is one run of text; line breaks become breaks.
func run(text, props string) string {
	var out strings.Builder
	out.WriteString("<w:r>")
	if props != "" {
		out.WriteString("<w:rPr>" + props + "</w:rPr>")
	}
	for i, line := range strings.Split(text, "\n") {
		if i > 0 {
			out.WriteString("<w:br/>")
		}
		out.WriteString(`<w:t xml:space="preserve">` + esc(line) + "</w:t>")
	}
	out.WriteString("</w:r>")
	return out.String()
}

func para(style, props, content string) string {
	if style != "" {
		props = `<w:pStyle w:val="` + style + `"/>` + props
	}
	return paraPr(props, content)
}

func paraPr(props, content string) string {
	if props != "" {
		props = "<w:pPr>" + props + "</w:pPr>"
	}
	return "<w:p>" + props + content + "</w:p>"
}

func align(jc string) string {
	if jc == "" {
		return ""
	}
	return `<w:jc w:val="` + jc + `"/>`
}

// table writes rows as a table with borders; the first row is a header
// when header is set.
func table(rows [][]string, header bool) string {
	if len(rows) == 0 {
		return ""
	}
	cols := 0
	for _, r := range rows {
		cols = max(cols, len(r))
	}
	var out strings.Builder
	out.WriteString(`<w:tbl><w:tblPr><w:tblStyle w:val="TableGrid"/><w:tblW w:w="5000" w:type="pct"/></w:tblPr><w:tblGrid>`)
	for range cols {
		fmt.Fprintf(&out, `<w:gridCol w:w="%d"/>`, 9070/cols)
	}
	out.WriteString("</w:tblGrid>")
	for i, r := range rows {
		head := header && i == 0
		out.WriteString("<w:tr>")
		if head {
			out.WriteString(`<w:trPr><w:tblHeader/></w:trPr>`)
		}
		for c := range cols {
			cell := ""
			if c < len(r) {
				cell = r[c]
			}
			props := ""
			cellPr := ""
			if head {
				props = "<w:b/>"
				cellPr = `<w:tcPr><w:shd w:val="clear" w:color="auto" w:fill="F2F2F2"/></w:tcPr>`
			}
			out.WriteString("<w:tc>" + cellPr + paraPr("", run(cell, props)) + "</w:tc>")
		}
		out.WriteString("</w:tr>")
	}
	out.WriteString("</w:tbl>")
	return out.String()
}

func (b *docxBuilder) rels() string {
	var out strings.Builder
	out.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
		`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
		`<Relationship Id="rS" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/>` +
		`<Relationship Id="rN" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/numbering" Target="numbering.xml"/>`)
	for i, l := range b.links {
		fmt.Fprintf(&out, `<Relationship Id="rL%d" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/hyperlink" Target="%s" TargetMode="External"/>`, i+1, esc(l))
	}
	for _, m := range b.media {
		fmt.Fprintf(&out, `<Relationship Id="%s" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/image" Target="media/%s"/>`, m.id, m.file)
	}
	out.WriteString("</Relationships>")
	return out.String()
}

// numbering defines the bullet list (1) and one decimal list per ordered
// list of the page (2 and on), each starting at 1.
func (b *docxBuilder) numbering() string {
	var out strings.Builder
	out.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
		`<w:numbering xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">` +
		`<w:abstractNum w:abstractNumId="0"><w:multiLevelType w:val="singleLevel"/><w:lvl w:ilvl="0"><w:start w:val="1"/><w:numFmt w:val="bullet"/><w:lvlText w:val="•"/><w:lvlJc w:val="left"/><w:pPr><w:ind w:left="720" w:hanging="360"/></w:pPr></w:lvl></w:abstractNum>` +
		`<w:abstractNum w:abstractNumId="1"><w:multiLevelType w:val="singleLevel"/><w:lvl w:ilvl="0"><w:start w:val="1"/><w:numFmt w:val="decimal"/><w:lvlText w:val="%1."/><w:lvlJc w:val="left"/><w:pPr><w:ind w:left="720" w:hanging="360"/></w:pPr></w:lvl></w:abstractNum>` +
		`<w:num w:numId="1"><w:abstractNumId w:val="0"/></w:num>`)
	for i := 1; i <= b.ordered; i++ {
		fmt.Fprintf(&out, `<w:num w:numId="%d"><w:abstractNumId w:val="1"/><w:lvlOverride w:ilvl="0"><w:startOverride w:val="1"/></w:lvlOverride></w:num>`, i+1)
	}
	out.WriteString("</w:numbering>")
	return out.String()
}

// esc escapes text for XML and drops the characters XML cannot hold.
func esc(s string) string {
	var out strings.Builder
	for _, r := range s {
		switch {
		case r == '&':
			out.WriteString("&amp;")
		case r == '<':
			out.WriteString("&lt;")
		case r == '>':
			out.WriteString("&gt;")
		case r == '"':
			out.WriteString("&quot;")
		case r == '\t' || r == '\n' || r == '\r' || (r >= 0x20 && r <= 0xD7FF) || (r >= 0xE000 && r <= 0xFFFD) || r >= 0x10000:
			out.WriteRune(r)
		}
	}
	return out.String()
}

const contentTypes = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
	`<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
	`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>` +
	`<Default Extension="xml" ContentType="application/xml"/>` +
	`<Default Extension="png" ContentType="image/png"/><Default Extension="jpeg" ContentType="image/jpeg"/><Default Extension="gif" ContentType="image/gif"/>` +
	`<Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>` +
	`<Override PartName="/word/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.styles+xml"/>` +
	`<Override PartName="/word/numbering.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.numbering+xml"/>` +
	`<Override PartName="/docProps/core.xml" ContentType="application/vnd.openxmlformats-package.core-properties+xml"/>` +
	`</Types>`

const rootRels = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
	`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
	`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/>` +
	`<Relationship Id="rId2" Type="http://schemas.openxmlformats.org/package/2006/relationships/metadata/core-properties" Target="docProps/core.xml"/>` +
	`</Relationships>`

const coreProps = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
	`<cp:coreProperties xmlns:cp="http://schemas.openxmlformats.org/package/2006/metadata/core-properties" xmlns:dc="http://purl.org/dc/elements/1.1/">` +
	`<dc:title>%s</dc:title><dc:creator>wikiwikibangbang</dc:creator></cp:coreProperties>`

const documentHead = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
	`<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" ` +
	`xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships" ` +
	`xmlns:wp="http://schemas.openxmlformats.org/drawingml/2006/wordprocessingDrawing"><w:body>`

// sectPr is an A4 page with margins of 2.5 cm.
const sectPr = `<w:sectPr><w:pgSz w:w="11906" w:h="16838"/><w:pgMar w:top="1417" w:right="1417" w:bottom="1134" w:left="1417" w:header="708" w:footer="708" w:gutter="0"/></w:sectPr>`

const styles = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
	`<w:styles xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">` +
	`<w:docDefaults><w:rPrDefault><w:rPr><w:rFonts w:ascii="Calibri" w:hAnsi="Calibri" w:eastAsia="Calibri" w:cs="Calibri"/><w:sz w:val="22"/><w:szCs w:val="22"/><w:lang w:val="de-CH"/></w:rPr></w:rPrDefault>` +
	`<w:pPrDefault><w:pPr><w:spacing w:after="120" w:line="276" w:lineRule="auto"/></w:pPr></w:pPrDefault></w:docDefaults>` +
	`<w:style w:type="paragraph" w:default="1" w:styleId="Normal"><w:name w:val="Normal"/><w:qFormat/></w:style>` +
	`<w:style w:type="paragraph" w:styleId="Title"><w:name w:val="Title"/><w:basedOn w:val="Normal"/><w:next w:val="Normal"/><w:qFormat/><w:pPr><w:spacing w:after="240"/></w:pPr><w:rPr><w:b/><w:sz w:val="40"/><w:szCs w:val="40"/></w:rPr></w:style>` +
	`<w:style w:type="paragraph" w:styleId="Heading1"><w:name w:val="heading 1"/><w:basedOn w:val="Normal"/><w:next w:val="Normal"/><w:qFormat/><w:pPr><w:keepNext/><w:spacing w:before="360" w:after="120"/><w:outlineLvl w:val="0"/></w:pPr><w:rPr><w:b/><w:sz w:val="32"/><w:szCs w:val="32"/></w:rPr></w:style>` +
	`<w:style w:type="paragraph" w:styleId="Heading2"><w:name w:val="heading 2"/><w:basedOn w:val="Normal"/><w:next w:val="Normal"/><w:qFormat/><w:pPr><w:keepNext/><w:spacing w:before="240" w:after="120"/><w:outlineLvl w:val="1"/></w:pPr><w:rPr><w:b/><w:sz w:val="28"/><w:szCs w:val="28"/></w:rPr></w:style>` +
	`<w:style w:type="paragraph" w:styleId="Heading3"><w:name w:val="heading 3"/><w:basedOn w:val="Normal"/><w:next w:val="Normal"/><w:qFormat/><w:pPr><w:keepNext/><w:spacing w:before="200" w:after="80"/><w:outlineLvl w:val="2"/></w:pPr><w:rPr><w:b/><w:sz w:val="24"/><w:szCs w:val="24"/></w:rPr></w:style>` +
	`<w:style w:type="paragraph" w:styleId="ListParagraph"><w:name w:val="List Paragraph"/><w:basedOn w:val="Normal"/><w:qFormat/><w:pPr><w:spacing w:after="60"/><w:ind w:left="720"/></w:pPr></w:style>` +
	`<w:style w:type="paragraph" w:styleId="Quote"><w:name w:val="Quote"/><w:basedOn w:val="Normal"/><w:qFormat/><w:pPr><w:pBdr><w:left w:val="single" w:sz="18" w:space="8" w:color="BFBFBF"/></w:pBdr><w:ind w:left="360"/></w:pPr><w:rPr><w:i/><w:color w:val="595959"/></w:rPr></w:style>` +
	`<w:style w:type="paragraph" w:styleId="Code"><w:name w:val="Code"/><w:basedOn w:val="Normal"/><w:pPr><w:shd w:val="clear" w:color="auto" w:fill="F4F5F7"/><w:spacing w:after="120" w:line="240" w:lineRule="auto"/></w:pPr><w:rPr><w:rFonts w:ascii="Consolas" w:hAnsi="Consolas" w:cs="Consolas"/><w:sz w:val="19"/><w:szCs w:val="19"/></w:rPr></w:style>` +
	`<w:style w:type="paragraph" w:styleId="Panel"><w:name w:val="Panel"/><w:basedOn w:val="Normal"/><w:pPr><w:pBdr><w:left w:val="single" w:sz="24" w:space="8" w:color="8C8C8C"/></w:pBdr><w:ind w:left="240" w:right="120"/></w:pPr></w:style>` +
	`<w:style w:type="paragraph" w:styleId="Caption"><w:name w:val="caption"/><w:basedOn w:val="Normal"/><w:pPr><w:jc w:val="center"/></w:pPr><w:rPr><w:i/><w:color w:val="595959"/><w:sz w:val="18"/><w:szCs w:val="18"/></w:rPr></w:style>` +
	`<w:style w:type="character" w:styleId="Hyperlink"><w:name w:val="Hyperlink"/><w:rPr><w:color w:val="0563C1"/><w:u w:val="single"/></w:rPr></w:style>` +
	`<w:style w:type="character" w:styleId="CodeChar"><w:name w:val="Code Char"/><w:rPr><w:rFonts w:ascii="Consolas" w:hAnsi="Consolas" w:cs="Consolas"/><w:shd w:val="clear" w:color="auto" w:fill="F4F5F7"/></w:rPr></w:style>` +
	`<w:style w:type="table" w:styleId="TableGrid"><w:name w:val="Table Grid"/><w:tblPr><w:tblBorders>` +
	`<w:top w:val="single" w:sz="4" w:space="0" w:color="BFBFBF"/><w:left w:val="single" w:sz="4" w:space="0" w:color="BFBFBF"/><w:bottom w:val="single" w:sz="4" w:space="0" w:color="BFBFBF"/>` +
	`<w:right w:val="single" w:sz="4" w:space="0" w:color="BFBFBF"/><w:insideH w:val="single" w:sz="4" w:space="0" w:color="BFBFBF"/><w:insideV w:val="single" w:sz="4" w:space="0" w:color="BFBFBF"/>` +
	`</w:tblBorders><w:tblCellMar><w:left w:w="100" w:type="dxa"/><w:right w:w="100" w:type="dxa"/></w:tblCellMar></w:tblPr></w:style>` +
	`</w:styles>`
