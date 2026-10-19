package item

import (
	"errors"
	"testing"
)

func TestEarlierBodiesCanBeReadAndRestored(t *testing.T) {
	s := permFixture(t)
	anna, ben, carl := s.As("anna"), s.As("ben"), s.As("carl")
	page := mustItem(t)(anna.Create(NewItem{Space: "PLAT", Type: "page", Title: "Runbook"}))
	noErr(t, anna.CommitBody(page.ID, Doc{Blocks: []Block{{Kind: Paragraph, Text: "first"}}}))
	first := s.Log()[len(s.Log())-1].Seq
	noErr(t, anna.CommitBody(page.ID, Doc{Blocks: []Block{{Kind: Paragraph, Text: "second"}}}))

	v, err := ben.BodyAt(page.ID, first)
	noErr(t, err)
	if v.Actor != "anna" || len(v.Body.Blocks) != 1 || v.Body.Blocks[0].Text != "first" {
		t.Fatalf("version %+v", v)
	}
	// The answer is a copy.
	v.Body.Blocks[0].Text = "changed"
	if again, _ := ben.BodyAt(page.ID, first); again.Body.Blocks[0].Text != "first" {
		t.Fatal("BodyAt shares the logged body")
	}
	if _, err := ben.BodyAt(page.ID, first-1); !errors.Is(err, ErrNotFound) {
		t.Errorf("a seq that is not a body commit: %v", err)
	}
	if _, err := carl.BodyAt(page.ID, first); !errors.Is(err, ErrNotFound) {
		t.Errorf("an outsider reads a version: %v", err)
	}
	if err := ben.RestoreBody(page.ID, first); !errors.Is(err, ErrForbidden) {
		t.Errorf("a viewer restores: %v", err)
	}
	noErr(t, anna.RestoreBody(page.ID, first))
	if got := mustItem(t)(anna.Get(page.ID)); got.Body.Blocks[0].Text != "first" {
		t.Fatalf("restored body %+v", got.Body)
	}
	// Purging erases every version.
	noErr(t, anna.Trash(page.ID))
	noErr(t, s.Purge("pat", page.ID))
	if _, err := s.As("pat").BodyAt(page.ID, first); err == nil {
		t.Error("a purged version is still readable")
	}
}
