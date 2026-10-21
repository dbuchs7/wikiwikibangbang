package autobackup

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/dbuchs7/wikiwikibangbang/internal/audit"
	"github.com/dbuchs7/wikiwikibangbang/internal/auth"
	"github.com/dbuchs7/wikiwikibangbang/internal/backup"
	"github.com/dbuchs7/wikiwikibangbang/internal/backup/dest"
	"github.com/dbuchs7/wikiwikibangbang/internal/item"
	"github.com/dbuchs7/wikiwikibangbang/internal/site"
	"github.com/dbuchs7/wikiwikibangbang/internal/storage"
)

const pass = "a long passphrase"

func init() { backup.Iterations = 1000 }

type fixture struct {
	db    *storage.DB
	r     *Runner
	dir   string
	clock time.Time
	logs  *bytes.Buffer
}

func setup(t *testing.T) *fixture {
	t.Helper()
	db, _, err := storage.OpenWith(t.TempDir(), storage.Options{Auth: []auth.Option{auth.WithIterations(1000)}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	err = db.Write(func(s *item.Store) error {
		if err := s.DefineType("admin", item.TypeDef{Name: "page", Body: true}); err != nil {
			return err
		}
		return s.CreateSpace("admin", "PLAT", "Platform")
	})
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{db: db, dir: t.TempDir(), clock: time.Date(2026, 10, 1, 2, 0, 0, 0, time.UTC), logs: &bytes.Buffer{}}
	env := map[string]string{PassphraseEnv: pass}
	f.r = New(db, func(k string) string { return env[k] }, "test", slog.New(slog.NewTextHandler(f.logs, nil)))
	f.r.now = func() time.Time { return f.clock }
	t.Cleanup(f.r.Close)
	if err := db.Site.SetBackup("admin", site.Backup{Enabled: true, Destination: f.dir, FullDays: 14, IncrementalDays: 1, Keep: 2}); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *fixture) page(t *testing.T, title string) {
	t.Helper()
	err := f.db.Write(func(s *item.Store) error {
		_, err := s.Create("admin", item.NewItem{Space: "PLAT", Type: "page", Title: title})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

func (f *fixture) stored(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(f.dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}

// The schedule takes a full backup, then incremental ones each day, a new
// full backup after FullDays, and keeps Keep versions; the newest backup
// restores with the ones it builds on.
func TestTheScheduleTakesAndPrunesBackups(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	kinds := func() []string {
		var out []string
		for _, v := range backup.Versions(f.stored(t)) {
			s := ""
			for _, b := range v.Backups {
				s += map[bool]string{true: "F", false: "i"}[b.Seq == 0]
			}
			out = append(out, s)
		}
		return out
	}
	f.r.Tick(ctx)
	if got := kinds(); !slices.Equal(got, []string{"F"}) {
		t.Fatalf("after the first tick %v", got)
	}
	f.clock = f.clock.Add(time.Hour)
	f.r.Tick(ctx)
	if got := kinds(); !slices.Equal(got, []string{"F"}) {
		t.Fatalf("an hour later %v", got)
	}
	for d := 1; d <= 30; d++ {
		f.clock = f.clock.Add(day)
		f.page(t, "day")
		f.r.Tick(ctx)
	}
	// Days 0 to 30: full backups on days 0, 14 and 28; the first is pruned.
	got := kinds()
	if len(got) != 2 || got[0] != "F"+strings.Repeat("i", 13) || got[1] != "Fii" {
		t.Fatalf("after 30 days %v", got)
	}
	st, err := f.r.Status(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Versions) != 2 || !st.Versions[0].Complete || len(st.Versions[0].Backups) != 3 || !st.Passphrase {
		t.Fatalf("status %+v", st)
	}
	if len(st.History) != historyLength || st.History[0].Kind != backup.Incremental || st.History[0].Error != "" || st.History[0].Size == 0 {
		t.Fatalf("history %+v", st.History[0])
	}
	if st.NextKind != backup.Incremental || !st.Next.Equal(f.clock.Add(day)) {
		t.Fatalf("next %v %s", st.Next, st.NextKind)
	}

	var want int
	f.db.Read(func(s *item.Store) { want = len(s.Find(item.Query{})) })
	target := filepath.Join(t.TempDir(), "r")
	var chain []backup.Source
	for _, b := range backup.Versions(f.stored(t))[1].Backups {
		chain = append(chain, func() (io.ReadCloser, error) { return os.Open(filepath.Join(f.dir, b.File)) })
	}
	if _, err := backup.RestoreChain(chain, pass, target); err != nil {
		t.Fatal(err)
	}
	restored, _, err := storage.Open(target)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	var got2 int
	restored.Read(func(s *item.Store) { got2 = len(s.Find(item.Query{})) })
	if got2 != want || want < 30 {
		t.Fatalf("restored %d items, want %d", got2, want)
	}
	n := 0
	for _, e := range f.db.Audit.List(0, 1000) {
		if e.Event == audit.BackupCreated && e.Actor == "" {
			n++
		}
	}
	if n != 31 {
		t.Errorf("%d backups in the audit trail", n)
	}
}

// An incremental backup whose chain is no longer at the destination, or
// another destination, starts a new chain with a full backup.
func TestABrokenChainStartsOver(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	if _, err := f.r.Run(ctx, backup.Incremental, "admin"); err != nil {
		t.Fatal(err)
	}
	if v := backup.Versions(f.stored(t)); len(v) != 1 || v[0].Backups[0].Seq != 0 {
		t.Fatalf("the first backup %v", v)
	}
	if run, err := f.r.Run(ctx, backup.Incremental, "admin"); err != nil || run.Kind != backup.Incremental {
		t.Fatalf("%+v %v", run, err)
	}
	for _, name := range f.stored(t) {
		if n, _ := backup.ParseName(name); n.Seq == 0 {
			os.Remove(filepath.Join(f.dir, name))
		}
	}
	if run, err := f.r.Run(ctx, backup.Incremental, "admin"); err != nil || run.Kind != backup.Full {
		t.Fatalf("after the full backup went: %+v %v", run, err)
	}
	other := t.TempDir()
	b := f.db.Site.Backup()
	b.Destination = other
	f.db.Site.SetBackup("admin", b)
	if run, err := f.r.Run(ctx, backup.Incremental, "admin"); err != nil || run.Kind != backup.Full {
		t.Fatalf("at another destination: %+v %v", run, err)
	}
}

// Failures are recorded and retried after a while, not at every check.
func TestFailuresAreRecordedAndRetried(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	os.RemoveAll(f.dir)
	f.r.Tick(ctx)
	st, _ := f.r.Status(ctx, true)
	if len(st.History) != 1 || st.History[0].Error == "" || st.StoredError == "" {
		t.Fatalf("status after a failure %+v", st)
	}
	if !st.Next.Equal(f.clock.Add(retryAfter)) || st.NextKind != backup.Full {
		t.Fatalf("retry %v %s", st.Next, st.NextKind)
	}
	f.clock = f.clock.Add(checkEvery)
	f.r.Tick(ctx)
	if st, _ := f.r.Status(ctx, false); len(st.History) != 1 {
		t.Fatalf("retried before retryAfter: %d runs", len(st.History))
	}
	os.Mkdir(f.dir, 0o700)
	f.clock = f.clock.Add(retryAfter)
	f.r.Tick(ctx)
	if st, _ := f.r.Status(ctx, false); len(st.History) != 2 || st.History[0].Error != "" {
		t.Fatalf("after the retry %+v", st.History)
	}
	failed := false
	for _, e := range f.db.Audit.List(0, 100) {
		failed = failed || e.Event == audit.BackupFailed
	}
	if !failed || !strings.Contains(f.logs.String(), "automatic backup failed") {
		t.Error("the failure is neither audited nor logged")
	}

	// Without a passphrase nothing is written.
	f.r.env = func(string) string { return "" }
	if _, err := f.r.Run(ctx, backup.Full, "admin"); !errors.Is(err, ErrPassphrase) {
		t.Fatalf("without a passphrase: %v", err)
	}
	if st, _ := f.r.Status(ctx, false); st.Passphrase {
		t.Error("a missing passphrase reported as set")
	}
}

func TestDue(t *testing.T) {
	now := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	b := site.Backup{Enabled: true, Destination: "/b", FullDays: 7, IncrementalDays: 1, Keep: 1}
	last := &backup.Manifest{}
	for name, c := range map[string]struct {
		b    site.Backup
		st   state
		want string
	}{
		"off":                {site.Backup{Destination: "/b", FullDays: 7}, state{}, ""},
		"never":              {b, state{}, backup.Full},
		"another place":      {b, state{Last: last, Destination: "/a", LastFull: now, LastBackup: now}, backup.Full},
		"just done":          {b, state{Last: last, Destination: "/b", LastFull: now, LastBackup: now}, ""},
		"a day later":        {b, state{Last: last, Destination: "/b", LastFull: now.Add(-day), LastBackup: now.Add(-day + checkEvery)}, backup.Incremental},
		"a week later":       {b, state{Last: last, Destination: "/b", LastFull: now.Add(-7 * day), LastBackup: now.Add(-day)}, backup.Full},
		"no incrementals":    {site.Backup{Enabled: true, Destination: "/b", FullDays: 7}, state{Last: last, Destination: "/b", LastFull: now.Add(-day), LastBackup: now.Add(-day)}, ""},
		"failed just now":    {b, state{LastAttempt: now.Add(-time.Minute), History: []Run{{Error: "x"}}}, ""},
		"failed a while ago": {b, state{LastAttempt: now.Add(-retryAfter), History: []Run{{Error: "x"}}}, backup.Full},
	} {
		if got := due(now, c.b, c.st); got != c.want {
			t.Errorf("%s: %q, want %q", name, got, c.want)
		}
	}
	if at, kind := next(now, site.Backup{}, state{}); !at.IsZero() || kind != "" {
		t.Errorf("next when off: %v %s", at, kind)
	}
	if at, kind := next(now, site.Backup{Enabled: true, Destination: "/b", FullDays: 7}, state{Last: last, Destination: "/b", LastFull: now.Add(-day), LastBackup: now.Add(-day)}); !at.Equal(now.Add(6*day)) || kind != backup.Full {
		t.Errorf("next without incrementals: %v %s", at, kind)
	}
}

// One backup runs at a time; Start runs one in the background and Close
// waits for it.
func TestOneBackupAtATime(t *testing.T) {
	f := setup(t)
	block := make(chan struct{})
	f.r.open = func(d string, env dest.Env) (dest.Target, error) {
		t, err := dest.Open(d, env)
		return slowTarget{Target: t, wait: block}, err
	}
	done := make(chan Run, 1)
	if err := f.r.Start(backup.Full, "admin", func(r Run, _ error) { done <- r }); err != nil {
		t.Fatal(err)
	}
	if err := f.r.Start(backup.Full, "admin", nil); !errors.Is(err, ErrBusy) {
		t.Fatalf("a second start: %v", err)
	}
	if _, err := f.r.Run(context.Background(), backup.Full, "admin"); !errors.Is(err, ErrBusy) {
		t.Fatalf("a run during a start: %v", err)
	}
	if st, _ := f.r.Status(context.Background(), false); st.Running != backup.Full {
		t.Fatalf("running %q", st.Running)
	}
	close(block)
	if r := <-done; r.Error != "" || r.Actor != "admin" {
		t.Fatalf("run %+v", r)
	}
	f.r.Close()
	if err := f.r.Start(backup.Full, "admin", func(r Run, _ error) { done <- r }); err != nil {
		t.Fatal(err)
	}
	if r := <-done; r.Error == "" {
		t.Fatal("a backup ran after Close")
	}
}

type slowTarget struct {
	dest.Target
	wait chan struct{}
}

func (s slowTarget) List(ctx context.Context) ([]string, error) {
	<-s.wait
	return s.Target.List(ctx)
}

func TestCheck(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	if err := f.r.Check(ctx, f.dir); err != nil {
		t.Fatal(err)
	}
	if names := f.stored(t); len(names) != 0 {
		t.Fatalf("the check left %v", names)
	}
	if err := f.r.Check(ctx, filepath.Join(f.dir, "missing")); err == nil {
		t.Error("a missing directory passed")
	}
	if err := f.r.Check(ctx, "relative"); err == nil {
		t.Error("a relative path passed")
	}
	for _, inside := range []string{f.db.Dir(), filepath.Join(f.db.Dir(), "attachments")} {
		if err := f.r.Check(ctx, inside); err == nil || !strings.Contains(err.Error(), "inside the data directory") {
			t.Errorf("%s: %v", inside, err)
		}
	}
	if err := f.r.Check(ctx, filepath.Dir(f.db.Dir())); err != nil {
		t.Errorf("the data directory's parent: %v", err)
	}
}

// Loop checks at once and ends with its context.
func TestLoop(t *testing.T) {
	f := setup(t)
	ctx, cancel := context.WithCancel(context.Background())
	ended := make(chan struct{})
	go func() { f.r.Loop(ctx); close(ended) }()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if st, _ := f.r.Status(ctx, false); len(st.History) > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	<-ended
	if len(f.stored(t)) != 1 {
		t.Fatalf("stored %v", f.stored(t))
	}
}

// A damaged state file is reported, not silently replaced.
func TestADamagedStateIsReported(t *testing.T) {
	f := setup(t)
	if err := f.db.SetBackupState([]byte("{")); err != nil {
		t.Fatal(err)
	}
	if _, err := f.r.Status(context.Background(), false); err == nil {
		t.Error("status read a damaged state")
	}
	if _, err := f.r.Run(context.Background(), backup.Full, ""); err == nil {
		t.Error("a backup ran over a damaged state")
	}
	f.r.Tick(context.Background())
	if !strings.Contains(f.logs.String(), "reading its state") {
		t.Error("the damaged state is not logged")
	}
}
