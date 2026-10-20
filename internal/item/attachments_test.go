package item

import (
	"errors"
	"strings"
	"testing"
)

func testFile(id, name string) Attachment {
	return Attachment{ID: ID(id), Name: name, Type: "image/png", Size: 3, SHA256: strings.Repeat("ab", 32)}
}

const (
	fileA = "0123456789abcdef0123456789abcdef"
	fileB = "fedcba9876543210fedcba9876543210"
)

func TestAttachments(t *testing.T) {
	s := permFixture(t)
	anna, ben, carl := s.As("anna"), s.As("ben"), s.As("carl")
	page := mustItem(t)(anna.Create(NewItem{Space: "PLAT", Type: "page", Title: "Runbook"}))

	a, err := anna.AddAttachment(page.ID, testFile(fileA, "diagram.png"))
	noErr(t, err)
	if a.Author != "anna" || a.Created == 0 || a.Name != "diagram.png" {
		t.Fatalf("attachment %+v", a)
	}
	bad := map[string]Attachment{
		"bad id":        testFile("../etc", "x.png"),
		"empty name":    testFile(fileB, " "),
		"slash":         testFile(fileB, "a/b.png"),
		"control":       testFile(fileB, "a\nb.png"),
		"no hash":       {ID: fileB, Name: "x", Type: "text/plain"},
		"duplicate id":  testFile(fileA, "again.png"),
		"long name":     testFile(fileB, strings.Repeat("x", 256)),
		"negative size": {ID: fileB, Name: "x", Type: "text/plain", Size: -1, SHA256: strings.Repeat("ab", 32)},
	}
	for name, f := range bad {
		if _, err := anna.AddAttachment(page.ID, f); !errors.Is(err, ErrValidation) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := ben.AddAttachment(page.ID, testFile(fileB, "x.png")); !errors.Is(err, ErrForbidden) {
		t.Errorf("a viewer attaches: %v", err)
	}
	if err := ben.CanAttach(page.ID); !errors.Is(err, ErrForbidden) {
		t.Errorf("a viewer may attach: %v", err)
	}
	if got, err := ben.Attachments(page.ID); err != nil || len(got) != 1 {
		t.Errorf("a viewer lists %v, %v", got, err)
	}
	if _, err := carl.Attachment(page.ID, fileA); !errors.Is(err, ErrNotFound) {
		t.Errorf("an outsider reads an attachment: %v", err)
	}

	// An image shows an attachment of the item, not of another.
	noErr(t, anna.CommitBody(page.ID, Doc{Blocks: []Block{{Kind: Image, Ref: fileA, Text: "The flow"}}}))
	other := mustItem(t)(anna.Create(NewItem{Space: "PLAT", Type: "page", Title: "Other"}))
	if err := anna.CommitBody(other.ID, Doc{Blocks: []Block{{Kind: Image, Ref: fileA}}}); !errors.Is(err, ErrNotFound) {
		t.Errorf("an image of another item's file: %v", err)
	}
	if err := anna.CommitBody(page.ID, Doc{Blocks: []Block{{Kind: Image, Ref: fileA, Marks: []Mark{{Type: MarkBold, Start: 0, End: 1}}, Text: "x"}}}); !errors.Is(err, ErrValidation) {
		t.Errorf("a formatted caption: %v", err)
	}

	// Deleting hides the file; the body that shows it stays editable.
	noErr(t, anna.DeleteAttachment(page.ID, fileA))
	if _, err := ben.Attachment(page.ID, fileA); !errors.Is(err, ErrNotFound) {
		t.Errorf("a deleted file is still served: %v", err)
	}
	if err := anna.DeleteAttachment(page.ID, fileA); !errors.Is(err, ErrNotFound) {
		t.Errorf("deleted twice: %v", err)
	}
	noErr(t, anna.CommitBody(page.ID, Doc{Blocks: []Block{{Kind: Image, Ref: fileA, Text: "kept"}}}))
	if err := anna.CommitBody(other.ID, Doc{Blocks: []Block{{Kind: Paragraph, Text: "x"}}}); err != nil {
		t.Fatal(err)
	}
	if live := s.LiveAttachments(); len(live[page.ID]) != 0 {
		t.Errorf("live attachments %v", live)
	}

	// Purging erases the description from the item and its history.
	_, err = anna.AddAttachment(page.ID, testFile(fileB, "secret.pdf"))
	noErr(t, err)
	noErr(t, anna.Trash(page.ID))
	noErr(t, s.Purge("pat", page.ID))
	if got := mustItem(t)(s.Get(page.ID)).Attachments; got != nil {
		t.Errorf("a purged item keeps %v", got)
	}
	for _, e := range s.History(page.ID) {
		if e.Kind == AttachmentAdded && e.Attachment != nil {
			t.Errorf("history keeps %+v", e.Attachment)
		}
	}
}

// TestPicturesListOnlyReadableFiles: the album (ADR-0054) lists the files
// of live items one may read, newest first, and nothing else.
func TestPicturesListOnlyReadableFiles(t *testing.T) {
	s := permFixture(t)
	anna, ben, carl, pat := s.As("anna"), s.As("ben"), s.As("carl"), s.As("pat")
	open := mustItem(t)(anna.Create(NewItem{Space: "PLAT", Type: "page", Title: "Open"}))
	secret := mustItem(t)(pat.Create(NewItem{Space: "PLAT", Type: "page", Title: "Secret"}))
	gone := mustItem(t)(anna.Create(NewItem{Space: "PLAT", Type: "page", Title: "Gone"}))
	_, err := anna.AddAttachment(open.ID, testFile(fileA, "a.png"))
	noErr(t, err)
	notes := testFile("00000000000000000000000000000001", "notes.txt")
	notes.Type = "text/plain"
	_, err = anna.AddAttachment(open.ID, notes)
	noErr(t, err)
	_, err = pat.AddAttachment(secret.ID, testFile(fileB, "b.png"))
	noErr(t, err)
	noErr(t, pat.Restrict(secret.ID, Restriction{Read: []string{"pat"}}))
	_, err = anna.AddAttachment(gone.ID, testFile("00000000000000000000000000000002", "c.png"))
	noErr(t, err)
	noErr(t, anna.Trash(gone.ID))

	images := func(a Attachment) bool { return a.Type == "image/png" }
	names := func(ps []Picture) (out []string) {
		for _, p := range ps {
			out = append(out, p.Item.Key+"/"+p.Attachment.Name)
		}
		return out
	}
	if got := names(ben.Pictures("", images)); len(got) != 1 || got[0] != open.Key+"/a.png" {
		t.Errorf("a viewer sees %v", got)
	}
	if got := names(pat.Pictures("PLAT", images)); len(got) != 2 {
		t.Errorf("the one who may read the restricted page sees %v", got)
	}
	if got := pat.Pictures("OPS", images); len(got) != 0 {
		t.Errorf("another space shows %v", names(got))
	}
	if got := carl.Pictures("", images); len(got) != 0 {
		t.Errorf("an outsider sees %v", names(got))
	}
}

// TestCopiesKnowTheirOrigin: a copy names its source, must have its
// content, and learns of a newer version of it; a reader who may not read
// the source learns nothing of it (ADR-0055).
func TestCopiesKnowTheirOrigin(t *testing.T) {
	s := permFixture(t)
	anna, ben, pat := s.As("anna"), s.As("ben"), s.As("pat")
	plans := mustItem(t)(anna.Create(NewItem{Space: "PLAT", Type: "page", Title: "Plans"}))
	runbook := mustItem(t)(anna.Create(NewItem{Space: "PLAT", Type: "page", Title: "Runbook"}))
	const v1, v2, c1, c2 = "0123456789abcdef0123456789abcdef", "fedcba9876543210fedcba9876543210", "00000000000000000000000000000001", "00000000000000000000000000000002"
	_, err := anna.AddAttachment(plans.ID, testFile(v1, "plan.drawing.png"))
	noErr(t, err)

	forged := testFile(c1, "plan.drawing.png")
	forged.SHA256 = strings.Repeat("cd", 32)
	forged.Source = &Source{Item: plans.ID, Attachment: v1}
	if _, err := anna.AddAttachment(runbook.ID, forged); !errors.Is(err, ErrValidation) {
		t.Errorf("a copy with other content: %v", err)
	}
	copied := testFile(c1, "plan.drawing.png")
	copied.Source = &Source{Item: plans.ID, Attachment: v1}
	_, err = anna.AddAttachment(runbook.ID, copied)
	noErr(t, err)
	if o, ok, err := anna.FileOrigin(runbook.ID, c1); err != nil || !ok || !o.Readable || o.Item.Key != plans.Key || o.Latest != nil {
		t.Fatalf("origin of a fresh copy: %+v %v %v", o, ok, err)
	}
	if _, ok, _ := anna.FileOrigin(plans.ID, v1); ok {
		t.Error("an original is no copy")
	}

	// The original is drawn again: the copy learns of the newer version,
	// even once the version it was copied from is deleted.
	next := testFile(v2, "plan.drawing.png")
	next.SHA256 = strings.Repeat("ef", 32)
	next.Source = &Source{Item: plans.ID, Attachment: v1}
	_, err = anna.AddAttachment(plans.ID, next)
	noErr(t, err)
	noErr(t, anna.DeleteAttachment(plans.ID, v1))
	if o, _, _ := ben.FileOrigin(runbook.ID, c1); o.Latest == nil || o.Latest.ID != v2 {
		t.Fatalf("a newer original: %+v", o)
	}
	// Taking it over: a copy of the newer version, which leads back to it.
	update := testFile(c2, "plan.drawing.png")
	update.SHA256 = next.SHA256
	update.Source = &Source{Item: plans.ID, Attachment: v2}
	_, err = anna.AddAttachment(runbook.ID, update)
	noErr(t, err)
	if o, _, _ := ben.FileOrigin(runbook.ID, c2); o.Latest != nil {
		t.Errorf("the taken-over copy is current: %+v", o)
	}
	if _, err := anna.AddAttachment(runbook.ID, Attachment{ID: "00000000000000000000000000000003", Name: "x.png", Type: "image/png", Size: 3,
		SHA256: next.SHA256, Source: &Source{Item: plans.ID, Attachment: v1}}); !errors.Is(err, ErrNotFound) {
		t.Errorf("a copy of a deleted file: %v", err)
	}

	// Restricted to pat, the source is not told to ben, and pat may not
	// copy what does not exist to him.
	noErr(t, pat.Restrict(plans.ID, Restriction{Read: []string{"pat"}}))
	if o, ok, err := ben.FileOrigin(runbook.ID, c2); err != nil || !ok || o.Readable || o.Item.Key != "" {
		t.Errorf("an unreadable source: %+v %v %v", o, ok, err)
	}
	stolen := testFile("00000000000000000000000000000004", "x.png")
	stolen.SHA256 = next.SHA256
	stolen.Source = &Source{Item: plans.ID, Attachment: v2}
	if _, err := anna.AddAttachment(runbook.ID, stolen); !errors.Is(err, ErrNotFound) {
		t.Errorf("a copy from an unreadable item: %v", err)
	}
}

// TestImagesHaveAWidth: an image may be shown at 10 to 100 percent of the
// column; no other block has a width (ADR-0056).
func TestImagesHaveAWidth(t *testing.T) {
	const ref = "0123456789abcdef0123456789abcdef"
	for _, ok := range []Block{{Kind: Image, Ref: ref}, {Kind: Image, Ref: ref, Width: 10}, {Kind: Image, Ref: ref, Width: 100}} {
		if err := CheckBlock(ok, "self"); err != nil {
			t.Errorf("%+v: %v", ok, err)
		}
	}
	for _, bad := range []Block{{Kind: Image, Ref: ref, Width: 5}, {Kind: Image, Ref: ref, Width: 101}, {Kind: Image, Ref: ref, Width: -1}, {Kind: Paragraph, Text: "x", Width: 50}} {
		if err := CheckBlock(bad, "self"); !errors.Is(err, ErrValidation) {
			t.Errorf("%+v: %v", bad, err)
		}
	}
}
