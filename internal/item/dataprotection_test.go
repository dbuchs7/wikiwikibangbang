package item

import (
	"errors"
	"testing"
)

// TestDataProtectionHoldsOpenItems: an indicator in content holds an item
// open to its space; only editors read it then, and a second person
// releases it (ADR-0058).
func TestDataProtectionHoldsOpenItems(t *testing.T) {
	s := permFixture(t)
	s.SetAssessor(testAssessor)
	noErr(t, s.GrantRole("root", "PLAT", "erik", Editor))
	anna, ben, pat, erik := s.As("anna"), s.As("ben"), s.As("pat"), s.As("erik")
	page := mustItem(t)(anna.Create(NewItem{Space: "PLAT", Type: "page", Title: "Visit"}))
	noErr(t, anna.CommitBody(page.ID, Doc{Blocks: []Block{{Kind: Paragraph, Text: "Diagnose: flu"}}}))

	if _, err := ben.Get(page.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a viewer reads a held item: %v", err)
	}
	got, err := erik.Get(page.ID)
	if err != nil || got.Hold == nil || len(got.Hold.Findings) != 1 || got.Hold.Savers[0] != "anna" {
		t.Fatalf("an editor reads the hold: %+v %v", got.Hold, err)
	}
	// Four eyes: not who saved it; a restriction first; a reason for a
	// false alarm.
	if err := anna.ReleaseHold(page.ID, HoldFalseAlarm, "no"); !errors.Is(err, ErrForbidden) {
		t.Errorf("the one who saved releases: %v", err)
	}
	if err := ben.ReleaseHold(page.ID, HoldFalseAlarm, "no"); !errors.Is(err, ErrNotFound) {
		t.Errorf("a viewer releases: %v", err)
	}
	if err := erik.ReleaseHold(page.ID, HoldRestricted, ""); !errors.Is(err, ErrValidation) {
		t.Errorf("released as restricted while open: %v", err)
	}
	if err := erik.ReleaseHold(page.ID, HoldFalseAlarm, " "); !errors.Is(err, ErrValidation) {
		t.Errorf("a false alarm without reason: %v", err)
	}
	noErr(t, erik.ReleaseHold(page.ID, HoldFalseAlarm, "the diagnosis of a server"))
	if _, err := ben.Get(page.ID); err != nil {
		t.Fatalf("released, a viewer reads again: %v", err)
	}
	// The cleared finding does not hold the item again.
	noErr(t, anna.CommitBody(page.ID, Doc{Blocks: []Block{{Kind: Paragraph, Text: "Diagnose: disk full"}}}))
	if got, _ := ben.Get(page.ID); got.Hold != nil {
		t.Errorf("a cleared finding holds again")
	}

	// A restricted item is assessed, not held; opened later, it is held.
	secret := mustItem(t)(pat.Create(NewItem{Space: "PLAT", Type: "page", Title: "Case"}))
	noErr(t, pat.Restrict(secret.ID, Restriction{Read: []string{"pat", "erik"}}))
	noErr(t, pat.CommitBody(secret.ID, Doc{Blocks: []Block{{Kind: Paragraph, Text: "Diagnose: fracture"}}}))
	if got, _ := pat.Get(secret.ID); got.Hold != nil {
		t.Fatalf("a restricted item is held")
	}
	noErr(t, pat.Restrict(secret.ID, Restriction{}))
	got, _ = pat.Get(secret.ID)
	if got.Hold == nil || got.Hold.Savers[0] != "pat" {
		t.Fatalf("opening an item with findings does not hold it: %+v", got.Hold)
	}
	if _, err := ben.Get(secret.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("a viewer reads the reopened item: %v", err)
	}
	// Released as restricted by someone else once reading is narrowed.
	noErr(t, pat.Restrict(secret.ID, Restriction{Read: []string{"pat", "erik"}}))
	noErr(t, erik.ReleaseHold(secret.ID, HoldRestricted, ""))
	if got, _ := pat.Get(secret.ID); got.Hold != nil {
		t.Errorf("still held after release")
	}
	// Without an assessor nothing is held.
	s.SetAssessor(nil)
	other := mustItem(t)(anna.Create(NewItem{Space: "PLAT", Type: "page", Title: "Diagnose"}))
	if got, _ := anna.Get(other.ID); got.Hold != nil {
		t.Errorf("held without regulations")
	}
}

// TestHoldsAreNotified: a hold reaches whoever saved and the space's
// administrators, a release whoever saved (ADR-0058).
func TestHoldsAreNotified(t *testing.T) {
	s := permFixture(t)
	s.SetAssessor(testAssessor)
	anna, ben, pat := s.As("anna"), s.As("ben"), s.As("pat")
	page := mustItem(t)(anna.Create(NewItem{Space: "PLAT", Type: "page", Title: "Visit"}))
	noErr(t, anna.CommitBody(page.ID, Doc{Blocks: []Block{{Kind: Paragraph, Text: "Diagnose: flu"}}}))
	kinds := func(x *Session) (out []EventKind) {
		list, _ := x.Notifications(0, 10)
		for _, n := range list {
			out = append(out, n.Kind)
		}
		return out
	}
	if got := kinds(anna); len(got) != 1 || got[0] != HoldSet {
		t.Errorf("the one who saved hears %v", got)
	}
	if got := kinds(pat); len(got) != 1 || got[0] != HoldSet {
		t.Errorf("the administrator hears %v", got)
	}
	if got := kinds(ben); len(got) != 0 {
		t.Errorf("a viewer hears %v", got)
	}
	noErr(t, pat.ReleaseHold(page.ID, HoldFalseAlarm, "a server"))
	if got := kinds(anna); len(got) != 2 || got[0] != HoldReleased {
		t.Errorf("the one who saved hears of the release: %v", got)
	}
	if got := kinds(pat); len(got) != 1 {
		t.Errorf("the one who released hears it again: %v", got)
	}
}
