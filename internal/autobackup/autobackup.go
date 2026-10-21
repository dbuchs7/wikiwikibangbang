// Package autobackup takes backups on a schedule and stores them at a
// destination (ADR-0073): a full backup every few days, incremental ones
// in between, and as many versions as the administrator keeps.
//
// The schedule and the destination are site settings; the passphrase and
// the destination's credentials come from the environment, so neither the
// data nor its backups hold them. Which backup the next incremental one
// builds on is kept in a state file beside the data, which a restored
// instance does not have: it starts with a full backup.
package autobackup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/dbuchs7/wikiwikibangbang/internal/audit"
	"github.com/dbuchs7/wikiwikibangbang/internal/backup"
	"github.com/dbuchs7/wikiwikibangbang/internal/backup/dest"
	"github.com/dbuchs7/wikiwikibangbang/internal/site"
	"github.com/dbuchs7/wikiwikibangbang/internal/storage"
)

// PassphraseEnv names the variable that holds the passphrase of
// automatic backups.
const PassphraseEnv = "WWBB_BACKUP_PASSPHRASE"

// The intervals of the schedule. A backup is due a check interval early,
// so a daily backup does not drift later by a check each day; after a
// failure the next attempt waits retryAfter.
const (
	checkEvery = 15 * time.Minute
	retryAfter = time.Hour
	day        = 24 * time.Hour
)

// historyLength is how many runs the state remembers.
const historyLength = 20

// Run is one backup the runner took or tried to take.
type Run struct {
	At      time.Time `json:"at"`
	Kind    string    `json:"kind"`
	File    string    `json:"file,omitempty"`
	Size    int64     `json:"size,omitempty"`
	Seconds float64   `json:"seconds"`
	// Actor is who asked for it; empty for the schedule.
	Actor string `json:"actor,omitempty"`
	Error string `json:"error,omitempty"`
}

// state is what the runner keeps beside the data.
type state struct {
	// Destination is where the chain of Last is stored; another
	// destination starts a new chain.
	Destination string           `json:"destination"`
	Last        *backup.Manifest `json:"last,omitempty"`
	LastFull    time.Time        `json:"lastFull,omitzero"`
	LastBackup  time.Time        `json:"lastBackup,omitzero"`
	LastAttempt time.Time        `json:"lastAttempt,omitzero"`
	History     []Run            `json:"history,omitempty"`
}

// Runner takes the backups of one data directory. It is safe for
// concurrent use; one backup runs at a time.
type Runner struct {
	db    *storage.DB
	env   dest.Env
	build string
	log   *slog.Logger
	now   func() time.Time
	open  func(string, dest.Env) (dest.Target, error)

	// ctx ends the backups Start began, when Close is called.
	ctx   context.Context
	close context.CancelFunc

	run     sync.Mutex
	mu      sync.Mutex
	running string
}

// New returns a runner for db; env reads the environment, build names the
// program version for the manifests.
func New(db *storage.DB, env dest.Env, build string, log *slog.Logger) *Runner {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	ctx, cancel := context.WithCancel(context.Background())
	r := &Runner{db: db, env: env, build: build, log: log, ctx: ctx, close: cancel, now: time.Now}
	r.open = r.openOutside
	return r
}

// openOutside opens a destination that is not inside the data directory,
// where backups would back up backups and be lost with the data.
func (r *Runner) openOutside(destination string, env dest.Env) (dest.Target, error) {
	if dir, ok := dest.Directory(destination); ok {
		data, err := filepath.Abs(r.db.Dir())
		if err != nil {
			return nil, err
		}
		if rel, err := filepath.Rel(data, dir); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return nil, errors.New("autobackup: the destination is inside the data directory; choose a directory apart from it")
		}
	}
	return dest.Open(destination, env)
}

// Close stops a backup Start began and waits until it has ended.
func (r *Runner) Close() {
	r.close()
	r.run.Lock()
	r.run.Unlock()
}

