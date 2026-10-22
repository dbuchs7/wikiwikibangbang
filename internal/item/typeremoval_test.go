package item

import (
	"errors"
	"testing"
)

// A type is removed only when nothing uses it; its versions stay for the
// items purged earlier, and defining it again starts its next version
// (ADR-0094).
func TestATypeIsRemovedOnlyWhenNothingUsesIt(t *testing.T) {
	s := setup(t)
	noErr(t, s.DefineType("admin", TypeDef{Name: "interface", Body: true}))
	noErr(t, s.GrantRole("admin", "PLAT", "anna", SpaceAdmin))
	anna := s.As("anna")
	it := mustItem(t)(anna.Create(NewItem{Space: "PLAT", Type: "interface", Title: "Old entry"}))

	if err := s.RemoveType("admin", "interface"); !errors.Is(err, ErrReferenced) {
		t.Fatalf("removed while a live item uses it: %v", err)
	}
	noErr(t, anna.Trash(it.ID))
	if err := s.RemoveType("admin", "interface"); !errors.Is(err, ErrReferenced) {
		t.Fatalf("removed while a trashed item uses it: %v", err)
	}
	noErr(t, anna.Purge(it.ID))
	noErr(t, s.RemoveType("admin", "interface"))

	for _, td := range s.Types() {
		if td.Name == "interface" {
			t.Fatal("a removed type is still listed")
		}
	}
	if s.Type("interface").Name != "" {
		t.Fatal("a removed type is still the latest of its name")
	}
	if _, ok := s.TypeAt("interface", 1); !ok {
		t.Fatal("a removed type's versions are gone")
	}
	if _, err := anna.Create(NewItem{Space: "PLAT", Type: "interface", Title: "New"}); err == nil {
		t.Fatal("an item was made of a removed type")
	}
	if err := s.RemoveType("admin", "interface"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("removed twice: %v", err)
	}

	r, err := Replay(s.Log())
	noErr(t, err)
	if r.Type("interface").Name != "" {
		t.Fatal("the removal does not survive a replay")
	}

	noErr(t, s.DefineType("admin", TypeDef{Name: "interface", Body: true}))
	if td := s.Type("interface"); td.Version != 2 {
		t.Fatalf("defined again as version %d, want 2", td.Version)
	}
}

func TestATypeOnAKanbanStays(t *testing.T) {
	s := setup(t)
	noErr(t, s.GrantRole("admin", "PLAT", "anna", SpaceAdmin))
	_, err := s.As("anna").SetKanban("PLAT", Kanban{Name: "Sprint", Type: "task", Label: "sprint"})
	noErr(t, err)
	if err := s.RemoveType("admin", "task"); !errors.Is(err, ErrReferenced) {
		t.Fatalf("removed while a kanban shows it: %v", err)
	}
	if err := s.RemoveType("admin", "nothing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("removed an unknown type: %v", err)
	}
}
