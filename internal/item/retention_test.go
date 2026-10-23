package item

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"
)

// drawnFourTimes makes a page with a drawing drawn four times, the second
// version shown in its text, and an unrelated picture.
func drawnFourTimes(t *testing.T) (*Store, *Session, Item, []ID) {
	t.Helper()
	s := setup(t)
	noErr(t, s.GrantRole("admin", "PLAT", "anna", Editor))
	noErr(t, s.GrantRole("admin", "PLAT", "ben", Viewer))
	anna := s.As("anna")
	page := mustItem(t)(anna.Create(NewItem{Space: "PLAT", Type: "page", Title: "Plan"}))
	ids := []ID{"a0000000000000000000000000000001", "a0000000000000000000000000000002", "a0000000000000000000000000000003", "a0000000000000000000000000000004"}
	for i, id := range ids {
		a := Attachment{ID: id, Name: "plan.drawing.png", Type: "image/png", Size: 3, SHA256: strings.Repeat("ab", 32)}
		if i > 0 {
			a.Source = &Source{Item: page.ID, Attachment: ids[i-1]}
		}
		_, err := anna.AddAttachment(page.ID, a)
		noErr(t, err)
	}
	_, err := anna.AddAttachment(page.ID, Attachment{ID: "b0000000000000000000000000000001", Name: "photo.png", Type: "image/png", Size: 3, SHA256: strings.Repeat("cd", 32)})
	noErr(t, err)
	noErr(t, anna.CommitBody(page.ID, Doc{Blocks: []Block{{Kind: Image, Ref: ids[1]}}}))
	return s, anna, page, ids
}

func idsOf(as []Attachment) (out []ID) {
	for _, a := range as {
		out = append(out, a.ID)
	}
	return out
}

// TestRetentionKeepsByNumberAgeAndUse: a rule keeps the newest versions it
// names and those younger than its days; the current version and the one
// the text shows always stay; a rule without limits keeps everything
// (ADR-0104).
func TestRetentionKeepsByNumberAgeAndUse(t *testing.T) {
	_, anna, page, ids := drawnFourTimes(t)
	now := time.Date(2026, 10, 16, 9, 0, 0, 0, time.UTC).Add(48 * time.Hour)
	cases := []struct {
		rule         Retention
		keep, remove []ID
	}{
		{Retention{}, []ID{ids[3], ids[2], ids[1], ids[0]}, nil},
		{Retention{Keep: 1}, []ID{ids[3], ids[1]}, []ID{ids[2], ids[0]}},
		{Retention{Keep: 2}, []ID{ids[3], ids[2], ids[1]}, []ID{ids[0]}},
		{Retention{Days: 1}, []ID{ids[3], ids[1]}, []ID{ids[2], ids[0]}},
		{Retention{Days: 3}, []ID{ids[3], ids[2], ids[1], ids[0]}, nil},
		{Retention{Keep: 2, Days: 1}, []ID{ids[3], ids[2], ids[1]}, []ID{ids[0]}},
	}
	for _, c := range cases {
		p, err := anna.PlanRetention(page.ID, ids[0], &c.rule, Retention{}, now)
		noErr(t, err)
		if !slices.Equal(idsOf(p.Keep), c.keep) || !slices.Equal(idsOf(p.Remove), c.remove) {
			t.Errorf("%+v: keeps %v, removes %v", c.rule, idsOf(p.Keep), idsOf(p.Remove))
		}
	}
	p, err := anna.PlanRetention(page.ID, ids[2], &Retention{Keep: 1}, Retention{}, now)
	noErr(t, err)
	if p.First != ids[0] || !slices.Equal(p.Used, []ID{ids[1]}) {
		t.Errorf("any version names the stack; the text keeps its version: %+v", p)
	}
	for _, bad := range []Retention{{Keep: -1}, {Days: -1}, {Keep: maxRetentionKeep + 1}, {Days: maxRetentionDays + 1}} {
		if _, err := anna.PlanRetention(page.ID, ids[0], &bad, Retention{}, now); !errors.Is(err, ErrValidation) {
			t.Errorf("%+v: %v", bad, err)
		}
	}
}

