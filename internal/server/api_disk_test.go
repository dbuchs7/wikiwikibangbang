package server

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dbuchs7/wikiwikibangbang/internal/diskspace"
	"github.com/dbuchs7/wikiwikibangbang/internal/item"
)

// fakeDisk is a file system whose free space a test sets.
type fakeDisk struct {
	mu  sync.Mutex
	u   diskspace.Usage
	err error
}

func (f *fakeDisk) set(free, total uint64) {
	f.mu.Lock()
	f.u, f.err = diskspace.Usage{Free: free, Total: total}, nil
	f.mu.Unlock()
}

func (f *fakeDisk) measure(string) (diskspace.Usage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.u, f.err
}

const gib = 1 << 30

// diskWorld is a world whose file system a test fills.
func diskWorld(t *testing.T) (world, *fakeDisk, *DiskWatch) {
	disk := &fakeDisk{u: diskspace.Usage{Free: 500 * gib, Total: 1000 * gib}}
	var watch *DiskWatch
	w := newWorld(t, func(d *Deps) {
		watch = NewDiskWatch(d.DB, nil, nil)
		watch.measure = disk.measure
		d.Disk = watch
	})
	return w, disk, watch
}

// TestStorageIsShownToAdministratorsAndThresholdsSet: the storage report
// says how full the file system is and what takes the space; only an
// administrator sees it or changes the thresholds, which apply at once
// and are audited (ADR-0150).
func TestStorageIsShownToAdministratorsAndThresholdsSet(t *testing.T) {
	w, disk, _ := diskWorld(t)
	w.bert.expect("GET", "/api/storage", "", http.StatusForbidden)
	r := w.admin.expect("GET", "/api/storage", "", http.StatusOK)
	if r["level"] != "ok" || r["free"].(float64) != 500*gib || r["total"].(float64) != 1000*gib {
		t.Fatalf("report %v", r)
	}
	parts := r["parts"].(map[string]any)
	if parts["events.wal"].(float64) <= 0 || parts["attachments"] == nil || r["data"].(float64) <= 0 {
		t.Errorf("parts %v", parts)
	}
	if th := r["thresholds"].(map[string]any); th["warnPercent"].(float64) != 15 || th["criticalMB"].(float64) != 1024 {
		t.Errorf("default thresholds %v", th)
	}

	disk.set(150*gib, 1000*gib) // 15 %: not below the default warning
	if got := w.admin.expect("GET", "/api/storage?fresh=1", "", http.StatusOK)["level"]; got != "ok" {
		t.Errorf("at 15 %%: %v", got)
	}
	w.bert.expect("PUT", "/api/settings/storage", `{"warnPercent":20,"warnMB":5120,"criticalPercent":5,"criticalMB":1024}`, http.StatusForbidden)
	w.admin.expect("PUT", "/api/settings/storage", `{"warnPercent":5,"warnMB":5120,"criticalPercent":15,"criticalMB":1024}`, http.StatusBadRequest)
	if got := w.admin.expect("PUT", "/api/settings/storage", `{"warnPercent":20,"warnMB":5120,"criticalPercent":5,"criticalMB":1024}`, http.StatusOK)["level"]; got != "warning" {
		t.Errorf("after raising the warning to 20 %%: %v", got)
	}
	if got := w.admin.expect("GET", "/api/storage", "", http.StatusOK)["level"]; got != "warning" {
		t.Errorf("the last measurement: %v", got)
	}
	metrics := w.admin.do("GET", "/metrics", "").Body.String()
	for _, name := range []string{"wwbb_disk_free_bytes 1.61", "wwbb_disk_level 1", "wwbb_disk_total_bytes", "wwbb_attachments_bytes"} {
		if !strings.Contains(metrics, name) {
			t.Errorf("metrics lack %s", name)
		}
	}
	audit := jsonOf(w.admin.expect("GET", "/api/audit?limit=5", "", http.StatusOK))
	if !strings.Contains(audit, "disk.thresholds.change") {
		t.Errorf("the change is not audited: %s", audit)
	}
}

