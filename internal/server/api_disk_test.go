package server

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dbuchs7/orbithek/internal/diskspace"
	"github.com/dbuchs7/orbithek/internal/item"
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
	for _, name := range []string{"ot_disk_free_bytes 1.61", "ot_disk_level 1", "ot_disk_total_bytes", "ot_attachments_bytes"} {
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

// TestAtCriticalNothingNewIsMade: from the critical level on, every
// command that makes something new is refused with 507 and says why —
// items through the API and MCP, child pages, copies, folders, kanbans,
// spaces and access requests, whose task would be new; nothing of them
// is recorded. Editing, transitions, labels, comments, the trash, purging
// and signing in go on, and every signed-in person learns the level, not
// the sizes. At warning everything is made; at full only reads and what
// makes room stay (ADR-0163).
func TestAtCriticalNothingNewIsMade(t *testing.T) {
	w, disk, watch := diskWorld(t)
	w.admin.expect("POST", "/api/types", `{"name":"page","body":true}`, http.StatusCreated)
	w.admin.expect("POST", "/api/types", `{"name":"todo","body":true,"fields":[{"name":"assignee","kind":"user"}],
		"workflow":{"initial":"open","states":["open","done"],"transitions":[{"name":"finish","from":["open"],"to":"done"}]}}`, http.StatusCreated)
	task := w.bert.expect("POST", "/api/items", `{"space":"PLAT","type":"task","title":"Budget","properties":{"priority":"low"}}`, http.StatusCreated)
	key := field(task, "item", "key").(string)
	page := w.bert.expect("POST", "/api/items", `{"space":"PLAT","type":"page","title":"Salaries","body":[{"kind":"paragraph","text":"Paul earns 9000."}]}`, http.StatusCreated)
	pageKey, pageID := field(page, "item", "key").(string), field(page, "item", "id").(string)
	secret := field(w.bert.expect("POST", "/api/items/"+pageKey+"/secrets", `{"fromBlock":0,"toBlock":0}`, http.StatusOK), "item", "secrets").([]any)[0].(map[string]any)["id"].(string)
	board := w.bert.expect("POST", "/api/spaces/PLAT/kanbans", `{"name":"Website","type":"task"}`, http.StatusCreated)

	disk.set(40*gib, 1000*gib) // 4 %: critical
	if l := watch.Check().level; l != diskspace.Critical {
		t.Fatalf("level %v", l)
	}
	for _, c := range []*client{w.bert, w.vera, w.admin} {
		if got := c.expect("GET", "/api/status", "", http.StatusOK); got["storage"] != "critical" || len(got) != 1 {
			t.Errorf("status %v", got)
		}
	}
	count := func() int {
		return len(w.admin.expect("GET", "/api/items?space=PLAT", "", http.StatusOK)["items"].([]any))
	}
	before := count()
	for _, r := range []struct {
		who                *client
		method, path, body string
	}{
		{w.bert, "POST", "/api/items", `{"space":"PLAT","type":"task","title":"More","properties":{"priority":"low"}}`},
		{w.bert, "POST", "/api/items", `{"space":"PLAT","type":"page","parent":"` + pageID + `","title":"Child"}`},
		{w.bert, "POST", "/api/items/" + key + "/copy", `{"space":"PLAT","title":"Budget (copy)"}`},
		{w.bert, "POST", "/api/album/folders", `{"space":"PLAT","name":"Logos"}`},
		{w.bert, "POST", "/api/spaces/PLAT/kanbans", `{"name":"Other","type":"task"}`},
		{w.admin, "POST", "/api/spaces", `{"key":"MORE","name":"More"}`},
		{w.vera, "POST", "/api/items/" + pageKey + "/secrets/" + secret + "/request", ""},
	} {
		rec := r.who.do(r.method, r.path, r.body)
		if rec.Code != http.StatusInsufficientStorage || !strings.Contains(rec.Body.String(), "no new pages, tasks or rooms can be created") {
			t.Errorf("%s %s: %d %s", r.method, r.path, rec.Code, rec.Body.String())
		}
	}
	if text, isErr := w.bert.tool("create_item", `{"space":"PLAT","type":"task","title":"From an agent","properties":{"priority":"low"}}`); !isErr || !strings.Contains(text, "storage is almost full") {
		t.Errorf("create_item over MCP: %v %s", isErr, text)
	}
	if n := count(); n != before {
		t.Errorf("%d items made while nothing new is made", n-before)
	}
	if got := w.admin.expect("GET", "/api/spaces", "", http.StatusOK); strings.Contains(jsonOf(got), "MORE") {
		t.Error("a space was made")
	}
	if asks := field(w.bert.expect("GET", "/api/items/"+pageKey, "", http.StatusOK), "item", "secrets").([]any)[0].(map[string]any)["asks"]; asks != nil && len(asks.([]any)) > 0 {
		t.Errorf("a request without its task: %v", asks)
	}

	// What exists still changes, and people sign in.
	w.bert.expect("PUT", "/api/items/"+key+"/title", `{"title":"Budget 2027"}`, http.StatusOK)
	w.bert.expect("PUT", "/api/items/"+key+"/labels", `{"labels":["money"]}`, http.StatusOK)
	w.bert.expect("POST", "/api/items/"+key+"/comments", `{"text":"Agreed."}`, http.StatusCreated)
	w.bert.expect("POST", "/api/items/"+key+"/transitions", `{"name":"finish"}`, http.StatusOK)
	w.bert.expect("PUT", "/api/spaces/PLAT/kanbans/"+field(board, "kanban", "id").(string), `{"name":"Web"}`, http.StatusOK)
	(&client{t: t, h: w.bert.h}).login("vera", "vera's password")
	w.bert.expect("POST", "/api/items/"+key+"/trash", "", http.StatusOK)
	w.admin.expect("POST", "/api/items/"+key+"/purge", "", http.StatusOK)

	// At warning, everything is made again.
	disk.set(120*gib, 1000*gib)
	watch.Check()
	if got := w.vera.expect("GET", "/api/status", "", http.StatusOK)["storage"]; got != "warning" {
		t.Errorf("status at warning %v", got)
	}
	w.bert.expect("POST", "/api/items", `{"space":"PLAT","type":"task","title":"More","properties":{"priority":"low"}}`, http.StatusCreated)
	w.admin.expect("POST", "/api/spaces", `{"key":"MORE","name":"More"}`, http.StatusCreated)
	w.vera.expect("POST", "/api/items/"+pageKey+"/secrets/"+secret+"/request", "", http.StatusOK)
	if asks, _ := field(w.bert.expect("GET", "/api/items/"+pageKey, "", http.StatusOK), "item", "secrets").([]any)[0].(map[string]any)["asks"].([]any); len(asks) != 1 {
		t.Errorf("the request at warning: %v", asks)
	}

	// At full, the wiki only reads, as before.
	disk.set(100<<20, 1000*gib)
	watch.Check()
	if got := w.vera.expect("GET", "/api/status", "", http.StatusOK)["storage"]; got != "full" {
		t.Errorf("status at full %v", got)
	}
	w.bert.expect("PUT", "/api/items/"+pageKey+"/title", `{"title":"Wages"}`, http.StatusInsufficientStorage)
	w.bert.expect("GET", "/api/items/"+pageKey, "", http.StatusOK)
}
