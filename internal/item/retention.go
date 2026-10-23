package item

import (
	"cmp"
	"slices"
	"time"
)

// The retention of a file's older versions (ADR-0104). A file edited
// again, e.g. a drawing, keeps every version it was (ADR-0055); the album
// shows them as one stack (ADR-0103). A rule says which older versions
// go: the album's default, set by administrators, or a rule of the stack's
// own, set by whoever may edit its item, which takes precedence.

// Limits of a retention rule.
const (
	maxRetentionKeep = 1000
	maxRetentionDays = 36500
)

// RetentionActor is the actor of the versions the automatic clean-up
// removes.
const RetentionActor = "retention"

// Retention keeps the Keep newest versions of a file, the current one
// counted, and every version younger than Days days. A version neither
// keeps is removed; 0 sets no limit of its kind, so a rule with neither
// keeps everything. The current version and the versions the item's text
// shows are always kept.
type Retention struct {
	Keep int `json:",omitempty"`
	Days int `json:",omitempty"`
}

// Validate checks the rule's limits.
func (r Retention) Validate() error {
	switch {
	case r.Keep < 0 || r.Keep > maxRetentionKeep:
		return invalid("keep 1 to %d versions, or 0 for no limit by number", maxRetentionKeep)
	case r.Days < 0 || r.Days > maxRetentionDays:
		return invalid("keep versions younger than 1 to %d days, or 0 for no limit by age", maxRetentionDays)
	}
	return nil
}

// Removes reports whether the rule removes anything at all.
func (r Retention) Removes() bool { return r.Keep > 0 || r.Days > 0 }

// keeps reports whether the rule keeps the version at place i of its
// stack, 0 being the newest, added at created.
func (r Retention) keeps(i int, created int64, now time.Time) bool {
	switch {
	case i == 0 || !r.Removes():
		return true
	case r.Keep > 0 && i < r.Keep:
		return true
	case r.Days > 0 && now.Sub(time.Unix(created, 0)) < time.Duration(r.Days)*24*time.Hour:
		return true
	}
	return false
}

// RetentionPlan is what a rule keeps and removes of one stack of versions,
// newest first.
type RetentionPlan struct {
	First ID
	Rule  Retention
	// Own is true when the rule is the stack's own, not the default.
	Own    bool
	Keep   []Attachment
	Remove []Attachment
	// Used are the kept versions the rule alone would remove: the item's
	// text shows them.
	Used []ID
	// Versions are all versions, kept or not, newest first.
	Versions []Attachment
}

// versions returns the versions of a file in its item that are not
// deleted, newest first: of two added in the same second, the later.
func versions(it *Item, first ID) []Attachment {
	type placed struct {
		a     Attachment
		order int
	}
	var out []placed
	for i, a := range it.Attachments {
		if !a.Deleted && firstVersion(it, a) == first {
			out = append(out, placed{a, i})
		}
	}
	slices.SortFunc(out, func(a, b placed) int {
		if c := cmp.Compare(b.a.Created, a.a.Created); c != 0 {
			return c
		}
		return cmp.Compare(b.order, a.order)
	})
	as := make([]Attachment, len(out))
	for i, p := range out {
		as[i] = p.a
	}
	return as
}

// ruleOf returns the stack's own rule, held by its first version.
func ruleOf(it *Item, first ID) *Retention {
	if a, ok := it.attachment(first); ok && a.Retention != nil {
		r := *a.Retention
		return &r
	}
	return nil
}

// plan applies a rule to a stack. rule nil uses the stack's own rule, or
// def without one.
func plan(it *Item, first ID, rule *Retention, def Retention, now time.Time) RetentionPlan {
	p := RetentionPlan{First: first, Rule: def}
	if own := ruleOf(it, first); own != nil {
		p.Rule, p.Own = *own, true
	}
	if rule != nil {
		p.Rule = *rule
	}
	p.Versions = versions(it, first)
	for i, a := range p.Versions {
		switch {
		case p.Rule.keeps(i, a.Created, now):
			p.Keep = append(p.Keep, a)
		case inBody(it.Body, a.ID):
			p.Keep = append(p.Keep, a)
			p.Used = append(p.Used, a.ID)
		default:
			p.Remove = append(p.Remove, a)
		}
	}
	return p
}

// stackOf finds the first version of a file of a readable item.
func (x *Session) stackOf(id, attachment ID) (*Item, ID, error) {
	it, err := x.readable(id)
	if err != nil {
		return nil, "", err
	}
	a, ok := it.attachment(attachment)
	if !ok || a.Deleted {
		return nil, "", notFound("attachment %s of item %s", attachment, it.Key)
	}
	return it, firstVersion(it, *a), nil
}

