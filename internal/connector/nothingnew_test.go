package connector

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/dbuchs7/orbithek/internal/diskspace"
)

// While the storage is almost full, a run keeps the existing pages in
// step but writes no new one; it says so in its result and once in the
// log; once there is room, the next run writes the new pages (ADR-0163).
func TestARunWritesNothingNewWhileTheStorageIsAlmostFull(t *testing.T) {
	f := setup(t)
	var logged bytes.Buffer
	f.r.log = slog.New(slog.NewTextHandler(&logged, nil))
	ctx := context.Background()
	f.ckan.datasets = []string{weather}
	if res, err := f.r.Run(ctx, id, false, ""); err != nil || res.New != 1 {
		t.Fatalf("first run %+v %v", res, err)
	}

	f.db.SetSpace(diskspace.Critical)
	f.ckan.datasets = []string{strings.Replace(weather, "api.meteo.example/v1", "api.meteo.example/v2", 1), geo, transport}
	for range 2 {
		res, err := f.r.Run(ctx, id, false, "")
		if err != nil || res.New != 0 || res.Changed+res.Unchanged != 1 || res.Paused != 2 || len(res.Problems) != 1 ||
			!strings.Contains(res.Problems[0], "2 new entries are not written while the storage is almost full") {
			t.Fatalf("run when critical %+v %v", res, err)
		}
	}
	if got := f.items(t); len(got) != 1 || !strings.Contains(pageText(got["d1"].Body.Blocks), "api.meteo.example/v2") {
		t.Fatalf("pages when critical %v", got)
	}
	if n := strings.Count(logged.String(), "not written while the storage is almost full"); n != 1 {
		t.Errorf("logged %d times:\n%s", n, logged.String())
	}
	// A dry run counts what it would make, whatever the level.
	if dry, err := f.r.Run(ctx, id, true, ""); err != nil || dry.New != 2 || dry.Paused != 0 {
		t.Errorf("dry run when critical %+v %v", dry, err)
	}

	f.db.SetSpace(diskspace.Warning)
	if res, err := f.r.Run(ctx, id, false, ""); err != nil || res.New != 2 || res.Paused != 0 {
		t.Fatalf("run with room %+v %v", res, err)
	}
	if len(f.items(t)) != 3 || !strings.Contains(logged.String(), "new entries are written again") {
		t.Errorf("after the pause: %d pages\n%s", len(f.items(t)), logged.String())
	}
}
