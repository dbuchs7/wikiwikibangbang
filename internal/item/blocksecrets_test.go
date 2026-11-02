package item

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
)

const (
	planV1  = ID("0123456789abcdef0123456789abcd01")
	planV2  = ID("0123456789abcdef0123456789abcd02")
	openPic = ID("0123456789abcdef0123456789abcd03")
)

// blockFixture: anna writes a page whose first section — a heading, a
// paragraph that mentions a task, a picture with an older version, and an
// embed of a decision — she protects for herself and @hr; an open
// paragraph and an open picture follow. ben views PLAT, vic edits it, pat
// administers it, hanna is in @hr.
func blockFixture(t *testing.T) (s *Store, page, task, decision Item, sec Secret) {
	t.Helper()
	s = permFixture(t)
	noErr(t, s.GrantRole("root", "PLAT", "vic", Editor))
	noErr(t, s.AddToGroup("root", "hr", "hanna"))
	noErr(t, s.GrantRole("root", "PLAT", "hanna", Viewer))
	anna := s.As("anna")
	task = mustItem(t)(anna.Create(NewItem{Space: "PLAT", Type: "task", Title: "Pay Paul", Properties: Props{"priority": "low"}}))
	decision = mustItem(t)(anna.Create(NewItem{Space: "PLAT", Type: "page", Title: "Raise"}))
	page = mustItem(t)(anna.Create(NewItem{Space: "PLAT", Type: "page", Title: "Budget"}))
	for _, a := range []Attachment{
		{ID: planV1, Name: "salaries.drawing.png", Type: "image/png", Size: 3, SHA256: strings.Repeat("ab", 32)},
		{ID: planV2, Name: "salaries.drawing.png", Type: "image/png", Size: 3, SHA256: strings.Repeat("cd", 32), Source: &Source{Item: page.ID, Attachment: planV1}},
		{ID: openPic, Name: "office.png", Type: "image/png", Size: 3, SHA256: strings.Repeat("ef", 32)},
	} {
		_, err := anna.AddAttachment(page.ID, a)
		noErr(t, err)
	}
	noErr(t, anna.CommitBody(page.ID, Doc{Blocks: []Block{
		{Kind: Heading, Level: 1, Text: "Salaries"},
		{Kind: Paragraph, Text: "Paul Muster earns 9000, see Pay Paul.", Mentions: []ID{task.ID}, Marks: []Mark{{Type: MarkMention, Start: 28, End: 36, Ref: task.ID}}},
		{Kind: Image, Ref: planV2, Text: "The salary plan"},
		{Kind: Embed, Ref: decision.ID},
		{Kind: Heading, Level: 1, Text: "Office"},
		{Kind: Paragraph, Text: "Open to all."},
		{Kind: Image, Ref: openPic, Text: "The office"},
	}}))
	var err error
	sec, err = anna.ProtectBlocks(page.ID, 0, 3, []string{"@hr"})
	noErr(t, err)
	page = mustItem(t)(anna.Get(page.ID))
	return s, page, task, decision, sec
}

func bodyText(d Doc) string {
	var parts []string
	for _, b := range d.Blocks {
		parts = append(parts, fmt.Sprintf("%s:%s:%s:%s", b.Kind, b.Text, b.Ref, b.Secret))
	}
	return strings.Join(parts, " | ")
}

