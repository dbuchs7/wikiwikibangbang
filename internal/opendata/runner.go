package opendata

import (
	"context"
	"errors"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"github.com/dbuchs7/wikiwikibangbang/internal/audit"
	"github.com/dbuchs7/wikiwikibangbang/internal/storage"
)

// checkEvery is how often the schedule is looked at; a run is due a check
// early, so a daily one does not drift.
const checkEvery = 10 * time.Minute

// historyLength is how many runs the status remembers.
const historyLength = 10

// ErrBusy is a run asked for while another runs.
var ErrBusy = errors.New("opendata: a synchronisation is running")

// Runner runs the synchronisation on its schedule and on request. The
// runs it remembers are held in memory: after a restart the schedule runs
// at once, which changes nothing that is in step.
type Runner struct {
	db  *storage.DB
	log *slog.Logger
	now func() time.Time
	// client makes the client of the API, replaced in tests.
	client func(base string) Client

	ctx   context.Context
	close context.CancelFunc

	run     sync.Mutex
	mu      sync.Mutex
	running bool
	lastRun time.Time
	history []Result
}

// New returns a runner for db.
func New(db *storage.DB, log *slog.Logger) *Runner {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Runner{db: db, log: log, now: time.Now, client: NewClient, ctx: ctx, close: cancel}
}

// Close stops a run Start began and waits until it has ended.
func (r *Runner) Close() {
	r.close()
	r.run.Lock()
	r.run.Unlock()
}

// Loop runs the synchronisation when it is due, until ctx ends.
func (r *Runner) Loop(ctx context.Context) {
	t := time.NewTicker(checkEvery)
	defer t.Stop()
	for {
		r.Tick(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// Tick runs the synchronisation if it is switched on and due.
func (r *Runner) Tick(ctx context.Context) {
	cfg := r.db.Site.OpenData()
	r.mu.Lock()
	due := cfg.Enabled && r.now().Sub(r.lastRun) >= time.Duration(cfg.IntervalHours)*time.Hour-checkEvery
	r.mu.Unlock()
	if !due {
		return
	}
	if _, err := r.Run(ctx, false, ""); err != nil && !errors.Is(err, ErrBusy) {
		r.log.Error("catalog synchronisation failed", "source", System, "err", err)
	}
}

// Start runs the synchronisation in the background, until Close.
func (r *Runner) Start(dry bool, actor string) error {
	if !r.run.TryLock() {
		return ErrBusy
	}
	r.setRunning(true)
	go func() {
		defer r.run.Unlock()
		if _, err := r.locked(r.ctx, dry, actor); err != nil {
			r.log.Error("catalog synchronisation failed", "source", System, "dry", dry, "err", err)
		}
	}()
	return nil
}

// Run runs the synchronisation now; with dry, it writes nothing and
// reports what it would do. actor is who asked; empty for the schedule.
func (r *Runner) Run(ctx context.Context, dry bool, actor string) (Result, error) {
	if !r.run.TryLock() {
		return Result{}, ErrBusy
	}
	defer r.run.Unlock()
	return r.locked(ctx, dry, actor)
}

func (r *Runner) locked(ctx context.Context, dry bool, actor string) (Result, error) {
	r.setRunning(true)
	defer r.setRunning(false)
	start := r.now()
	cfg := r.db.Site.OpenData()
	res := Result{DryRun: dry, Problems: []string{}, Examples: []Change{}}
	datasets, err := r.client(cfg.BaseURL).Datasets(ctx, cfg.Formats, cfg.Organizations)
	if err == nil {
		entries, problems := Entries(datasets, cfg.Formats, cfg.PortalURL)
		res, err = Apply(r.db, cfg, entries, dry)
		res.Datasets = len(datasets)
		for _, p := range problems {
			res.problem("%s", p)
		}
	}
	res.Started, res.Seconds = start.UTC(), r.now().Sub(start).Seconds()
	if err != nil {
		res.Error = err.Error()
	}
	r.mu.Lock()
	if !dry {
		r.lastRun = start
	}
	r.history = append([]Result{res}, r.history...)
	if len(r.history) > historyLength {
		r.history = r.history[:historyLength]
	}
	r.mu.Unlock()
	if !dry {
		detail := map[string]string{"source": System, "new": strconv.Itoa(res.New), "changed": strconv.Itoa(res.Changed), "withdrawn": strconv.Itoa(res.Withdrawn)}
		if err != nil {
			detail["error"] = err.Error()
		}
		r.db.Audit.Record(audit.Entry{Event: audit.CatalogSynced, Actor: actor, Subject: cfg.Space, Detail: detail})
	}
	return res, err
}

func (r *Runner) setRunning(v bool) {
	r.mu.Lock()
	r.running = v
	r.mu.Unlock()
}

// Status is what an administrator sees of the synchronisation.
type Status struct {
	Running bool      `json:"running"`
	Next    time.Time `json:"next,omitzero"`
	History []Result  `json:"history"`
}

// Status reports the last runs and when the schedule runs next.
func (r *Runner) Status() Status {
	cfg := r.db.Site.OpenData()
	r.mu.Lock()
	defer r.mu.Unlock()
	s := Status{Running: r.running, History: append([]Result{}, r.history...)}
	if cfg.Enabled {
		s.Next = r.lastRun.Add(time.Duration(cfg.IntervalHours) * time.Hour)
		if s.Next.Before(r.now()) {
			s.Next = r.now()
		}
	}
	return s
}
