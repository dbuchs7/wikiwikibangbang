package item

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

// secretFixture: anna writes a page and protects the name in it for
// herself and @hr; ben views PLAT, vic edits it, pat administers it.
func secretFixture(t *testing.T) (*Store, Item, Secret) {
	t.Helper()
	s := permFixture(t)
	noErr(t, s.GrantRole("root", "PLAT", "vic", Editor))
	noErr(t, s.AddToGroup("root", "hr", "hanna"))
	noErr(t, s.GrantRole("root", "PLAT", "hanna", Viewer))
	anna := s.As("anna")
	page := mustItem(t)(anna.Create(NewItem{Space: "PLAT", Type: "page", Title: "Review", Body: &Doc{Blocks: []Block{
		{Kind: Paragraph, Text: "Salary of Paul Muster is due.", Marks: []Mark{{Type: MarkBold, Start: 0, End: 9}}},
		{Kind: Paragraph, Text: "Open to all."},
	}}}))
	sec, err := anna.Protect(page.ID, 0, 10, 21, []string{"@hr"})
	noErr(t, err)
	return s, page, sec
}

func TestAPassageIsReadByItsReadersOnly(t *testing.T) {
	s, page, sec := secretFixture(t)
	for _, who := range []string{"anna", "hanna"} {
		got := mustItem(t)(s.As(who).Get(page.ID))
		if got.Body.Blocks[0].Text != "Salary of Paul Muster is due." {
			t.Errorf("%s reads %q", who, got.Body.Blocks[0].Text)
		}
	}
	for _, who := range []string{"ben", "vic", "pat"} {
		got := mustItem(t)(s.As(who).Get(page.ID))
		b := got.Body.Blocks[0]
		if b.Text != "Salary of "+SecretPlaceholder+" is due." || strings.Contains(b.Text, "Paul") {
			t.Fatalf("%s reads %q", who, b.Text)
		}
		// The passage's mark covers the placeholder; the bold before it
		// stays.
		if !slices.Contains(b.Marks, Mark{Type: MarkSecret, Start: 10, End: 18, Ref: sec.ID}) || !slices.Contains(b.Marks, Mark{Type: MarkBold, Start: 0, End: 9}) {
			t.Errorf("%s marks %+v", who, b.Marks)
		}
		if got.Body.Blocks[1].Text != "Open to all." {
			t.Errorf("the rest is veiled too: %q", got.Body.Blocks[1].Text)
		}
		// Search does not find what one may not read; the rest it does.
		if hits, _ := s.As(who).Search("Muster", Query{}); len(hits) != 0 {
			t.Errorf("%s finds the passage: %+v", who, hits)
		}
		if hits, _ := s.As(who).Search("Salary", Query{}); len(hits) != 1 || strings.Contains(hits[0].Snippet, "Paul") {
			t.Errorf("%s searches %+v", who, hits)
		}
		page, _, _, _ := s.As(who).FindPage(Query{Space: "PLAT"}, "", 50)
		for _, it := range page {
			for _, b := range it.Body.Blocks {
				if strings.Contains(b.Text, "Paul") {
					t.Errorf("%s lists %q", who, b.Text)
				}
			}
		}
	}
	if hits, _ := s.As("hanna").Search("Muster", Query{}); len(hits) != 1 {
		t.Errorf("a reader does not find it: %+v", hits)
	}
}

