// Package tasklink keeps the linked tasks of the wiki in step with the open
// user tasks of process engines (ADR-0151): each round reads an engine's
// open tasks, links those a rule of the engine matches as items of the
// wiki, and ends the items whose task is no longer open.
//
// Linked items are made restricted to the synchronisation, so nobody reads
// one before the engine has said who may. Who sees a task is decided by
// each connected person's own task list, read with the person's grant:
// an open linked item is restricted to the connected people whose list
// holds its task.
package tasklink

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dbuchs7/wikiwikibangbang/internal/engine"
	"github.com/dbuchs7/wikiwikibangbang/internal/item"
	"github.com/dbuchs7/wikiwikibangbang/internal/site"
	"github.com/dbuchs7/wikiwikibangbang/internal/storage"
)

// Every is how often an engine whose task links are switched on is read.
const Every = time.Minute

// VisibilityEvery is how often the connected people's own task lists are
// read again, to tell who sees each linked task. A round that linked a
// task, or follows a person connecting, reads them at once.
const VisibilityEvery = 2 * time.Minute

// maxProblems is how many problems a report names; it says how many more
// there were.
const maxProblems = 20

// ErrBusy is a round asked for while one runs.
var ErrBusy = errors.New("tasklink: a round is running")

// Actor is who the synchronisation of an engine acts as: no person, so a
// linked item restricted to it is restricted to nobody a person is.
func Actor(engineID string) string { return "engine:" + engineID }

// Report is what one round did, or, dry, would do.
type Report struct {
	Engine string    `json:"engine"`
	At     time.Time `json:"at"`
	Dry    bool      `json:"dry"`
	// Open is how many open tasks the engine listed; Complete says the
	// list was whole, without which no task is ended.
	Open     int  `json:"open"`
	Complete bool `json:"complete"`
	// Linked are the items made, Updated those changed, Ended those whose
	// task ended, Unmatched the open tasks no rule matched.
	Linked    int `json:"linked"`
	Updated   int `json:"updated"`
	Ended     int `json:"ended"`
	Unmatched int `json:"unmatched"`
	// People are the connected people whose task lists were read,
	// Unread those whose lists could not be, who keep what they saw;
	// Readers counts the items whose readers changed. Visible says the
	// round read the lists at all.
	Visible bool `json:"visible"`
	People  int  `json:"people"`
	Unread  int  `json:"unread"`
	Readers int  `json:"readers"`
	// Problems are what went wrong, the first maxProblems of them; More
	// counts the others.
	Problems []string `json:"problems"`
	More     int      `json:"more,omitempty"`
	// Error is why the round did nothing.
	Error string `json:"error,omitempty"`
}

func (r *Report) problem(format string, args ...any) {
	if len(r.Problems) < maxProblems {
		r.Problems = append(r.Problems, fmt.Sprintf(format, args...))
	} else {
		r.More++
	}
}

// Lister lists the open tasks of an engine, and those a person sees with
// the access token of their grant; *engine.Reader is one.
type Lister interface {
	OpenTasks(ctx context.Context, e site.Engine) ([]engine.Task, bool, error)
	OpenTasksAs(ctx context.Context, e site.Engine, bearer string) ([]engine.Task, bool, error)
}

// Grants gives the access token of a connected person's grant at an
// engine, and is told when the engine refused one; *engineauth.Broker is
// one.
type Grants interface {
	Token(ctx context.Context, e site.Engine, user string) (string, error)
	Refused(user, engineID string)
}

// Runner reads the engines on their schedule and on request, one round
// per engine at a time. The last report of each is held in memory.
type Runner struct {
	db     *storage.DB
	tasks  Lister
	grants Grants
	log    *slog.Logger
	now    func() time.Time
	nudge  chan string
	mu     sync.Mutex
	run    map[string]bool
	report map[string]Report
	// seen is when each engine's people's lists were last read; due says
	// they are to be read in the next round.
	seen map[string]time.Time
	due  map[string]bool
}

// New returns a runner for db that lists tasks with tasks.
func New(db *storage.DB, tasks Lister, log *slog.Logger) *Runner {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Runner{db: db, tasks: tasks, log: log, now: time.Now, nudge: make(chan string, 1), run: map[string]bool{}, report: map[string]Report{},
		seen: map[string]time.Time{}, due: map[string]bool{}}
}

// WithGrants has the runner tell who sees each linked task from the
// connected people's own task lists, read with grants. Without it, linked
// tasks stay restricted to the synchronisation.
func (r *Runner) WithGrants(g Grants) *Runner {
	r.grants = g
	return r
}

// Nudge asks for a round of the engine soon, its people's lists read
// again, as after a person connected.
func (r *Runner) Nudge(engineID string) {
	r.mu.Lock()
	r.due[engineID] = true
	r.mu.Unlock()
	select {
	case r.nudge <- engineID:
	default: // one is asked for already
	}
}

// Loop runs a round of every engine whose task links are switched on,
// every Every, until ctx ends.
func (r *Runner) Loop(ctx context.Context) {
	t := time.NewTicker(Every)
	defer t.Stop()
	for {
		r.Tick(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-r.nudge:
		}
	}
}

