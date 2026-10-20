package item

import (
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/dbuchs7/wikiwikibangbang/internal/governance"
)

// GovernanceActor is the actor of the restrictions the store sets on its
// own when content brings in sensitive data (ADR-0040).
const GovernanceActor = "governance"

// guard returns the restrictions a batch needs: an item whose new content
// holds sensitive data its content did not hold before becomes readable
// only by the person who wrote it, in the same batch, so nobody else can
// read it in between. Space administrators still read it (ADR-0022).
// Data already in the item, e.g. after an administrator lifted the
// restriction, does not restrict it again.
func (s *Store) guard(actor string, batch []Event) []Event {
	var out []Event
	done := map[ID]bool{}
	for _, e := range batch {
		if !e.HasContent() || e.Item == "" || done[e.Item] {
			continue
		}
		old := map[string]bool{}
		var restriction Restriction
		if it := s.items[e.Item]; it != nil {
			old = it.fingerprints()
			restriction = it.Restriction
		}
		added := false
		for fp := range eventFingerprints(e) {
			if !old[fp] {
				added = true
			}
		}
		if !added || slices.Equal(restriction.Read, []string{actor}) {
			continue
		}
		done[e.Item] = true
		r := Restriction{Read: []string{actor}, Edit: slices.Clone(restriction.Edit)}
		out = append(out, Event{Kind: RestrictionSet, Item: e.Item, Restriction: &r, Actor: GovernanceActor})
	}
	return out
}

// contentTexts lists every text of an item's content: title, string
// properties, body blocks, comments, labels and file names.
func (it *Item) contentTexts() []string {
	texts := []string{it.Title}
	for _, v := range it.Properties {
		if s, ok := v.(string); ok {
			texts = append(texts, s)
		}
	}
	for _, b := range it.Body.Blocks {
		texts = append(texts, b.Text)
	}
	for _, c := range it.Comments {
		texts = append(texts, c.Text)
		if c.Anchor != nil {
			texts = append(texts, c.Anchor.Prefix+c.Anchor.Quote+c.Anchor.Suffix)
		}
	}
	texts = append(texts, it.Labels...)
	for _, a := range it.Attachments {
		texts = append(texts, a.Name)
	}
	return texts
}

func (it *Item) fingerprints() map[string]bool { return fingerprintsOf(it.contentTexts()) }

// eventFingerprints identifies the sensitive data an event brings in.
func eventFingerprints(e Event) map[string]bool {
	texts := append([]string{e.Title, e.Text}, e.Labels...)
	if e.Attachment != nil {
		texts = append(texts, e.Attachment.Name)
	}
	if e.Anchor != nil {
		texts = append(texts, e.Anchor.Prefix+e.Anchor.Quote+e.Anchor.Suffix)
	}
	if s, ok := e.Value.(string); ok {
		texts = append(texts, s)
	}
	for _, v := range e.Props {
		if s, ok := v.(string); ok {
			texts = append(texts, s)
		}
	}
	if e.Body != nil {
		for _, b := range e.Body.Blocks {
			texts = append(texts, b.Text)
		}
	}
	return fingerprintsOf(texts)
}

func fingerprintsOf(texts []string) map[string]bool {
	out := map[string]bool{}
	for _, t := range texts {
		for fp := range governance.Fingerprints(t) {
			out[fp] = true
		}
	}
	return out
}

// Report is a governance check of one space (ADR-0040): what the reader
// may read now, with the person responsible for each finding.
type Report struct {
	Sensitive   []SensitiveFinding
	Stale       []StaleItem
	Implausible []Implausible
	// Personal lists the items whose content holds indicators of data in
	// need of protection (ADR-0058): held ones waiting for review, and
	// restricted ones, which are assessed but not held.
	Personal []PersonalData
}

// PersonalData is an item whose content holds indicators of a regulation.
type PersonalData struct {
	Item     ID
	Key      string
	Title    string
	Findings []DPFinding
	// Held reports an open review; Savers then brought the findings in and
	// may not release it.
	Held        bool
	Savers      []string
	Restricted  bool
	Responsible string
	// MayRelease reports whether the reader may release the hold.
	MayRelease bool
}

// SensitiveFinding is sensitive data in an item, shown masked.
type SensitiveFinding struct {
	Item  ID
	Key   string
	Title string
	// Where is "title", "property <name>", "block <n>" or "comment <n>".
	Where       string
	Kind        string
	Masked      string
	Responsible string
	// Restricted reports whether reading the item is restricted.
	Restricted bool
}

// StaleItem is an item not changed for longer than the report's limit.
type StaleItem struct {
	Item        ID
	Key         string
	Title       string
	LastChange  time.Time
	Responsible string
}

// Implausible is a formal contradiction in an item.
type Implausible struct {
	Item        ID
	Key         string
	Title       string
	Problem     string
	Responsible string
}

// dueField matches the names of date fields that are deadlines.
var dueField = regexp.MustCompile(`(?i)due|deadline|f(ä|ae)llig`)

