package item

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/dbuchs7/wikiwikibangbang/internal/budget"
)

func hitKeys(hits []Hit) []string {
	out := make([]string, len(hits))
	for i, h := range hits {
		out[i] = h.Item.Key
	}
	return out
}

func searchFixture(t *testing.T) (*Store, map[string]Item) {
	t.Helper()
	s := permFixture(t)
	anna := s.As("anna")
	items := map[string]Item{}
	add := func(name string, n NewItem, body ...string) {
		it := mustItem(t)(anna.Create(n))
		if len(body) > 0 {
			var d Doc
			for _, b := range body {
				d.Blocks = append(d.Blocks, Block{Kind: Paragraph, Text: b})
			}
			noErr(t, anna.CommitBody(it.ID, d))
		}
		items[name] = it
	}
	add("runbook", NewItem{Space: "PLAT", Type: "page", Title: "Database runbook"},
		"Restart the primary first.", "Then check the Replica lag.")
	add("backup", NewItem{Space: "PLAT", Type: "page", Title: "Backup policy"},
		"Backups of the database run nightly at the Strasse office.")
	add("decision", NewItem{Space: "PLAT", Type: "decision", Title: "Use one log",
		Properties: Props{"deciders": "Maintainers of the storage layer"}})
	add("secret", NewItem{Space: "PLAT", Type: "page", Title: "Database credentials"}, "Never share.")
	noErr(t, anna.Restrict(items["secret"].ID, Restriction{Read: []string{"anna"}}))
	return s, items
}

func TestSearchMatchesEveryTermAnywhere(t *testing.T) {
	s, _ := searchFixture(t)
	ben := s.As("ben")
	cases := []struct {
		query string
		want  []string
	}{
		{"database", []string{"PLAT-1", "PLAT-2"}},     // the title ranks first
		{"DATABASE replica", []string{"PLAT-1"}},       // case-folded, every term must match
		{"replic", []string{"PLAT-1"}},                 // a term matches inside words
		{"storage layer", []string{"PLAT-3"}},          // text properties are searched
		{"straße", []string{"PLAT-2"}},                 // ß and ss are the same letter
		{"database nothing-like-this", nil},            // no partial matches
		{"plat-2", []string{"PLAT-2"}},                 // a key finds its item first
		{"  restart,  primary!  ", []string{"PLAT-1"}}, // punctuation separates terms
	}
	for _, c := range cases {
		hits, err := ben.Search(c.query, Query{})
		if err != nil {
			t.Fatalf("%q: %v", c.query, err)
		}
		if got := hitKeys(hits); !slices.Equal(got, c.want) {
			t.Errorf("%q: got %v, want %v", c.query, got, c.want)
		}
	}
}

func TestSearchRanksTitleAboveBody(t *testing.T) {
	s, _ := searchFixture(t)
	hits, err := s.As("ben").Search("database", Query{})
	noErr(t, err)
	if len(hits) != 2 || hits[0].Item.Key != "PLAT-1" || hits[0].Score <= hits[1].Score {
		t.Fatalf("hits %v", hits)
	}
}

func TestSearchSnippetShowsTheMatchingBlock(t *testing.T) {
	s, _ := searchFixture(t)
	hits, err := s.As("ben").Search("replica", Query{})
	noErr(t, err)
	if len(hits) != 1 || hits[0].Snippet != "Then check the Replica lag." {
		t.Fatalf("hits %#v", hits)
	}
	long := strings.Repeat("filler ", 60) + "needle " + strings.Repeat("tail ", 60)
	it := mustItem(t)(s.As("anna").Create(NewItem{Space: "PLAT", Type: "page", Title: "Long"}))
	noErr(t, s.As("anna").CommitBody(it.ID, Doc{Blocks: []Block{{Kind: Paragraph, Text: long}}}))
	hits, err = s.As("ben").Search("needle", Query{})
	noErr(t, err)
	snip := hits[0].Snippet
	if !strings.Contains(snip, "needle") || len([]rune(snip)) > SnippetRunes+2 ||
		!strings.HasPrefix(snip, "…") || !strings.HasSuffix(snip, "…") {
		t.Fatalf("snippet %q", snip)
	}
}