// Tick runs a round of every engine whose task links are switched on.
func (r *Runner) Tick(ctx context.Context) {
	for _, e := range r.db.Site.Engines() {
		if !r.db.Site.TaskLinks(e.ID).Enabled {
			continue
		}
		rep, err := r.Run(ctx, e.ID, false)
		switch {
		case errors.Is(err, ErrBusy):
		case err != nil:
			r.log.Warn("task links: round failed", "engine", e.ID, "err", err)
		case len(rep.Problems) > 0:
			r.log.Warn("task links: round had problems", "engine", e.ID, "problems", len(rep.Problems)+rep.More)
		}
	}
}

// Last is the report of the engine's last round, if one ran.
func (r *Runner) Last(engineID string) (Report, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	rep, ok := r.report[engineID]
	return rep, ok
}

// Run runs one round of an engine's task links now; dry, it reports what
// it would do and changes nothing. A round runs whether the links are
// switched on or not: an administrator tries rules before switching them
// on.
func (r *Runner) Run(ctx context.Context, engineID string, dry bool) (Report, error) {
	r.mu.Lock()
	if r.run[engineID] {
		r.mu.Unlock()
		return Report{}, ErrBusy
	}
	r.run[engineID] = true
	r.mu.Unlock()
	defer func() {
		r.mu.Lock()
		delete(r.run, engineID)
		r.mu.Unlock()
	}()

	rep := Report{Engine: engineID, At: r.now().UTC(), Dry: dry, Problems: []string{}}
	err := r.round(ctx, engineID, &rep)
	if err != nil {
		rep.Error = err.Error()
	} else if !dry && r.grants != nil {
		r.mu.Lock()
		read := rep.Linked > 0 || r.due[engineID] || r.now().Sub(r.seen[engineID]) >= VisibilityEvery
		r.mu.Unlock()
		if read {
			r.visibility(ctx, engineID, &rep)
		}
	}
	if !dry {
		r.mu.Lock()
		r.report[engineID] = rep
		r.mu.Unlock()
	}
	return rep, err
}

func (r *Runner) round(ctx context.Context, engineID string, rep *Report) error {
	e, ok := r.db.Site.Engine(engineID)
	if !ok {
		return fmt.Errorf("no engine %q", engineID)
	}
	links := r.db.Site.TaskLinks(engineID)
	tasks, complete, err := r.tasks.OpenTasks(ctx, e)
	if err != nil {
		return err
	}
	rep.Open, rep.Complete = len(tasks), complete
	if !complete {
		rep.problem("the engine listed more open tasks than this server reads in one round; tasks beyond them are not linked and none are ended")
	}
	actor := Actor(engineID)
	open := map[string]bool{}
	var wanted []item.LinkedTask
	for _, t := range tasks {
		key := strconv.FormatUint(t.Key, 10)
		open[key] = true
		if t.Wizard == "public" {
			continue // an anonymous filler's step, never anybody's task here
		}
		rule, ok := match(links.Rules, t)
		if !ok {
			rep.Unmatched++
			continue
		}
		wanted = append(wanted, linked(e, rule, t, rep))
	}
	write := func(fn func(*item.Store) error) error {
		if rep.Dry {
			var err error
			r.db.Read(func(s *item.Store) { err = fn(s) })
			return err
		}
		return r.db.Write(fn)
	}
	return write(func(s *item.Store) error {
		for _, w := range wanted {
			if rep.Dry {
				if _, had := s.FindByOrigin(item.LinkedSystem, w.Engine, "task/"+w.Key); had {
					rep.Updated++ // at most: an unchanged task writes nothing
				} else {
					rep.Linked++
				}
				continue
			}
			_, had := s.FindByOrigin(item.LinkedSystem, w.Engine, "task/"+w.Key)
			_, changed, err := s.LinkTask(actor, w)
			switch {
			case err != nil:
				rep.problem("task %s (%s): %v", w.Key, w.Title, err)
			case changed && had:
				rep.Updated++
			case changed:
				rep.Linked++
			}
		}
		if !complete {
			return nil
		}
		for _, it := range s.LinkedTasks(engineID) {
			if it.Origin == nil || open[it.Origin.ExternalKey] {
				continue
			}
			if s.Type(it.Type).Workflow.Finished(it.State) {
				continue
			}
			rule, ok := RuleOf(links.Rules, it)
			if !ok {
				rep.problem("%s: its task ended, and no rule names its space %s and type %s, so it stays open", it.Key, it.Space, it.Type)
				continue
			}
			if rep.Dry {
				rep.Ended++
				continue
			}
			if err := s.EndTask(actor, it.ID, rule.Completed); err != nil {
				rep.problem("%s: %v", it.Key, err)
				continue
			}
			rep.Ended++
		}
		return nil
	})
}

