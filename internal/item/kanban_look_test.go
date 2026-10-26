package item

import (
	"errors"
	"testing"
)

// TestKanbanLimitsAndColours: a column may name how many cards it should
// hold, and a kanban may colour its cards by an enumeration field, each
// value with a tone of its own or of its place among the values
// (ADR-0133). Both are kept when a change leaves them unset.
func TestKanbanLimitsAndColours(t *testing.T) {
	_, anna, k, _ := columnsBoard(t)
	k.Columns = []KanbanColumn{{Name: "To do", State: "open", Limit: 5}, {Name: "Doing", State: "in progress", Limit: 2}, {Name: "Review", Limit: 1}}
	k.ColorField, k.Colors = "priority", map[string]string{"high": "red", "low": "blue"}
	k, err := anna.SetKanban("PLAT", k)
	noErr(t, err)
	if k.Columns[0].Limit != 5 || k.Columns[1].Limit != 2 || k.Columns[2].Limit != 1 || k.ColorField != "priority" || k.Colors["high"] != "red" {
		t.Fatalf("kanban %+v", k)
	}
	_, board, err := anna.Board("PLAT", k.ID)
	noErr(t, err)
	if len(board) != 4 || board[1].Limit != 2 || board[3].State != "done" || board[3].Limit != 0 {
		t.Fatalf("the board's columns carry their limits: %+v", board)
	}

	// A change that leaves them unset keeps the limits with the columns
	// and the colouring.
	kept, err := anna.SetKanban("PLAT", Kanban{ID: k.ID, Name: "Website"})
	noErr(t, err)
	if kept.Columns[1].Limit != 2 || kept.ColorField != "priority" || len(kept.Colors) != 2 {
		t.Fatalf("kept %+v", kept)
	}
	// A field without tones of its own: each value takes its place's tone.
	auto, err := anna.SetKanban("PLAT", Kanban{ID: k.ID, Name: "Website", ColorField: "priority"})
	noErr(t, err)
	if auto.ColorField != "priority" || auto.Colors != nil {
		t.Fatalf("automatic tones %+v", auto)
	}
	// No field and an empty set of tones take the colouring off.
	off, err := anna.SetKanban("PLAT", Kanban{ID: k.ID, Name: "Website", Colors: map[string]string{}})
	noErr(t, err)
	if off.ColorField != "" || off.Colors != nil {
		t.Fatalf("colouring off %+v", off)
	}
}

func TestKanbanLimitsAndColoursAreChecked(t *testing.T) {
	_, anna, k, _ := columnsBoard(t)
	for name, change := range map[string]func(*Kanban){
		"a negative limit":     func(k *Kanban) { k.Columns = []KanbanColumn{{Name: "A", State: "open", Limit: -1}} },
		"a limit too high":     func(k *Kanban) { k.Columns = []KanbanColumn{{Name: "A", State: "open", Limit: maxColumnLimit + 1}} },
		"an unknown field":     func(k *Kanban) { k.ColorField = "colour" },
		"not an enumeration":   func(k *Kanban) { k.ColorField = "assignee" },
		"an unknown value":     func(k *Kanban) { k.ColorField, k.Colors = "priority", map[string]string{"urgent": "red"} },
		"an unknown tone":      func(k *Kanban) { k.ColorField, k.Colors = "priority", map[string]string{"high": "pink"} },
		"tones without fields": func(k *Kanban) { k.Colors = map[string]string{"high": "red"} },
	} {
		bad := k
		change(&bad)
		if _, err := anna.SetKanban("PLAT", bad); !errors.Is(err, ErrValidation) {
			t.Errorf("%s: %v", name, err)
		}
	}
}
