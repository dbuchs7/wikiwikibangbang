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
