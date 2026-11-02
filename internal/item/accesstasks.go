package item

import (
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

// An access request becomes a task for the page's owner (ADR-0162). A
// request to read a protection (ADR-0088, ADR-0160) is recorded with a
// task item, made by the server in the same batch on behalf of the one who
// asks: in the room of the protected item, below it, of a type with a
// workflow and a user field named assignee, assigned to the item's owner
// or, failing that, to the one who protected the content. The task is
// restricted to those it concerns and labelled AccessLabel; deciding the
// request finishes it by its workflow, and finishing it without deciding
// declines the request. Nothing here knows a type by its name (D6): types
// are chosen by their definition, and the only names are those of the
// fields the maintainer chose, owner and assignee.

// AccessActorPrefix begins the actor of what the access-request mechanism
// makes; AccessActor appends the one it acts for.
const AccessActorPrefix = "access-request:"

// AccessActor is who makes the task of a request: no person, so its
// history tells it apart from what people do, and it names the one the
// task is made for, as the synchronisation of an engine names its engine
// (ADR-0151).
func AccessActor(requester string) string { return AccessActorPrefix + requester }

// AccessLabel labels every task made for an access request, so boards and
// the task list filter them.
const AccessLabel = "access-request"

// OwnerField and AssigneeField are the user fields the mechanism reads: the
// item's owner, who is assigned the task and also decides about the item's
// protections, and the task's assignee (ADR-0162).
const (
	OwnerField    = "owner"
	AssigneeField = "assignee"
)

// AccessTask is what the title and text of a request's task are made
// from. It holds nothing of the protected content: only which protection
// of which item, counted from 1 as the item's card counts them.
type AccessTask struct {
	// Owner: the task is assigned to the item's owner, not to the one who
	// protected the content.
	Requester, Assignee string
	Owner               bool
	Item                ID
	Key, Title, Space   string
	Number              int
	Blocks              bool
	At                  time.Time
}

// AccessOutcome is how a request's task ends.
type AccessOutcome int

// The outcomes of a request.
const (
	// AccessGranted: the one who asked reads the protection now.
	AccessGranted AccessOutcome = iota + 1
	// AccessDeclined: the request was declined.
	AccessDeclined
	// AccessClosed: the task was finished without a decision, which
	// declines the request.
	AccessClosed
	// AccessRemoved: the protection was taken off or erased; there is
	// nothing left to decide.
	AccessRemoved
)

// AccessTexts words the task of a request and the comments that record
// how it ended. The server words them in the language of the one who reads
// them (ADR-0017); without it, plain English is used. They run in commands
// only, never on replay, so the log alone rebuilds the state.
type AccessTexts interface {
	Task(a AccessTask) (title string, body Doc)
	Outcome(o AccessOutcome, actor string, a AccessTask) string
}

// SetAccessTexts sets the words of the tasks of access requests; nil uses
// plain English.
func (s *Store) SetAccessTexts(t AccessTexts) { s.accessTexts = t }

func (s *Store) texts() AccessTexts {
	if s.accessTexts == nil {
		return plainAccessTexts{}
	}
	return s.accessTexts
}

// plainAccessTexts are the English words used without the server's: they
// name people by their IDs.
type plainAccessTexts struct{}

func (plainAccessTexts) Task(a AccessTask) (string, Doc) {
	what := "protected passage"
	if a.Blocks {
		what = "protected blocks"
	}
	return fmt.Sprintf("Access request: %s for «%s»", a.Requester, a.Title), Doc{Blocks: []Block{
		MentionParagraph(fmt.Sprintf("%s asked on %s to read %s %d of ", a.Requester, a.At.Format("2006-01-02"), what, a.Number), a.Item, a.Title, "."),
		{Kind: Paragraph, Text: "Grant or decline with the buttons on this task; the decision closes it. Closing it without deciding declines the request."},
	}}
}

func (plainAccessTexts) Outcome(o AccessOutcome, actor string, _ AccessTask) string {
	switch o {
	case AccessGranted:
		return "Granted by " + actor + "."
	case AccessDeclined:
		return "Declined by " + actor + "."
	case AccessClosed:
		return "Closed without granting; declined by " + actor + "."
	}
	return "The protection was taken off by " + actor + "; there is nothing left to decide."
}

// MentionParagraph is a paragraph that mentions an item by its label
// between two texts, for the text of a request's task.
func MentionParagraph(before string, item ID, label, after string) Block {
	start := utf8.RuneCountInString(before)
	return Block{Kind: Paragraph, Text: before + label + after, Mentions: []ID{item},
		Marks: []Mark{{Type: MarkMention, Start: start, End: start + utf8.RuneCountInString(label), Ref: item}}}
}

// TypeTemplates are the ready-made types an installation starts from
// (types are data, ADR-0001): the UI offers them to administrators, and a
// request's task is of the first of them that can hold one when the
// installation has no such type yet (ADR-0162).
func TypeTemplates() []TypeDef {
	return []TypeDef{
		{Name: "page", Body: true, Fields: []FieldDef{{Name: "owner", Kind: KindUser}}},
		{Name: "task", Body: true,
			Fields: []FieldDef{{Name: "assignee", Kind: KindUser}, {Name: "due", Kind: KindDate}, {Name: "priority", Kind: KindEnum, Values: []string{"low", "normal", "high"}}},
			Workflow: &Workflow{Initial: "open", States: []string{"open", "in progress", "done"}, Transitions: []Transition{
				{Name: "start", From: []string{"open"}, To: "in progress"},
				{Name: "finish", From: []string{"open", "in progress"}, To: "done"},
				{Name: "reopen", From: []string{"done"}, To: "open"}}}},
		{Name: "decision", Body: true, Fields: []FieldDef{{Name: "assignee", Kind: KindUser}, {Name: "decided", Kind: KindDate}},
			Workflow: &Workflow{Initial: "proposed", States: []string{"proposed", "accepted", "rejected"}, Transitions: []Transition{
				{Name: "accept", From: []string{"proposed"}, To: "accepted"},
				{Name: "reject", From: []string{"proposed"}, To: "rejected"}}}},
	}
}

// holdsRequests reports whether items of a type can be the task of a
// request: it has a body and a workflow, a user field named assignee and
// no other required field, and from its initial state a transition
// finishes it.
func holdsRequests(td TypeDef) bool {
	if !td.Body || td.Workflow == nil {
		return false
	}
	if f, ok := td.field(AssigneeField); !ok || f.Kind != KindUser {
		return false
	}
	if slices.ContainsFunc(td.Fields, func(f FieldDef) bool { return f.Required && f.Name != AssigneeField }) {
		return false
	}
	_, ok := finishing(td.Workflow, td.Workflow.Initial, Props{AssigneeField: "x"})
	return ok
}

// finishing names the transition that finishes an item in state from
// with the properties props (ADR-0082): one to the workflow's last state
// if there is one, else one to another finished state, the first in the
// workflow's order; one whose required fields are empty does not count.
func finishing(w *Workflow, from string, props Props) (string, bool) {
	last := w.States[len(w.States)-1]
	found := ""
	for _, tr := range w.Transitions {
		if !slices.Contains(tr.From, from) || !w.Finished(tr.To) ||
			slices.ContainsFunc(tr.Requires, func(f string) bool { return props[f] == nil }) {
			continue
		}
		if tr.To == last {
			return tr.Name, true
		}
		if found == "" {
			found = tr.Name
		}
	}
	return found, found != ""
}

// requestType chooses the type of a request's task: the first ready-made
// type that can hold one, if it is defined and can; otherwise the other
// type that can and is most like it. With neither, and the ready-made type's
// name free, the ready-made type is to be defined (define). ok is false
// when no type can be had: the request is then made without a task.
func (s *Store) requestType() (td TypeDef, define, ok bool) {
	var tmpl TypeDef
	for _, t := range TypeTemplates() {
		if holdsRequests(t) {
			tmpl = t
			break
		}
	}
	cur, exists := s.latest(tmpl.Name)
	if exists && holdsRequests(cur) {
		return cur, false, true
	}
	// Of the others, the one most like the template: the most of its
	// fields and states, then by name.
	best, score := TypeDef{}, -1
	for _, t := range s.Types() {
		if n := likeness(t, tmpl); holdsRequests(t) && n > score {
			best, score = t, n
		}
	}
	if score >= 0 {
		return best, false, true
	}
	if exists {
		return TypeDef{}, false, false
	}
	tmpl.Version = len(s.types[tmpl.Name]) + 1
	return tmpl, true, true
}

// likeness counts the fields (by name and kind) and the workflow states a
// type shares with the template.
func likeness(t, tmpl TypeDef) int {
	n := 0
	for _, f := range tmpl.Fields {
		if g, ok := t.field(f.Name); ok && g.Kind == f.Kind {
			n++
		}
	}
	if t.Workflow != nil && tmpl.Workflow != nil {
		for _, st := range tmpl.Workflow.States {
			if slices.Contains(t.Workflow.States, st) {
				n++
			}
		}
	}
	return n
}

// ownerOf is the item's owner: the value of its user field named owner.
func (s *Store) ownerOf(it *Item) string {
	td, _ := s.latest(it.Type)
	if f, ok := td.field(OwnerField); !ok || f.Kind != KindUser {
		return ""
	}
	o, _ := it.Properties[OwnerField].(string)
	return o
}

// assigneeOf chooses who is assigned a request's task: the item's owner,
// if one is set who reads the item and is not the one who asks (owner);
// otherwise the one who protected the content.
func (s *Store) assigneeOf(it *Item, sec Secret, requester string) (who string, owner bool) {
	if o := s.ownerOf(it); o != "" && o != requester && s.canRead(o, it) {
		return o, true
	}
	return sec.By, false
}

// answersRequest reports whether the principal answers a request of
// requester to read a protection: those who decide about it (ADR-0088),
// and the item's owner, if they read the item, for the requests of others
// (ADR-0162). The owner answers requests only: they do not change the
// readers, take the protection off or answer their own request.
func (s *Store) answersRequest(principal string, it *Item, sec Secret, requester string) bool {
	if s.decidesSecret(principal, it, sec) {
		return true
	}
	return principal != requester && s.answersAsOwner(principal, it)
}

func (s *Store) answersAsOwner(principal string, it *Item) bool {
	o := s.ownerOf(it)
	return o != "" && o == principal && s.canRead(principal, it)
}

// requestTask returns the events that make the task of a request, and the
// task's ID; none when the item is not live or no type can hold the task.
// They follow the request in its batch and are the mechanism's
// (AccessActor): the task is made for one who may only read the room.
func (s *Store) requestTask(it *Item, sec Secret, requester string) ([]Event, ID, error) {
	if it.Presence != Live {
		return nil, "", nil
	}
	td, define, ok := s.requestType()
	if !ok {
		return nil, "", nil
	}
	i, _ := it.secret(sec.ID)
	assignee, owner := s.assigneeOf(it, sec, requester)
	a := AccessTask{Requester: requester, Assignee: assignee, Owner: owner, Item: it.ID, Key: it.Key, Title: it.Title, Space: it.Space,
		Number: i + 1, Blocks: it.protectsBlocks(sec.ID), At: s.now().UTC()}
	title, body := s.texts().Task(a)
	if strings.TrimSpace(title) == "" {
		return nil, "", invalid("the task of a request has no title")
	}
	props := Props{AssigneeField: assignee}
	if err := checkProps(td, props); err != nil {
		return nil, "", err
	}
	actor := AccessActor(requester)
	id := s.newID()
	var events []Event
	if define {
		events = append(events, Event{Kind: TypeDefined, TypeDef: &td, Actor: actor})
	}
	created := Event{Kind: ItemCreated, Item: id, Space: it.Space, TypeName: td.Name, TypeVersion: td.Version, Parent: it.ID, Title: title, Props: props, Actor: actor}
	for j, b := range body.Blocks {
		if err := checkBlock(b, id); err != nil {
			return nil, "", wrapf(err, "the task's block %d", j+1)
		}
	}
	for _, r := range body.refs() {
		if t := s.items[r.To]; t == nil || t.Presence != Live {
			return nil, "", notFound("referenced item %s", r.To)
		}
	}
	text := body.clone()
	// Who it concerns reads it: the one assigned, the one who asks, the
	// one who protected the content and the item's owner; the room's
	// administrators read it as they read everything (ADR-0022). Like any
	// restriction, it only narrows (D9).
	who := []string{assignee, requester, sec.By}
	if o := s.ownerOf(it); o != "" && s.canRead(o, it) {
		who = append(who, o)
	}
	var seen []string
	for _, p := range who {
		if p != "" && !slices.Contains(seen, p) {
			seen = append(seen, p)
		}
	}
	events = append(events, created,
		Event{Kind: BodyCommitted, Item: id, Body: &text, Actor: actor},
		Event{Kind: LabelsSet, Item: id, Labels: []string{AccessLabel}, Actor: actor},
		Event{Kind: RestrictionSet, Item: id, Restriction: &Restriction{Read: seen, Edit: slices.Clone(seen)}, Actor: actor},
		// The one assigned and the one who asks follow it, so they hear
		// of each other's comments (ADR-0046).
		Event{Kind: Watched, Item: id, Principal: assignee, Actor: actor})
	if requester != assignee {
		events = append(events, Event{Kind: Watched, Item: id, Principal: requester, Actor: actor})
	}
	return events, id, nil
}

// accessRef is the request a task was made for: the protection of an
// item, and who asked.
type accessRef struct {
	item, secret ID
	principal    string
	at           time.Time
}

// waitingOn returns the item, protection and principal of the request a
// task was made for, while the request waits with this task.
func (s *Store) waitingOn(task ID) (*Item, Secret, string, bool) {
	ref, ok := s.accessTasks[task]
	if !ok {
		return nil, Secret{}, "", false
	}
	it := s.items[ref.item]
	if it == nil || it.Presence == Purged {
		return nil, Secret{}, "", false
	}
	i, ok := it.secret(ref.secret)
	if !ok {
		return nil, Secret{}, "", false
	}
	sec := it.Secrets[i]
	if !slices.Contains(sec.Requests, ref.principal) || sec.asked[ref.principal].task != task {
		return nil, Secret{}, "", false
	}
	return it, sec, ref.principal, true
}

// endTask returns the events that record how a request ended on its task:
// a comment by the actor, and the transition that finishes the task, when
// finish is set and it is not finished yet. A request without a live task
// has none.
func (s *Store) endTask(it *Item, sec Secret, principal string, o AccessOutcome, actor string, finish bool) []Event {
	task := sec.asked[principal].task
	t := s.items[task]
	if task == "" || t == nil || t.Presence != Live {
		return nil
	}
	i, _ := it.secret(sec.ID)
	a := AccessTask{Requester: principal, Assignee: fmt.Sprint(t.Properties[AssigneeField]), Item: it.ID, Key: it.Key, Title: it.Title, Space: it.Space,
		Number: i + 1, Blocks: it.protectsBlocks(sec.ID), At: sec.asked[principal].at}
	events := []Event{{Kind: CommentAdded, Item: task, Comment: s.newID(), Text: s.texts().Outcome(o, actor, a), Actor: actor}}
	td, _ := s.latest(t.Type)
	if finish && td.Workflow != nil && !td.Workflow.Finished(t.State) {
		if name, ok := finishing(td.Workflow, t.State, t.Properties); ok {
			tr, _ := td.Workflow.transition(name)
			events = append(events, Event{Kind: Transitioned, Item: task, Transition: name, From: t.State, To: tr.To, Actor: actor})
		}
	}
	return events
}

// --- sessions ---------------------------------------------------------------

// AccessRequestView is the request a task was made for, as one who reads
// both the task and the protected item sees it (ADR-0162).
type AccessRequestView struct {
	Item              ID
	Key, Title, Space string
	Secret            ID
	Number            int
	Blocks            bool
	Principal         string
	At                time.Time
	// Waiting: the request still waits with this task. Answers: the
	// reader may grant or decline it.
	Waiting, Answers bool
}

// AccessRequestOf returns the request a readable task was made for, if it
// was made for one and the reader reads the protected item too (D10).
func (x *Session) AccessRequestOf(task ID) (AccessRequestView, bool) {
	if _, err := x.readable(task); err != nil {
		return AccessRequestView{}, false
	}
	ref, ok := x.s.accessTasks[task]
	if !ok {
		return AccessRequestView{}, false
	}
	it, err := x.readable(ref.item)
	if err != nil {
		return AccessRequestView{}, false
	}
	v := AccessRequestView{Item: it.ID, Key: it.Key, Title: it.Title, Space: it.Space, Secret: ref.secret, Principal: ref.principal,
		At: ref.at}
	if i, ok := it.secret(ref.secret); ok {
		v.Number, v.Blocks = i+1, it.protectsBlocks(ref.secret)
	}
	if _, sec, p, ok := x.s.waitingOn(task); ok {
		v.Waiting = true
		v.Answers = it.Presence == Live && x.s.answersRequest(x.who, it, sec, p)
	}
	return v, true
}

// closeTask finishes the task of a waiting request by its transition name
// as the principal, who answers the request: the request is declined in
// the same batch, with a comment that says so (ADR-0162). One who does not
// answer it may not finish the task while it waits.
func (x *Session) closeTask(t *Item, name string) (bool, error) {
	it, sec, p, ok := x.s.waitingOn(t.ID)
	if !ok {
		return false, nil
	}
	td, _ := x.s.latest(t.Type)
	if td.Workflow == nil {
		return false, nil
	}
	tr, ok := td.Workflow.transition(name)
	if !ok || !td.Workflow.Finished(tr.To) {
		return false, nil
	}
	if !x.s.canRead(x.who, it) || it.Presence != Live || !x.s.answersRequest(x.who, it, sec, p) {
		return true, forbidden("%s holds a request to read a protection of a page; those who decide it close it by granting or declining", t.Key)
	}
	e, err := x.s.transitionEvent(t.ID, name)
	if err != nil {
		return true, err
	}
	events := append([]Event{e}, Event{Kind: SecretDeclined, Item: it.ID, Secret: &Secret{ID: sec.ID}, Principal: p})
	events = append(events, x.s.endTask(it, sec, p, AccessClosed, x.who, false)...)
	return true, x.s.recordBatch(x.who, events...)
}

// mayChangeOwner refuses a change of the owner field of an item with
// protections by one who neither reads them all nor administers the room:
// the owner answers requests to read them (ADR-0162), so whoever sets the
// owner hands that on.
func (x *Session) mayChangeOwner(it *Item, field string) error {
	if field != OwnerField || len(it.Secrets) == 0 {
		return nil
	}
	if td, _ := x.s.latest(it.Type); !slices.ContainsFunc(td.Fields, func(f FieldDef) bool { return f.Name == OwnerField && f.Kind == KindUser }) {
		return nil
	}
	if x.s.readsAllSecrets(x.who, it) || x.s.spaceRole(x.who, it.Space) >= SpaceAdmin {
		return nil
	}
	return forbidden("%s holds protected content; its owner, who answers requests to read it, is set by those who read it all and the room's administrators", it.Key)
}