// ErrBusy is a backup asked for while another runs.
var ErrBusy = errors.New("autobackup: a backup is running")

// ErrPassphrase is a missing or short passphrase in the environment.
var ErrPassphrase = fmt.Errorf("autobackup: set %s to a passphrase of at least %d characters", PassphraseEnv, backup.MinPassphrase)

func (r *Runner) passphrase() (string, bool) {
	p := r.env(PassphraseEnv)
	return p, len([]rune(p)) >= backup.MinPassphrase
}

// Loop takes the backups that fall due until ctx ends.
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

// Tick takes a backup if one is due.
func (r *Runner) Tick(ctx context.Context) {
	st, err := r.load()
	if err != nil {
		r.log.Error("automatic backup: reading its state", "err", err)
		return
	}
	kind := due(r.now(), r.db.Site.Backup(), st)
	if kind == "" {
		return
	}
	if _, err := r.Run(ctx, kind, ""); err != nil && !errors.Is(err, ErrBusy) {
		r.log.Error("automatic backup failed", "kind", kind, "err", err)
	}
}

// due names the kind of backup the schedule asks for now, or "".
func due(now time.Time, b site.Backup, st state) string {
	if !b.Enabled || b.Destination == "" {
		return ""
	}
	if len(st.History) > 0 && st.History[0].Error != "" && now.Sub(st.LastAttempt) < retryAfter {
		return ""
	}
	if st.Last == nil || st.Destination != b.Destination || now.Sub(st.LastFull) >= time.Duration(b.FullDays)*day-checkEvery {
		return backup.Full
	}
	if b.IncrementalDays > 0 && now.Sub(st.LastBackup) >= time.Duration(b.IncrementalDays)*day-checkEvery {
		return backup.Incremental
	}
	return ""
}

// Run takes a backup now: a full one, or an incremental one when there is
// a chain at the destination to build on. actor is who asked; empty for
// the schedule.
func (r *Runner) Run(ctx context.Context, kind, actor string) (Run, error) {
	if !r.run.TryLock() {
		return Run{}, ErrBusy
	}
	defer r.run.Unlock()
	return r.locked(ctx, kind, actor)
}

// Start takes a backup as Run does, in the background, until Close is
// called; done, if set, is called with its outcome.
func (r *Runner) Start(kind, actor string, done func(Run, error)) error {
	if !r.run.TryLock() {
		return ErrBusy
	}
	r.setRunning(kind)
	go func() {
		defer r.run.Unlock()
		run, err := r.locked(r.ctx, kind, actor)
		if err != nil {
			r.log.Error("backup failed", "kind", kind, "actor", actor, "err", err)
		}
		if done != nil {
			done(run, err)
		}
	}()
	return nil
}

func (r *Runner) locked(ctx context.Context, kind, actor string) (Run, error) {
	r.setRunning(kind)
	defer r.setRunning("")

	start := r.now()
	st, err := r.load()
	if err != nil {
		return Run{}, err
	}
	run, m, err := r.take(ctx, kind, &st)
	run.At, run.Actor, run.Seconds = start.UTC(), actor, r.now().Sub(start).Seconds()
	st.LastAttempt = start
	if err != nil {
		run.Error = err.Error()
		r.db.Audit.Record(audit.Entry{Event: audit.BackupFailed, Actor: actor, Subject: run.File, Detail: map[string]string{"kind": run.Kind, "error": run.Error}})
	} else {
		if m.IsFull() {
			st.LastFull = start
		}
		st.Last, st.LastBackup = &m, start
		r.db.Audit.Record(audit.Entry{Event: audit.BackupCreated, Actor: actor, Subject: run.File, Detail: map[string]string{"kind": run.Kind}})
	}
	st.History = append([]Run{run}, st.History...)
	if len(st.History) > historyLength {
		st.History = st.History[:historyLength]
	}
	if serr := r.save(st); err == nil && serr != nil {
		err = fmt.Errorf("autobackup: the backup is stored, its state is not: %w", serr)
	}
	return run, err
}