// Governance checks the live items of a space that the principal may
// read: sensitive data, items unchanged for longer than staleAfter before
// now, and implausible values. A space in which the principal holds no
// role is not found.
func (x *Session) Governance(space string, now time.Time, staleAfter time.Duration) (Report, error) {
	var rep Report
	if x.s.spaceRole(x.who, space) < Viewer {
		return rep, notFound("space %q", space)
	}
	var items []*Item
	for _, it := range x.s.items {
		if it.Space == space && it.Presence == Live && x.s.canRead(x.who, it) {
			items = append(items, it)
		}
	}
	slices.SortFunc(items, func(a, b *Item) int { return a.created - b.created })
	for _, it := range items {
		responsible, last := x.s.lastEditor(it.ID)
		x.s.sensitiveIn(it, responsible, &rep)
		if now.Sub(last) > staleAfter {
			rep.Stale = append(rep.Stale, StaleItem{Item: it.ID, Key: it.Key, Title: it.Title, LastChange: last, Responsible: responsible})
		}
		for _, p := range x.s.implausible(it, now) {
			rep.Implausible = append(rep.Implausible, Implausible{Item: it.ID, Key: it.Key, Title: it.Title, Problem: p, Responsible: responsible})
		}
		if pd, ok := x.personal(it, responsible); ok {
			rep.Personal = append(rep.Personal, pd)
		}
	}
	return rep, nil
}

// personal reports an item's data protection findings: its open hold, or
// what the regulations find in a restricted item (ADR-0058).
func (x *Session) personal(it *Item, responsible string) (PersonalData, bool) {
	pd := PersonalData{Item: it.ID, Key: it.Key, Title: it.Title, Restricted: len(it.Restriction.Read) > 0, Responsible: responsible}
	if it.Hold != nil {
		pd.Held, pd.Findings, pd.Savers = true, slices.Clone(it.Hold.Findings), slices.Clone(it.Hold.Savers)
		pd.MayRelease = x.MayEdit(it.ID) && !slices.Contains(it.Hold.Savers, x.who)
		return pd, true
	}
	if x.s.assess == nil {
		return pd, false
	}
	pd.Findings = slices.DeleteFunc(x.s.assess(it.contentTexts()), func(f DPFinding) bool { return slices.Contains(it.Cleared, f.key()) })
	return pd, len(pd.Findings) > 0
}

func (s *Store) sensitiveIn(it *Item, responsible string, rep *Report) {
	add := func(where, text string) {
		for _, f := range governance.Sensitive(text) {
			rep.Sensitive = append(rep.Sensitive, SensitiveFinding{Item: it.ID, Key: it.Key, Title: it.Title, Where: where,
				Kind: f.Kind, Masked: f.Masked, Responsible: responsible, Restricted: len(it.Restriction.Read) > 0})
		}
	}
	add("title", it.Title)
	names := slices.Sorted(maps.Keys(it.Properties))
	for _, n := range names {
		if v, ok := it.Properties[n].(string); ok {
			add("property "+n, v)
		}
	}
	for i, b := range it.Body.Blocks {
		add(fmt.Sprintf("block %d", i+1), b.Text)
	}
	for i, c := range it.Comments {
		add(fmt.Sprintf("comment %d", i+1), c.Text)
	}
}

// implausible lists formal contradictions: a deadline passed while the
// item is not in its workflow's last state; a required field of the
// type's latest version left empty; an article without workflow whose
// body is empty.
func (s *Store) implausible(it *Item, now time.Time) []string {
	var out []string
	td, _ := s.latest(it.Type)
	done := td.Workflow != nil && len(td.Workflow.States) > 0 && it.State == td.Workflow.States[len(td.Workflow.States)-1]
	for _, f := range td.Fields {
		if f.Kind != KindDate || !dueField.MatchString(f.Name) || td.Workflow == nil || done {
			continue
		}
		if d, ok := it.Properties[f.Name].(time.Time); ok && d.Before(now) {
			out = append(out, fmt.Sprintf("%s %s has passed, but the state is %s", f.Name, d.Format("2006-01-02"), it.State))
		}
	}
	for _, f := range td.Fields {
		if f.Required && it.Properties[f.Name] == nil {
			out = append(out, fmt.Sprintf("required field %q is empty", f.Name))
		}
	}
	if td.Body && td.Workflow == nil && bodyEmpty(it.Body) {
		out = append(out, "the body is empty")
	}
	return out
}

func bodyEmpty(d Doc) bool {
	for _, b := range d.Blocks {
		if strings.TrimSpace(b.Text) != "" || b.Kind == Embed {
			return false
		}
	}
	return true
}

// lastEditor is who changed an item's content last, and when the item
// last changed at all.
func (s *Store) lastEditor(id ID) (string, time.Time) {
	t := s.touching[id]
	var who string
	var when time.Time
	for i := len(t) - 1; i >= 0; i-- {
		e := s.log[t[i]]
		if when.IsZero() {
			when = e.At
		}
		if e.Item == id && e.HasContent() && e.Actor != GovernanceActor && e.Kind != HoldReleased {
			who = e.Actor
			break
		}
	}
	return who, when
}

// Findings returns the governance findings of every space the principal
// holds a role in for which the principal is responsible, i.e. whose
// content they changed last (ADR-0057). It is what their bell reminds them
// of; the reports per space list everyone's.
func (x *Session) Findings(now time.Time, staleAfter time.Duration) Report {
	var mine Report
	for _, sp := range x.Spaces() {
		rep, err := x.Governance(sp.Key, now, staleAfter)
		if err != nil {
			continue
		}
		for _, f := range rep.Sensitive {
			if f.Responsible == x.who {
				mine.Sensitive = append(mine.Sensitive, f)
			}
		}
		for _, f := range rep.Stale {
			if f.Responsible == x.who {
				mine.Stale = append(mine.Stale, f)
			}
		}
		for _, f := range rep.Implausible {
			if f.Responsible == x.who {
				mine.Implausible = append(mine.Implausible, f)
			}
		}
		// A hold is the business of whoever may release it and of whoever
		// brought it in (ADR-0058).
		for _, f := range rep.Personal {
			if f.Held && (f.MayRelease || slices.Contains(f.Savers, x.who)) {
				mine.Personal = append(mine.Personal, f)
			}
		}
	}
	return mine
}