func TestProtectedBlocksAreReadByTheirReadersOnly(t *testing.T) {
	s, page, _, _, sec := blockFixture(t)
	for _, who := range []string{"anna", "hanna"} {
		got := mustItem(t)(s.As(who).Get(page.ID))
		if len(got.Body.Blocks) != 7 || got.Body.Blocks[2].Ref != planV2 || got.Body.Blocks[0].Secret != sec.ID || got.Body.Blocks[4].Secret != "" {
			t.Errorf("%s reads %s", who, bodyText(got.Body))
		}
		if len(got.Attachments) != 3 {
			t.Errorf("%s has %d files", who, len(got.Attachments))
		}
	}
	for _, who := range []string{"ben", "vic", "pat"} {
		x := s.As(who)
		got := mustItem(t)(x.Get(page.ID))
		// One placeholder for the four blocks: neither their number nor
		// their kinds show.
		want := "paragraph:" + SecretPlaceholder + "::" + string(sec.ID) + " | heading:Office:: | paragraph:Open to all.:: | image:The office:" + string(openPic) + ":"
		if bodyText(got.Body) != want {
			t.Fatalf("%s reads %s", who, bodyText(got.Body))
		}
		if len(got.Attachments) != 1 || got.Attachments[0].ID != openPic {
			t.Errorf("%s counts the files %+v", who, got.Attachments)
		}
		views := mustSecrets(t, x, page.ID)
		if len(views) != 1 || views[0].Readable || !views[0].Blocks || (views[0].Readers != nil) != views[0].Decides {
			t.Errorf("%s sees the protection %+v", who, views)
		}
		if hits, _ := x.Search("Muster", Query{}); len(hits) != 0 {
			t.Errorf("%s finds the protected text: %+v", who, hits)
		}
		if hits, _ := x.Search("salary plan", Query{}); len(hits) != 0 {
			t.Errorf("%s finds the protected caption: %+v", who, hits)
		}
		listed, _, _, _ := x.FindPage(Query{Space: "PLAT"}, "", 50)
		for _, it := range listed {
			if strings.Contains(bodyText(it.Body), "Muster") || strings.Contains(bodyText(it.Body), string(planV2)) {
				t.Errorf("%s lists %s", who, bodyText(it.Body))
			}
		}
		if ok := x.ReadsAllSecrets(page.ID); ok {
			t.Errorf("%s reads every protection", who)
		}
	}
	if hits, _ := s.As("hanna").Search("Muster", Query{}); len(hits) != 1 {
		t.Errorf("a reader does not find it: %+v", hits)
	}
}

func mustSecrets(t *testing.T, x *Session, id ID) []SecretView {
	t.Helper()
	v, err := x.Secrets(id)
	noErr(t, err)
	return v
}

func TestTheFilesOfProtectedBlocksAreGivenToTheirReadersOnly(t *testing.T) {
	s, page, _, _, _ := blockFixture(t)
	ben := s.As("ben")
	// The file the block shows and the older version of its stack.
	for _, f := range []ID{planV1, planV2} {
		if _, err := ben.Attachment(page.ID, f); !errors.Is(err, ErrNotFound) {
			t.Errorf("ben gets the protected file %s: %v", f, err)
		}
		if _, _, err := ben.FileOrigin(page.ID, f); !errors.Is(err, ErrNotFound) {
			t.Errorf("ben learns the origin of %s: %v", f, err)
		}
		if _, err := ben.PlanRetention(page.ID, f, nil, Retention{}, s.now()); !errors.Is(err, ErrNotFound) {
			t.Errorf("ben plans the versions of %s: %v", f, err)
		}
		if _, err := s.As("hanna").Attachment(page.ID, f); err != nil {
			t.Errorf("hanna does not get %s: %v", f, err)
		}
	}
	if err := s.As("vic").DeleteAttachment(page.ID, planV1); !errors.Is(err, ErrNotFound) {
		t.Errorf("vic deletes a file he may not see: %v", err)
	}
	if err := s.As("vic").SetPictureLabels(page.ID, planV2, []string{"x"}); !errors.Is(err, ErrNotFound) {
		t.Errorf("vic labels a file he may not see: %v", err)
	}
	files, _ := ben.Attachments(page.ID)
	if len(files) != 1 || files[0].ID != openPic {
		t.Errorf("ben lists %+v", files)
	}
	for _, p := range ben.Pictures("", IsPicture) {
		if p.Item.ID == page.ID && p.Attachment.ID != openPic {
			t.Errorf("the album shows ben %s", p.Attachment.ID)
		}
	}
	if n := len(s.As("anna").Pictures("", func(a Attachment) bool { return true })); n != 3 {
		t.Errorf("the album shows anna %d files", n)
	}
	// A copy into another page would leave the protection: refused, even
	// to a reader; one who may not read it learns nothing.
	other := mustItem(t)(s.As("anna").Create(NewItem{Space: "PLAT", Type: "page", Title: "Open page"}))
	copyOf := Attachment{ID: "fedcba9876543210fedcba9876543210", Name: "c.png", Type: "image/png", Size: 3, SHA256: strings.Repeat("cd", 32),
		Source: &Source{Item: page.ID, Attachment: planV2}}
	if _, err := s.As("anna").AddAttachment(other.ID, copyOf); !errors.Is(err, ErrForbidden) {
		t.Errorf("a protected picture is copied out of its protection: %v", err)
	}
	if _, err := s.As("vic").AddAttachment(other.ID, copyOf); !errors.Is(err, ErrNotFound) {
		t.Errorf("vic copies a picture he may not see: %v", err)
	}
	// A new version in the same item stays in the stack, so protected.
	next := Attachment{ID: "0123456789abcdef0123456789abcd04", Name: "salaries.drawing.png", Type: "image/png", Size: 3, SHA256: strings.Repeat("aa", 32),
		Source: &Source{Item: page.ID, Attachment: planV2}}
	_, err := s.As("anna").AddAttachment(page.ID, next)
	noErr(t, err)
	if _, err := ben.Attachment(page.ID, next.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("a new version of a protected drawing is open: %v", err)
	}
}

