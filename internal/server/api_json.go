package server

import (
	"slices"
	"time"

	"github.com/dbuchs7/wikiwikibangbang/internal/item"
)

// The JSON shapes of the item API (ADR-0028). They are separate from the
// model's Go types, so a field rename in the model is a compile error here,
// not a silent API change.

type fieldJSON struct {
	Name     string         `json:"name"`
	Kind     item.FieldKind `json:"kind"`
	Required bool           `json:"required,omitempty"`
	Values   []string       `json:"values,omitempty"`
}

type transitionJSON struct {
	Name     string   `json:"name"`
	From     []string `json:"from"`
	To       string   `json:"to"`
	Requires []string `json:"requires,omitempty"`
}

type workflowJSON struct {
	Initial     string           `json:"initial"`
	States      []string         `json:"states"`
	Transitions []transitionJSON `json:"transitions"`
}

type typeJSON struct {
	Name     string        `json:"name"`
	Version  int           `json:"version,omitempty"`
	Fields   []fieldJSON   `json:"fields"`
	Workflow *workflowJSON `json:"workflow,omitempty"`
	Body     bool          `json:"body"`
}

func toTypeJSON(td item.TypeDef) typeJSON {
	t := typeJSON{Name: td.Name, Version: td.Version, Body: td.Body, Fields: []fieldJSON{}}
	for _, f := range td.Fields {
		t.Fields = append(t.Fields, fieldJSON{Name: f.Name, Kind: f.Kind, Required: f.Required, Values: f.Values})
	}
	if wf := td.Workflow; wf != nil {
		t.Workflow = &workflowJSON{Initial: wf.Initial, States: wf.States, Transitions: []transitionJSON{}}
		for _, tr := range wf.Transitions {
			t.Workflow.Transitions = append(t.Workflow.Transitions, transitionJSON{Name: tr.Name, From: tr.From, To: tr.To, Requires: tr.Requires})
		}
	}
	return t
}

func (t typeJSON) toTypeDef() item.TypeDef {
	td := item.TypeDef{Name: t.Name, Body: t.Body}
	for _, f := range t.Fields {
		td.Fields = append(td.Fields, item.FieldDef{Name: f.Name, Kind: f.Kind, Required: f.Required, Values: f.Values})
	}
	if wf := t.Workflow; wf != nil {
		td.Workflow = &item.Workflow{Initial: wf.Initial, States: wf.States}
		for _, tr := range wf.Transitions {
			td.Workflow.Transitions = append(td.Workflow.Transitions, item.Transition{Name: tr.Name, From: tr.From, To: tr.To, Requires: tr.Requires})
		}
	}
	return td
}

type spaceJSON struct {
	Key  string `json:"key"`
	Name string `json:"name"`
	// Role is the caller's role in the space (viewer, editor or
	// space-admin), so the UI offers only what the caller may do.
	Role string `json:"role,omitempty"`
}

type markJSON struct {
	Type  item.MarkType `json:"type"`
	Start int           `json:"start"`
	End   int           `json:"end"`
	Href  string        `json:"href,omitempty"`
	Ref   item.ID       `json:"ref,omitempty"`
}

type blockJSON struct {
	Kind     item.BlockKind `json:"kind"`
	Text     string         `json:"text,omitempty"`
	Level    int            `json:"level,omitempty"`
	Marks    []markJSON     `json:"marks,omitempty"`
	Mentions []item.ID      `json:"mentions,omitempty"`
	Ref      item.ID        `json:"ref,omitempty"`
	Checked  bool           `json:"checked,omitempty"`
	Tone     string         `json:"tone,omitempty"`
	Width    int            `json:"width,omitempty"`
}

func toBlockJSON(b item.Block) blockJSON {
	j := blockJSON{Kind: b.Kind, Text: b.Text, Level: b.Level, Mentions: b.Mentions, Ref: b.Ref, Checked: b.Checked, Tone: b.Tone, Width: b.Width}
	for _, m := range b.Marks {
		j.Marks = append(j.Marks, markJSON{Type: m.Type, Start: m.Start, End: m.End, Href: m.Href, Ref: m.Ref})
	}
	return j
}

// toDoc accepts the block kinds a person writes. "unsupported" blocks come
// only from importers (ADR-0023): a person may keep one that the current
// body already has (kept reports it), never add one. A block mentions every
// item a mention mark points to, so derived relations follow the marks
// (ADR-0032).
func toDoc(blocks []blockJSON, kept func(text string) bool) (item.Doc, error) {
	var d item.Doc
	for i, b := range blocks {
		switch b.Kind {
		case item.Paragraph, item.Heading, item.Embed, item.BulletItem, item.OrderedItem, item.Quote, item.CodeBlock,
			item.TaskItem, item.Panel, item.Table, item.TOC, item.Children, item.Image:
		case item.Unsupported:
			if kept == nil || !kept(b.Text) {
				return d, badInput("block %d: unsupported blocks come only from imports", i+1)
			}
		default:
			return d, badInput("block %d: kind must be paragraph, heading, bullet, ordered, quote, code, task, panel, table, toc, children, image or embed", i+1)
		}
		blk := item.Block{Kind: b.Kind, Text: b.Text, Level: b.Level, Mentions: b.Mentions, Ref: b.Ref, Checked: b.Checked, Tone: b.Tone, Width: b.Width}
		for _, m := range b.Marks {
			blk.Marks = append(blk.Marks, item.Mark{Type: m.Type, Start: m.Start, End: m.End, Href: m.Href, Ref: m.Ref})
			if m.Type == item.MarkMention && m.Ref != "" && !slices.Contains(blk.Mentions, m.Ref) {
				blk.Mentions = append(blk.Mentions, m.Ref)
			}
		}
		d.Blocks = append(d.Blocks, blk)
	}
	return d, nil
}

