package migrate

import (
	"encoding/json"
	"strconv"
	"strings"
)

// adfNode is a node of the Atlassian Document Format, Jira Cloud's rich
// text in API v3.
type adfNode struct {
	Type    string         `json:"type"`
	Text    string         `json:"text"`
	Attrs   map[string]any `json:"attrs"`
	Marks   []adfMark      `json:"marks"`
	Content []adfNode      `json:"content"`
}

type adfMark struct {
	Type  string         `json:"type"`
	Attrs map[string]any `json:"attrs"`
}

// ADFText converts a document in the Atlassian Document Format to the plain
// text of SrcIssue.Description: blocks separated by a blank line, list items
// as blocks of their own. Formatting is dropped; a link keeps its address
// after its text; mentions, emoji, dates and statuses keep their text.
// Attachments and other media have no text and become a placeholder, which
// is reported through the second result.
func ADFText(doc json.RawMessage) (text string, dropped []string) {
	var root adfNode
	if len(doc) == 0 || string(doc) == "null" || json.Unmarshal(doc, &root) != nil {
		return "", nil
	}
	var blocks []string
	w := adfWriter{dropped: map[string]bool{}}
	w.blocks(root.Content, "", &blocks)
	for k := range w.dropped {
		dropped = append(dropped, k)
	}
	return strings.Join(blocks, "\n\n"), dropped
}

type adfWriter struct{ dropped map[string]bool }

// blocks appends the text of block nodes; prefix starts a list item.
func (w adfWriter) blocks(nodes []adfNode, prefix string, out *[]string) {
	for _, n := range nodes {
		switch n.Type {
		case "bulletList", "orderedList":
			start := 1
			if f, ok := n.Attrs["order"].(float64); ok && f >= 1 {
				start = int(f)
			}
			for i, li := range n.Content {
				mark := "- "
				if n.Type == "orderedList" {
					mark = strconv.Itoa(start+i) + ". "
				}
				w.blocks(li.Content, prefix+mark, out)
			}
		case "blockquote", "panel", "expand", "nestedExpand", "layoutSection", "layoutColumn", "listItem", "taskList", "decisionList":
			w.blocks(n.Content, prefix, out)
		case "taskItem", "decisionItem":
			box := "[ ] "
			if n.Attrs["state"] == "DONE" || n.Attrs["state"] == "DECIDED" {
				box = "[x] "
			}
			w.emit(prefix+box+w.inline(n.Content), out)
		case "table":
			for _, row := range n.Content {
				var cells []string
				for _, cell := range row.Content {
					var parts []string
					w.blocks(cell.Content, "", &parts)
					cells = append(cells, strings.Join(parts, " "))
				}
				w.emit(prefix+strings.Join(cells, " | "), out)
			}
		case "rule":
		case "mediaSingle", "mediaGroup", "media", "mediaInline":
			w.dropped["attachments"] = true
			w.emit(prefix+"[attachment]", out)
		case "codeBlock", "paragraph", "heading":
			w.emit(prefix+w.inline(n.Content), out)
		default:
			// An unknown block: its text, if it has any.
			w.emit(prefix+w.inline(n.Content), out)
		}
		prefix = indent(prefix)
	}
}

// indent turns a list item's marker into the indentation of its further
// blocks.
func indent(prefix string) string { return strings.Repeat(" ", len(prefix)) }

func (w adfWriter) emit(s string, out *[]string) {
	if strings.TrimSpace(s) != "" {
		*out = append(*out, strings.TrimRight(s, " "))
	}
}

func (w adfWriter) inline(nodes []adfNode) string {
	var b strings.Builder
	for _, n := range nodes {
		switch n.Type {
		case "text":
			b.WriteString(n.Text)
			for _, m := range n.Marks {
				if href, ok := m.Attrs["href"].(string); m.Type == "link" && ok && href != n.Text {
					b.WriteString(" (" + href + ")")
				}
			}
		case "hardBreak":
			b.WriteString("\n")
		case "inlineCard", "blockCard", "embedCard":
			if u, ok := n.Attrs["url"].(string); ok {
				b.WriteString(u)
			}
		case "mention", "emoji", "status":
			if t, ok := n.Attrs["text"].(string); ok {
				b.WriteString(t)
			} else if t, ok := n.Attrs["shortName"].(string); ok {
				b.WriteString(t)
			}
		case "date":
			if ts, ok := n.Attrs["timestamp"].(string); ok {
				b.WriteString(adfDate(ts))
			}
		case "mediaInline":
			w.dropped["attachments"] = true
			b.WriteString("[attachment]")
		default:
			b.WriteString(w.inline(n.Content))
		}
	}
	return b.String()
}

// adfDate formats a date node's timestamp, milliseconds since the epoch, as
// an ISO date.
func adfDate(ms string) string {
	n, err := strconv.ParseInt(ms, 10, 64)
	if err != nil {
		return ms
	}
	return unixDate(n / 1000)
}
