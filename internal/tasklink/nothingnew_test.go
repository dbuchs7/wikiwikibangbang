package tasklink

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/dbuchs7/orbithek/internal/diskspace"
	"github.com/dbuchs7/orbithek/internal/engine"
)

// While the storage is almost full, a round links no new task but keeps
// the linked ones in step, without calling it a problem, and the log says
// so once; with room again, the next round links the new tasks (ADR-0163).
func TestARoundLinksNothingNewWhileTheStorageIsAlmostFull(t *testing.T) {
	f := setup(t)
	var logged bytes.Buffer
	f.r.log = slog.New(slog.NewTextHandler(&logged, nil))
	ctx := context.Background()
	f.l.tasks = []engine.Task{holidayTask(1)}
	f.r.Tick(ctx)
	if len(f.linked(t)) != 1 {
		t.Fatal("not linked")
	}

	f.db.SetSpace(diskspace.Critical)
	renamed := holidayTask(1)
	renamed.Name = "Urlaub genehmigen (Ben)"
	f.l.tasks = []engine.Task{renamed, holidayTask(2)}
	for range 2 {
		f.r.Tick(ctx)
		rep, _ := f.r.Last("atlas-prod")
		if rep.Paused != 1 || rep.Linked != 0 || strings.Contains(strings.Join(rep.Problems, "\n"), "storage") {
			t.Fatalf("round when critical = %+v", rep)
		}
	}
	if items := f.linked(t); len(items) != 1 || items[0].Title != "Urlaub genehmigen (Ben)" {
		t.Fatalf("linked when critical = %+v", items)
	}
	if n := strings.Count(logged.String(), "not linked while the storage is almost full"); n != 1 {
		t.Errorf("logged %d times:\n%s", n, logged.String())
	}

	f.db.SetSpace(diskspace.OK)
	f.r.Tick(ctx)
	if rep, _ := f.r.Last("atlas-prod"); rep.Linked != 1 || rep.Paused != 0 || len(f.linked(t)) != 2 {
		t.Errorf("round with room = %+v", rep)
	}
	if !strings.Contains(logged.String(), "new tasks are linked again") {
		t.Errorf("the end of the pause is not logged:\n%s", logged.String())
	}
}

// A task the feed tells of while the storage is almost full is linked by
// the comparison of a later round, as the feed has passed it (ADR-0163).
func TestATaskOfTheFeedLeftOutIsLinkedLater(t *testing.T) {
	f := setup(t)
	f.l.events = true
	f.l.tasks = []engine.Task{holidayTask(1)}
	f.l.feed = []engine.TaskFact{{Kind: engine.FactCreated, Key: 1, Process: "urlaub", Element: "genehmigen", Groups: []string{"hr"}}}
	if rep, err := f.r.Run(context.Background(), "atlas-prod", false); err != nil || rep.Linked != 1 {
		t.Fatalf("first round = %+v %v", rep, err)
	}

	f.db.SetSpace(diskspace.Critical)
	f.l.tasks = append(f.l.tasks, holidayTask(2))
	f.l.feed = append(f.l.feed, engine.TaskFact{Kind: engine.FactCreated, Key: 2, Process: "urlaub", Element: "genehmigen", Groups: []string{"hr"}})
	if rep, err := f.r.Run(context.Background(), "atlas-prod", false); err != nil || rep.Compared || rep.Paused != 1 || rep.Linked != 0 {
		t.Fatalf("the round that follows when critical = %+v %v", rep, err)
	}

	f.db.SetSpace(diskspace.OK)
	if rep, err := f.r.Run(context.Background(), "atlas-prod", false); err != nil || !rep.Compared || rep.Linked != 1 {
		t.Fatalf("the round with room = %+v %v", rep, err)
	}
	if len(f.linked(t)) != 2 {
		t.Error("the task left out is not linked")
	}
}
