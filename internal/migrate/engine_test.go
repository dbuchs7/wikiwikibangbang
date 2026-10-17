package migrate

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/dbuchs7/wikiwikibangbang/internal/item"
)

const site = "acme.atlassian.example"

// sample is a small but realistic snapshot: a Jira project with custom
// fields, a workflow, a sub-task and an issue link, and a Confluence space
// with a page tree, a Jira macro, a page link, an info macro and a read
// restriction. Both use the key PLAT and land in the same space.
func sample() Snapshot {
	workflow := SrcWorkflow{
		Initial:  "To Do",
		Statuses: []string{"To Do", "In Progress", "Done"},
		Transitions: []SrcTransition{
			{Name: "Start", From: []string{"To Do"}, To: "In Progress"},
			{Name: "Resolve", From: []string{"To Do", "In Progress"}, To: "Done"},
		},
	}
	return Snapshot{
		Instance: site,
		Users: []SrcUser{
			{ID: "acc-anna", Name: "Anna Muster", Email: "Anna@Acme.example"},
			{ID: "acc-ghost", Name: "Former Employee"},
		},
		Projects: []SrcSpace{{Key: "PLAT", Name: "Platform"}},
		IssueTypes: []SrcIssueType{
			{Name: "Bug", Workflow: workflow, Fields: []SrcField{
				{ID: "customfield_1", Name: "Severity", Kind: "option", Options: []string{"low", "high"}},
				{ID: "customfield_2", Name: "Story Points", Kind: "number"},
				{ID: "duedate", Name: "Due", Kind: "date"},
				{ID: "customfield_3", Name: "Reviewer", Kind: "user"},
				{ID: "customfield_4", Name: "Legacy Script", Kind: "scripted"},
				{ID: "customfield_5", Name: "Title", Kind: "string"},
			}},
			{Name: "Sub-task", Workflow: workflow},
		},
		Issues: []SrcIssue{
			{ID: "10002", Key: "PLAT-13", Project: "PLAT", Type: "Sub-task", Summary: "Write test", Status: "To Do", Parent: "10001", Version: "1"},
			{ID: "10001", Key: "PLAT-12", Project: "PLAT", Type: "Bug", Summary: "Login fails", Status: "In Progress", Version: "1",
				Description: "Steps:\n\nOpen the page.",
				Fields: map[string]any{
					"customfield_1": "high", "customfield_2": 3, "duedate": "2026-11-01",
					"customfield_3": "acc-anna", "customfield_4": "x", "customfield_5": "legacy",
				},
				Links: []SrcLink{{Type: "Blocks", Target: "10003"}},
				Raw:   `{"id":"10001"}`},
			{ID: "10003", Key: "PLAT-20", Project: "PLAT", Type: "Bug", Summary: "Session timeout", Status: "Done", Version: "1"},
		},
		Spaces: []SrcSpace{{Key: "PLAT", Name: "Platform"}},
		Pages: []SrcPage{
			{ID: "2002", Space: "PLAT", Title: "Postmortem", Parent: "2001", Version: "1",
				Body: `<p>See <ac:link><ri:page ri:content-title="Runbook"/></ac:link>.</p><ac:structured-macro ac:name="info"><ac:rich-text-body><p>n</p></ac:rich-text-body></ac:structured-macro>`},
			{ID: "2001", Space: "PLAT", Title: "Runbook", Version: "1",
				Body:          `<h1>Runbook</h1><ac:structured-macro ac:name="jira"><ac:parameter ac:name="key">PLAT-12</ac:parameter></ac:structured-macro><p>Next: <ac:link><ri:page ri:content-title="Postmortem"/></ac:link></p>`,
				ReadAllowList: []string{"acc-anna", "acc-ghost"}},
		},
	}
}

func newStore(t *testing.T) *item.Store {
	t.Helper()
	s := item.NewStore()
	if err := s.DefineType("admin", item.TypeDef{Name: "page", Body: true}); err != nil {
		t.Fatal(err)
	}
	return s
}

func run(t *testing.T, s *item.Store, snap Snapshot, opt Options) Report {
	t.Helper()
	rep, err := Run(s, snap, Derive(snap), opt)
	if err != nil {
		t.Fatal(err)
	}
	return rep
}

func byOrigin(t *testing.T, s *item.Store, system, id string) item.Item {
	t.Helper()
	it, ok := s.FindByOrigin(system, site, id)
	if !ok {
		t.Fatalf("%s %s not imported", system, id)
	}
	return it
}

func hasProblem(rep Report, substr string) bool {
	for _, p := range rep.Problems {
		if strings.Contains(p.Source+" "+p.Message, substr) {
			return true
		}
	}
	return false
}

