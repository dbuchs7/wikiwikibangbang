package item

import (
	"errors"
	"testing"
)

func jiraRef(id, key string) Origin {
	return Origin{System: "jira", Instance: "acme.example", ExternalID: id, ExternalKey: key, Version: "1", Raw: `{"id":"` + id + `"}`}
}

func TestUpsertCreatesThenUpdatesByExternalID(t *testing.T) {
	s := setup(t)
	u := Upsert{Origin: jiraRef("10001", "PLAT-7"), Space: "PLAT", Type: "task", Title: "Patch DB",
		Properties: Props{"priority": "high"}, State: "in progress", Key: "PLAT-7"}

	it, changed, err := s.Upsert("import", u)
	if err != nil || !changed {
		t.Fatalf("first upsert: changed=%v err=%v", changed, err)
	}
	if it.Key != "PLAT-7" || it.State != "in progress" || it.Origin.ExternalID != "10001" {
		t.Fatalf("imported item = %+v", it)
	}
	// The space counter continues above the highest imported number.
	next := mustItem(t)(s.Create("anna", NewItem{Space: "PLAT", Type: "page", Title: "after import"}))
	if next.Key != "PLAT-8" {
		t.Fatalf("next key = %s, want PLAT-8", next.Key)
	}

	// Same data again: no change, no event.
	before := len(s.Log())
	again, changed, err := s.Upsert("import", u)
	if err != nil || changed || again.ID != it.ID || len(s.Log()) != before {
		t.Fatalf("re-run: changed=%v err=%v id=%s events+%d", changed, err, again.ID, len(s.Log())-before)
	}

	// A source change updates the same item.
	u.Title, u.State, u.Origin.Version = "Patch DB now", "done", "2"
	upd, changed, err := s.Upsert("import", u)
	if err != nil || !changed || upd.ID != it.ID || upd.Title != "Patch DB now" || upd.State != "done" {
		t.Fatalf("update: %+v changed=%v err=%v", upd, changed, err)
	}
	if got, ok := s.FindByOrigin("jira", "acme.example", "10001"); !ok || got.ID != it.ID {
		t.Fatal("FindByOrigin must resolve the imported item")
	}
	if _, ok := s.FindByOrigin("jira", "other.example", "10001"); ok {
		t.Fatal("the instance is part of the identity")
	}
	h := s.History(it.ID)
	if h[0].Kind != ItemImported || h[len(h)-1].Kind != ItemImported {
		t.Fatalf("history kinds = %v", h)
	}
}

func TestUpsertValidates(t *testing.T) {
	s := setup(t)
	base := Upsert{Origin: jiraRef("1", "PLAT-1"), Space: "PLAT", Type: "task", Title: "t", Properties: Props{"priority": "low"}}
	cases := map[string]func(u *Upsert){
		"no origin":        func(u *Upsert) { u.Origin = Origin{} },
		"unknown state":    func(u *Upsert) { u.State = "blocked" },
		"state on no-wf":   func(u *Upsert) { u.Type, u.State, u.Properties = "page", "open", nil },
		"missing required": func(u *Upsert) { u.Properties = nil },
		"empty title":      func(u *Upsert) { u.Title = "" },
	}
	for name, mutate := range cases {
		u := base
		mutate(&u)
		if _, _, err := s.Upsert("import", u); !errors.Is(err, ErrValidation) {
			t.Errorf("%s: err = %v, want ErrValidation", name, err)
		}
	}
	u := base
	u.Space = "NOPE"
	if _, _, err := s.Upsert("import", u); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown space: err = %v", err)
	}
	if len(s.Find(Query{IncludeDeleted: true})) != 0 {
		t.Fatal("refused upserts must not create items")
	}
}

func TestKeyPreservationFallsBack(t *testing.T) {
	s := setup(t)
	taken := mustItem(t)(s.Create("anna", NewItem{Space: "PLAT", Type: "page", Title: "first"})) // PLAT-1
	for name, key := range map[string]string{"taken": taken.Key, "other prefix": "OPS-5", "malformed": "PLAT-x"} {
		it, _, err := s.Upsert("import", Upsert{Origin: jiraRef(name, key), Space: "PLAT", Type: "page", Title: name, Key: key})
		if err != nil {
			t.Fatal(err)
		}
		if it.Key == key {
			t.Errorf("%s: key %s must not be granted", name, key)
		}
	}
}

func TestUpsertMovesWithinSpaceAndRefusesSpaceChange(t *testing.T) {
	s := setup(t)
	parent, _, _ := s.Upsert("import", Upsert{Origin: jiraRef("p", "PLAT-1"), Space: "PLAT", Type: "page", Title: "parent"})
	u := Upsert{Origin: jiraRef("c", "PLAT-2"), Space: "PLAT", Type: "page", Title: "child"}
	child, _, _ := s.Upsert("import", u)
	u.Parent = parent.ID
	moved, changed, err := s.Upsert("import", u)
	if err != nil || !changed || moved.Parent != parent.ID || moved.ID != child.ID {
		t.Fatalf("re-parent by upsert: %+v %v %v", moved, changed, err)
	}
	u.Space, u.Parent = "OPS", ""
	if _, _, err := s.Upsert("import", u); !errors.Is(err, ErrValidation) {
		t.Fatalf("space change by upsert: err = %v, want ErrValidation", err)
	}
}

