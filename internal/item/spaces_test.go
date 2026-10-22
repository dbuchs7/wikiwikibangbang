package item

import (
	"errors"
	"strings"
	"testing"
)

func TestSpaceTrashIsRefusedWhileReferencedFromOutside(t *testing.T) {
	s, _, _, _, foreign := trashFixture(t)
	err := s.TrashSpace("root", "PLAT")
	if !errors.Is(err, ErrReferenced) || !strings.Contains(err.Error(), foreign.Key) {
		t.Fatalf("err = %v, want ErrReferenced naming %s", err, foreign.Key)
	}
	// References from inside the space, or from what is in the trash, do not
	// block.
	noErr(t, s.Trash("ben", foreign.ID))
	noErr(t, s.TrashSpace("root", "PLAT"))
}

func TestSpaceInTheTrashIsOutOfEveryonesSight(t *testing.T) {
	s, page, child, _, foreign := trashFixture(t)
	unreference(t, s, foreign)
	noErr(t, s.GrantSystemRole("root", "root", RoleAdmin))
	noErr(t, s.GrantRole("root", "PLAT", "vera", Viewer))
	noErr(t, s.TrashSpace("root", "PLAT"))

	for _, who := range []string{"root", "vera"} {
		x := s.As(who)
		if _, err := x.Get(page.ID); !errors.Is(err, ErrNotFound) {
			t.Fatalf("%s reads a page of a space in the trash: %v", who, err)
		}
		for _, sp := range x.Spaces() {
			if sp.Key == "PLAT" {
				t.Fatalf("%s still lists the space", who)
			}
		}
	}
	if _, err := s.As("root").Create(NewItem{Space: "PLAT", Type: "page", Title: "New"}); err == nil {
		t.Fatal("an item was created in a space in the trash")
	}
	if err := s.TrashSpace("root", "PLAT"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("trashing twice: %v", err)
	}
	if got := s.TrashedSpaces(); len(got) != 1 || got[0].Key != "PLAT" || got[0].Items != 3 {
		t.Fatalf("trashed spaces: %+v", got)
	}

	// Restoring brings everything back as it was.
	noErr(t, s.RestoreSpace("root", "PLAT"))
	if it, err := s.As("vera").Get(child.ID); err != nil || it.Title != "Patch" {
		t.Fatalf("after restore: %+v %v", it, err)
	}
	if err := s.RestoreSpace("root", "PLAT"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("restoring a live space: %v", err)
	}
}

func TestSpacePurgeLeavesTombstonesAndKeepsTheKey(t *testing.T) {
	s, page, child, _, foreign := trashFixture(t)
	unreference(t, s, foreign)
	noErr(t, s.GrantRole("root", "PLAT", "vera", Viewer))
	if err := s.PurgeSpace("root", "PLAT"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("purging a live space: %v", err)
	}
	noErr(t, s.TrashSpace("root", "PLAT"))
	noErr(t, s.PurgeSpace("root", "PLAT"))

	for _, id := range []ID{page.ID, child.ID} {
		it := s.items[id]
		if it.Presence != Purged || it.Title != "" {
			t.Fatalf("%s after purge: %+v", id, it)
		}
	}
	// The keys still resolve to the tombstones (D7), and are not reused.
	if s.keys[page.Key] != page.ID {
		t.Fatalf("key %s no longer resolves", page.Key)
	}
	if err := s.CreateSpace("root", "PLAT", "Again"); err == nil || !strings.Contains(err.Error(), "not used again") {
		t.Fatalf("reusing the key: %v", err)
	}
	if len(s.spaceRoles["PLAT"]) != 0 || len(s.TrashedSpaces()) != 0 {
		t.Fatal("roles or the trash entry outlived the purge")
	}
	// The purge took the content out of the history, too.
	for _, e := range s.Log() {
		if e.Item == page.ID && e.Kind == ItemCreated && e.Title != "" {
			t.Fatalf("the history still shows the title: %+v", e)
		}
	}
	if err := s.RestoreSpace("root", "PLAT"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("restoring a purged space: %v", err)
	}
}

// A space's administrators rename it; the key and the items' keys stay
// (ADR-0086).
func TestASpaceIsRenamedByItsAdministrators(t *testing.T) {
	s := permFixture(t)
	page := mustItem(t)(s.As("anna").Create(NewItem{Space: "PLAT", Type: "page", Title: "Runbook"}))
	noErr(t, s.GrantRole("root", "PLAT", "ben", Viewer))
	noErr(t, s.GrantRole("root", "PLAT", "dora", SpaceAdmin))
	if err := s.As("ben").RenameSpace("PLAT", "Mine"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("a viewer renames: %v", err)
	}
	if err := s.As("carl").RenameSpace("PLAT", "Mine"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("an outsider renames: %v", err)
	}
	for _, bad := range []string{"", "  ", "two\nlines", strings.Repeat("x", maxSpaceName+1)} {
		if err := s.As("dora").RenameSpace("PLAT", bad); !errors.Is(err, ErrValidation) {
			t.Errorf("%q: %v", bad, err)
		}
	}
	before := len(s.Log())
	noErr(t, s.As("dora").RenameSpace("PLAT", "  Platform team "))
	noErr(t, s.As("dora").RenameSpace("PLAT", "Platform team")) // unchanged is nothing
	if len(s.Log()) != before+1 {
		t.Fatalf("events: %d", len(s.Log())-before)
	}
	var name string
	for _, sp := range s.As("ben").Spaces() {
		if sp.Key == "PLAT" {
			name = sp.Name
		}
	}
	got, err := s.As("anna").Get(page.ID)
	noErr(t, err)
	if name != "Platform team" || !strings.HasPrefix(got.Key, "PLAT-") {
		t.Fatalf("name %q, key %s", name, got.Key)
	}
	if err := s.RenameSpace("root", "NOPE", "x"); !errors.Is(err, ErrNotFound) {
		t.Errorf("an unknown space: %v", err)
	}
}