// PlanRetention tells what a rule would keep and remove of the versions of
// a file: rule, or without one the stack's own rule, or def. It changes
// nothing, so a reader may ask.
func (x *Session) PlanRetention(id, attachment ID, rule *Retention, def Retention, now time.Time) (RetentionPlan, error) {
	if rule != nil {
		if err := rule.Validate(); err != nil {
			return RetentionPlan{}, err
		}
	}
	it, first, err := x.stackOf(id, attachment)
	if err != nil {
		return RetentionPlan{}, err
	}
	return plan(it, first, rule, def, now), nil
}

// SetPictureRetention gives the versions of a file a rule of their own,
// kept on the first version; nil returns them to the default. Whoever may
// edit the item may.
func (x *Session) SetPictureRetention(id, attachment ID, rule *Retention) error {
	if rule != nil {
		if err := rule.Validate(); err != nil {
			return err
		}
	}
	if _, err := x.editable(id); err != nil {
		return err
	}
	it, first, err := x.stackOf(id, attachment)
	if err != nil {
		return err
	}
	if it.Presence != Live {
		return invalid("%s is %s", it.Key, it.Presence)
	}
	own := ruleOf(it, first)
	if (own == nil && rule == nil) || (own != nil && rule != nil && *own == *rule) {
		return nil
	}
	var r *Retention
	if rule != nil {
		c := *rule
		r = &c
	}
	return x.s.record(x.who, Event{Kind: AttachmentRetentionSet, Item: id, Attachment: &Attachment{ID: first, Retention: r}})
}

// CleanVersions removes the versions of a file that the rule (as for
// PlanRetention) does not keep, in one change, and returns the plan it
// followed. Whoever may edit the item may.
func (x *Session) CleanVersions(id, attachment ID, rule *Retention, def Retention, now time.Time) (RetentionPlan, error) {
	if _, err := x.editable(id); err != nil {
		return RetentionPlan{}, err
	}
	p, err := x.PlanRetention(id, attachment, rule, def, now)
	if err != nil {
		return RetentionPlan{}, err
	}
	if it, _ := x.s.item(id); it == nil || it.Presence != Live {
		return RetentionPlan{}, invalid("only the files of a live item are cleaned up")
	}
	return p, x.s.removeVersions(x.who, id, p.Remove)
}

func (s *Store) removeVersions(actor string, id ID, remove []Attachment) error {
	if len(remove) == 0 {
		return nil
	}
	events := make([]Event, len(remove))
	for i, a := range remove {
		events[i] = Event{Kind: AttachmentDeleted, Item: id, Attachment: &Attachment{ID: a.ID}}
	}
	return s.recordBatch(actor, events...)
}

// CleanedStack is what the automatic clean-up removed of one stack.
type CleanedStack struct {
	Item    ID
	First   ID
	Removed int
}

// PlanAllRetention applies each stack's rule, or def, to the files of
// every live item for which keep returns true, and returns the plans that
// remove something. Items a connector keeps are left to it (ADR-0077), and
// items held for a data protection review to the review (ADR-0058).
func (s *Store) PlanAllRetention(def Retention, now time.Time, keep func(Attachment) bool) map[ID][]RetentionPlan {
	out := map[ID][]RetentionPlan{}
	for _, it := range s.items {
		if it.Presence != Live || it.Managed != "" || it.Hold != nil {
			continue
		}
		seen := map[ID]bool{}
		for _, a := range it.Attachments {
			if a.Deleted || !keep(a) {
				continue
			}
			first := firstVersion(it, a)
			if seen[first] {
				continue
			}
			seen[first] = true
			if p := plan(it, first, nil, def, now); len(p.Remove) > 0 {
				out[it.ID] = append(out[it.ID], p)
			}
		}
	}
	return out
}

// ApplyRetention removes, as RetentionActor, what PlanAllRetention plans:
// one change per item.
func (s *Store) ApplyRetention(def Retention, now time.Time, keep func(Attachment) bool) ([]CleanedStack, error) {
	plans := s.PlanAllRetention(def, now, keep)
	ids := make([]ID, 0, len(plans))
	for id := range plans {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	var done []CleanedStack
	for _, id := range ids {
		var remove []Attachment
		var cleaned []CleanedStack
		for _, p := range plans[id] {
			remove = append(remove, p.Remove...)
			cleaned = append(cleaned, CleanedStack{Item: id, First: p.First, Removed: len(p.Remove)})
		}
		if err := s.removeVersions(RetentionActor, id, remove); err != nil {
			return done, err
		}
		done = append(done, cleaned...)
	}
	return done, nil
}