func TestSearchRespectsReadPermissions(t *testing.T) {
	s, items := searchFixture(t)
	if got := hitKeys(must(s.As("anna").Search("credentials", Query{}))); !slices.Equal(got, []string{"PLAT-4"}) {
		t.Fatalf("anna: %v", got)
	}
	// A viewer of the space does not see a restricted item, an outsider sees nothing.
	for _, who := range []string{"ben", "carl"} {
		if got := must(s.As(who).Search("credentials", Query{})); len(got) != 0 {
			t.Errorf("%s finds %v", who, hitKeys(got))
		}
	}
	if got := must(s.As("carl").Search("database", Query{})); len(got) != 0 {
		t.Errorf("an outsider finds %v", hitKeys(got))
	}
	_ = items
}

func TestSearchNeverFindsErasedContent(t *testing.T) {
	s, items := searchFixture(t)
	anna := s.As("anna")
	noErr(t, anna.Trash(items["backup"].ID))
	if got := must(anna.Search("nightly", Query{})); len(got) != 0 {
		t.Fatalf("trashed item found: %v", hitKeys(got))
	}
	noErr(t, s.As("pat").Purge(items["backup"].ID))
	if got := must(anna.Search("nightly", Query{IncludeDeleted: true})); len(got) != 0 {
		t.Fatalf("purged content found: %v", hitKeys(got))
	}
	noErr(t, s.As("erin").Redact(items["runbook"].ID, "contains a password"))
	if got := must(anna.Search("replica", Query{IncludeDeleted: true})); len(got) != 0 {
		t.Fatalf("redacted content found: %v", hitKeys(got))
	}
}

func TestSearchFilters(t *testing.T) {
	s, _ := searchFixture(t)
	if got := hitKeys(must(s.As("ben").Search("use", Query{Type: "decision"}))); !slices.Equal(got, []string{"PLAT-3"}) {
		t.Fatalf("type filter: %v", got)
	}
	if got := must(s.As("ben").Search("database", Query{Space: "OPS"})); len(got) != 0 {
		t.Fatalf("space filter: %v", hitKeys(got))
	}
}

func TestSearchRejectsUnusableQueries(t *testing.T) {
	s, _ := searchFixture(t)
	for _, q := range []string{"", "   ", "!!!", strings.Repeat("a", budget.Get().SearchQueryBytes+1),
		manyTerms(budget.Get().SearchQueryTerms + 1)} {
		if _, err := s.As("ben").Search(q, Query{}); !errors.Is(err, ErrValidation) {
			t.Errorf("%.20q: err = %v, want ErrValidation", q, err)
		}
	}
}

func manyTerms(n int) string {
	var b strings.Builder
	for i := range n {
		fmt.Fprintf(&b, "t%d ", i)
	}
	return b.String()
}

func must(hits []Hit, err error) []Hit {
	if err != nil {
		panic(err)
	}
	return hits
}

func BenchmarkSearch(b *testing.B) {
	s := NewStore()
	for _, td := range builtinTypes() {
		if err := s.DefineType("admin", td); err != nil {
			b.Fatal(err)
		}
	}
	if err := s.CreateSpace("admin", "PLAT", "Platform"); err != nil {
		b.Fatal(err)
	}
	para := strings.Repeat("Lorem ipsum dolor sit amet, consectetur adipiscing elit. ", 20) // about 1 KB
	const n = 10000
	for i := range n {
		it, err := s.Create("admin", NewItem{Space: "PLAT", Type: "page", Title: "Page"})
		if err != nil {
			b.Fatal(err)
		}
		text := para
		if i%100 == 0 {
			text += " needle"
		}
		if err := s.CommitBody("admin", it.ID, Doc{Blocks: []Block{{Kind: Paragraph, Text: text}}}); err != nil {
			b.Fatal(err)
		}
	}
	noErrB := func(err error) {
		if err != nil {
			b.Fatal(err)
		}
	}
	noErrB(s.GrantSystemRole("admin", "admin", RoleAdmin))
	x := s.As("admin")
	b.ResetTimer()
	for b.Loop() {
		hits, err := x.Search("needle", Query{})
		if err != nil || len(hits) != n/100 {
			b.Fatalf("%d hits, %v", len(hits), err)
		}
	}
}