func (r *Runner) take(ctx context.Context, kind string, st *state) (Run, backup.Manifest, error) {
	run := Run{Kind: kind}
	var m backup.Manifest
	pass, ok := r.passphrase()
	if !ok {
		return run, m, ErrPassphrase
	}
	settings := r.db.Site.Backup()
	t, err := r.open(settings.Destination, r.env)
	if err != nil {
		return run, m, err
	}
	files, err := t.List(ctx)
	if err != nil {
		return run, m, fmt.Errorf("autobackup: listing the destination: %w", err)
	}
	// An incremental backup builds on the last one only if its whole chain
	// is still at the destination; otherwise it could not be restored.
	var base *backup.Manifest
	if kind == backup.Incremental && st.Last != nil && st.Destination == settings.Destination && chainStored(files, *st.Last) {
		base = st.Last
	}
	run.Kind = backup.Full
	id, err := backup.NewID()
	if err != nil {
		return run, m, err
	}
	chain, seq := id, 0
	if base != nil {
		run.Kind, chain, seq = backup.Incremental, base.Chain, base.Seq+1
	}
	now := r.now()
	run.File = backup.FileName(now, chain, seq)
	snap, err := r.db.Snapshot()
	if err != nil {
		for _, f := range snap {
			f.R.Close()
		}
		return run, m, err
	}
	pr, pw := io.Pipe()
	done := make(chan error, 1)
	go func() {
		var err error
		m, err = backup.WriteBackup(pw, snap, backup.Options{Passphrase: pass, Build: r.build, Now: now, ID: id, Base: base})
		pw.CloseWithError(err)
		done <- err
	}()
	cr := &counter{r: pr}
	err = t.Put(ctx, run.File, cr)
	pr.CloseWithError(errors.New("autobackup: the destination stopped reading"))
	if werr := <-done; err == nil {
		err = werr
	}
	run.Size = cr.n
	if err != nil {
		return run, m, err
	}
	st.Destination = settings.Destination
	if err := prune(ctx, t, append(files, run.File), m.Chain, settings.Keep); err != nil {
		r.log.Warn("automatic backup: deleting old versions", "err", err)
	}
	return run, m, nil
}

// chainStored tells whether every backup of last's chain up to it is at
// the destination.
func chainStored(files []string, last backup.Manifest) bool {
	for _, v := range backup.Versions(files) {
		if v.Chain == last.Chain {
			return v.Complete() && v.Backups[len(v.Backups)-1].Seq >= last.Seq
		}
	}
	return false
}

// prune deletes the versions older than the newest keep, each with its
// incremental backups; current is never deleted.
func prune(ctx context.Context, t dest.Target, files []string, current string, keep int) error {
	versions := backup.Versions(files)
	var errs []error
	for i, v := range versions {
		if i >= len(versions)-keep || v.Chain == current {
			continue
		}
		for _, b := range v.Backups {
			if err := t.Delete(ctx, b.File); err != nil {
				errs = append(errs, err)
			}
		}
	}
	return errors.Join(errs...)
}

type counter struct {
	r io.Reader
	n int64
}

func (c *counter) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}

func (r *Runner) setRunning(kind string) {
	r.mu.Lock()
	r.running = kind
	r.mu.Unlock()
}

func (r *Runner) load() (state, error) {
	var st state
	data, err := r.db.BackupState()
	if err != nil || data == nil {
		return st, err
	}
	if err := json.Unmarshal(data, &st); err != nil {
		return state{}, fmt.Errorf("autobackup: reading its state: %w", err)
	}
	return st, nil
}

func (r *Runner) save(st state) error {
	data, err := json.Marshal(st)
	if err != nil {
		return err
	}
	return r.db.SetBackupState(data)
}

