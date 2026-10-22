package item

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

const albumPic = "0123456789abcdef0123456789abcdef"

func addPicture(t *testing.T, x *Session, id ID, aid ID, name string) {
	t.Helper()
	_, err := x.AddAttachment(id, Attachment{ID: aid, Name: name, Type: "image/png", Size: 3, SHA256: strings.Repeat("ab", 32)})
	noErr(t, err)
}

// Folders of the album are items: made where one may create, nested by
// the parent, read as their space allows (ADR-0090).
func TestFoldersOfTheAlbum(t *testing.T) {
	s := permFixture(t)
	anna, ben := s.As("anna"), s.As("ben")
	if _, err := ben.CreateFolder("PLAT", "", "Logos"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("a viewer made a folder: %v", err)
	}
	logos := mustItem(t)(anna.CreateFolder("PLAT", "", "Logos"))
	brand := mustItem(t)(anna.CreateFolder("", logos.ID, "Brand"))
	mustItem(t)(anna.CreateFolder("PLAT", "", "archive"))
	if brand.Space != "PLAT" || brand.Parent != logos.ID || logos.Type != FolderType {
		t.Fatalf("folders %+v %+v", logos, brand)
	}
	if got := ben.Folders("PLAT", ""); len(got) != 2 || got[0].Title != "archive" || got[1].Title != "Logos" {
		t.Errorf("top folders %+v", got)
	}
	if got := ben.Folders("", logos.ID); len(got) != 1 || got[0].ID != brand.ID {
		t.Errorf("sub folders %+v", got)
	}
	if got := s.As("carl").Folders("", ""); len(got) != 0 {
		t.Errorf("one without a role sees %+v", got)
	}
	page := mustItem(t)(anna.Create(NewItem{Space: "PLAT", Type: "page", Title: "Runbook"}))
	if _, err := anna.CreateFolder("", page.ID, "x"); !errors.Is(err, ErrValidation) {
		t.Errorf("a folder in a page: %v", err)
	}
	if _, err := ben.CreateFolder("", logos.ID, "x"); !errors.Is(err, ErrForbidden) {
		t.Errorf("a viewer made a subfolder: %v", err)
	}
}

func TestAFolderTypeOfAnotherShapeIsNotTaken(t *testing.T) {
	s := permFixture(t)
	noErr(t, s.DefineType("root", TypeDef{Name: FolderType, Body: true}))
	if _, err := s.As("anna").CreateFolder("PLAT", "", "Logos"); !errors.Is(err, ErrValidation) {
		t.Fatalf("took a type of another shape: %v", err)
	}
}

// A picture's labels are its own: they are not the item's, so no kanban
// or label board shows them (ADR-0090).
func TestPicturesAreLabelled(t *testing.T) {
	s := permFixture(t)
	anna, ben := s.As("anna"), s.As("ben")
	folder := mustItem(t)(anna.CreateFolder("PLAT", "", "Logos"))
	addPicture(t, anna, folder.ID, albumPic, "logo.png")
	noErr(t, anna.SetPictureLabels(folder.ID, albumPic, []string{"Print", "brand", "print"}))
	noErr(t, anna.SetPictureLabels(folder.ID, albumPic, []string{"brand", "print"})) // no change
	if err := ben.SetPictureLabels(folder.ID, albumPic, []string{"x"}); !errors.Is(err, ErrForbidden) {
		t.Errorf("a viewer labelled: %v", err)
	}
	if err := anna.SetPictureLabels(folder.ID, albumPic, []string{"no good!"}); !errors.Is(err, ErrValidation) {
		t.Errorf("a bad label: %v", err)
	}
	if err := anna.SetPictureLabels(folder.ID, "fedcba9876543210fedcba9876543210", nil); !errors.Is(err, ErrNotFound) {
		t.Errorf("an unknown picture: %v", err)
	}
	many := make([]string, maxPictureLabels+1)
	for i := range many {
		many[i] = "l" + strings.Repeat("x", i+1)
	}
	if err := anna.SetPictureLabels(folder.ID, albumPic, many); !errors.Is(err, ErrValidation) {
		t.Errorf("too many labels: %v", err)
	}
	got := mustItem(t)(ben.Get(folder.ID))
	if !slices.Equal(got.Attachments[0].Labels, []string{"brand", "print"}) || len(got.Labels) != 0 {
		t.Fatalf("labels %v, item labels %v", got.Attachments[0].Labels, got.Labels)
	}
	if labels := ben.Labels(""); len(labels) != 0 {
		t.Errorf("a picture's label counts as an item's: %v", labels)
	}
	re, err := Replay(s.Log())
	noErr(t, err)
	if r := mustItem(t)(re.As("ben").Get(folder.ID)); !slices.Equal(r.Attachments[0].Labels, []string{"brand", "print"}) {
		t.Errorf("replayed %v", r.Attachments[0].Labels)
	}
	noErr(t, anna.DeleteAttachment(folder.ID, albumPic))
	if err := anna.SetPictureLabels(folder.ID, albumPic, []string{"x"}); !errors.Is(err, ErrNotFound) {
		t.Errorf("labelled a deleted picture: %v", err)
	}
}

// The album tells whether a picture is in its item's text and whether the
// reader may change it.
func TestPicturesSayWhetherTheyAreUsed(t *testing.T) {
	s := permFixture(t)
	anna := s.As("anna")
	page := mustItem(t)(anna.Create(NewItem{Space: "PLAT", Type: "page", Title: "Plans"}))
	addPicture(t, anna, page.ID, albumPic, "used.png")
	const spare = "fedcba9876543210fedcba9876543210"
	addPicture(t, anna, page.ID, spare, "spare.png")
	noErr(t, anna.CommitBody(page.ID, Doc{Blocks: []Block{{Kind: Image, Ref: albumPic, Text: "Plan"}}}))
	all := func(Attachment) bool { return true }
	for who, editable := range map[string]bool{"anna": true, "ben": false} {
		pics := s.As(who).Pictures("PLAT", all)
		if len(pics) != 2 {
			t.Fatalf("%s sees %d pictures", who, len(pics))
		}
		for _, p := range pics {
			if p.Used != (p.Attachment.ID == albumPic) || p.Editable != editable || p.Item.Parent != "" {
				t.Errorf("%s: %s used %v editable %v", who, p.Attachment.Name, p.Used, p.Editable)
			}
		}
	}
}
