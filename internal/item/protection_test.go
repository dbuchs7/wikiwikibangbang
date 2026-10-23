package item

import (
	"errors"
	"strings"
	"testing"
)

// protectedBelow makes a page of anna's with a subpage carl protects from
// her, and one she may read but not edit.
func protectedBelow(t *testing.T) (*Store, Item, Item, Item) {
	t.Helper()
	s := permFixture(t)
	noErr(t, s.GrantRole("root", "PLAT", "carl", Editor))
	anna, carl := s.As("anna"), s.As("carl")
	page := mustItem(t)(anna.Create(NewItem{Space: "PLAT", Type: "page", Title: "Handbook"}))
	hidden := mustItem(t)(carl.Create(NewItem{Space: "PLAT", Type: "page", Parent: page.ID, Title: "Salaries"}))
	noErr(t, carl.Restrict(hidden.ID, Restriction{Read: []string{"carl"}}))
	locked := mustItem(t)(carl.Create(NewItem{Space: "PLAT", Type: "page", Parent: page.ID, Title: "Policy"}))
	noErr(t, carl.Restrict(locked.ID, Restriction{Edit: []string{"carl"}}))
	return s, page, hidden, locked
}

// TestRemovalsDoNotNameHiddenPages: trashing or moving a page with a
// subpage the actor may not see is refused without naming it, and the
// items that reference a page are named only when the actor may read them
// (D10).
func TestRemovalsDoNotNameHiddenPages(t *testing.T) {
	s, page, hidden, locked := protectedBelow(t)
	anna := s.As("anna")
	for name, err := range map[string]error{"trash": anna.Trash(page.ID), "move": anna.Move(page.ID, "PLAT", "")} {
		if !errors.Is(err, ErrForbidden) || strings.Contains(err.Error(), hidden.Key) {
			t.Errorf("%s: %v names the hidden page %s", name, err, hidden.Key)
		}
	}
	if anna.MayTrash(page.ID) {
		t.Error("anna is offered to trash a page with a hidden subpage")
	}
	// A subpage she may read is named.
	noErr(t, s.As("carl").Restrict(hidden.ID, Restriction{}))
	if err := anna.Trash(page.ID); !errors.Is(err, ErrForbidden) || !strings.Contains(err.Error(), locked.Key) {
		t.Errorf("a readable subpage is not named: %v", err)
	}

	// References: one from a page anna reads, one from a page she does not.
	target := mustItem(t)(anna.Create(NewItem{Space: "PLAT", Type: "page", Title: "Glossary"}))
	open := mustItem(t)(anna.Create(NewItem{Space: "PLAT", Type: "page", Title: "Index"}))
	noErr(t, s.AddRelation("anna", "blocks", open.ID, target.ID))
	secret := mustItem(t)(s.As("carl").Create(NewItem{Space: "PLAT", Type: "page", Title: "Board minutes"}))
	noErr(t, s.As("carl").Restrict(secret.ID, Restriction{Read: []string{"carl"}}))
	noErr(t, s.AddRelation("carl", "blocks", secret.ID, target.ID))
	err := anna.Trash(target.ID)
	if !errors.Is(err, ErrReferenced) || !strings.Contains(err.Error(), open.Key) || strings.Contains(err.Error(), secret.Key) ||
		!strings.Contains(err.Error(), "1 item(s) you may not see") {
		t.Errorf("referrers: %v", err)
	}
	if err := s.As("pat").Trash(target.ID); !strings.Contains(err.Error(), secret.Key) {
		t.Errorf("a space admin reads every referrer: %v", err)
	}
}

// TestProtectingAPageNeedsItsSubpages: a restriction is inherited by every
// page below, so setting or lifting one needs edit rights on them all, as
// moving does (ADR-0022).
func TestProtectingAPageNeedsItsSubpages(t *testing.T) {
	s, page, hidden, _ := protectedBelow(t)
	anna := s.As("anna")
	err := anna.Restrict(page.ID, Restriction{Read: []string{"anna"}})
	if !errors.Is(err, ErrForbidden) || strings.Contains(err.Error(), hidden.Key) {
		t.Fatalf("anna locks carl out of his page: %v", err)
	}
	if _, err := s.As("carl").Get(hidden.ID); err != nil {
		t.Fatalf("carl lost his page: %v", err)
	}
	// A space admin may.
	noErr(t, s.As("pat").Restrict(page.ID, Restriction{Read: []string{"anna", "carl", "pat"}}))
	// A page with nothing below is protected as before.
	leaf := mustItem(t)(anna.Create(NewItem{Space: "PLAT", Type: "page", Title: "Leaf"}))
	noErr(t, anna.Restrict(leaf.ID, Restriction{Read: []string{"anna"}}))
}

