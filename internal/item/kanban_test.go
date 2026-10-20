package item

import (
	"errors"
	"testing"
)

// TestKanbans: editors make kanbans of a workflow type, named, with a
// label; renaming keeps type and label; readers list them; a kanban's
// items are those of its type and label, on as many kanbans as labels
// (ADR-0060).
func TestKanbans(t *testing.T) {
	s := permFixture(t)
	anna, ben, carl := s.As("anna"), s.As("ben"), s.As("carl")
	if _, err := ben.SetKanban("PLAT", Kanban{Name: "Web", Type: "task"}); !errors.Is(err, ErrForbidden) {
		t.Errorf("a viewer makes a kanban: %v", err)
	}
	if _, err := anna.SetKanban("PLAT", Kanban{Name: "Web", Type: "page"}); !errors.Is(err, ErrValidation) {
		t.Errorf("a kanban of a type without workflow: %v", err)
	}
	if _, err := anna.SetKanban("PLAT", Kanban{Name: " ", Type: "task"}); !errors.Is(err, ErrValidation) {
		t.Errorf("a kanban without a name: %v", err)
	}
	web, err := anna.SetKanban("PLAT", Kanban{Name: "Website Relaunch", Type: "task"})
	if err != nil || web.Label != "website-relaunch" || web.ID == "" {
		t.Fatalf("kanban %+v %v", web, err)
	}
	if _, err := anna.SetKanban("PLAT", Kanban{Name: "Again", Type: "task", Label: "website-relaunch"}); !errors.Is(err, ErrValidation) {
		t.Errorf("a second kanban for the same type and label: %v", err)
	}
	web.Name, web.Label = "Website", "other"
	renamed, err := anna.SetKanban("PLAT", web)
	if err != nil || renamed.Name != "Website" || renamed.Label != "website-relaunch" {
		t.Fatalf("renamed %+v %v", renamed, err)
	}
	if got := ben.Kanbans(""); len(got["PLAT"]) != 1 || got["PLAT"][0].Name != "Website" {
		t.Errorf("a reader lists %v", got)
	}
	if got := carl.Kanbans(""); len(got) != 0 {
		t.Errorf("an outsider lists %v", got)
	}
	if err := ben.RemoveKanban("PLAT", web.ID); !errors.Is(err, ErrForbidden) {
		t.Errorf("a viewer removes a kanban: %v", err)
	}
	noErr(t, anna.RemoveKanban("PLAT", web.ID))
	if got := anna.Kanbans("PLAT"); len(got) != 0 {
		t.Errorf("removed kanban still listed: %v", got)
	}
}