func TestAFileIsProtectedInOnePlaceOnly(t *testing.T) {
	s, page, _, _, sec := blockFixture(t)
	anna := s.As("anna")
	body := mustItem(t)(anna.Get(page.ID)).Body
	// The protected picture, or its older version, shown again in an open
	// block: refused.
	for _, f := range []ID{planV2, planV1} {
		d := body.clone()
		d.Blocks = append(d.Blocks, Block{Kind: Image, Ref: f})
		if err := anna.CommitBody(page.ID, d); !errors.Is(err, ErrValidation) {
			t.Errorf("an open block shows the protected file %s: %v", f, err)
		}
	}
	// The open picture shown again in a second place: protecting one of
	// them is refused, as the other would still show it.
	d := body.clone()
	d.Blocks = append(d.Blocks, Block{Kind: Image, Ref: openPic})
	noErr(t, anna.CommitBody(page.ID, d))
	if _, err := anna.ProtectBlocks(page.ID, 6, 6, nil); !errors.Is(err, ErrValidation) {
		t.Errorf("protected while another block shows the file: %v", err)
	}
	// The protected blocks may be moved, and the picture may leave the
	// text: the file stays protected until the protection is taken off.
	d = mustItem(t)(anna.Get(page.ID)).Body
	d.Blocks = slices.Delete(d.Blocks, 2, 3)
	noErr(t, anna.CommitBody(page.ID, d))
	if _, err := s.As("ben").Attachment(page.ID, planV2); !errors.Is(err, ErrNotFound) {
		t.Errorf("the picture left the text and its file is open: %v", err)
	}
	if !mustSecrets(t, anna, page.ID)[0].Blocks {
		t.Errorf("the protection no longer covers blocks")
	}
	// A passage of a protected block, or a protected block over one, is
	// refused: a protection covers passages or blocks.
	if _, err := anna.Protect(page.ID, 1, 0, 4, nil); !errors.Is(err, ErrValidation) {
		t.Errorf("a passage in a protected block: %v", err)
	}
	if _, err := anna.Protect(page.ID, 4, 0, 4, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := anna.ProtectBlocks(page.ID, 4, 4, nil); !errors.Is(err, ErrValidation) {
		t.Errorf("a block with a passage protected whole: %v", err)
	}
	if _, err := anna.ProtectBlocks(page.ID, 0, 0, nil); !errors.Is(err, ErrValidation) {
		t.Errorf("a block protected twice: %v", err)
	}
	if _, err := anna.ProtectBlocks(page.ID, 3, 99, nil); !errors.Is(err, ErrValidation) {
		t.Errorf("blocks out of range: %v", err)
	}
	d = mustItem(t)(anna.Get(page.ID)).Body
	d.Blocks[5].Secret = "nope"
	if err := anna.CommitBody(page.ID, d); !errors.Is(err, ErrValidation) {
		t.Errorf("a block names an unknown protection: %v", err)
	}
	d.Blocks[5].Secret = mustSecrets(t, anna, page.ID)[1].ID // a passage's
	if err := anna.CommitBody(page.ID, d); !errors.Is(err, ErrValidation) {
		t.Errorf("a passage's protection over a block: %v", err)
	}
	_ = sec
}

func TestProtectedBlocksHideWhatTheyRelateTo(t *testing.T) {
	s, page, task, decision, _ := blockFixture(t)
	ben, hanna := s.As("ben"), s.As("hanna")
	for _, c := range []struct {
		who  *Session
		want bool
	}{{ben, false}, {hanna, true}} {
		in, _ := c.who.Relations(task.ID, Incoming)
		if got := len(in) == 1; got != c.want {
			t.Errorf("incoming relations of the task %+v", in)
		}
		out, _ := c.who.Relations(page.ID, Outgoing)
		if got := len(out) == 2; got != c.want {
			t.Errorf("outgoing relations of the page %+v", out)
		}
		embeds, _ := c.who.Embeds(page.ID)
		if got := len(embeds) == 1; got != c.want {
			t.Errorf("embeds %+v", embeds)
		}
		m, err := c.who.Starmap("PLAT", 600)
		noErr(t, err)
		var edges, pics int
		for _, e := range m.Edges {
			if e.From == string(page.ID) && (e.Kind == RelMentions || e.Kind == RelEmbeds) {
				edges++
			}
		}
		for _, n := range m.Nodes {
			if n.Kind == StarPicture && n.Item == page.ID {
				pics++
			}
		}
		if (edges == 2) != c.want || (pics == 2) != c.want || (!c.want && pics != 1) {
			t.Errorf("the starmap holds %d edges and %d pictures of the page (reader %v)", edges, pics, c.want)
		}
		imp, err := c.who.Impact(string(decision.ID), ImpactAffected, 1, 100)
		noErr(t, err)
		reached := slices.ContainsFunc(imp.Nodes, func(n ImpactNode) bool { return n.ID == string(page.ID) })
		if reached != c.want {
			t.Errorf("impact of the decision reaches the page: %v", reached)
		}
	}
	if _, err := ben.Impact("picture:"+string(page.ID)+"/"+string(planV2), ImpactDependsOn, 1, 100); err == nil {
		t.Errorf("ben starts an impact at the protected picture")
	}
	// Trashing the task is refused while the page mentions it; ben's
	// colleague vic is not told which page, as he may not read the
	// mention.
	err := s.As("vic").Trash(task.ID)
	if !errors.Is(err, ErrReferenced) || strings.Contains(err.Error(), page.Key) {
		t.Errorf("vic is told %v", err)
	}
	if err := s.As("anna").Trash(task.ID); err == nil || !strings.Contains(err.Error(), page.Key) {
		t.Errorf("anna is not told %v", err)
	}
}

func TestOnlyThoseWhoReadEveryProtectedBlockChangeTheText(t *testing.T) {
	s, page, _, _, sec := blockFixture(t)
	vic := s.As("vic")
	veiled := mustItem(t)(vic.Get(page.ID))
	if err := vic.CommitBody(page.ID, veiled.Body); !errors.Is(err, ErrForbidden) {
		t.Fatalf("vic commits the body: %v", err)
	}
	if _, err := vic.ProtectBlocks(page.ID, 5, 5, nil); !errors.Is(err, ErrForbidden) {
		t.Fatalf("vic protects: %v", err)
	}
	if _, err := vic.BodyAt(page.ID, 1); !errors.Is(err, ErrForbidden) {
		t.Fatalf("vic reads earlier versions: %v", err)
	}
	if err := vic.CopySecrets(page.ID, page.ID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("vic copies: %v", err)
	}
	// Title, labels and comments stay his to change.
	noErr(t, vic.SetTitle(page.ID, "Budget 2027"))
	noErr(t, vic.SetLabels(page.ID, []string{"budget"}))
	// He asks; anna grants; he reads it and may change the text.
	noErr(t, vic.RequestSecret(page.ID, sec.ID))
	noErr(t, s.As("anna").DecideSecret(page.ID, sec.ID, "vic", true))
	got := mustItem(t)(vic.Get(page.ID))
	if got.Body.Blocks[1].Text != "Paul Muster earns 9000, see Pay Paul." {
		t.Fatalf("granted, vic reads %s", bodyText(got.Body))
	}
	if _, err := vic.Attachment(page.ID, planV2); err != nil {
		t.Fatalf("granted, vic does not get the picture: %v", err)
	}
	noErr(t, vic.CommitBody(page.ID, got.Body))
}

func TestCommentsAndFindingsDoNotQuoteProtectedBlocks(t *testing.T) {
	s, page, _, _, _ := blockFixture(t)
	c, err := s.As("anna").Comment(page.ID, NewComment{Text: "Too much?", Anchor: &Anchor{Quote: "earns 9000", Prefix: "Paul Muster "}})
	noErr(t, err)
	list, err := s.As("ben").Comments(page.ID)
	noErr(t, err)
	for _, got := range list {
		if got.ID == c.ID && (got.Anchor.Quote != SecretPlaceholder || strings.Contains(got.Anchor.Prefix, "Muster")) {
			t.Errorf("ben reads the quote %+v", got.Anchor)
		}
	}
	// A mention of @ben in a protected block tells him nothing.
	body := mustItem(t)(s.As("anna").Get(page.ID)).Body
	body.Blocks[0].Text = "Salaries for @ben"
	noErr(t, s.As("anna").CommitBody(page.ID, body))
	if bodyMentions(body, "ben") {
		t.Errorf("a protected block mentions ben")
	}
}

func TestUnprotectingBlocksOpensThemAndTheirFiles(t *testing.T) {
	s, page, task, _, sec := blockFixture(t)
	if err := s.As("vic").Unprotect(page.ID, sec.ID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("vic takes the protection off: %v", err)
	}
	noErr(t, s.As("pat").Unprotect(page.ID, sec.ID))
	got := mustItem(t)(s.As("ben").Get(page.ID))
	if len(got.Body.Blocks) != 7 || got.Body.Blocks[0].Secret != "" || got.Body.Blocks[1].Text != "Paul Muster earns 9000, see Pay Paul." {
		t.Fatalf("after unprotecting ben reads %s", bodyText(got.Body))
	}
	if _, err := s.As("ben").Attachment(page.ID, planV1); err != nil {
		t.Errorf("the file stays closed: %v", err)
	}
	if in, _ := s.As("ben").Relations(task.ID, Incoming); len(in) != 1 {
		t.Errorf("the mention stays hidden: %+v", in)
	}
}

func TestProtectedBlocksAreErasedWithTheirFiles(t *testing.T) {
	s, page, task, _, sec := blockFixture(t)
	_, err := s.As("anna").Comment(page.ID, NewComment{Text: "Too much?", Anchor: &Anchor{Quote: "earns 9000"}})
	noErr(t, err)
	if err := s.As("vic").EraseSecret(page.ID, sec.ID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("vic erases: %v", err)
	}
	noErr(t, s.As("pat").EraseSecret(page.ID, sec.ID))
	got := mustItem(t)(s.As("anna").Get(page.ID))
	if bodyText(got.Body) != "heading:Office:: | paragraph:Open to all.:: | image:The office:"+string(openPic)+":" || len(got.Secrets) != 0 {
		t.Fatalf("after erasure anna reads %s", bodyText(got.Body))
	}
	for _, f := range []ID{planV1, planV2} {
		if _, err := s.As("anna").Attachment(page.ID, f); !errors.Is(err, ErrNotFound) {
			t.Errorf("the protected file %s survives its erasure: %v", f, err)
		}
	}
	if in, _ := s.As("anna").Relations(task.ID, Incoming); len(in) != 0 {
		t.Errorf("the mention survives: %+v", in)
	}
	list, _ := s.As("anna").Comments(page.ID)
	if list[0].Anchor.Quote != SecretPlaceholder {
		t.Errorf("the comment still quotes %q", list[0].Anchor.Quote)
	}
	// The versions since the protection hold the placeholder; those before
	// are withheld.
	for _, e := range s.History(page.ID) {
		if e.Kind != BodyCommitted || e.Item != page.ID {
			continue
		}
		v, err := s.As("anna").BodyAt(page.ID, e.Seq)
		if err != nil {
			continue
		}
		if t2 := bodyText(*v.Body); strings.Contains(t2, "Muster") || strings.Contains(t2, string(planV2)) {
			t.Errorf("version %d holds %s", e.Seq, t2)
		}
	}
	// A body cannot name an erased protection again.
	d := got.Body.clone()
	d.Blocks[0].Secret = sec.ID
	if err := s.As("anna").CommitBody(page.ID, d); !errors.Is(err, ErrValidation) {
		t.Errorf("an erased protection is named again: %v", err)
	}
}

func TestACopyOfAnItemKeepsItsProtectedBlocks(t *testing.T) {
	s, page, _, _, sec := blockFixture(t)
	anna := s.As("anna")
	cp := mustItem(t)(anna.Create(NewItem{Space: "PLAT", Type: "page", Title: "Budget copy"}))
	noErr(t, anna.CopySecrets(page.ID, cp.ID))
	// The copied file is protected from its first moment, before the body
	// shows it.
	c := Attachment{ID: "fedcba9876543210fedcba9876543211", Name: "salaries.drawing.png", Type: "image/png", Size: 3, SHA256: strings.Repeat("cd", 32),
		Source: &Source{Item: page.ID, Attachment: planV2}}
	_, err := anna.AddAttachment(cp.ID, c)
	noErr(t, err)
	if _, err := s.As("ben").Attachment(cp.ID, c.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("the copied file is open before its body: %v", err)
	}
	body := mustItem(t)(anna.Get(page.ID)).Body
	body.Blocks[2].Ref = c.ID
	body.Blocks = slices.Delete(body.Blocks, 6, 7) // the open picture was not copied
	noErr(t, anna.CommitBody(cp.ID, body))
	got := mustItem(t)(s.As("ben").Get(cp.ID))
	if got.Body.Blocks[0].Secret != sec.ID || got.Body.Blocks[0].Text != SecretPlaceholder {
		t.Fatalf("ben reads the copy %s", bodyText(got.Body))
	}
}

func TestBlockSecretsSurviveTheJournalAndErasureByKey(t *testing.T) {
	log := secretBlocksLog(t)
	// Sealed with the protection's own key: the body sealed with the
	// item's key holds nothing of the blocks.
	var seen bool
	for _, e := range log {
		data, err := EncodeBatchSealed([]Event{e}, fakeSealer{})
		noErr(t, err)
		if e.Kind == BodyCommitted && e.Body != nil && slices.ContainsFunc(e.Body.Blocks, func(b Block) bool { return b.Secret != "" }) {
			seen = true
			if !strings.Contains(string(data), `"secretBlocks"`) {
				t.Errorf("no sealed blocks: %s", data)
			}
			itemPart := string(data)[:strings.Index(string(data), `"secretBlocks"`)]
			if strings.Contains(itemPart, "Salaries") || strings.Contains(itemPart, "plan.png") || strings.Contains(itemPart, "0123456789abcdef0123456789abcdee") {
				t.Errorf("the item's part holds protected blocks: %s", itemPart)
			}
		}
	}
	if !seen {
		t.Fatal("the log protects no blocks")
	}
	// Destroying the protection's key leaves stand-ins that name only
	// the protection.
	var sec ID
	for _, e := range log {
		if e.Kind == SecretSet && sec == "" {
			sec = e.Secret.ID
		}
	}
	for _, e := range log {
		if e.Kind != BodyCommitted || e.Body == nil || !slices.ContainsFunc(e.Body.Blocks, func(b Block) bool { return b.Secret == sec }) {
			continue
		}
		data, err := EncodeBatchSealed([]Event{e}, fakeSealer{})
		noErr(t, err)
		back, err := DecodeBatchSealed(data, fakeSealer{shredded: map[ID]bool{SecretKey(e.Item, sec): true}})
		noErr(t, err)
		for _, b := range back[0].Body.Blocks {
			if b.Secret == sec && (b.Text != "" || b.Ref != "" || b.Kind != Paragraph) {
				t.Errorf("a block of the erased protection holds %+v", b)
			}
		}
		if !strings.Contains(bodyText(*back[0].Body), "Open to all.") {
			t.Errorf("the rest of the body is gone: %s", bodyText(*back[0].Body))
		}
		if veiled := veil(*back[0].Body, map[ID]bool{sec: true}); veiled.Blocks[0].Text != SecretPlaceholder {
			t.Errorf("an erased block reads %s", bodyText(veiled))
		}
	}
}