func TestSearchFindsCommentsButNotDeletedOnes(t *testing.T) {
	s, items := searchFixture(t)
	anna := s.As("anna")
	c, err := anna.AddComment(items["decision"].ID, "We agreed on the quorum rule.")
	noErr(t, err)
	hits := must(s.As("ben").Search("quorum", Query{}))
	if len(hits) != 1 || hits[0].Item.Key != "PLAT-3" || hits[0].Comment != c.ID || hits[0].Snippet != "We agreed on the quorum rule." {
		t.Fatalf("hits %+v", hits)
	}
	// A body match is preferred as the snippet.
	_, err = anna.AddComment(items["runbook"].ID, "replica lag was high")
	noErr(t, err)
	hits = must(s.As("ben").Search("replica", Query{}))
	if len(hits) != 1 || hits[0].Comment != "" || hits[0].Snippet != "Then check the Replica lag." {
		t.Fatalf("hits %+v", hits)
	}
	noErr(t, anna.DeleteComment(items["decision"].ID, c.ID))
	if got := must(s.As("ben").Search("quorum", Query{})); len(got) != 0 {
		t.Fatalf("a deleted comment is found: %v", hitKeys(got))
	}
}

// TestHitsAreRatedWithStars: each hit is rated on its own, by where and how
// the terms occur (ADR-0072).
func TestHitsAreRatedWithStars(t *testing.T) {
	s := setup(t)
	noErr(t, s.GrantRole("admin", "PLAT", "anna", Editor))
	mk := func(title string, body ...string) Item {
		var blocks []Block
		for _, b := range body {
			blocks = append(blocks, Block{Kind: Paragraph, Text: b})
		}
		it := mustItem(t)(s.Create("anna", NewItem{Space: "PLAT", Type: "page", Title: title}))
		if len(blocks) > 0 {
			noErr(t, s.CommitBody("anna", it.ID, Doc{Blocks: blocks}))
		}
		return it
	}
	exact := mk("Rote Buche")
	inTitle := mk("Die rote Buche im Jura")
	someTitle := mk("Buchen", "Laub und rote Blätter")
	phrase := mk("Bäume", "Die rote Buche wächst langsam.")
	oneBlock := mk("Wald", "Eine Buche hat im Herbst rote Blätter.")
	spread := mk("Notiz", "rote", "eine Buche")
	want := map[ID]int{exact.ID: 5, inTitle.ID: 4, someTitle.ID: 3, phrase.ID: 3, oneBlock.ID: 2, spread.ID: 1}
	hits, err := s.As("anna").Search("rote Buche", Query{})
	noErr(t, err)
	if len(hits) != len(want) {
		t.Fatalf("hits: %d", len(hits))
	}
	for _, h := range hits {
		if h.Stars != want[h.Item.ID] {
			t.Errorf("%q: %d stars, want %d", h.Item.Title, h.Stars, want[h.Item.ID])
		}
	}
	// The key typed in full is five stars.
	byKey, err := s.As("anna").Search(spread.Key, Query{})
	noErr(t, err)
	if len(byKey) == 0 || byKey[0].Item.ID != spread.ID || byKey[0].Stars != 5 {
		t.Fatalf("by key: %+v", byKey)
	}
}

// spaced builds in one pass what joining the folded words gives.
func TestSpacedIsTheJoinedWords(t *testing.T) {
	for _, text := range []string{"", " ", "Hello, World!", "  Straße — Übergröße 42 ", "a", "-a-", "ÄÖÜ äöü ß", "x\ty\nz", "日本語 テキスト", "1-2-3", "!!!"} {
		if got, want := spaced(text), " "+strings.Join(wordsOf(text), " ")+" "; got != want {
			t.Errorf("spaced(%q) = %q, want %q", text, got, want)
		}
	}
}
