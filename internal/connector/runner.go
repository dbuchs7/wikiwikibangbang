package connector

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dbuchs7/wikiwikibangbang/internal/audit"
	"github.com/dbuchs7/wikiwikibangbang/internal/site"
	"github.com/dbuchs7/wikiwikibangbang/internal/storage"
)

// checkEvery is how often the schedule is looked at; a run is due a check
// early, so a daily one does not drift.
const checkEvery = 10 * time.Minute

// historyLength is how many runs the status remembers per connector.
const historyLength = 10

// ErrBusy is a run asked for while the connector runs.
var ErrBusy = errors.New("connector: a synchronisation is running")

// ErrUnknown is a run of a connector that does not exist.
var ErrUnknown = errors.New("connector: no such connector")

// Runner runs the connectors on their schedules and on request, each one
// run at a time. The runs it remembers are held in memory: after a
// restart the schedule runs at once, which changes nothing that is in
// step.
type Runner struct {
	db     *storage.DB
	log    *slog.Logger
	now    func() time.Time
	getenv func(string) string
	// client makes the client of a CKAN API, replaced in tests.
	client func(base, token string) Client

	ctx   context.Context
	close context.CancelFunc
	wg    sync.WaitGroup

	mu    sync.Mutex
	state map[string]*state
	// filesRun is when the pages made from files in a text were last
	// read (ADR-0131).
	filesRun time.Time
}

type state struct {
	running bool
	lastRun time.Time
	history []Result
	// seen is what the last run that applied its reading without a
	// problem remembered of its source (reading.seen).
	seen string
}

// New returns a runner for db; credentials come from the process
// environment.
func New(db *storage.DB, log *slog.Logger) *Runner {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	ctx, cancel := context.WithCancel(context.Background())
	r := &Runner{db: db, log: log, now: time.Now, getenv: os.Getenv, ctx: ctx, close: cancel, state: map[string]*state{}}
	// A CKAN client reaches its source like every reader (guard.go).
	r.client = func(base, token string) Client {
		c := NewClient(base, token)
		c.HTTP = r.httpClient()
		return c
	}
	return r
}

// TokenVar is the environment variable that holds a connector's
// credential, e.g. WWBB_CONNECTOR_OPENDATA_SWISS_TOKEN. Credentials are
// never kept in the settings (ADR-0077).
func TokenVar(id string) string { return envVar(id, "TOKEN") }

// EmailVar is the environment variable that holds the e-mail address of
// the account a connector of the kind atlassian signs in with, which goes
// with its API token (ADR-0128).
func EmailVar(id string) string { return envVar(id, "EMAIL") }

func envVar(id, what string) string {
	return "WWBB_CONNECTOR_" + strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' {
			return r - 'a' + 'A'
		}
		if r >= '0' && r <= '9' {
			return r
		}
		return '_'
	}, id) + "_" + what
}

// Close stops the runs Start began and waits until they have ended.
func (r *Runner) Close() {
	r.close()
	r.wg.Wait()
}