func TestOnlyThoseWhoReadEveryPassageChangeTheBody(t *testing.T) {
	s, page, _ := secretFixture(t)
	vic := s.As("vic")
	veiled := mustItem(t)(vic.Get(page.ID))
	if err := vic.CommitBody(page.ID, veiled.Body); !errors.Is(err, ErrForbidden) {
		t.Fatalf("one who cannot read a passage commits the body: %v", err)
	}
	if _, err := vic.BodyAt(page.ID, 1); !errors.Is(err, ErrForbidden) {
		t.Fatalf("earlier versions shown to one who cannot read a passage: %v", err)
	}
	if s.As("vic").ReadsAllSecrets(page.ID) || !s.As("anna").ReadsAllSecrets(page.ID) {
		t.Fatal("ReadsAllSecrets")
	}
	// The fields, the title and the comments stay theirs to change.
	noErr(t, vic.SetTitle(page.ID, "Review 2026"))
	// A passage holds no link, mention or other passage; a passage of
	// another item is refused.
	anna := s.As("anna")
	full := mustItem(t)(anna.Get(page.ID)).Body
	bad := full.clone()
	bad.Blocks[0].Marks = append(bad.Blocks[0].Marks, Mark{Type: MarkLink, Start: 12, End: 14, Href: "https://x.example"})
	if err := anna.CommitBody(page.ID, bad); !errors.Is(err, ErrValidation) {
		t.Errorf("a link in a passage: %v", err)
	}
	bad = full.clone()
	bad.Blocks[1].Marks = []Mark{{Type: MarkSecret, Start: 0, End: 4, Ref: "elsewhere"}}
	if err := anna.CommitBody(page.ID, bad); !errors.Is(err, ErrValidation) {
		t.Errorf("an unknown passage: %v", err)
	}
	if _, err := anna.Create(NewItem{Space: "PLAT", Type: "page", Title: "x", Body: &bad}); !errors.Is(err, ErrValidation) {
		t.Errorf("a passage in a new item: %v", err)
	}
	if _, err := anna.Protect(page.ID, 0, 12, 14, nil); !errors.Is(err, ErrValidation) {
		t.Errorf("a passage over a passage: %v", err)
	}
	if _, err := anna.Protect(page.ID, 1, 0, 99, nil); !errors.Is(err, ErrValidation) {
		t.Errorf("a passage beyond the text: %v", err)
	}
	if _, err := vic.Protect(page.ID, 1, 0, 4, nil); !errors.Is(err, ErrForbidden) {
		t.Errorf("one who cannot read every passage protects another: %v", err)
	}
	if _, err := s.As("ben").Protect(page.ID, 1, 0, 4, nil); !errors.Is(err, ErrForbidden) {
		t.Errorf("a viewer protects: %v", err)
	}
	// Those who read every passage edit as before, the passage with them.
	edited := full.clone()
	edited.Blocks[1].Text = "Open to all, really."
	noErr(t, anna.CommitBody(page.ID, edited))
	if got := mustItem(t)(s.As("hanna").Get(page.ID)); got.Body.Blocks[0].Text != "Salary of Paul Muster is due." {
		t.Fatalf("the passage after an edit: %q", got.Body.Blocks[0].Text)
	}
}

func TestAPassageIsAskedForAndGranted(t *testing.T) {
	s, page, sec := secretFixture(t)
	vic, anna, pat := s.As("vic"), s.As("anna"), s.As("pat")
	views, err := vic.Secrets(page.ID)
	noErr(t, err)
	if len(views) != 1 || views[0].Readable || views[0].Decides || views[0].Readers != nil || views[0].By != "anna" {
		t.Fatalf("vic sees %+v", views)
	}
	noErr(t, vic.RequestSecret(page.ID, sec.ID))
	noErr(t, vic.RequestSecret(page.ID, sec.ID)) // once is enough
	if v, _ := vic.Secrets(page.ID); !v[0].Requested {
		t.Fatal("the request is not shown to the one who asked")
	}
	if err := s.As("hanna").RequestSecret(page.ID, sec.ID); !errors.Is(err, ErrValidation) {
		t.Errorf("a reader asks: %v", err)
	}
	// The one who protected it and the space's administrators hear of it
	// and decide; others do not.
	for _, who := range []string{"anna", "pat"} {
		if n, _ := s.As(who).Notifications(0, 10); len(n) == 0 || n[0].Kind != SecretRequested {
			t.Errorf("%s is not told: %+v", who, n)
		}
	}
	if n, _ := s.As("hanna").Notifications(0, 10); len(n) != 0 {
		t.Errorf("a reader who does not decide is told: %+v", n)
	}
	if err := s.As("hanna").DecideSecret(page.ID, sec.ID, "vic", true); !errors.Is(err, ErrForbidden) {
		t.Errorf("a reader decides: %v", err)
	}
	if v, _ := pat.Secrets(page.ID); !v[0].Decides || v[0].Readable || !slices.Equal(v[0].Requests, []string{"vic"}) || !slices.Equal(v[0].Readers, []string{"@hr"}) {
		t.Fatalf("pat sees %+v", v)
	}
	noErr(t, pat.DecideSecret(page.ID, sec.ID, "vic", true))
	if got := mustItem(t)(vic.Get(page.ID)); !strings.Contains(got.Body.Blocks[0].Text, "Paul Muster") {
		t.Fatalf("granted, vic reads %q", got.Body.Blocks[0].Text)
	}
	if n, _ := vic.Notifications(0, 10); len(n) == 0 || n[0].Kind != SecretGranted {
		t.Errorf("vic is not told: %+v", n)
	}
	// Ben asks and is declined.
	noErr(t, s.As("ben").RequestSecret(page.ID, sec.ID))
	noErr(t, anna.DecideSecret(page.ID, sec.ID, "ben", false))
	if err := anna.DecideSecret(page.ID, sec.ID, "ben", true); !errors.Is(err, ErrNotFound) {
		t.Errorf("an answered request is answered again: %v", err)
	}
	if got := mustItem(t)(s.As("ben").Get(page.ID)); strings.Contains(got.Body.Blocks[0].Text, "Paul") {
		t.Fatal("declined, ben reads it")
	}
	// Readers set by hand; the protection taken off, everyone reads it.
	noErr(t, anna.SetSecretReaders(page.ID, sec.ID, []string{"@hr", "ben", "ben"}))
	if got := mustItem(t)(s.As("ben").Get(page.ID)); !strings.Contains(got.Body.Blocks[0].Text, "Paul") {
		t.Fatal("a reader set by hand does not read it")
	}
	if err := anna.SetSecretReaders(page.ID, sec.ID, []string{"two words"}); !errors.Is(err, ErrValidation) {
		t.Errorf("a bad reader: %v", err)
	}
	noErr(t, anna.Unprotect(page.ID, sec.ID))
	got := mustItem(t)(s.As("ben").Get(page.ID))
	if !strings.Contains(got.Body.Blocks[0].Text, "Paul") || len(got.Secrets) != 0 || slices.ContainsFunc(got.Body.Blocks[0].Marks, func(m Mark) bool { return m.Type == MarkSecret }) {
		t.Fatalf("unprotected: %+v", got.Body.Blocks[0])
	}
	if err := anna.Unprotect(page.ID, sec.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("unprotected twice: %v", err)
	}
	// The history replays to the same passages and requests.
	r, err := Replay(s.Log())
	noErr(t, err)
	if got := mustItem(t)(r.As("vic").Get(page.ID)); !strings.Contains(got.Body.Blocks[0].Text, "Paul") {
		t.Fatal("replayed differently")
	}
}

