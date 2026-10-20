package item

import (
	"slices"
	"strings"
)

// Data protection review (ADR-0058). Regulations such as the Swiss Federal
// Act on Data Protection name categories of personal data that need special
// protection; an installation keeps them, with indicators, in its settings
// (internal/site). When content brings in an indicator, an item that is open
// to its whole space is put on hold: only those who may edit it read it,
// until a second person releases it, either because reading is restricted
// to a circle of people or because the finding is a false alarm. An item
// already restricted is assessed but not held.

// DPFinding is one indicator found in content: of a category of a
// regulation.
type DPFinding struct {
	Regulation string
	Category   string
	Indicator  string
}

func (f DPFinding) key() string { return f.Regulation + "\x00" + f.Category + "\x00" + f.Indicator }

// Hold is an item's open data protection review.
type Hold struct {
	Findings []DPFinding
	// Savers are those whose saves brought the findings in; none of them
	// may release the hold (four eyes).
	Savers []string
	// Since is the sequence number of the event that set the hold.
	Since int `json:"-"`
}

// How a hold is released.
const (
	HoldRestricted = "restricted"  // reading is restricted to a circle of people
	HoldFalseAlarm = "false-alarm" // no data in need of protection
)

// Assessor finds the indicators of the active regulations in texts. The
// store calls it only for live commands, never on replay, so the outcome
// is in the log (HoldSet) and replay stays deterministic.
type Assessor func(texts []string) []DPFinding

// SetAssessor sets the assessment of content on save; nil turns it off.
func (s *Store) SetAssessor(a Assessor) { s.assess = a }

// eventTexts lists the texts of user content an event brings in.
func eventTexts(e Event) []string {
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
	return texts
}

// holds returns the holds a batch needs (ADR-0058): for each item whose
// content brings in findings it did not hold before, or that a batch makes
// readable by its whole space while it holds findings, a HoldSet unless the
// item stays restricted. restricted are the restrictions earlier in the
// batch, including governance's own (ADR-0040).
func (s *Store) holds(actor string, batch []Event) []Event {
	if s.assess == nil {
		return nil
	}
	type change struct {
		texts  []string
		opened bool
	}
	changes := map[ID]*change{}
	var order []ID
	readAfter := map[ID][]string{}
	for _, e := range batch {
		if e.Item == "" || e.Kind == HoldSet || e.Kind == HoldReleased {
			continue
		}
		if e.Kind == RestrictionSet && e.Restriction != nil {
			readAfter[e.Item] = e.Restriction.Read
		}
		if !e.HasContent() && e.Kind != RestrictionSet {
			continue
		}
		c := changes[e.Item]
		if c == nil {
			c = &change{}
			changes[e.Item] = c
			order = append(order, e.Item)
		}
		if e.Kind == RestrictionSet {
			c.opened = e.Restriction != nil && len(e.Restriction.Read) == 0
			continue
		}
		c.texts = append(c.texts, eventTexts(e)...)
	}
	var out []Event
	for _, id := range order {
		c := changes[id]
		it := s.items[id]
		var hold Hold
		var read, cleared []string
		var old []DPFinding
		if it != nil {
			read, cleared = it.Restriction.Read, it.Cleared
			if it.Hold != nil {
				hold = *it.Hold
			}
			old = s.assess(it.contentTexts())
		}
		if r, ok := readAfter[id]; ok {
			read = r
		}
		known := map[string]bool{}
		for _, f := range hold.Findings {
			known[f.key()] = true
		}
		for _, k := range cleared {
			known[k] = true
		}
		var added []DPFinding
		consider := func(fs []DPFinding, unlessOld bool) {
			for _, f := range fs {
				if known[f.key()] || (unlessOld && slices.ContainsFunc(old, func(o DPFinding) bool { return o.key() == f.key() })) {
					continue
				}
				known[f.key()] = true
				added = append(added, f)
			}
		}
		consider(s.assess(c.texts), true)
		if c.opened {
			// Opening the item to its space brings its findings to everyone.
			consider(old, false)
		}
		// An item restricted to people or groups is assessed, not held; one
		// already held collects what is added.
		if len(added) == 0 || (len(read) > 0 && hold.Findings == nil) {
			continue
		}
		next := Hold{Findings: append(slices.Clone(hold.Findings), added...), Savers: slices.Clone(hold.Savers)}
		if !slices.Contains(next.Savers, actor) {
			next.Savers = append(next.Savers, actor)
		}
		out = append(out, Event{Kind: HoldSet, Item: id, Hold: &next, Actor: GovernanceActor})
	}
	return out
}

func (s *Store) applyHold(e Event, touch func(...ID)) {
	it := s.items[e.Item]
	if it == nil {
		return
	}
	touch(it.ID)
	it.Version++
	switch e.Kind {
	case HoldSet:
		h := Hold{Since: e.Seq}
		if e.Hold != nil {
			h.Findings, h.Savers = slices.Clone(e.Hold.Findings), slices.Clone(e.Hold.Savers)
		}
		it.Hold = &h
	case HoldReleased:
		if e.Outcome == HoldFalseAlarm && it.Hold != nil {
			for _, f := range it.Hold.Findings {
				if !slices.Contains(it.Cleared, f.key()) {
					it.Cleared = append(it.Cleared, f.key())
				}
			}
		}
		it.Hold = nil
	}
}

// --- sessions ---------------------------------------------------------------

// ReleaseHold ends an item's data protection review. The one releasing
// must be able to edit the item and must not have brought the findings in
// (four eyes). HoldRestricted needs the item's reading restricted to named
// people or groups; HoldFalseAlarm needs a reason, and the findings then do
// not hold the item again.
func (x *Session) ReleaseHold(id ID, outcome, reason string) error {
	it, err := x.editable(id)
	if err != nil {
		return err
	}
	if it.Hold == nil {
		return invalid("%s waits for no data protection review", it.Key)
	}
	if slices.Contains(it.Hold.Savers, x.who) {
		return forbidden("four eyes: someone other than who saved the content releases %s", it.Key)
	}
	reason = strings.TrimSpace(reason)
	switch outcome {
	case HoldRestricted:
		if len(it.Restriction.Read) == 0 {
			return invalid("restrict reading %s to people or groups first", it.Key)
		}
	case HoldFalseAlarm:
		if reason == "" {
			return invalid("a false alarm needs a reason")
		}
	default:
		return invalid("outcome %q: %s or %s", outcome, HoldRestricted, HoldFalseAlarm)
	}
	return x.s.record(x.who, Event{Kind: HoldReleased, Item: id, Outcome: outcome, Text: reason})
}