func TestDryRunWritesNothingAndReports(t *testing.T) {
	s := newStore(t)
	before := len(s.Log())
	rep := run(t, s, sample(), Options{Actor: "import", DryRun: true})
	if len(s.Log()) != before {
		t.Fatal("a dry run must not write to the store")
	}
	if rep.Created != 5 || rep.Updated != 0 {
		t.Fatalf("dry-run counts: %+v", rep)
	}
	for _, want := range []string{"Legacy Script", "acc-ghost", "info", "jira_title"} {
		if !hasProblem(rep, want) {
			t.Errorf("report misses a problem about %q: %+v", want, rep.Problems)
		}
	}
}

func TestImportMapsJiraAndConfluence(t *testing.T) {
	s := newStore(t)
	rep := run(t, s, sample(), Options{Actor: "import"})
	if rep.Created != 5 {
		t.Fatalf("created = %d, report %+v", rep.Created, rep)
	}

	bug := byOrigin(t, s, "jira", "10001")
	if bug.Key != "PLAT-12" || bug.Type != "bug" || bug.State != "In Progress" || bug.Title != "Login fails" {
		t.Fatalf("bug = %+v", bug)
	}
	due := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	if bug.Properties["severity"] != "high" || bug.Properties["story_points"] != 3.0 ||
		!bug.Properties["due"].(time.Time).Equal(due) || bug.Properties["reviewer"] != "anna@acme.example" {
		t.Fatalf("bug properties = %v", bug.Properties)
	}
	if _, ok := bug.Properties["legacy_script"]; ok {
		t.Fatal("an unmappable field must not be imported as a property")
	}
	if bug.Properties["jira_title"] != "legacy" {
		t.Fatalf("a field named like a reserved name must be renamed: %v", bug.Properties)
	}
	if bug.Origin == nil || bug.Origin.Raw != `{"id":"10001"}` || bug.Origin.ExternalKey != "PLAT-12" {
		t.Fatalf("origin = %+v", bug.Origin)
	}
	if len(bug.Body.Blocks) != 2 || bug.Body.Blocks[1].Text != "Open the page." {
		t.Fatalf("description = %+v", bug.Body)
	}
	sub := byOrigin(t, s, "jira", "10002")
	if sub.Parent != bug.ID || sub.Key != "PLAT-13" {
		t.Fatalf("sub-task = %+v", sub)
	}
	blocked := byOrigin(t, s, "jira", "10003")
	if rels := s.Relations(bug.ID, item.Outgoing); len(rels) != 1 || rels[0].Type != "blocks" || rels[0].To != blocked.ID {
		t.Fatalf("issue link = %+v", rels)
	}

	runbook := byOrigin(t, s, "confluence", "2001")
	post := byOrigin(t, s, "confluence", "2002")
	if post.Parent != runbook.ID || runbook.Space != "PLAT" {
		t.Fatalf("page tree: runbook=%+v post=%+v", runbook, post)
	}
	// The Jira macro became an embed of the imported issue: the cross-tool
	// link is now native.
	if b := runbook.Body.Blocks; len(b) != 3 || b[1].Kind != item.Embed || b[1].Ref != bug.ID || b[2].Mentions[0] != post.ID {
		t.Fatalf("runbook body = %+v", runbook.Body)
	}
	if post.Body.Blocks[1].Kind != item.Unsupported {
		t.Fatalf("info macro must be kept as unsupported: %+v", post.Body)
	}
	// Restrictions: mapped users and a placeholder for the unmapped one —
	// never wider than in the source.
	if r := runbook.Restriction.Read; len(r) != 2 || r[0] != "anna@acme.example" || r[1] != "external:acc-ghost" {
		t.Fatalf("restriction = %v", runbook.Restriction)
	}
	// Jira project and Confluence space share PLAT: the pages took PLAT-21
	// and PLAT-22 after the highest imported issue key, PLAT-20.
	if runbook.Key != "PLAT-21" || post.Key != "PLAT-22" {
		t.Fatalf("page keys = %s, %s", runbook.Key, post.Key)
	}
	next, err := s.Create("anna", item.NewItem{Space: "PLAT", Type: "page", Title: "new"})
	if err != nil || next.Key != "PLAT-23" {
		t.Fatalf("next key = %s (%v), want PLAT-23", next.Key, err)
	}
}