// visibility reads every connected person's own task list with their
// grant and restricts each open linked item, for reading and editing, to
// the synchronisation and the connected people whose list holds its task.
// A person whose list could not be read keeps what they saw; a person no
// longer connected sees nothing.
func (r *Runner) visibility(ctx context.Context, engineID string, rep *Report) {
	e, ok := r.db.Site.Engine(engineID)
	if !ok {
		return
	}
	sees := map[string][]string{} // task key -> people
	unsure := map[string]bool{}
	var failed error
	for _, c := range r.db.Connections.Of("", engineID) {
		tasks, complete, err := r.tasksOf(ctx, e, c.User)
		if _, still := r.db.Connections.Get(c.User, engineID); !still {
			continue // the engine refused the grant, and the connection ended
		}
		if err != nil {
			unsure[c.User] = true
			rep.Unread++
			failed = cmp.Or(failed, err)
			continue
		}
		rep.People++
		if !complete {
			unsure[c.User] = true // what lies beyond the list stays as it was
		}
		for _, t := range tasks {
			key := strconv.FormatUint(t.Key, 10)
			sees[key] = append(sees[key], c.User)
		}
	}
	if failed != nil {
		rep.problem("task lists of connected people not read (%d), who keep what they saw: %v", rep.Unread, failed)
	}
	actor := Actor(engineID)
	err := r.db.Write(func(s *item.Store) error {
		for _, it := range s.LinkedTasks(engineID) {
			if it.Origin == nil || s.Type(it.Type).Workflow.Finished(it.State) {
				continue // an ended task's readers stay as they were
			}
			want := append([]string{actor}, sees[it.Origin.ExternalKey]...)
			for _, p := range it.Restriction.Read {
				if unsure[p] {
					want = append(want, p)
				}
			}
			slices.Sort(want)
			want = slices.Compact(want)
			if slices.Equal(want, sortedCopy(it.Restriction.Read)) && slices.Equal(want, sortedCopy(it.Restriction.Edit)) {
				continue
			}
			if err := s.Restrict(actor, it.ID, item.Restriction{Read: want, Edit: want}); err != nil {
				rep.problem("%s: %v", it.Key, err)
				continue
			}
			rep.Readers++
		}
		return nil
	})
	if err != nil {
		rep.problem("who sees the tasks was not written: %v", err)
		return
	}
	rep.Visible = true
	r.mu.Lock()
	r.seen[engineID] = r.now()
	delete(r.due, engineID)
	r.mu.Unlock()
}

// tasksOf lists the open tasks a person sees, with their grant. An access
// token the engine refuses is given back, so the next round renews it and
// learns whether the grant still stands.
func (r *Runner) tasksOf(ctx context.Context, e site.Engine, user string) ([]engine.Task, bool, error) {
	tok, err := r.grants.Token(ctx, e, user)
	if err != nil {
		return nil, false, err
	}
	tasks, complete, err := r.tasks.OpenTasksAs(ctx, e, tok)
	if errors.Is(err, engine.ErrRefused) {
		r.grants.Refused(user, e.ID)
	}
	return tasks, complete, err
}

func sortedCopy(list []string) []string {
	out := slices.Clone(list)
	slices.Sort(out)
	return out
}

// match is the first rule whose filter takes the task.
func match(rules []site.TaskRule, t engine.Task) (site.TaskRule, bool) {
	for _, r := range rules {
		if (len(r.Processes) == 0 || slices.Contains(r.Processes, t.Process)) &&
			(len(r.Elements) == 0 || slices.Contains(r.Elements, t.Element)) &&
			(len(r.Groups) == 0 || slices.ContainsFunc(t.Groups, func(g string) bool { return slices.Contains(r.Groups, g) })) {
			return r, true
		}
	}
	return site.TaskRule{}, false
}

// RuleOf is the first rule that makes items where a linked item stands:
// its ended task goes to that rule's state.
func RuleOf(rules []site.TaskRule, it item.Item) (site.TaskRule, bool) {
	for _, r := range rules {
		if r.Space == it.Space && r.Type == it.Type {
			return r, true
		}
	}
	return site.TaskRule{}, false
}

// linked is the item a rule makes of a task.
func linked(e site.Engine, r site.TaskRule, t engine.Task, rep *Report) item.LinkedTask {
	key := strconv.FormatUint(t.Key, 10)
	w := item.LinkedTask{Engine: e.ID, Key: key, URL: engine.TaskURL(e, t.Key), Space: r.Space, Type: r.Type,
		Title: cmp.Or(t.Name, t.Element, "Task "+key)}
	if r.DueField != "" {
		w.Fields = item.Props{r.DueField: nil}
		if !t.Due.IsZero() {
			d := t.Due.UTC()
			w.Fields[r.DueField] = time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, time.UTC)
		}
	}
	for _, l := range r.Labels {
		values := []string{l}
		if strings.Contains(l, "{group}") {
			values = nil // a label per group the task is offered to
			for _, g := range t.Groups {
				values = append(values, strings.ReplaceAll(l, "{group}", g))
			}
		}
		for _, v := range values {
			v = strings.NewReplacer("{process}", t.Process, "{element}", t.Element).Replace(v)
			n, ok := item.NormalLabel(v)
			if !ok {
				rep.problem("task %s: %q is no label", key, v)
				continue
			}
			w.Labels = append(w.Labels, n)
		}
	}
	return w
}