// TestRestoringNeedsEverythingTrashedWithIt: a batch is restored only by
// someone who may edit each of its pages, and the UI is told so.
func TestRestoringNeedsEverythingTrashedWithIt(t *testing.T) {
	s, page, _, _ := protectedBelow(t)
	pat, anna := s.As("pat"), s.As("anna")
	noErr(t, pat.Trash(page.ID))
	if err := anna.Restore(page.ID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("anna restores pages she may not edit: %v", err)
	}
	if anna.MayRestore(page.ID) || anna.MayPurge(page.ID) {
		t.Error("anna is offered restore or purge")
	}
	if !pat.MayRestore(page.ID) || !pat.MayPurge(page.ID) {
		t.Error("the space admin is not offered restore and purge")
	}
	noErr(t, pat.Restore(page.ID))
	// Restoring and purging act on the page the trash started from.
	child := mustItem(t)(anna.Create(NewItem{Space: "PLAT", Type: "page", Parent: page.ID, Title: "Child"}))
	noErr(t, pat.Trash(page.ID))
	if pat.MayRestore(child.ID) || pat.MayPurge(child.ID) {
		t.Error("restore or purge is offered on a page that was trashed with another")
	}
}

// TestASpaceKeepsASpaceAdmin: a space admin cannot revoke or lower the
// last space admin role of a space, their own included; an administrator
// can.
func TestASpaceKeepsASpaceAdmin(t *testing.T) {
	s := permFixture(t)
	pat := s.As("pat")
	if err := pat.Revoke("PLAT", "pat"); !errors.Is(err, ErrValidation) {
		t.Fatalf("the last space admin revokes themselves: %v", err)
	}
	if err := pat.Grant("PLAT", "pat", Editor); !errors.Is(err, ErrValidation) {
		t.Fatalf("the last space admin lowers their role: %v", err)
	}
	// An empty group does not count as a space admin.
	noErr(t, pat.Grant("PLAT", "@nobody", SpaceAdmin))
	if err := pat.Revoke("PLAT", "pat"); !errors.Is(err, ErrValidation) {
		t.Fatalf("an empty group keeps the space: %v", err)
	}
	noErr(t, pat.Grant("PLAT", "anna", SpaceAdmin))
	noErr(t, pat.Revoke("PLAT", "pat"))
	noErr(t, s.GrantSystemRole("root", "sam", RoleAdmin))
	noErr(t, s.As("sam").Revoke("PLAT", "anna"))
	noErr(t, s.As("sam").Revoke("PLAT", "@nobody"))
}

// TestACopyKeepsInheritedProtection: a page protected through a page
// above it is not copied to where that protection does not reach.
func TestACopyKeepsInheritedProtection(t *testing.T) {
	s := permFixture(t)
	anna := s.As("anna")
	parent := mustItem(t)(anna.Create(NewItem{Space: "PLAT", Type: "page", Title: "HR"}))
	child := mustItem(t)(anna.Create(NewItem{Space: "PLAT", Type: "page", Parent: parent.ID, Title: "Salaries"}))
	other := mustItem(t)(anna.Create(NewItem{Space: "PLAT", Type: "page", Title: "Open"}))
	noErr(t, anna.KeepsInherited(child.ID, ""))
	noErr(t, anna.Restrict(parent.ID, Restriction{Read: []string{"anna"}}))
	for _, to := range []ID{"", other.ID} {
		if err := anna.KeepsInherited(child.ID, to); !errors.Is(err, ErrValidation) {
			t.Errorf("copy to %q sheds the parent's protection: %v", to, err)
		}
	}
	noErr(t, anna.KeepsInherited(child.ID, parent.ID))
	noErr(t, anna.KeepsInherited(parent.ID, ""))
	if err := s.As("ben").KeepsInherited(child.ID, ""); !errors.Is(err, ErrNotFound) {
		t.Errorf("ben copies a page he cannot read: %v", err)
	}
	if err := anna.MayRestrictIn("PLAT", Restriction{Read: []string{"ben"}}); !errors.Is(err, ErrValidation) {
		t.Errorf("anna may put on a copy a restriction that locks her out: %v", err)
	}
}
