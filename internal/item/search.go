package item

import (
	"bytes"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/dbuchs7/wikiwikibangbang/internal/budget"
)

const (
	// SnippetRunes is the length of a hit's snippet, without the ellipses.
	SnippetRunes = 160
)

// Hit is one search result.
type Hit struct {
	Item  Item
	Score int
	// Snippet is the start of the first body block that matches, or the
	// part around the match in a long block; failing that, of the first
	// comment that matches; empty when only the title, key or properties
	// matched.
	Snippet string
	// Comment is the comment the snippet comes from, or empty when it
	// comes from the body (ADR-0035).
	Comment ID
	// Stars rate, from 1 to 5, how well the item answers the search on its
	// own, not compared with the other hits (ADR-0072): 5 the key or the
	// whole title, 4 every term in the title, 3 some in the title or the
	// words as typed in the text, 2 every term in one block, 1 the terms
	// spread over the item.
	Stars int
}

// Search returns the readable items in which every term of text occurs,
// best first: by stars, then by score (ADR-0033, ADR-0072). q narrows the candidates as in Find.
//
// It scans the live state instead of keeping an index: content that is
// purged or redacted leaves memory with the item, so no copy of it
// survives in a search structure (ADR-0026).
func (x *Session) Search(text string, q Query) ([]Hit, error) {
	terms, err := searchTerms(text)
	if err != nil {
		return nil, err
	}
	phrase := " " + strings.Join(wordsOf(text), " ") + " "
	wholeKey := strings.ToUpper(strings.TrimSpace(text))
	var hits []Hit
	var m matcher
	for _, it := range x.s.items {
		if (!q.IncludeDeleted && it.Presence != Live) ||
			(q.Type != "" && it.Type != q.Type) ||
			(q.Space != "" && it.Space != q.Space) ||
			(q.State != "" && it.State != q.State) ||
			(q.Parent != "" && it.Parent != q.Parent) ||
			(q.Label != "" && !slices.Contains(it.Labels, q.Label)) ||
			!whereMatches(it, q.WhereText) ||
			!x.s.canRead(x.who, it) {
			continue
		}
		if h, ok := m.match(it, terms, wholeKey); ok {
			h.Stars = stars(it, terms, phrase, it.Key == wholeKey)
			hits = append(hits, h)
		}
	}
	slices.SortFunc(hits, func(a, b Hit) int {
		// The stars come first, so the order never contradicts them.
		if a.Stars != b.Stars {
			return b.Stars - a.Stars
		}
		if a.Score != b.Score {
			return b.Score - a.Score
		}
		return b.Item.created - a.Item.created // newer first
	})
	return hits, nil
}

// appendFold appends text in a comparable form: lower case, and ß as ss,
// the Swiss spelling. It allocates only when buf has to grow, so a search
// can fold every item into one buffer.
func appendFold(buf []byte, text string) []byte {
	for i := 0; i < len(text); {
		c := text[i]
		if c < utf8.RuneSelf {
			if 'A' <= c && c <= 'Z' {
				c += 'a' - 'A'
			}
			buf = append(buf, c)
			i++
			continue
		}
		r, n := utf8.DecodeRuneInString(text[i:])
		if r == 'ß' || r == 'ẞ' {
			buf = append(buf, "ss"...)
		} else {
			buf = utf8.AppendRune(buf, unicode.ToLower(r))
		}
		i += n
	}
	return buf
}

func fold(text string) string { return string(appendFold(nil, text)) }

// wordsOf splits a text into its folded words, as a search does.
func wordsOf(text string) []string {
	return strings.FieldsFunc(fold(text), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
}

// spaced joins words between spaces, so a phrase is found as whole words:
// " " + the folded words joined by " " + " ", built in one pass, as it
// runs for every block of every hit (ADR-0081).
func spaced(text string) string {
	folded := appendFold(nil, text)
	out := make([]byte, 0, len(folded)+2)
	out = append(out, ' ')
	for i := 0; i < len(folded); {
		r, n := utf8.DecodeRune(folded[i:])
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			out = append(out, folded[i:i+n]...)
		} else if out[len(out)-1] != ' ' {
			out = append(out, ' ')
		}
		i += n
	}
	if out[len(out)-1] != ' ' {
		out = append(out, ' ')
	}
	if len(out) == 1 {
		return "  "
	}
	return string(out)
}

