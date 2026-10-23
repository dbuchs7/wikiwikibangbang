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
	// Stars rate, from 1 to 5, how well the item answers the search
	// (ADR-0072, ADR-0083): 5 the key, the whole title, every word as a
	// whole word in the title, or the only hit with every word; 4 every
	// word in the title, or every word as a whole word and either in one
	// block or as typed; 3 every word somewhere; 2 at least half of the
	// words; 1 fewer.
	Stars int
	// Matched and Missing are the words of the search, as typed, that the
	// item holds and that it does not (ADR-0083).
	Matched, Missing []string
}

// Search returns the readable items in which a term of text occurs, best
// first: by stars, then by how many terms they hold, then by score
// (ADR-0033, ADR-0072, ADR-0083). q narrows the candidates as in Find.
//
// It scans the live state instead of keeping an index: content that is
// purged or redacted leaves memory with the item, so no copy of it
// survives in a search structure (ADR-0026).
func (x *Session) Search(text string, q Query) ([]Hit, error) {
	terms, err := searchTerms(text)
	if err != nil {
		return nil, err
	}
	shown := typedWords(text, terms)
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
		// Passages the principal may not read are not searched (ADR-0088).
		it = x.s.veiled(x.who, it)
		if h, ok := m.match(it, terms, wholeKey); ok {
			h.Stars = m.stars(it, terms, phrase, it.Key == wholeKey)
			for i, f := range m.found {
				if f.any {
					h.Matched = append(h.Matched, shown[i])
				} else {
					h.Missing = append(h.Missing, shown[i])
				}
			}
			hits = append(hits, h)
		}
	}
	// The only hit that holds every word is the answer (ADR-0083).
	if len(hits) == 1 && len(hits[0].Missing) == 0 {
		hits[0].Stars = 5
	}
	slices.SortFunc(hits, func(a, b Hit) int {
		// The stars come first, so the order never contradicts them.
		if a.Stars != b.Stars {
			return b.Stars - a.Stars
		}
		if len(a.Matched) != len(b.Matched) {
			return len(b.Matched) - len(a.Matched)
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

// HasWords reports whether text holds every word of query, ignoring case,
// as the filter of a list compares (ADR-0098). An empty query matches.
func HasWords(text, query string) bool {
	folded := fold(text)
	for _, w := range strings.Fields(fold(query)) {
		if !strings.Contains(folded, w) {
			return false
		}
	}
	return true
}

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

// typedWords are the terms as the search typed them, for showing: the
// first spelling of each term (ADR-0083).
func typedWords(text string, terms []string) []string {
	out := slices.Clone(terms)
	set := make([]bool, len(terms))
	for _, w := range strings.FieldsFunc(text, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
		if i := slices.Index(terms, fold(w)); i >= 0 && !set[i] {
			out[i], set[i] = w, true
		}
	}
	return out
}

// wordRuneAt reports whether the rune that starts at b[i] is a letter or a
// digit; outside b counts as not.
func wordRuneAt(b []byte, i int) bool {
	if i < 0 || i >= len(b) {
		return false
	}
	r, _ := utf8.DecodeRune(b[i:])
	return unicode.IsLetter(r) || unicode.IsDigit(r)
}

// wordRuneBefore reports whether the rune that ends at b[i] is a letter or
// a digit.
func wordRuneBefore(b []byte, i int) bool {
	if i <= 0 {
		return false
	}
	r, _ := utf8.DecodeLastRune(b[:i])
	return unicode.IsLetter(r) || unicode.IsDigit(r)
}

// containsWord reports whether t occurs in b as a whole word, not inside
// a longer one.
func containsWord(b, t []byte) bool {
	for at := 0; ; {
		i := bytes.Index(b[at:], t)
		if i < 0 {
			return false
		}
		i += at
		if !wordRuneBefore(b, i) && !wordRuneAt(b, i+len(t)) {
			return true
		}
		at = i + 1
	}
}

// stars rates a hit after match (ADR-0072, ADR-0083). It works on what
// match found per term and per region, so rating many hits stays cheap.
func (m *matcher) stars(it *Item, terms []string, phrase string, keyTyped bool) int {
	n, found, whole, title, titleWhole := len(terms), 0, 0, 0, 0
	for _, f := range m.found {
		if f.any {
			found++
		}
		if f.whole {
			whole++
		}
		if f.title {
			title++
		}
		if f.titleWhole {
			titleWhole++
		}
	}
	switch {
	case keyTyped || spaced(it.Title) == phrase || titleWhole == n:
		return 5
	case found == n && title == n:
		return 4
	case whole == n && m.oneRegion(phrase):
		return 4
	case found == n:
		return 3
	case 2*found >= n:
		return 2
	}
	return 1
}

// oneRegion reports whether one region holds every term as a whole word,
// or the words as typed, in their order.
func (m *matcher) oneRegion(phrase string) bool {
	for i, r := range m.regions {
		if m.whole[i] == len(m.found) {
			return true
		}
		if m.count[i] == len(m.found) && strings.Contains(spacedFolded(m.buf[r.start:r.end]), phrase) {
			return true
		}
	}
	return false
}

// spacedFolded is spaced for text that is folded already.
func spacedFolded(b []byte) string {
	out := make([]byte, 0, len(b)+2)
	out = append(out, ' ')
	for i := 0; i < len(b); {
		r, n := utf8.DecodeRune(b[i:])
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			out = append(out, b[i:i+n]...)
		} else if out[len(out)-1] != ' ' {
			out = append(out, ' ')
		}
		i += n
	}
	if out[len(out)-1] != ' ' {
		out = append(out, ' ')
	}
	return string(out)
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
	title      bool
}

// found is where one term occurs in the item being matched.
type found struct {
	any, whole        bool // anywhere; as a whole word anywhere
	title, titleWhole bool // in the title; as a whole word there
}

// matcher folds one item after another into the same buffer, and keeps
// per term and per region what it found, for the stars.
type matcher struct {
	buf     []byte
	regions []region
	found   []found
	count   []int // per region: the terms it holds
	whole   []int // per region: the terms it holds as whole words
}

func (m *matcher) add(text string, score, block, comment int) {
	start := len(m.buf)
	m.buf = appendFold(m.buf, text)
	m.regions = append(m.regions, region{start, len(m.buf), score, block, comment, len(m.regions) == 0})
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
	// Labels are found as words, as properties are (ADR-0092).
	for _, l := range it.Labels {
		m.add(l, scoreOther, -1, -1)
	}
	// A deleted comment's text is already empty, so it cannot match.
	for i, c := range it.Comments {
		m.add(c.Text, scoreOther, -1, i)
	}
	h := Hit{}
	if it.Key == wholeKey {
		h.Score += scoreKey
	}
	m.found = append(m.found[:0], make([]found, len(terms))...)
	m.count = append(m.count[:0], make([]int, len(m.regions))...)
	m.whole = append(m.whole[:0], make([]int, len(m.regions))...)
	hit := false
	snippetBlock, snippetComment := -1, -1
	for ti, t := range terms {
		inBody, inProps, inComments := false, false, false
		f := &m.found[ti]
		for ri, r := range m.regions {
			text := m.buf[r.start:r.end]
			if !bytes.Contains(text, []byte(t)) {
				continue
			}
			whole := containsWord(text, []byte(t))
			f.any, hit = true, true
			m.count[ri]++
			if whole {
				f.whole = true
				m.whole[ri]++
			}
			if r.title {
				f.title = true
				f.titleWhole = f.titleWhole || whole
			}
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
	}
	if !hit {
		return Hit{}, false
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
