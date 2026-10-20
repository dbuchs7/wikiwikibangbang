package item

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"
)

// lastRestriction returns the latest RestrictionSet event of an item.
func lastRestriction(s *Store, id ID) (Event, bool) {
	h := s.History(id)
	for i := len(h) - 1; i >= 0; i-- {
		if h[i].Kind == RestrictionSet {
			return h[i], true
		}
	}
	return Event{}, false
}

func TestSensitiveContentRestrictsTheItemAtOnce(t *testing.T) {
	s := permFixture(t)
	anna, ben, pat := s.As("anna"), s.As("ben"), s.As("pat")
	page := mustItem(t)(anna.Create(NewItem{Space: "PLAT", Type: "page", Title: "VPN"}))
	if _, err := ben.Get(page.ID); err != nil {
		t.Fatal("ben cannot read the page before")
	}
	noErr(t, anna.CommitBody(page.ID, Doc{Blocks: []Block{{Kind: Paragraph, Text: "password: Sommer2026!"}}}))

	got, _ := s.Get(page.ID)
	if !slices.Equal(got.Restriction.Read, []string{"anna"}) {
		t.Fatalf("restriction %+v, want read by anna only", got.Restriction)
	}
	e, _ := lastRestriction(s, page.ID)
	if e.Actor != GovernanceActor {
		t.Fatalf("restricted by %q, want %q", e.Actor, GovernanceActor)
	}
	// The restriction is part of the same batch as the content: nobody
	// could read the secret in between.
	commit := s.Log()[len(s.Log())-2]
	if commit.Kind != BodyCommitted || commit.Batch == 0 || commit.Batch != e.Batch {
		t.Fatalf("content and restriction are not one batch: %+v / %+v", commit, e)
	}
	if _, err := ben.Get(page.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("ben still reads the page: %v", err)
	}
	if _, err := pat.Get(page.ID); err != nil {
		t.Fatalf("the space admin cannot read it: %v", err)
	}

	// An admin lifts the restriction after checking; the secret already in
	// the content does not lock it again on the next save.
	noErr(t, pat.Restrict(page.ID, Restriction{}))
	noErr(t, anna.SetTitle(page.ID, "VPN access"))
	noErr(t, anna.CommitBody(page.ID, Doc{Blocks: []Block{{Kind: Heading, Text: "Access"}, {Kind: Paragraph, Text: "password: Sommer2026!"}}}))
	if got, _ := s.Get(page.ID); len(got.Restriction.Read) != 0 {
		t.Fatalf("an old finding restricted again: %+v", got.Restriction)
	}
	// A new secret does.
	noErr(t, anna.SetTitle(page.ID, "token ghp_abcdefghijklmnopqrstuvwxyz0123456789"))
	if got, _ := s.Get(page.ID); !slices.Equal(got.Restriction.Read, []string{"anna"}) {
		t.Fatalf("a new finding did not restrict: %+v", got.Restriction)
	}
}

func TestEveryKindOfContentIsGuarded(t *testing.T) {
	cases := map[string]func(s *Store, id ID) error{
		"create": nil,
		"property": func(s *Store, id ID) error {
			return s.As("anna").SetProperty(id, "assignee", "pwd=Tr0ub4dor&3")
		},
		"comment": func(s *Store, id ID) error {
			_, err := s.As("anna").AddComment(id, "Karte 4111 1111 1111 1111")
			return err
		},
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			s := permFixture(t)
			title := "Patch"
			if change == nil {
				title = "IBAN CH93 0076 2011 6238 5295 7"
			}
			task := mustItem(t)(s.As("anna").Create(NewItem{Space: "PLAT", Type: "task", Title: title, Properties: Props{"priority": "low"}}))
			if change != nil {
				noErr(t, change(s, task.ID))
			}
			if got, _ := s.Get(task.ID); !slices.Equal(got.Restriction.Read, []string{"anna"}) {
				t.Fatalf("restriction %+v", got.Restriction)
			}
		})
	}
}

func TestOrdinaryContentIsNotRestricted(t *testing.T) {
	s := permFixture(t)
	page := mustItem(t)(s.As("anna").Create(NewItem{Space: "PLAT", Type: "page", Title: "Reset your password"}))
	noErr(t, s.As("anna").CommitBody(page.ID, Doc{Blocks: []Block{{Kind: Paragraph, Text: "Passwort: siehe Tresor"}}}))
	if got, _ := s.Get(page.ID); len(got.Restriction.Read) != 0 {
		t.Fatalf("restricted without a finding: %+v", got.Restriction)
	}
}