func TestQuotesOfAPassageAreVeiled(t *testing.T) {
	s, page, _ := secretFixture(t)
	anna := s.As("anna")
	_, err := anna.Comment(page.ID, NewComment{Text: "check", Anchor: &Anchor{Quote: "Paul Muster", Prefix: "Salary of ", Suffix: " is due."}})
	noErr(t, err)
	_, err = anna.Comment(page.ID, NewComment{Text: "fine", Anchor: &Anchor{Quote: "Open to all."}})
	noErr(t, err)
	for who, want := range map[string]string{"hanna": "Paul Muster", "vic": SecretPlaceholder} {
		cs, err := s.As(who).Comments(page.ID)
		noErr(t, err)
		if cs[0].Anchor.Quote != want || cs[1].Anchor.Quote != "Open to all." {
			t.Errorf("%s reads the quotes %+v %+v", who, cs[0].Anchor, cs[1].Anchor)
		}
	}
	if !shares("ary of Paul", "Paul Muster") || shares("Open to all.", "Paul Muster") || shares("", "x") {
		t.Error("shares")
	}
}

func TestVersionsBeforeAPassageAreRestoredWithoutIt(t *testing.T) {
	s, page, sec := secretFixture(t)
	anna := s.As("anna")
	protected := s.History(page.ID)[len(s.History(page.ID))-1].Seq
	noErr(t, anna.Unprotect(page.ID, sec.ID))
	// Restoring the version that had the passage: its protection is gone,
	// so the text reads as any other.
	noErr(t, anna.RestoreBody(page.ID, protected))
	if got := mustItem(t)(s.As("ben").Get(page.ID)); !strings.Contains(got.Body.Blocks[0].Text, "Paul") {
		t.Fatalf("restored: %q", got.Body.Blocks[0].Text)
	}
}

// A passage's text is sealed apart, with its own key: the item's key
// alone does not open it, and destroying the passage's key erases its
// text from every version while the rest stays (ADR-0088).
func TestAPassageIsSealedWithItsOwnKey(t *testing.T) {
	s, page, sec := secretFixture(t)
	var batch []Event
	for _, e := range s.Log() {
		if e.Item == page.ID && e.Kind == BodyCommitted {
			batch = append(batch, e)
		}
	}
	data, err := EncodeBatchSealed(batch, fakeSealer{})
	noErr(t, err)
	// The fake sealer keeps plaintext readable after its prefix: only the
	// passage's own sealing holds its text.
	if n := strings.Count(string(data), "Paul"); n != 0 {
		t.Fatalf("the passage is in the item's sealed content %d times", n)
	}
	if _, err := DecodeBatch(data); err == nil {
		t.Fatal("decoded sealed passages without keys")
	}
	back, err := DecodeBatchSealed(data, fakeSealer{})
	noErr(t, err)
	if got := back[len(back)-1].Body.Blocks[0].Text; got != "Salary of Paul Muster is due." {
		t.Fatalf("decoded %q", got)
	}
	gone, err := DecodeBatchSealed(data, fakeSealer{shredded: map[ID]bool{SecretKey(page.ID, sec.ID): true}})
	noErr(t, err)
	got := gone[len(gone)-1].Body.Blocks[0].Text
	if strings.Contains(got, "Paul") || !strings.HasPrefix(got, "Salary of ") || !strings.HasSuffix(got, " is due.") || gone[len(gone)-1].Erased {
		t.Fatalf("with the passage's key destroyed: %q", got)
	}
	if id, ok := SecretKeyItem(SecretKey(page.ID, sec.ID)); !ok || id != page.ID {
		t.Fatal("SecretKeyItem")
	}
	if _, ok := SecretKeyItem(page.ID); ok {
		t.Fatal("an item's key taken for a passage's")
	}
}
