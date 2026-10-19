package item

import (
	"errors"
	"slices"
	"testing"
)

func TestLabels(t *testing.T) {
	s := permFixture(t)
	anna, ben, carl := s.As("anna"), s.As("ben"), s.As("carl")
	a := mustItem(t)(anna.Create(NewItem{Space: "PLAT", Type: "page", Title: "Runbook"}))
	b := mustItem(t)(anna.Create(NewItem{Space: "PLAT", Type: "page", Title: "Postmortem"}))
	secret := mustItem(t)(anna.Create(NewItem{Space: "PLAT", Type: "page", Title: "Secret"}))
	noErr(t, anna.SetLabels(a.ID, []string{" Ops ", "backup", "ops", "Disaster Recovery"}))
	if got := mustItem(t)(anna.Get(a.ID)).Labels; !slices.Equal(got, []string{"backup", "disaster-recovery", "ops"}) {
		t.Fatalf("labels %v, want normalised, sorted and unique", got)
	}
	version := mustItem(t)(anna.Get(a.ID)).Version
	noErr(t, anna.SetLabels(a.ID, []string{"ops", "backup", "disaster-recovery"}))
	if mustItem(t)(anna.Get(a.ID)).Version != version {
		t.Error("setting the same labels recorded a change")
	}
	noErr(t, anna.SetLabels(b.ID, []string{"ops"}))
	noErr(t, anna.SetLabels(secret.ID, []string{"ops", "hidden"}))
	noErr(t, anna.Restrict(secret.ID, Restriction{Read: []string{"anna"}}))

	for _, bad := range []string{"", "-x", "x-", "a b\tc!", "ünïcode✓"} {
		if err := anna.SetLabels(a.ID, []string{bad}); !errors.Is(err, ErrValidation) {
			t.Errorf("label %q: %v", bad, err)
		}
	}
	if err := ben.SetLabels(a.ID, []string{"x"}); !errors.Is(err, ErrForbidden) {
		t.Errorf("a viewer labels: %v", err)
	}
	// Counting and finding see only what one may read.
	if got := ben.Labels("PLAT"); !slices.Equal(got, []LabelCount{{"ops", 2}, {"backup", 1}, {"disaster-recovery", 1}}) {
		t.Errorf("ben counts %v", got)
	}
	if got := anna.Labels(""); got[0] != (LabelCount{"ops", 3}) || !slices.Contains(got, LabelCount{"hidden", 1}) {
		t.Errorf("anna counts %v", got)
	}
	if got := carl.Labels(""); len(got) != 0 {
		t.Errorf("an outsider counts %v", got)
	}
	if got := ids(ben.Find(Query{Label: "ops"})); len(got) != 2 {
		t.Errorf("ben finds %v", got)
	}
	hits, err := ben.Search("runbook", Query{Label: "backup"})
	if err != nil || len(hits) != 1 {
		t.Errorf("search with a label: %v, %v", hits, err)
	}
	// Labels are content: governance reads them, and purge erases them.
	noErr(t, anna.SetLabels(b.ID, []string{"4111111111111111"}))
	if got := mustItem(t)(anna.Get(b.ID)).Restriction.Read; !slices.Equal(got, []string{"anna"}) {
		t.Errorf("a card number as a label did not restrict the item: %v", got)
	}
	noErr(t, anna.Trash(a.ID))
	noErr(t, s.Purge("pat", a.ID))
	if got := mustItem(t)(s.Get(a.ID)).Labels; got != nil {
		t.Errorf("a purged item keeps labels %v", got)
	}
	for _, e := range s.History(a.ID) {
		if e.Kind == LabelsSet && (len(e.Labels) > 0 || !e.Erased) {
			t.Errorf("history keeps the labels of a purged item: %+v", e)
		}
	}
}