// TestAnAlmostFullStorageRefusesUploadsThenWrites: below critical, uploads
// are refused while texts still change; below the floor, only what makes
// room is done, and the live editor is refused too; with room again,
// everything works (ADR-0150).
func TestAnAlmostFullStorageRefusesUploadsThenWrites(t *testing.T) {
	w, disk, watch := diskWorld(t)
	it := w.bert.expect("POST", "/api/items", `{"space":"PLAT","type":"task","title":"Budget","properties":{"priority":"low"}}`, http.StatusCreated)
	key, id := field(it, "item", "key").(string), field(it, "item", "id").(string)
	upload := func() int {
		return w.bert.upload("/api/items/"+key+"/attachments?name=notes.txt", "application/octet-stream", []byte("plain text")).Code
	}
	if c := upload(); c != http.StatusCreated {
		t.Fatalf("upload with room: %d", c)
	}

	disk.set(40*gib, 1000*gib) // 4 %: critical
	if l := watch.Check().level; l != diskspace.Critical {
		t.Fatalf("level %v", l)
	}
	if c := upload(); c != http.StatusInsufficientStorage {
		t.Errorf("upload when critical: %d", c)
	}
	w.bert.expect("PUT", "/api/items/"+key+"/title", `{"title":"Budget 2027"}`, http.StatusOK)

	disk.set(100<<20, 1000*gib) // 100 MB: below the floor
	watch.Check()
	w.bert.expect("PUT", "/api/items/"+key+"/title", `{"title":"Budget 2028"}`, http.StatusInsufficientStorage)
	w.bert.expect("POST", "/api/items", `{"space":"PLAT","type":"task","title":"More","properties":{"priority":"low"}}`, http.StatusInsufficientStorage)
	if err := (storeBackend{w.db}).Commit(item.ID(id), w.ids["bert"], item.Doc{}); err == nil {
		t.Error("the live editor wrote on a full file system")
	}
	w.admin.expect("GET", "/api/items/"+key, "", http.StatusOK)          // reading goes on
	w.bert.expect("POST", "/api/items/"+key+"/trash", "", http.StatusOK) // making room goes on
	w.admin.expect("POST", "/api/items/"+key+"/purge", "", http.StatusOK)

	disk.set(500*gib, 1000*gib)
	watch.Check()
	w.bert.expect("POST", "/api/items", `{"space":"PLAT","type":"task","title":"More","properties":{"priority":"low"}}`, http.StatusCreated)
}

// TestDiskWatchTellsOnceAndEstimatesGrowth: a failed measurement keeps the
// level; the growth is estimated once six hours are known; administrators
// are told when the level gets worse, not again while it stays.
func TestDiskWatchTellsOnceAndEstimatesGrowth(t *testing.T) {
	w, disk, watch := diskWorld(t)
	now := time.Date(2026, 10, 28, 8, 0, 0, 0, time.UTC)
	watch.now = func() time.Time { return now }
	first := watch.Check()
	if first.GrowthPerDay != 0 {
		t.Errorf("growth from one sample: %d", first.GrowthPerDay)
	}
	now = now.Add(12 * time.Hour)
	w.bert.expect("POST", "/api/items", `{"space":"PLAT","type":"task","title":"`+strings.Repeat("x", 500)+`","properties":{"priority":"low"}}`, http.StatusCreated)
	later := watch.Check()
	if later.GrowthPerDay <= 0 || later.DaysLeft <= 0 {
		t.Errorf("growth %d, days left %d", later.GrowthPerDay, later.DaysLeft)
	}

	disk.set(120*gib, 1000*gib)
	if r := watch.Check(); r.Level != "warning" || watch.told != diskspace.Warning {
		t.Fatalf("level %s, told %v", r.Level, watch.told)
	}
	disk.mu.Lock()
	disk.err = errors.New("the disk does not answer")
	disk.mu.Unlock()
	if r := watch.Check(); r.Problem == "" || w.db.Space() != diskspace.Warning || r.Level != "warning" {
		t.Errorf("a failed measurement: %+v, store at %v", r, w.db.Space())
	}
	disk.set(500*gib, 1000*gib)
	if watch.Check(); watch.told != diskspace.OK {
		t.Errorf("recovered, told %v", watch.told)
	}
	if got := watch.Report(); got.Level != "ok" {
		t.Errorf("report %+v", got)
	}
	// The loop measures at once and ends with its context.
	disk.set(40*gib, 1000*gib)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	watch.Loop(ctx)
	if w.db.Space() != diskspace.Critical {
		t.Errorf("after the loop: %v", w.db.Space())
	}
	if formatBytes(3*gib/2) != "1.5 GB" || formatBytes(300<<20) != "300 MB" || formatPercent(1, 8) != "12.5 %" {
		t.Error("formatting")
	}
}