// Status is what an administrator sees of automatic backups.
type Status struct {
	// Passphrase tells whether the environment holds a usable passphrase.
	Passphrase bool `json:"passphrase"`
	// Running names the kind of backup running now, if one is.
	Running string `json:"running,omitempty"`
	// Next is when the schedule takes the next backup, and of which kind.
	Next     time.Time `json:"next,omitzero"`
	NextKind string    `json:"nextKind,omitempty"`
	History  []Run     `json:"history"`
	// Versions are the versions stored at the destination, newest first,
	// or StoredError why they could not be listed.
	Versions    []Version `json:"versions"`
	StoredError string    `json:"storedError,omitempty"`
}

// Version is a stored version: a full backup and the incremental backups
// after it.
type Version struct {
	Chain    string    `json:"chain"`
	Created  time.Time `json:"created"`
	Latest   time.Time `json:"latest"`
	Backups  []string  `json:"backups"`
	Complete bool      `json:"complete"`
}

// Status reports the schedule, the last runs and, if list is set, the
// versions at the destination.
func (r *Runner) Status(ctx context.Context, list bool) (Status, error) {
	st, err := r.load()
	if err != nil {
		return Status{}, err
	}
	s := Status{History: st.History, Versions: []Version{}}
	if s.History == nil {
		s.History = []Run{}
	}
	_, s.Passphrase = r.passphrase()
	r.mu.Lock()
	s.Running = r.running
	r.mu.Unlock()
	settings := r.db.Site.Backup()
	s.Next, s.NextKind = next(r.now(), settings, st)
	if list && settings.Destination != "" {
		t, err := r.open(settings.Destination, r.env)
		var files []string
		if err == nil {
			files, err = t.List(ctx)
		}
		if err != nil {
			s.StoredError = err.Error()
		}
		vs := backup.Versions(files)
		for i := len(vs) - 1; i >= 0; i-- {
			v := Version{Chain: vs[i].Chain, Created: vs[i].Backups[0].At, Latest: vs[i].Backups[len(vs[i].Backups)-1].At, Complete: vs[i].Complete()}
			for _, b := range vs[i].Backups {
				v.Backups = append(v.Backups, b.File)
			}
			s.Versions = append(s.Versions, v)
		}
	}
	return s, nil
}

// next is when the schedule takes its next backup.
func next(now time.Time, b site.Backup, st state) (time.Time, string) {
	if !b.Enabled || b.Destination == "" {
		return time.Time{}, ""
	}
	if kind := due(now, b, st); kind != "" {
		return now, kind
	}
	if len(st.History) > 0 && st.History[0].Error != "" && now.Sub(st.LastAttempt) < retryAfter {
		kind := backup.Incremental
		if st.Last == nil || st.Destination != b.Destination {
			kind = backup.Full
		}
		return st.LastAttempt.Add(retryAfter), kind
	}
	at, kind := st.LastFull.Add(time.Duration(b.FullDays)*day), backup.Full
	if b.IncrementalDays > 0 {
		if inc := st.LastBackup.Add(time.Duration(b.IncrementalDays) * day); inc.Before(at) {
			at, kind = inc, backup.Incremental
		}
	}
	return at, kind
}

// Check tries a destination: it stores a small file there, lists it and
// deletes it again.
func (r *Runner) Check(ctx context.Context, destination string) error {
	t, err := r.open(destination, r.env)
	if err != nil {
		return err
	}
	const probe = "wikiwikibangbang-check.tmp"
	if err := t.Put(ctx, probe, strings.NewReader("wikiwikibangbang checks that it may store backups here\n")); err != nil {
		return fmt.Errorf("storing a file: %w", err)
	}
	files, err := t.List(ctx)
	if err != nil {
		return fmt.Errorf("listing the files: %w", err)
	}
	found := false
	for _, f := range files {
		found = found || f == probe
	}
	if !found {
		return errors.New("a stored file is not listed")
	}
	if err := t.Delete(ctx, probe); err != nil {
		return fmt.Errorf("deleting a file: %w", err)
	}
	return nil
}
