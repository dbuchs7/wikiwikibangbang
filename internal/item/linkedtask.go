package item

import (
	"maps"
	"slices"
	"strings"
)

// LinkedSystem is the origin system of a linked task: an item that is a
// user task of a process engine (ADR-0151). The origin's instance is the
// engine's ID and its external ID "task/" and the task's key.
const LinkedSystem = "engine"

// LinkedTask is a user task of a process engine as the synchronisation
// wants its item to be (ADR-0151).
type LinkedTask struct {
	// Engine is the engine's ID, Key the task's key there and URL where
	// the engine shows it.
	Engine, Key, URL string
	// Space and Type are where a new item is made; an item made before
	// stays where it is.
	Space, Type string
	// Title is the task's name.
	Title string
	// Fields are the fields the engine owns, by name; a nil value clears
	// one. The item's other fields stay as people set them.
	Fields Props
	// Labels are the labels a new item is made with; an item made before
	// keeps the labels people gave it.
	Labels []string
}

// LinkedOrigin is the origin of the item of a task of an engine.
func LinkedOrigin(engine, key string) Origin {
	return Origin{System: LinkedSystem, Instance: engine, ExternalID: "task/" + key, ExternalKey: key}
}

// LinkTask makes or updates the item of a user task of an engine, matched
// by its origin, as the trusted synchronisation (ADR-0151). A new item is
// restricted, for reading and editing, to the synchronisation's actor —
// to nobody a person is — in the same batch, so it is never readable before
// the engine has said who sees it; it carries the labels it is made with.
// An item made before keeps its place, state, labels and restriction, and
// the fields the engine does not own. An item no longer live is left as
// it is.
func (s *Store) LinkTask(actor string, t LinkedTask) (Item, bool, error) {
	if !ValidEngineID(t.Engine) || strings.TrimSpace(t.Key) == "" {
		return Item{}, false, invalid("a linked task needs an engine and a key")
	}
	origin := LinkedOrigin(t.Engine, t.Key)
	origin.URL = t.URL
	owned := slices.Sorted(maps.Keys(t.Fields))
	u := Upsert{Origin: origin, Space: t.Space, Type: t.Type, Title: t.Title,
		Linked: t.Engine, LinkedFields: owned}
	id, existed := s.bySource[origin.key()]
	if existed {
		it := s.items[id]
		if it.Presence != Live {
			return it.clone(), false, nil
		}
		u.Space, u.Type, u.Parent = it.Space, it.Type, it.Parent
		u.Properties = maps.Clone(it.Properties)
	}
	if u.Properties == nil {
		u.Properties = Props{}
	}
	for name, v := range t.Fields {
		if v == nil {
			delete(u.Properties, name)
		} else {
			u.Properties[name] = v
		}
	}
	var labels []string
	if !existed {
		for _, l := range t.Labels {
			n, ok := NormalLabel(l)
			if !ok {
				return Item{}, false, invalid("label %q: use letters and digits, joined by -, _ or ., up to 64 characters", l)
			}
			labels = append(labels, n)
		}
		slices.Sort(labels)
		labels = slices.Compact(labels)
	}
	e, current, changed, err := s.upsertEvent(u)
	if err != nil || !changed {
		return current, false, err
	}
	events := []Event{e}
	if !existed {
		events = append(events, Event{Kind: RestrictionSet, Item: e.Item,
			Restriction: &Restriction{Read: []string{actor}, Edit: []string{actor}}})
		if len(labels) > 0 {
			events = append(events, Event{Kind: LabelsSet, Item: e.Item, Labels: labels})
		}
	}
	if err := s.recordBatch(actor, events...); err != nil {
		return Item{}, false, err
	}
	return s.items[e.Item].clone(), true, nil
}

// EndTask moves the item of a linked task to state, as its engine ended the
// task: by the one transition from the item's state that leads there
// (D3). An item already in that state, or no longer live, is left; a
// state no single transition reaches is refused.
func (s *Store) EndTask(actor string, id ID, state string) error {
	it, ok := s.items[id]
	if !ok || it.Presence != Live || it.State == state {
		return nil
	}
	if it.Linked == "" {
		return invalid("%s is no linked task", it.Key)
	}
	td, _ := s.latest(it.Type)
	if td.Workflow == nil {
		return invalid("type %q has no workflow", td.Name)
	}
	name, err := td.Workflow.only(it.State, state)
	if err != nil {
		return err
	}
	return s.Transition(actor, id, name)
}

// only is the name of the one transition from state from to state to.
func (w *Workflow) only(from, to string) (string, error) {
	var names []string
	for _, tr := range w.Transitions {
		if tr.To == to && slices.Contains(tr.From, from) {
			names = append(names, tr.Name)
		}
	}
	switch len(names) {
	case 0:
		return "", invalid("no transition leads from %q to %q", from, to)
	case 1:
		return names[0], nil
	}
	return "", invalid("several transitions lead from %q to %q: %s", from, to, strings.Join(names, ", "))
}

// LinkedTasks returns the live items of the tasks of an engine, in the
// order they were made.
func (s *Store) LinkedTasks(engine string) []Item {
	var out []Item
	for _, it := range s.items {
		if it.Linked == engine && it.Presence == Live {
			out = append(out, it.clone())
		}
	}
	slices.SortFunc(out, func(a, b Item) int { return a.created - b.created })
	return out
}

// TaskReach checks that items of a type in a space can be the linked
// tasks of an engine whose ended tasks go to the states completed and
// cancelled, the engine owning the fields owned (ADR-0151): every field the
// type requires is one of them; the type has a workflow with both states,
// each a finished one; and every other state has one transition to each
// that requires no field, so the synchronisation can always end a task.
func (s *Store) TaskReach(space, typeName, completed, cancelled string, owned []string) error {
	if !s.HasSpace(space) {
		return notFound("space %q", space)
	}
	td, ok := s.latest(typeName)
	if !ok {
		return notFound("type %q", typeName)
	}
	for _, f := range td.Fields {
		if f.Required && !slices.Contains(owned, f.Name) {
			return invalid("type %q requires the field %q, which the engine does not set", td.Name, f.Name)
		}
	}
	for _, name := range owned {
		if _, ok := td.field(name); !ok {
			return invalid("type %q has no field %q", td.Name, name)
		}
	}
	w := td.Workflow
	if w == nil {
		return invalid("type %q has no workflow", td.Name)
	}
	for _, end := range []string{completed, cancelled} {
		if !slices.Contains(w.States, end) {
			return invalid("type %q has no state %q", td.Name, end)
		}
		if !w.Finished(end) {
			return invalid("state %q of type %q is not finished: a transition leaves it", end, td.Name)
		}
		for _, from := range w.States {
			if from == end || w.Finished(from) {
				continue
			}
			name, err := w.only(from, end)
			if err != nil {
				return err
			}
			if tr, _ := w.transition(name); len(tr.Requires) > 0 {
				return invalid("transition %q requires %s; the synchronisation cannot fill it", name, strings.Join(tr.Requires, ", "))
			}
		}
	}
	return nil
}

// linkedRefusal is why a person may not change a linked task in a way only
// its engine may (ADR-0151), or nil.
func linkedRefusal(it *Item, what string) error {
	if it.Linked == "" {
		return nil
	}
	return forbidden("%s is a task of the engine %s; %s", it.Key, it.Linked, what)
}
