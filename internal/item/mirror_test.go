package item

import (
	"errors"
	"testing"
)

func fileOrigin(host ID, path string) Origin {
	return Origin{System: "git:file", Instance: "https://git.example/o/r", ExternalID: string(host) + "/" + path,
		ExternalKey: "main", URL: "https://git.example/o/r/blob/main/" + path, Version: "c1", Raw: "# A"}
}

// A person makes a page that mirrors a file below an item they may edit,
// and updates it later; who may not edit the item may not (ADR-0128).
func TestMirrorAsAPerson(t *testing.T) {
	s := setup(t)
	noErr(t, s.GrantRole("admin", "PLAT", "anna", Editor))
	noErr(t, s.GrantRole("admin", "PLAT", "carl", Viewer))
	host := mustItem(t)(s.As("anna").Create(NewItem{Space: "PLAT", Type: "page", Title: "Host"}))
	u := Upsert{Origin: fileOrigin(host.ID, "docs/a.md"), Parent: host.ID, Type: "page", Title: "A", Space: "OPS", Key: "OPS-9", State: "x", Managed: "x"}

	if _, err := s.As("carl").Mirror(u); !errors.Is(err, ErrForbidden) {
		t.Fatalf("a viewer mirrors: %v", err)
	}
	if _, err := s.As("anna").Mirror(Upsert{Origin: u.Origin, Type: "page", Title: "A"}); !errors.Is(err, ErrValidation) {
		t.Fatalf("without a parent: %v", err)
	}
	page, err := s.As("anna").Mirror(u)
	if err != nil {
		t.Fatal(err)
	}
	if page.Space != "PLAT" || page.Parent != host.ID || page.Managed != "" || page.Origin.Raw != "# A" || !s.As("anna").Watching(page.ID) {
		t.Fatalf("mirrored %+v", page)
	}
	// It stays where a person moved it, and is found by its system.
	other := mustItem(t)(s.As("anna").Create(NewItem{Space: "PLAT", Type: "page", Title: "Elsewhere"}))
	noErr(t, s.As("anna").Move(page.ID, "PLAT", other.ID))
	u.Title, u.Origin.Version = "A again", "c2"
	again, err := s.As("anna").Mirror(u)
	if err != nil || again.ID != page.ID || again.Parent != other.ID || again.Title != "A again" || again.Origin.Version != "c2" {
		t.Fatalf("again %+v %v", again, err)
	}
	if found := s.FindBySystem("git:file"); len(found) != 1 || found[0].ID != page.ID {
		t.Fatalf("by system %+v", found)
	}
	if found := s.FindBySystem("git"); len(found) != 0 {
		t.Fatalf("a system is matched whole: %+v", found)
	}
	// Who may not edit the page may not update it.
	noErr(t, s.As("anna").Restrict(page.ID, Restriction{Edit: []string{"anna"}}))
	noErr(t, s.GrantRole("admin", "PLAT", "dora", Editor))
	if _, err := s.As("dora").Mirror(u); !errors.Is(err, ErrForbidden) {
		t.Fatalf("an editor the page excludes: %v", err)
	}
	noErr(t, s.Trash("anna", page.ID))
	if len(s.FindBySystem("git:file")) != 0 {
		t.Fatal("a trashed page is not found")
	}
}