func TestGovernanceReport(t *testing.T) {
	s := permFixture(t)
	noErr(t, s.DefineType("root", TypeDef{Name: "ticket", Body: true,
		Fields: []FieldDef{{Name: "due", Kind: KindDate}, {Name: "note", Kind: KindText}},
		Workflow: &Workflow{Initial: "open", States: []string{"open", "closed"},
			Transitions: []Transition{{Name: "close", From: []string{"open"}, To: "closed"}}}}))
	anna, ben, pat := s.As("anna"), s.As("ben"), s.As("pat")

	secret := mustItem(t)(anna.Create(NewItem{Space: "PLAT", Type: "page", Title: "VPN"}))
	noErr(t, anna.CommitBody(secret.ID, Doc{Blocks: []Block{{Kind: Paragraph, Text: "intro"}, {Kind: Paragraph, Text: "password: Sommer2026!"}}}))
	empty := mustItem(t)(anna.Create(NewItem{Space: "PLAT", Type: "page", Title: "Empty page"}))
	past := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	overdue := mustItem(t)(anna.Create(NewItem{Space: "PLAT", Type: "ticket", Title: "Renew certificate", Properties: Props{"due": past}}))
	closed := mustItem(t)(anna.Create(NewItem{Space: "PLAT", Type: "ticket", Title: "Done already", Properties: Props{"due": past}}))
	noErr(t, anna.Transition(closed.ID, "close"))
	// A new required field makes existing tickets incomplete.
	noErr(t, s.DefineType("root", TypeDef{Name: "ticket", Body: true,
		Fields: []FieldDef{{Name: "due", Kind: KindDate}, {Name: "note", Kind: KindText}, {Name: "owner", Kind: KindUser, Required: true}},
		Workflow: &Workflow{Initial: "open", States: []string{"open", "closed"},
			Transitions: []Transition{{Name: "close", From: []string{"open"}, To: "closed"}}}}))

	now := time.Date(2027, 12, 1, 0, 0, 0, 0, time.UTC) // over a year after every change
	rep, err := pat.Governance("PLAT", now, 365*24*time.Hour)
	noErr(t, err)

	if len(rep.Sensitive) != 1 {
		t.Fatalf("sensitive %+v", rep.Sensitive)
	}
	f := rep.Sensitive[0]
	if f.Item != secret.ID || f.Where != "block 2" || f.Kind != "password" || f.Responsible != "anna" || !f.Restricted ||
		strings.Contains(f.Masked, "Sommer2026") {
		t.Errorf("sensitive finding %+v", f)
	}
	if len(rep.Stale) != 4 {
		t.Errorf("stale %d, want all 4 items", len(rep.Stale))
	}
	problems := map[ID][]string{}
	for _, p := range rep.Implausible {
		problems[p.Item] = append(problems[p.Item], p.Problem)
	}
	if want := []string{"the body is empty"}; !slices.Equal(problems[empty.ID], want) {
		t.Errorf("empty page: %v", problems[empty.ID])
	}
	if got := problems[overdue.ID]; len(got) != 2 || !strings.Contains(got[0], "due") || !strings.Contains(got[1], "owner") {
		t.Errorf("overdue ticket: %v", got)
	}
	if got := problems[closed.ID]; len(got) != 1 || !strings.Contains(got[0], "owner") {
		t.Errorf("a closed ticket is not overdue: %v", got)
	}

	// ben reads the space but not the restricted page: its finding is not
	// his to see (D10).
	rep, err = ben.Governance("PLAT", now, 365*24*time.Hour)
	noErr(t, err)
	if len(rep.Sensitive) != 0 || len(rep.Stale) != 3 {
		t.Errorf("ben sees %d findings and %d stale items", len(rep.Sensitive), len(rep.Stale))
	}
	if _, err := s.As("carl").Governance("PLAT", now, time.Hour); !errors.Is(err, ErrNotFound) {
		t.Errorf("outsider: %v", err)
	}
}

// TestGovernanceRestrictionsAreNotified: whoever saved sensitive data and
// the space's administrators hear of the restriction without watching the
// item; others do not, and nobody hears of a change they may not read
// (ADR-0057).
func TestGovernanceRestrictionsAreNotified(t *testing.T) {
	s := permFixture(t)
	anna, ben, pat := s.As("anna"), s.As("ben"), s.As("pat")
	page := mustItem(t)(anna.Create(NewItem{Space: "PLAT", Type: "page", Title: "Runbook"}))
	noErr(t, anna.CommitBody(page.ID, Doc{Blocks: []Block{{Kind: Paragraph, Text: "Passwort: Sommer2026!"}}}))
	told := func(x *Session) []EventKind {
		var kinds []EventKind
		list, _ := x.Notifications(0, 10)
		for _, n := range list {
			if n.Item == page.ID {
				kinds = append(kinds, n.Kind)
			}
		}
		return kinds
	}
	if got := told(anna); len(got) != 1 || got[0] != RestrictionSet {
		t.Errorf("the one who saved hears %v", got)
	}
	if got := told(pat); len(got) != 1 || got[0] != RestrictionSet {
		t.Errorf("the space administrator hears %v", got)
	}
	if got := told(ben); len(got) != 0 {
		t.Errorf("a reader the item is now hidden from hears %v", got)
	}
	if anna.Unread(0, 10) != 1 {
		t.Errorf("unread for the one who saved: %d", anna.Unread(0, 10))
	}
	// The one who saved is responsible for the finding.
	if f := anna.Findings(time.Now(), 365*24*time.Hour); len(f.Sensitive) != 1 || len(pat.Findings(time.Now(), 365*24*time.Hour).Sensitive) != 0 {
		t.Errorf("findings: anna %+v", f)
	}
}
