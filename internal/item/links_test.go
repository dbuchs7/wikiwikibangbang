package item

import (
	"errors"
	"testing"
)

func linkBlock(text, href string) Block {
	return Block{Kind: Paragraph, Text: text, Marks: []Mark{{Type: MarkLink, Start: 0, End: len([]rune(text)), Href: href}}}
}

func TestWebLinksNameItemAndResponsible(t *testing.T) {
	s := permFixture(t)
	anna, ben, pat := s.As("anna"), s.As("ben"), s.As("pat")
	page := mustItem(t)(anna.Create(NewItem{Space: "PLAT", Type: "page", Title: "Runbook"}))
	noErr(t, anna.CommitBody(page.ID, Doc{Blocks: []Block{
		linkBlock("guide", "https://example.org/guide"),
		{Kind: Paragraph, Text: "plain"},
		linkBlock("mail", "mailto:ops@example.org"),
	}}))
	// pat edits the body last: pat is responsible now.
	body := Doc{Blocks: []Block{linkBlock("guide", "https://example.org/guide"), linkBlock("api", "http://api.example.org/v1")}}
	noErr(t, pat.CommitBody(page.ID, body))
	secret := mustItem(t)(pat.Create(NewItem{Space: "PLAT", Type: "page", Title: "Secret"}))
	noErr(t, pat.CommitBody(secret.ID, Doc{Blocks: []Block{linkBlock("x", "https://secret.example.org/")}}))
	noErr(t, pat.Restrict(secret.ID, Restriction{Read: []string{"pat"}}))
	gone := mustItem(t)(anna.Create(NewItem{Space: "PLAT", Type: "page", Title: "Old"}))
	noErr(t, anna.CommitBody(gone.ID, Doc{Blocks: []Block{linkBlock("old", "https://old.example.org/")}}))
	noErr(t, anna.Trash(gone.ID))

	links, err := pat.WebLinks("PLAT")
	noErr(t, err)
	if len(links) != 3 {
		t.Fatalf("pat sees %d links, want 3 (web links of live items only): %+v", len(links), links)
	}
	if l := links[0]; l.Item != page.ID || l.Block != 0 || l.Href != "https://example.org/guide" || l.Responsible != "pat" {
		t.Errorf("first link %+v", l)
	}
	if links[1].Href != "http://api.example.org/v1" || links[1].Block != 1 {
		t.Errorf("second link %+v", links[1])
	}
	// A reader sees the links of what it may read; the restricted item's
	// link stays hidden (D10).
	if got, _ := ben.WebLinks("PLAT"); len(got) != 2 {
		t.Errorf("ben sees %d links, want 2", len(got))
	}
	if !pat.MayAdminister("PLAT") || anna.MayAdminister("PLAT") {
		t.Error("only pat administers PLAT")
	}
	if _, err := s.As("carl").WebLinks("PLAT"); !errors.Is(err, ErrNotFound) {
		t.Errorf("outsider: %v", err)
	}
}
