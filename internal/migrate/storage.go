package migrate

import (
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/dbuchs7/wikiwikibangbang/internal/item"
)

// Resolver finds the items that links in a source body point at.
type Resolver interface {
	// Page resolves a Confluence page link, which names its target by space
	// key and title.
	Page(space, title string) (item.ID, bool)
	// Issue resolves a Jira issue key.
	Issue(key string) (item.ID, bool)
	// User resolves a source account to a display name for "@" mentions.
	User(account string) (string, bool)
}

// blockTags and inlineTags are the elements the converter understands. Any
// other element becomes an Unsupported block that keeps its original markup.
var (
	headingTags = map[string]bool{"h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true}
	inlineTags  = map[string]bool{
		"strong": true, "b": true, "em": true, "i": true, "u": true, "s": true, "code": true,
		"span": true, "a": true, "sub": true, "sup": true,
		"ac:inline-comment-marker": true, "ac:link-body": true, "ac:plain-text-link-body": true,
	}
)

// ConvertStorage converts a Confluence storage-format body into a block
// document (ADR-0023). Headings and paragraphs become blocks, list items
// become paragraphs, page links become mentions, Jira issue macros become
// embeds, and everything else becomes an Unsupported block with the
// original markup. It returns problems for links it could not resolve and
// for markup it could not parse; it never fails.
func ConvertStorage(body, space string, r Resolver) (item.Doc, []string) {
	c := &converter{src: "<root>" + body + "</root>", space: space, r: r}
	c.dec = xml.NewDecoder(strings.NewReader(c.src))
	c.dec.Strict = false
	c.dec.Entity = xml.HTMLEntity
	c.run()
	return item.Doc{Blocks: c.blocks}, c.problems
}

type converter struct {
	src      string
	space    string
	r        Resolver
	dec      *xml.Decoder
	blocks   []item.Block
	problems []string

	cur   *item.Block      // block being filled with text, or nil
	lists []item.BlockKind // enclosing lists, innermost last (ADR-0032)
}

func name(n xml.Name) string {
	if n.Space != "" {
		return n.Space + ":" + n.Local
	}
	return n.Local
}

func attr(se xml.StartElement, space, local string) string {
	for _, a := range se.Attr {
		if a.Name.Space == space && a.Name.Local == local {
			return a.Value
		}
	}
	return ""
}

func (c *converter) open(kind item.BlockKind) {
	c.flush()
	c.cur = &item.Block{Kind: kind}
}

func (c *converter) text(s string) {
	if c.cur == nil {
		if strings.TrimSpace(s) == "" {
			return
		}
		c.open(item.Paragraph)
	}
	c.cur.Text += s
}

func (c *converter) flush() {
	if c.cur == nil {
		return
	}
	c.cur.Text = strings.TrimSpace(c.cur.Text)
	if c.cur.Text != "" || len(c.cur.Mentions) > 0 {
		c.blocks = append(c.blocks, *c.cur)
	}
	c.cur = nil
}

func (c *converter) emit(b item.Block) {
	c.flush()
	c.blocks = append(c.blocks, b)
}

func (c *converter) problem(format string, args ...any) {
	c.problems = append(c.problems, fmt.Sprintf(format, args...))
}

func (c *converter) run() {
	if _, err := c.dec.Token(); err != nil { // <root>
		return
	}
	// The root element must be closed by the input's own end; reaching the
	// end of input first means the body was cut off or not well-formed.
	if err := c.children(); err != nil {
		if errors.Is(err, io.EOF) {
			err = io.ErrUnexpectedEOF
		}
		c.problem("body could not be parsed completely: %v", err)
	}
	c.flush()
}

// children processes tokens until the end of the current element.
func (c *converter) children() error {
	for {
		start := int(c.dec.InputOffset())
		tok, err := c.dec.Token()
		if err != nil {
			return err
		}
		switch t := tok.(type) {
		case xml.EndElement:
			return nil
		case xml.CharData:
			c.text(string(t))
		case xml.StartElement:
			if err := c.element(t, start); err != nil {
				return err
			}
		}
	}
}

func (c *converter) element(se xml.StartElement, start int) error {
	n := name(se.Name)
	switch {
	case n == "p" || n == "blockquote" || headingTags[n]:
		kind := item.Paragraph
		switch {
		case headingTags[n]:
			kind = item.Heading
		case n == "blockquote":
			kind = item.Quote
		case n == "p" && len(c.lists) > 0 && c.cur != nil && c.cur.Text == "":
			// A paragraph directly inside a list item continues the item.
			return c.children()
		}
		c.open(kind)
		if headingTags[n] {
			c.cur.Level = min(int(n[1]-'0'), 3) // h4 to h6 become level 3
		}
		err := c.children()
		c.flush()
		return err
	case n == "ul" || n == "ol":
		c.flush()
		kind := item.BulletItem
		if n == "ol" {
			kind = item.OrderedItem
		}
		c.lists = append(c.lists, kind)
		err := c.children()
		c.lists = c.lists[:len(c.lists)-1]
		return err
	case n == "li":
		kind := item.BulletItem
		if len(c.lists) > 0 {
			kind = c.lists[len(c.lists)-1]
		}
		c.open(kind)
		err := c.children()
		c.flush()
		return err
	case n == "ac:link":
		return c.link()
	case n == "ac:structured-macro" && attr(se, "ac", "name") == "jira":
		return c.jiraMacro()
	case n == "br":
		c.text("\n")
		return c.children()
	case inlineTags[n]:
		return c.children()
	default:
		if err := c.dec.Skip(); err != nil {
			return err
		}
		c.emit(item.Block{Kind: item.Unsupported, Text: c.src[start:c.dec.InputOffset()]})
		return nil
	}
}

// link handles <ac:link> with a page or user target.
func (c *converter) link() error {
	for {
		tok, err := c.dec.Token()
		if err != nil {
			return err
		}
		switch t := tok.(type) {
		case xml.EndElement:
			if name(t.Name) == "ac:link" {
				return nil
			}
		case xml.StartElement:
			switch name(t.Name) {
			case "ri:page":
				title, space := attr(t, "ri", "content-title"), attr(t, "ri", "space-key")
				if space == "" {
					space = c.space
				}
				c.text(title)
				if id, ok := c.r.Page(space, title); ok {
					c.cur.Mentions = append(c.cur.Mentions, id)
				} else {
					c.problem("link to page %q in %s could not be resolved", title, space)
				}
			case "ri:user":
				account := attr(t, "ri", "account-id")
				display, ok := c.r.User(account)
				if !ok {
					display = "unknown-user"
					c.problem("user %q could not be resolved", account)
				}
				c.text("@" + display)
			}
		}
	}
}

// jiraMacro turns a Jira issue macro into an embed of the imported issue.
func (c *converter) jiraMacro() error {
	key := ""
	depth := 1
	inKey := false
	for depth > 0 {
		tok, err := c.dec.Token()
		if err != nil {
			return err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			depth++
			inKey = name(t.Name) == "ac:parameter" && attr(t, "ac", "name") == "key"
		case xml.EndElement:
			depth--
			inKey = false
		case xml.CharData:
			if inKey {
				key += string(t)
			}
		}
	}
	key = strings.TrimSpace(key)
	if id, ok := c.r.Issue(key); ok {
		c.emit(item.Block{Kind: item.Embed, Ref: id})
		return nil
	}
	c.problem("Jira issue %s is not imported; kept as text", key)
	c.emit(item.Block{Kind: item.Paragraph, Text: "[Jira issue " + key + "]"})
	return nil
}