func TestManagedItemsAreReadOnlyUntilReleased(t *testing.T) {
	s := permFixture(t)
	it, _, err := s.Upsert("import", Upsert{Origin: jiraRef("9", "PLAT-9"), Space: "PLAT", Type: "page", Title: "synced", Managed: "sync:acme"})
	if err != nil {
		t.Fatal(err)
	}
	anna := s.As("anna")
	if err := anna.SetTitle(it.ID, "local edit"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("edit of a synced item: err = %v, want ErrForbidden", err)
	}
	if _, err := anna.CreateEmbedded(it.ID, NewItem{Type: "page", Title: "x"}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("inline creation in a synced item: err = %v", err)
	}
	if _, err := anna.Get(it.ID); err != nil {
		t.Fatalf("synced items stay readable: %v", err)
	}
	// The sync itself keeps updating it.
	if _, changed, err := s.Upsert("import", Upsert{Origin: Origin{System: "jira", Instance: "acme.example", ExternalID: "9", Version: "2"}, Space: "PLAT", Type: "page", Title: "synced v2", Managed: "sync:acme"}); err != nil || !changed {
		t.Fatalf("sync update: %v %v", changed, err)
	}
	if err := s.Release("admin", "sync:acme", "PLAT"); err != nil {
		t.Fatal(err)
	}
	noErr(t, anna.SetTitle(it.ID, "now ours"))
	if err := s.Release("admin", "", "PLAT"); !errors.Is(err, ErrValidation) {
		t.Fatalf("release without tag: err = %v", err)
	}
}

func TestPurgeAndRedactRemoveRawSource(t *testing.T) {
	s := setup(t)
	it, _, _ := s.Upsert("import", Upsert{Origin: jiraRef("r", "PLAT-3"), Space: "PLAT", Type: "page", Title: "with raw"})
	noErr(t, s.Redact("erin", it.ID, "personal data"))
	got := mustItem(t)(s.Get(it.ID))
	if got.Origin == nil || got.Origin.Raw != "" || got.Origin.ExternalID != "r" {
		t.Fatalf("redaction must drop the raw source but keep the identity: %+v", got.Origin)
	}
	// A tombstone is not resurrected by a later import run.
	if _, _, err := s.Upsert("import", Upsert{Origin: jiraRef("r", "PLAT-3"), Space: "PLAT", Type: "page", Title: "with raw"}); !errors.Is(err, ErrValidation) {
		t.Fatalf("upsert onto a tombstone: err = %v, want ErrValidation", err)
	}
}

func TestReplayWithImports(t *testing.T) {
	s := setup(t)
	_, _, _ = s.Upsert("import", Upsert{Origin: jiraRef("1", "PLAT-41"), Space: "PLAT", Type: "task", Title: "a", Properties: Props{"priority": "low"}, State: "done", Key: "PLAT-41", Managed: "sync:x"})
	noErr(t, s.Release("admin", "sync:x", "PLAT"))
	mustItem(t)(s.Create("anna", NewItem{Space: "PLAT", Type: "page", Title: "b"}))
	r, err := Replay(s.Log())
	if err != nil {
		t.Fatal(err)
	}
	if got, want := snapshot(r), snapshot(s); got != want {
		t.Fatalf("replayed state differs\n got: %s\nwant: %s", got, want)
	}
}

func TestUpsertRefusals(t *testing.T) {
	s := setup(t)
	other := mustItem(t)(s.Create("anna", NewItem{Space: "OPS", Type: "page", Title: "ops"}))
	p, _, _ := s.Upsert("import", Upsert{Origin: jiraRef("p", ""), Space: "PLAT", Type: "page", Title: "p"})
	c, _, _ := s.Upsert("import", Upsert{Origin: jiraRef("c", ""), Space: "PLAT", Type: "page", Title: "c", Parent: p.ID})
	checks := map[string]struct {
		u    Upsert
		want error
	}{
		"unknown type":           {Upsert{Origin: jiraRef("x", ""), Space: "PLAT", Type: "nope", Title: "x"}, ErrNotFound},
		"unknown parent":         {Upsert{Origin: jiraRef("x", ""), Space: "PLAT", Type: "page", Title: "x", Parent: "missing"}, ErrNotFound},
		"parent in other space":  {Upsert{Origin: jiraRef("x", ""), Space: "PLAT", Type: "page", Title: "x", Parent: other.ID}, ErrValidation},
		"type change":            {Upsert{Origin: jiraRef("p", ""), Space: "PLAT", Type: "decision", Title: "p", Properties: Props{"deciders": "x"}}, ErrValidation},
		"move below own subtree": {Upsert{Origin: jiraRef("p", ""), Space: "PLAT", Type: "page", Title: "p", Parent: c.ID}, ErrValidation},
	}
	for name, ch := range checks {
		if _, _, err := s.Upsert("import", ch.u); !errors.Is(err, ch.want) {
			t.Errorf("%s: err = %v, want %v", name, err, ch.want)
		}
	}
	noErr(t, s.Trash("anna", c.ID))
	if _, _, err := s.Upsert("import", Upsert{Origin: jiraRef("c", ""), Space: "PLAT", Type: "page", Title: "c"}); !errors.Is(err, ErrValidation) {
		t.Errorf("upsert onto a trashed item: err = %v", err)
	}
	if err := s.Release("admin", "sync:x", "NOPE"); !errors.Is(err, ErrNotFound) {
		t.Errorf("release in unknown space: err = %v", err)
	}
}