func TestRerunIsIdempotentAndUpdatesChanges(t *testing.T) {
	s := newStore(t)
	run(t, s, sample(), Options{Actor: "import"})
	before := len(s.Log())
	rep := run(t, s, sample(), Options{Actor: "import"})
	if rep.Created != 0 || rep.Updated != 0 || rep.Unchanged != 5 || len(s.Log()) != before {
		t.Fatalf("re-run: %+v, %d new events", rep, len(s.Log())-before)
	}

	snap := sample()
	snap.Issues[1].Summary, snap.Issues[1].Version = "Login fails on Safari", "2"
	snap.Pages[1].Body = `<p>Rewritten</p>`
	rep = run(t, s, snap, Options{Actor: "import"})
	if rep.Updated != 2 || rep.Created != 0 {
		t.Fatalf("update run: %+v", rep)
	}
	if got := byOrigin(t, s, "jira", "10001"); got.Title != "Login fails on Safari" {
		t.Fatalf("title not updated: %q", got.Title)
	}
	if got := byOrigin(t, s, "confluence", "2001"); len(got.Body.Blocks) != 1 || got.Body.Blocks[0].Text != "Rewritten" {
		t.Fatalf("body not updated: %+v", got.Body)
	}
}

func TestRemovedInSourceIsReportedNotDeleted(t *testing.T) {
	s := newStore(t)
	run(t, s, sample(), Options{Actor: "import"})
	snap := sample()
	snap.Issues = snap.Issues[:2] // PLAT-20 removed in the source
	snap.Issues[1].Links = nil
	rep := run(t, s, snap, Options{Actor: "import"})
	if !hasProblem(rep, "PLAT-20") || !hasProblem(rep, "removed in the source") {
		t.Fatalf("removal not reported: %+v", rep.Problems)
	}
	if it := byOrigin(t, s, "jira", "10003"); it.Presence != item.Live {
		t.Fatal("an import never deletes (D8)")
	}
}

func TestSyncModeKeepsItemsReadOnlyUntilCutOver(t *testing.T) {
	s := newStore(t)
	if err := s.GrantRole("admin", "PLAT", "anna@acme.example", item.Editor); !errors.Is(err, item.ErrNotFound) {
		t.Fatalf("space does not exist before the import: %v", err)
	}
	run(t, s, sample(), Options{Actor: "import", Managed: "sync:" + site})
	if err := s.GrantRole("admin", "PLAT", "anna@acme.example", item.Editor); err != nil {
		t.Fatal(err)
	}
	bug := byOrigin(t, s, "jira", "10001")
	if err := s.As("anna@acme.example").SetTitle(bug.ID, "local"); !errors.Is(err, item.ErrForbidden) {
		t.Fatalf("edit during sync: err = %v, want ErrForbidden", err)
	}
	if err := s.Release("admin", "sync:"+site, "PLAT"); err != nil {
		t.Fatal(err)
	}
	if err := s.As("anna@acme.example").SetTitle(bug.ID, "ours now"); err != nil {
		t.Fatalf("edit after cut-over: %v", err)
	}
}

func TestRefusedItemsAreReported(t *testing.T) {
	s := newStore(t)
	snap := sample()
	snap.Issues[1].Status = "Waiting for customer" // not in the workflow
	snap.Issues[1].Fields["customfield_1"] = "urgent"
	rep := run(t, s, snap, Options{Actor: "import"})
	if !hasProblem(rep, "PLAT-12") {
		t.Fatalf("refused issue not reported: %+v", rep.Problems)
	}
	if _, ok := s.FindByOrigin("jira", site, "10001"); ok {
		t.Fatal("a refused issue must not be imported half")
	}
	// Its sub-task cannot find its parent and is reported too.
	if !hasProblem(rep, "PLAT-13") {
		t.Fatalf("orphaned sub-task not reported: %+v", rep.Problems)
	}
}

// TestKeyCollisionDuringSyncIsReported: while a sync runs, a key the source
// assigns later may already be taken in the shared space. The issue is
// imported under a new key, its source key stays in the origin, and the
// report says so.
func TestKeyCollisionDuringSyncIsReported(t *testing.T) {
	s := newStore(t)
	run(t, s, sample(), Options{Actor: "import"})
	snap := sample()
	snap.Issues = append(snap.Issues, SrcIssue{ID: "10009", Key: "PLAT-21", Project: "PLAT", Type: "Bug", Summary: "New in Jira", Status: "To Do", Version: "1"})
	rep := run(t, s, snap, Options{Actor: "import"})
	it := byOrigin(t, s, "jira", "10009")
	if it.Key == "PLAT-21" || it.Origin.ExternalKey != "PLAT-21" {
		t.Fatalf("collided issue: key=%s origin=%+v", it.Key, it.Origin)
	}
	if !hasProblem(rep, "PLAT-21 was taken") {
		t.Fatalf("collision not reported: %+v", rep.Problems)
	}
}