// stars rates a hit (ADR-0072). It runs for hits only, so its allocations
// stay with the results, not with every item searched.
func stars(it *Item, terms []string, phrase string, wholeKey bool) int {
	title := spaced(it.Title)
	if wholeKey || title == phrase {
		return 5
	}
	inTitle := 0
	for _, t := range terms {
		if strings.Contains(title, t) {
			inTitle++
		}
	}
	if inTitle == len(terms) {
		return 4
	}
	texts := make([]string, 0, len(it.Body.Blocks)+len(it.Properties))
	for _, b := range it.Body.Blocks {
		texts = append(texts, spaced(b.Text))
	}
	for _, v := range it.Properties {
		if s, ok := v.(string); ok {
			texts = append(texts, spaced(s))
		}
	}
	oneBlock := false
	for _, x := range texts {
		if strings.Contains(x, phrase) {
			return 3
		}
		all := true
		for _, t := range terms {
			all = all && strings.Contains(x, t)
		}
		oneBlock = oneBlock || all
	}
	switch {
	case inTitle > 0:
		return 3
	case oneBlock:
		return 2
	}
	return 1
}

func searchTerms(text string) ([]string, error) {
	limits := budget.Get() // ADR-0015, ADR-0033
	if len(text) > limits.SearchQueryBytes {
		return nil, invalid("a search is at most %d bytes", limits.SearchQueryBytes)
	}
	words := wordsOf(text)
	var terms []string
	for _, w := range words {
		if !slices.Contains(terms, w) {
			terms = append(terms, w)
		}
	}
	switch {
	case len(terms) == 0:
		return nil, invalid("a search needs a letter or a digit")
	case len(terms) > limits.SearchQueryTerms:
		return nil, invalid("a search has at most %d terms", limits.SearchQueryTerms)
	}
	return terms, nil
}

// Scores: a key typed in full outranks everything, a term in the title
// outranks one in the body or the properties.
const (
	scoreKey   = 1000
	scoreTitle = 10
	scoreOther = 1
)

// region is a part of an item folded into the search buffer.
type region struct {
	start, end int
	score      int // what a term found here adds
	block      int // body block index, or -1
	comment    int // comment index, or -1
}

// matcher folds one item after another into the same buffer.
type matcher struct {
	buf     []byte
	regions []region
}

func (m *matcher) add(text string, score, block, comment int) {
	start := len(m.buf)
	m.buf = appendFold(m.buf, text)
	m.regions = append(m.regions, region{start, len(m.buf), score, block, comment})
}

func (m *matcher) match(it *Item, terms []string, wholeKey string) (Hit, bool) {
	m.buf, m.regions = m.buf[:0], m.regions[:0]
	m.add(it.Title, scoreTitle, -1, -1)
	m.add(it.Key, 0, -1, -1)
	for i, b := range it.Body.Blocks {
		m.add(b.Text, scoreOther, i, -1)
	}
	for _, v := range it.Properties {
		if s, ok := v.(string); ok {
			m.add(s, scoreOther, -1, -1)
		}
	}
	// A deleted comment's text is already empty, so it cannot match.
	for i, c := range it.Comments {
		m.add(c.Text, scoreOther, -1, i)
	}
	h := Hit{}
	if it.Key == wholeKey {
		h.Score += scoreKey
	}
	snippetBlock, snippetComment := -1, -1
	for _, t := range terms {
		found, inBody, inProps, inComments := false, false, false, false
		for _, r := range m.regions {
			if !bytes.Contains(m.buf[r.start:r.end], []byte(t)) {
				continue
			}
			found = true
			switch {
			case r.block >= 0:
				if !inBody {
					h.Score += r.score
					inBody = true
				}
				if snippetBlock < 0 {
					snippetBlock = r.block
				}
			case r.comment >= 0:
				if !inComments {
					h.Score += r.score
					inComments = true
				}
				if snippetComment < 0 {
					snippetComment = r.comment
				}
			case r.score == scoreOther:
				if !inProps {
					h.Score += r.score
					inProps = true
				}
			default:
				h.Score += r.score
			}
		}
		if !found {
			return Hit{}, false
		}
	}
	switch {
	case snippetBlock >= 0:
		text := it.Body.Blocks[snippetBlock].Text
		h.Snippet = snippet(text, fold(text), terms)
	case snippetComment >= 0:
		c := it.Comments[snippetComment]
		h.Snippet, h.Comment = snippet(c.Text, fold(c.Text), terms), c.ID
	}
	h.Item = it.clone()
	return h, true
}

// snippet cuts SnippetRunes runes of text around the first term found in
// its folded form. Folding can change the length of a few letters (ß), so
// the position is carried over in runes and clamped; the snippet may then
// be off by a letter, never out of range.
func snippet(text, folded string, terms []string) string {
	runes := []rune(text)
	if len(runes) <= SnippetRunes {
		return text
	}
	at := 0
	for _, t := range terms {
		if i := strings.Index(folded, t); i >= 0 {
			at = len([]rune(folded[:i]))
			break
		}
	}
	start := max(0, min(at-SnippetRunes/3, len(runes)-SnippetRunes))
	end := start + SnippetRunes
	out := string(runes[start:end])
	if start > 0 {
		out = "…" + out
	}
	if end < len(runes) {
		out += "…"
	}
	return out
}