// Loop runs the connectors when they are due, until ctx ends.
func (r *Runner) Loop(ctx context.Context) {
	t := time.NewTicker(checkEvery)
	defer t.Stop()
	for {
		r.Tick(ctx)
		r.TickFiles(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// Tick runs every connector that is switched on and due.
func (r *Runner) Tick(ctx context.Context) {
	for _, c := range r.db.Site.Connectors() {
		r.mu.Lock()
		due := c.Enabled && r.now().Sub(r.of(c.ID).lastRun) >= time.Duration(c.IntervalHours)*time.Hour-checkEvery
		r.mu.Unlock()
		if !due {
			continue
		}
		if _, err := r.Run(ctx, c.ID, false, ""); err != nil && !errors.Is(err, ErrBusy) {
			r.log.Error("connector failed", "connector", c.ID, "err", err)
		}
	}
}

// of is the state of a connector; the caller holds mu.
func (r *Runner) of(id string) *state {
	st := r.state[id]
	if st == nil {
		st = &state{}
		r.state[id] = st
	}
	return st
}

// begin marks a connector running, or reports that it is.
func (r *Runner) begin(id string) (site.Connector, error) {
	c, ok := r.db.Site.Connector(id)
	if !ok {
		return c, ErrUnknown
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	st := r.of(id)
	if st.running {
		return c, ErrBusy
	}
	st.running = true
	return c, nil
}

// Start runs a connector in the background, until Close.
func (r *Runner) Start(id string, dry bool, actor string) error {
	c, err := r.begin(id)
	if err != nil {
		return err
	}
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		if _, err := r.run(r.ctx, c, dry, actor); err != nil {
			r.log.Error("connector failed", "connector", id, "dry", dry, "err", err)
		}
	}()
	return nil
}

// Run runs a connector now; with dry, it writes nothing and reports what
// it would do. actor is who asked; empty for the schedule.
func (r *Runner) Run(ctx context.Context, id string, dry bool, actor string) (Result, error) {
	c, err := r.begin(id)
	if err != nil {
		return Result{}, err
	}
	return r.run(ctx, c, dry, actor)
}

func (r *Runner) run(ctx context.Context, c site.Connector, dry bool, actor string) (Result, error) {
	start := r.now()
	res := Result{Connector: c.ID, DryRun: dry, Problems: []string{}, Examples: []Change{}}
	var rd reading
	rdr, err := reader(c.Kind)
	if err == nil {
		env := r.env(c.ID)
		r.mu.Lock()
		env.Seen, env.Force = r.of(c.ID).seen, actor != ""
		r.mu.Unlock()
		rd, err = rdr.Read(ctx, c, env)
	}
	switch {
	case err == nil && rd.write != nil:
		res, err = rd.write(r.db, dry)
	case err == nil && rd.apply:
		res, err = Apply(r.db, c, rd.entries, dry)
	}
	res.Connector, res.Datasets, res.Formats, res.Version, res.Same = c.ID, rd.datasets, rd.formats, rd.version, rd.same
	res.Problems = append(append([]string{}, rd.problems...), res.Problems...)
	if len(res.Problems) > problemsShown {
		res.Problems = res.Problems[:problemsShown]
	}
	res.Started, res.Seconds = start.UTC(), r.now().Sub(start).Seconds()
	if err != nil {
		res.Error = err.Error()
	}
	r.mu.Lock()
	st := r.of(c.ID)
	st.running = false
	if !dry {
		st.lastRun = start
		if err == nil && !res.retry && rd.seen != "" {
			st.seen = rd.seen
		}
	}
	st.history = append([]Result{res}, st.history...)
	if len(st.history) > historyLength {
		st.history = st.history[:historyLength]
	}
	r.mu.Unlock()
	if !dry {
		detail := map[string]string{"source": c.ID, "new": strconv.Itoa(res.New), "changed": strconv.Itoa(res.Changed), "withdrawn": strconv.Itoa(res.Withdrawn)}
		if err != nil {
			detail["error"] = err.Error()
		}
		r.db.Audit.Record(audit.Entry{Event: audit.CatalogSynced, Actor: actor, Subject: cmp.Or(c.Space, c.ID), Detail: detail})
	}
	return res, err
}

// env is what a connector's reader is given (ADR-0127).
func (r *Runner) env(id string) Env {
	return Env{Token: r.getenv(TokenVar(id)), Email: r.getenv(EmailVar(id)), HTTP: r.httpClient(), CKAN: r.client}
}

// Forget drops what the runner remembers of a removed connector.
func (r *Runner) Forget(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if st := r.state[id]; st != nil && !st.running {
		delete(r.state, id)
	}
}

// SourceFormats asks a CKAN source which formats its data sets use, of
// the organizations if any are named, so an administrator can choose; id
// names the connector whose credential is sent (ADR-0075).
func (r *Runner) SourceFormats(ctx context.Context, id, base string, orgs []string) ([]FormatCount, error) {
	client := r.client(base, r.getenv(TokenVar(id)))
	if unknown, err := client.UnknownOrganizations(ctx, orgs); err == nil && len(unknown) > 0 {
		return nil, fmt.Errorf("%w: the source knows no organization %s; name publishers as the source's addresses do, e.g. bundesamt-fur-statistik-bfs, or leave the list empty for all",
			ErrUnknownOrganization, strings.Join(unknown, ", "))
	}
	return client.Formats(ctx, orgs)
}

// ErrUnknownOrganization is a publisher the source does not know.
var ErrUnknownOrganization = errors.New("connector: unknown organization")

// Status is what an administrator sees of a connector's runs.
type Status struct {
	Running bool      `json:"running"`
	Next    time.Time `json:"next,omitzero"`
	History []Result  `json:"history"`
	// Credential tells whether the connector's credential is set in the
	// environment, and names the variable; never its value.
	Credential    bool   `json:"credential"`
	CredentialVar string `json:"credentialVar"`
	// Email does the same for the account's e-mail address, which a
	// connector of the kind atlassian signs in with (ADR-0128).
	Email    bool   `json:"email,omitempty"`
	EmailVar string `json:"emailVar,omitempty"`
}

// Status reports a connector's last runs and when it runs next.
func (r *Runner) Status(id string) Status {
	c, _ := r.db.Site.Connector(id)
	s := Status{CredentialVar: TokenVar(id), Credential: r.getenv(TokenVar(id)) != ""}
	if c.Kind == site.KindAtlassian {
		s.EmailVar, s.Email = EmailVar(id), r.getenv(EmailVar(id)) != ""
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	st := r.state[id]
	if st == nil {
		st = &state{}
	}
	s.Running, s.History = st.running, append([]Result{}, st.history...)
	if c.Enabled {
		s.Next = st.lastRun.Add(time.Duration(c.IntervalHours) * time.Hour)
		if s.Next.Before(r.now()) {
			s.Next = r.now()
		}
	}
	return s
}

// describe lists the first n formats with their counts.
func describe(fs []FormatCount, n int) string {
	if len(fs) == 0 {
		return "none"
	}
	var parts []string
	for _, f := range fs[:min(n, len(fs))] {
		parts = append(parts, fmt.Sprintf("%s (%d)", f.Format, f.Count))
	}
	if len(fs) > n {
		parts = append(parts, fmt.Sprintf("and %d more", len(fs)-n))
	}
	return strings.Join(parts, ", ")
}
