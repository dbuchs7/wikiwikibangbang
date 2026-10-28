package server

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/dbuchs7/wikiwikibangbang/internal/diskspace"
	"github.com/dbuchs7/wikiwikibangbang/internal/item"
	"github.com/dbuchs7/wikiwikibangbang/internal/notify"
	"github.com/dbuchs7/wikiwikibangbang/internal/storage"
)

// diskEvery is how often the free space is measured.
const diskEvery = 5 * time.Minute

// DiskWatch measures the file system of the data directory, keeps the
// level the store goes by and tells administrators when it gets worse
// (ADR-0150).
type DiskWatch struct {
	db       *storage.DB
	messages *Messenger
	log      *slog.Logger
	measure  func(string) (diskspace.Usage, error)
	now      func() time.Time

	mu      sync.Mutex
	last    *diskReport
	told    diskspace.Level
	samples []diskSample
}

// diskReport is what one measurement found.
type diskReport struct {
	Checked    time.Time            `json:"checked"`
	Free       uint64               `json:"free"`
	Total      uint64               `json:"total"`
	Level      string               `json:"level"`
	Thresholds diskspace.Thresholds `json:"thresholds"`
	// Parts are the bytes of the data files and of the attachments.
	Parts map[string]int64 `json:"parts"`
	// Data is what the data directory holds in all.
	Data int64 `json:"data"`
	// GrowthPerDay is how much the data grew per day over the last days,
	// 0 while too little is known; DaysLeft is when the free space would
	// end at that pace.
	GrowthPerDay int64  `json:"growthPerDay"`
	DaysLeft     int64  `json:"daysLeft,omitempty"`
	Problem      string `json:"problem,omitempty"`
	level        diskspace.Level
}

type diskSample struct {
	at   time.Time
	data int64
}

// How the growth is estimated: a sample at most every half hour, the last
// week kept, a pace once six hours are known.
const (
	sampleEvery = 30 * time.Minute
	sampleKeep  = 7 * 24 * time.Hour
	sampleFirst = 6 * time.Hour
)

// NewDiskWatch returns a watch over db's directory; messages may be nil.
func NewDiskWatch(db *storage.DB, messages *Messenger, log *slog.Logger) *DiskWatch {
	if log == nil {
		log = slog.Default()
	}
	return &DiskWatch{db: db, messages: messages, log: log, measure: diskspace.Of, now: time.Now}
}

// Loop measures now and then every few minutes until ctx ends.
func (d *DiskWatch) Loop(ctx context.Context) {
	tick := time.NewTicker(diskEvery)
	defer tick.Stop()
	for {
		d.Check()
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

// Report is the last measurement, or a new one when there is none.
func (d *DiskWatch) Report() diskReport {
	d.mu.Lock()
	last := d.last
	d.mu.Unlock()
	if last == nil {
		return d.Check()
	}
	return *last
}

// Check measures the file system, puts the level in force for the store,
// tells administrators when it got worse and keeps the report.
func (d *DiskWatch) Check() diskReport {
	now := d.now().UTC()
	t := d.db.Site.DiskSpace()
	r := diskReport{Checked: now, Thresholds: t, Parts: map[string]int64{}}
	for name, n := range d.db.Sizes() {
		r.Parts[name] = n
		r.Data += n
	}
	r.Parts[storage.AttachmentsDir] = d.db.AttachmentBytes()
	r.Data += r.Parts[storage.AttachmentsDir]
	u, err := d.measure(d.db.Dir())
	if err != nil {
		// Unmeasured, the store keeps the level it had: a failing
		// measurement neither locks the wiki nor lifts a lock.
		r.Problem = err.Error()
		r.level = d.db.Space()
		d.log.Warn("free space not measured", "err", err)
	} else {
		r.Free, r.Total = u.Free, u.Total
		r.level = diskspace.LevelOf(u, t)
		d.db.SetSpace(r.level)
	}
	r.Level = r.level.String()

	d.mu.Lock()
	defer d.mu.Unlock()
	if n := len(d.samples); n == 0 || now.Sub(d.samples[n-1].at) >= sampleEvery {
		d.samples = append(d.samples, diskSample{at: now, data: r.Data})
	}
	for len(d.samples) > 1 && now.Sub(d.samples[0].at) > sampleKeep {
		d.samples = d.samples[1:]
	}
	if first, span := d.samples[0], now.Sub(d.samples[0].at); span >= sampleFirst && r.Data > first.data {
		r.GrowthPerDay = int64(float64(r.Data-first.data) * float64(24*time.Hour) / float64(span))
		if r.GrowthPerDay > 0 && err == nil {
			r.DaysLeft = int64(r.Free) / r.GrowthPerDay
		}
	}
	switch {
	case err != nil:
	case r.level > d.told:
		d.log.Warn("the data directory's file system is getting full", "level", r.Level, "free", r.Free, "total", r.Total)
		d.tell(r)
		d.told = r.level
	case r.level < d.told:
		d.log.Info("free space recovered", "level", r.Level, "free", r.Free)
		d.told = r.level
	}
	d.last = &r
	return r
}

// tell sends every administrator word of a worse level.
func (d *DiskWatch) tell(r diskReport) {
	if d.messages == nil {
		return
	}
	free, share := formatBytes(r.Free), "?"
	if r.Total > 0 {
		share = formatPercent(r.Free, r.Total)
	}
	key := "disk." + r.Level
	d.messages.later(func(ctx context.Context) {
		for _, u := range d.db.Accounts.Users() {
			admin := false
			d.db.Read(func(s *item.Store) { admin = s.HasSystemRole(string(u.ID), item.RoleAdmin) })
			if !admin {
				continue
			}
			lang := d.db.Accounts.Language(u.ID)
			d.messages.Send(ctx, u.ID, notify.Message{Subject: text(lang, key+".subject", free), Text: text(lang, key+".text", free, share),
				Link: d.messages.n.Link("#/admin")})
		}
	})
}

// formatBytes writes a size as people read it, in GB or MB.
func formatBytes(n uint64) string {
	if n >= 1<<30 {
		return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
	}
	return fmt.Sprintf("%d MB", n>>20)
}

// formatPercent writes part as a share of all.
func formatPercent(part, all uint64) string {
	return fmt.Sprintf("%.1f %%", float64(part)*100/float64(all))
}