type restrictionJSON struct {
	Read []string `json:"read"`
	Edit []string `json:"edit"`
}

type originJSON struct {
	System      string `json:"system"`
	Instance    string `json:"instance"`
	ExternalID  string `json:"externalId"`
	ExternalKey string `json:"externalKey,omitempty"`
	URL         string `json:"url,omitempty"`
}

type itemJSON struct {
	ID          item.ID          `json:"id"`
	Key         string           `json:"key"`
	Type        string           `json:"type"`
	TypeVersion int              `json:"typeVersion"`
	Space       string           `json:"space"`
	Parent      item.ID          `json:"parent,omitempty"`
	Title       string           `json:"title"`
	Properties  map[string]any   `json:"properties"`
	State       string           `json:"state,omitempty"`
	Body        []blockJSON      `json:"body"`
	Version     int              `json:"version"`
	Presence    item.Presence    `json:"presence"`
	Restriction *restrictionJSON `json:"restriction,omitempty"`
	Origin      *originJSON      `json:"origin,omitempty"`
	Managed     string           `json:"managed,omitempty"`
	Labels      []string         `json:"labels"`
}

// toItemJSON omits the raw import source: it is large and is content kept
// for re-conversion, not for display.
func toItemJSON(it item.Item) itemJSON {
	j := itemJSON{ID: it.ID, Key: it.Key, Type: it.Type, TypeVersion: it.TypeVersion, Space: it.Space,
		Parent: it.Parent, Title: it.Title, Properties: map[string]any(it.Properties), State: it.State,
		Body: []blockJSON{}, Version: it.Version, Presence: it.Presence, Managed: it.Managed, Labels: it.Labels}
	if j.Labels == nil {
		j.Labels = []string{}
	}
	if j.Properties == nil {
		j.Properties = map[string]any{}
	}
	for _, b := range it.Body.Blocks {
		j.Body = append(j.Body, toBlockJSON(b))
	}
	if len(it.Restriction.Read)+len(it.Restriction.Edit) > 0 {
		j.Restriction = &restrictionJSON{Read: it.Restriction.Read, Edit: it.Restriction.Edit}
	}
	if o := it.Origin; o != nil {
		j.Origin = &originJSON{System: o.System, Instance: o.Instance, ExternalID: o.ExternalID, ExternalKey: o.ExternalKey, URL: o.URL}
	}
	return j
}

type embedJSON struct {
	Ref        item.ID `json:"ref,omitempty"`
	Key        string  `json:"key,omitempty"`
	Title      string  `json:"title,omitempty"`
	Child      bool    `json:"child"`
	Presence   string  `json:"presence,omitempty"`
	Restricted bool    `json:"restricted,omitempty"`
}

type relationJSON struct {
	ID      item.ID `json:"id"`
	Type    string  `json:"type"`
	From    item.ID `json:"from"`
	To      item.ID `json:"to"`
	Derived bool    `json:"derived,omitempty"`
}

// eventJSON is one history entry: who did what, when. Bodies are not
// repeated in the history; the item shows the current one.
type eventJSON struct {
	Seq        int            `json:"seq"`
	At         time.Time      `json:"at"`
	Actor      string         `json:"actor"`
	Kind       item.EventKind `json:"kind"`
	Item       item.ID        `json:"item,omitempty"`
	Erased     bool           `json:"erased,omitempty"`
	Title      string         `json:"title,omitempty"`
	Field      string         `json:"field,omitempty"`
	Value      any            `json:"value,omitempty"`
	Transition string         `json:"transition,omitempty"`
	From       string         `json:"from,omitempty"`
	To         string         `json:"to,omitempty"`
	Space      string         `json:"space,omitempty"`
	Parent     item.ID        `json:"parent,omitempty"`
	Reason     string         `json:"reason,omitempty"`
	// Comment names the comment of a comment event; its text is never part
	// of the history, so a deleted comment stays hidden (ADR-0035).
	Comment item.ID `json:"comment,omitempty"`
	// Labels are the item's labels after a LabelsSet (ADR-0044).
	Labels []string `json:"labels,omitempty"`
}

func toEventJSON(e item.Event) eventJSON {
	return eventJSON{Seq: e.Seq, At: e.At, Actor: e.Actor, Kind: e.Kind, Item: e.Item, Erased: e.Erased,
		Title: e.Title, Field: e.Field, Value: e.Value, Transition: e.Transition, From: e.From, To: e.To,
		Space: e.Space, Parent: e.Parent, Reason: e.Reason, Comment: e.Comment, Labels: e.Labels}
}