// TestPictureRuleOverridesTheDefault: a stack's own rule takes precedence
// over the default, outlives the deletion of the first version that holds
// it, and is set only by whoever may edit the item.
func TestPictureRuleOverridesTheDefault(t *testing.T) {
	s, anna, page, ids := drawnFourTimes(t)
	now := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	def := Retention{Keep: 1}
	if err := s.As("ben").SetPictureRetention(page.ID, ids[3], &Retention{Keep: 3}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("a viewer sets a rule: %v", err)
	}
	noErr(t, anna.SetPictureRetention(page.ID, ids[3], &Retention{Keep: 3}))
	noErr(t, anna.SetPictureRetention(page.ID, ids[3], &Retention{Keep: 3})) // unchanged: no event
	p, err := s.As("ben").PlanRetention(page.ID, ids[3], nil, def, now)
	noErr(t, err)
	if !p.Own || p.Rule.Keep != 3 || !slices.Equal(idsOf(p.Remove), []ID{ids[0]}) {
		t.Fatalf("the own rule: %+v", p)
	}
	pics := anna.Pictures("PLAT", func(Attachment) bool { return true })
	if i := slices.IndexFunc(pics, func(p Picture) bool { return p.Attachment.ID == ids[3] }); i < 0 || pics[i].Retention == nil || pics[i].Retention.Keep != 3 {
		t.Errorf("the album tells the stack's rule: %+v", pics)
	}

	done, err := anna.CleanVersions(page.ID, ids[3], nil, def, now)
	noErr(t, err)
	if !slices.Equal(idsOf(done.Remove), []ID{ids[0]}) {
		t.Fatalf("cleaned %+v", done)
	}
	if p, _ := anna.PlanRetention(page.ID, ids[3], nil, def, now); !p.Own || p.Rule.Keep != 3 || len(p.Remove) != 0 {
		t.Errorf("the rule outlives its first version: %+v", p)
	}
	noErr(t, anna.SetPictureRetention(page.ID, ids[3], nil))
	if p, _ := anna.PlanRetention(page.ID, ids[3], nil, def, now); p.Own || !slices.Equal(idsOf(p.Remove), []ID{ids[2]}) {
		t.Errorf("back to the default: %+v", p)
	}
	if _, err := s.As("ben").CleanVersions(page.ID, ids[3], nil, def, now); !errors.Is(err, ErrForbidden) {
		t.Errorf("a viewer cleans up: %v", err)
	}
}

// TestAutomaticRetention: the automatic clean-up applies each stack's
// rule, or the default, as RetentionActor, leaves single files alone and
// skips items on hold.
func TestAutomaticRetention(t *testing.T) {
	s, anna, page, ids := drawnFourTimes(t)
	other := mustItem(t)(anna.Create(NewItem{Space: "PLAT", Type: "page", Title: "Kept"}))
	for i, id := range []ID{"c0000000000000000000000000000001", "c0000000000000000000000000000002"} {
		a := Attachment{ID: id, Name: "kept.drawing.png", Type: "image/png", Size: 3, SHA256: strings.Repeat("ef", 32)}
		if i > 0 {
			a.Source = &Source{Item: other.ID, Attachment: "c0000000000000000000000000000001"}
		}
		_, err := anna.AddAttachment(other.ID, a)
		noErr(t, err)
	}
	noErr(t, anna.SetPictureRetention(other.ID, "c0000000000000000000000000000001", &Retention{}))
	now := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	all := func(Attachment) bool { return true }
	if plans := s.PlanAllRetention(Retention{}, now, all); len(plans) != 0 {
		t.Fatalf("a default without limits removes %v", plans)
	}
	done, err := s.ApplyRetention(Retention{Keep: 1}, now, all)
	noErr(t, err)
	if len(done) != 1 || done[0].Item != page.ID || done[0].First != ids[0] || done[0].Removed != 2 {
		t.Fatalf("cleaned %+v", done)
	}
	last := s.Log()[len(s.Log())-1]
	if last.Kind != AttachmentDeleted || last.Actor != RetentionActor {
		t.Errorf("the clean-up is recorded as %s by %s", last.Kind, last.Actor)
	}
	left, err := anna.Attachments(page.ID)
	noErr(t, err)
	if len(left) != 3 {
		t.Errorf("left %v", idsOf(left))
	}
	if again, _ := s.ApplyRetention(Retention{Keep: 1}, now, all); len(again) != 0 {
		t.Errorf("a second run removes %+v", again)
	}
}