// TestARestoreKeepsTheIdentity covers ADR-0124: a restored item comes back
// with its ID, which the store must not know in any presence (D7), and
// its protected passages come back with theirs.
func TestARestoreKeepsTheIdentity(t *testing.T) {
	s := setup(t)
	const id = ID("0123456789abcdef0123456789abcdef")
	u := Upsert{Origin: Origin{System: "backup", Instance: "b1", ExternalID: string(id)}, ID: id, Space: "PLAT", Type: "page", Title: "Back"}
	it, changed, err := s.Upsert("restore", u)
	noErr(t, err)
	if it.ID != id || !changed {
		t.Fatalf("restored as %s", it.ID)
	}
	u.Origin.ExternalID = "other"
	if _, _, err := s.Upsert("restore", u); !errors.Is(err, ErrValidation) {
		t.Fatalf("an ID used again: %v", err)
	}
	u.ID = "not an id"
	if _, _, err := s.Upsert("restore", u); !errors.Is(err, ErrValidation) {
		t.Fatalf("a bad ID: %v", err)
	}

	noErr(t, s.CommitBody("anna", id, Doc{Blocks: []Block{{Kind: Paragraph, Text: "Paul Muster"}}}))
	sec := Secret{ID: "fedcba9876543210fedcba9876543210", By: "anna", Readers: []string{"@hr", "@hr"}}
	noErr(t, s.RestoreSecrets("restore", id, []Secret{sec}))
	noErr(t, s.RestoreSecrets("restore", id, nil))
	if got, _ := s.Get(id); len(got.Secrets) != 1 || got.Secrets[0].ID != sec.ID || got.Secrets[0].By != "anna" || len(got.Secrets[0].Readers) != 1 {
		t.Fatalf("passages %+v", got.Secrets)
	}
	if err := s.RestoreSecrets("restore", id, []Secret{{ID: "a", By: "anna", Readers: []string{"two words"}}}); !errors.Is(err, ErrValidation) {
		t.Fatalf("a bad reader: %v", err)
	}
	if err := s.RestoreSecrets("restore", "missing", []Secret{sec}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a missing item: %v", err)
	}
}

func TestSpaceState(t *testing.T) {
	s := setup(t)
	noErr(t, s.CreateSpace("root", "OLD", "Old"))
	noErr(t, s.CreateSpace("root", "BIN", "Bin"))
	noErr(t, s.TrashSpace("root", "OLD"))
	noErr(t, s.TrashSpace("root", "BIN"))
	noErr(t, s.PurgeSpace("root", "BIN"))
	for key, want := range map[string]string{"PLAT": "live", "OLD": "trashed", "BIN": "purged", "NONE": ""} {
		if got, known := s.SpaceState(key); got != want || known != (want != "") {
			t.Errorf("%s: %q %v", key, got, known)
		}
	}
}

// TestImportSetsStateSince: an import sets the time an item reached its
// state when it changes the state, not otherwise (ADR-0133).
func TestImportSetsStateSince(t *testing.T) {
	s := setup(t)
	u := Upsert{Origin: jiraRef("10001", "PLAT-70"), Space: "PLAT", Type: "task", Title: "Imported",
		Properties: Props{"priority": "high"}, State: "in progress", Key: "PLAT-70"}
	imp, _, err := s.Upsert("import", u)
	noErr(t, err)
	first := imp.StateSince
	u.Title, u.Origin.Version = "Imported again", "2"
	imp, _, err = s.Upsert("import", u)
	noErr(t, err)
	if !imp.StateSince.Equal(first) {
		t.Fatalf("an import that keeps the state keeps the time: %v, first %v", imp.StateSince, first)
	}
	u.State, u.Origin.Version = "done", "3"
	imp, _, err = s.Upsert("import", u)
	noErr(t, err)
	if !imp.StateSince.After(first) {
		t.Fatalf("an import that changes the state sets the time: %v, first %v", imp.StateSince, first)
	}
}
