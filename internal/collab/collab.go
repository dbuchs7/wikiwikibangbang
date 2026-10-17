// Package collab holds live editing sessions on item bodies (ADR-0010).
//
// A session is ephemeral server state: the blocks of one body, each with a
// session-local ID, an optional lock and two versions — the draft being
// typed and the stable text its last holder released. Clients (one per
// browser tab or agent connection) receive every change as an event and
// send operations; a block is edited only by the client holding its lock.
//
// Releasing a lock commits the body as an ordinary version, attributed to
// the releaser (ADR-0001, ADR-0008). A commit contains only stable text, so
// one person's unfinished paragraph never appears in a version attributed
// to someone else. Locks lapse after a TTL without activity and when their
// client leaves; both commit the draft as its holder, so no typed text is
// silently dropped.
package collab

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"slices"
	"sync"
	"time"

	"github.com/dbuchs7/wikiwikibangbang/internal/item"
)

// Budgets (ADR-0015).
const (
	MaxSessions = 1000
	MaxClients  = 50   // per session
	MaxBlocks   = 5000 // per session
	// LockTTL is how long a lock lasts without an operation by its holder.
	LockTTL = 60 * time.Second
	// buffer is the events a client may fall behind before it is dropped.
	buffer = 256
)

var (
	// ErrLocked means another client holds a block the operation needs.
	ErrLocked = errors.New("locked")
	// ErrNotJoined means the client is not, or no longer, in the session.
	ErrNotJoined = errors.New("not in the session")
	// ErrForbidden means the principal may read but not edit the item.
	ErrForbidden = errors.New("forbidden")
	// ErrDiverged means the body was changed outside the session; the
	// session has been reset to the stored body.
	ErrDiverged = errors.New("the body was changed outside the live session")
	// ErrBusy means a budget is exhausted.
	ErrBusy = errors.New("too many live sessions or participants")
	// ErrInvalid means an operation is malformed.
	ErrInvalid = errors.New("invalid operation")
)

// Backend is the item store as the hub needs it. Its methods are called
// with the hub's lock held and must not call back into the hub.
type Backend interface {
	// Access reports what principal may do with the item: read, edit. An
	// item that does not exist or is not live is not readable.
	Access(id item.ID, principal string) (read, edit bool)
	// Body returns the stored body.
	Body(id item.ID) (item.Doc, error)
	// Commit stores a body as a new version by principal.
	Commit(id item.ID, principal string, d item.Doc) error
}

// Holder is the client holding a lock.
type Holder struct {
	Client    string
	Principal string
	Name      string
}

// Block is one block of a session.
type Block struct {
	ID     string
	Block  item.Block
	Holder *Holder
}

// Participant is a principal with at least one client in the session.
type Participant struct {
	Principal string
	Name      string
}

// Event kinds.
const (
	Snapshot  = "snapshot"  // Blocks, Order, Participants; first event of a client, and after a reset
	Set       = "block"     // ID, Block, Holder: a block's draft changed
	Insert    = "insert"    // ID, After, Block, Holder, Order
	Delete    = "delete"    // ID, Order
	Lock      = "lock"      // ID, Holder (nil: released)
	Presence  = "presence"  // Participants
	Committed = "committed" // By, Name: a version was stored
	Closed    = "closed"    // Reason; the stream ends
)

// Event is one message to the clients of a session.
type Event struct {
	Kind         string
	Seq          int
	Client       string // the client the stream belongs to (snapshot only)
	Editable     bool   // whether that client may edit (snapshot only)
	ID           string
	After        string
	Block        *item.Block
	Holder       *Holder
	Blocks       []Block
	Order        []string
	Participants []Participant
	By           string // the principal who committed
	Name         string // By's display name
	Reason       string
}

// Op is one operation of a client. All operations of one Apply succeed or
// fail together.
type Op struct {
	Op    string // "lock", "set", "insert", "delete", "release"
	ID    string
	After string // insert: the block to insert after; empty inserts first
	Block item.Block
}

type sblock struct {
	id        string
	draft     item.Block
	stable    item.Block
	hasStable bool // false for a block inserted and never released
	holder    *client
	touched   time.Time
}

type client struct {
	id, principal, name string
	ch                  chan Event
}

type session struct {
	id        item.ID
	blocks    []*sblock
	committed item.Doc // the stored body, as far as the session knows
	clients   map[string]*client
	seq       int
}

// Hub holds every live session.
type Hub struct {
	mu       sync.Mutex
	b        Backend
	now      func() time.Time
	sessions map[item.ID]*session
}

// Option configures a Hub.
type Option func(*Hub)

// WithClock replaces time.Now, for tests.
func WithClock(now func() time.Time) Option { return func(h *Hub) { h.now = now } }

// NewHub returns a hub over a backend.
func NewHub(b Backend, opts ...Option) *Hub {
	h := &Hub{b: b, now: time.Now, sessions: map[item.ID]*session{}}
	for _, o := range opts {
		o(h)
	}
	return h
}

// Sub is a client's subscription to a session.
type Sub struct {
	// Client identifies the client in operations.
	Client string
	// C delivers the events, starting with a snapshot. It is closed when
	// the client leaves, falls behind, loses access or the session closes.
	C   <-chan Event
	hub *Hub
	id  item.ID
}

// Leave ends the subscription and releases the client's locks.
func (s *Sub) Leave() { s.hub.leave(s.id, s.Client) }

func newID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

var clientIDRE = regexp.MustCompile(`^[A-Za-z0-9_-]{1,40}$`)

// Join adds a client of principal (shown as name) to the item's session,
// opening the session if needed. Reading is enough to join; editing is
// checked per operation.
func (h *Hub) Join(id item.ID, principal, name string) (*Sub, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	read, edit := h.b.Access(id, principal)
	if !read {
		return nil, fmt.Errorf("%w: item %s", item.ErrNotFound, id)
	}
	s := h.sessions[id]
	if s == nil {
		if len(h.sessions) >= MaxSessions {
			return nil, ErrBusy
		}
		doc, err := h.b.Body(id)
		if err != nil {
			return nil, err
		}
		s = &session{id: id, clients: map[string]*client{}}
		s.load(doc)
		h.sessions[id] = s
	}
	if len(s.clients) >= MaxClients {
		return nil, ErrBusy
	}
	c := &client{id: newID(), principal: principal, name: name, ch: make(chan Event, buffer)}
	s.clients[c.id] = c
	h.send(s, c, Event{Kind: Snapshot, Client: c.id, Editable: edit, Blocks: s.snapshot(), Order: s.order(), Participants: s.participants()})
	h.broadcast(s, Event{Kind: Presence, Participants: s.participants()})
	return &Sub{Client: c.id, C: c.ch, hub: h, id: id}, nil
}

func (s *session) load(doc item.Doc) {
	s.committed = cloneDoc(doc)
	s.blocks = s.blocks[:0]
	for _, b := range doc.Blocks {
		s.blocks = append(s.blocks, &sblock{id: newID(), draft: b, stable: b, hasStable: true})
	}
}

func (s *session) snapshot() []Block {
	out := make([]Block, len(s.blocks))
	for i, b := range s.blocks {
		out[i] = Block{ID: b.id, Block: b.draft, Holder: holder(b.holder)}
	}
	return out
}

func (s *session) order() []string {
	out := make([]string, len(s.blocks))
	for i, b := range s.blocks {
		out[i] = b.id
	}
	return out
}

func (s *session) participants() []Participant {
	var out []Participant
	for _, c := range s.clients {
		if !slices.ContainsFunc(out, func(p Participant) bool { return p.Principal == c.principal }) {
			out = append(out, Participant{Principal: c.principal, Name: c.name})
		}
	}
	slices.SortFunc(out, func(a, b Participant) int {
		if a.Name < b.Name {
			return -1
		}
		if a.Name > b.Name {
			return 1
		}
		return 0
	})
	return out
}

func (s *session) find(id string) int {
	return slices.IndexFunc(s.blocks, func(b *sblock) bool { return b.id == id })
}

func holder(c *client) *Holder {
	if c == nil {
		return nil
	}
	return &Holder{Client: c.id, Principal: c.principal, Name: c.name}
}

// send delivers an event to one client; a client that has fallen too far
// behind is dropped rather than slowing everyone down.
func (h *Hub) send(s *session, c *client, e Event) {
	s.seq++
	e.Seq = s.seq
	select {
	case c.ch <- e:
	default:
		h.drop(s, c)
	}
}

func (h *Hub) broadcast(s *session, e Event) {
	s.seq++
	e.Seq = s.seq
	for _, c := range s.clients {
		select {
		case c.ch <- e:
		default:
			h.drop(s, c)
		}
	}
}

// drop removes a client without committing: it also runs where the hub
// cannot commit (while sending). Its locks become free; leave commits
// before dropping, and Tick discards drafts a dropped client left behind,
// since nobody can finish or answer for them.
func (h *Hub) drop(s *session, c *client) {
	if _, ok := s.clients[c.id]; !ok {
		return
	}
	delete(s.clients, c.id)
	close(c.ch)
	for _, b := range s.blocks {
		if b.holder == c {
			b.holder = nil
			b.touched = time.Time{}
		}
	}
}

func (h *Hub) leave(id item.ID, clientID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	s := h.sessions[id]
	if s == nil {
		return
	}
	c := s.clients[clientID]
	if c == nil {
		return
	}
	h.releaseAll(s, c)
	h.drop(s, c)
	h.broadcast(s, Event{Kind: Presence, Participants: s.participants()})
	h.gc(s)
}

// gc closes a session nobody is in and nothing is pending in.
func (h *Hub) gc(s *session) {
	if len(s.clients) > 0 {
		return
	}
	for _, b := range s.blocks {
		if !b.hasStable || !reflect.DeepEqual(b.draft, b.stable) {
			return
		}
	}
	delete(h.sessions, s.id)
}

// releaseAll commits the drafts of every block c holds, as c's principal.
func (h *Hub) releaseAll(s *session, c *client) {
	var held []*sblock
	for _, b := range s.blocks {
		if b.holder == c {
			held = append(held, b)
		}
	}
	if len(held) > 0 {
		h.release(s, c.principal, held, false)
	}
}

// release makes the drafts of blocks stable, frees their locks and commits
// the body as principal. If the commit fails and keep is set, the blocks
// stay locked and unchanged so the holder can correct them; otherwise the
// drafts are discarded.
func (h *Hub) release(s *session, principal string, blocks []*sblock, keep bool) error {
	type saved struct {
		stable    item.Block
		hasStable bool
	}
	before := make([]saved, len(blocks))
	for i, b := range blocks {
		before[i] = saved{b.stable, b.hasStable}
		b.stable, b.hasStable = b.draft, true
	}
	err := h.commit(s, principal)
	if errors.Is(err, ErrDiverged) {
		return err
	}
	for i, b := range blocks {
		if err != nil {
			b.stable, b.hasStable = before[i].stable, before[i].hasStable
			if keep {
				continue
			}
			if b.hasStable {
				b.draft = b.stable
				h.broadcast(s, Event{Kind: Set, ID: b.id, Block: ptr(b.draft)})
			}
		}
		b.holder, b.touched = nil, time.Time{}
		h.broadcast(s, Event{Kind: Lock, ID: b.id})
	}
	if err != nil && !keep {
		h.dropUnstable(s)
	}
	return err
}

// dropUnstable removes blocks that were inserted, never released and are
// no longer held: nobody can finish them.
func (h *Hub) dropUnstable(s *session) {
	for i := 0; i < len(s.blocks); {
		b := s.blocks[i]
		if !b.hasStable && b.holder == nil {
			s.blocks = slices.Delete(s.blocks, i, i+1)
			h.broadcast(s, Event{Kind: Delete, ID: b.id, Order: s.order()})
			continue
		}
		i++
	}
}

func ptr(b item.Block) *item.Block { return &b }

// commit stores the stable blocks as principal, unless they equal the
// stored body. A body changed outside the session resets the session.
func (h *Hub) commit(s *session, principal string) error {
	var doc item.Doc
	for _, b := range s.blocks {
		if b.hasStable {
			doc.Blocks = append(doc.Blocks, b.stable)
		}
	}
	if reflect.DeepEqual(normal(doc), normal(s.committed)) {
		return nil
	}
	stored, err := h.b.Body(s.id)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(normal(stored), normal(s.committed)) {
		h.reset(s, stored)
		return ErrDiverged
	}
	if err := h.b.Commit(s.id, principal, doc); err != nil {
		return err
	}
	s.committed = cloneDoc(doc)
	name := principal
	for _, c := range s.clients {
		if c.principal == principal {
			name = c.name
		}
	}
	h.broadcast(s, Event{Kind: Committed, By: principal, Name: name})
	return nil
}

// reset reloads the stored body, dropping every lock and draft.
func (h *Hub) reset(s *session, stored item.Doc) {
	s.load(stored)
	h.broadcast(s, Event{Kind: Snapshot, Blocks: s.snapshot(), Order: s.order(), Participants: s.participants()})
}

// normal makes empty and nil slices compare equal.
func normal(d item.Doc) item.Doc {
	out := item.Doc{}
	for _, b := range d.Blocks {
		if len(b.Mentions) == 0 {
			b.Mentions = nil
		}
		if len(b.Marks) == 0 {
			b.Marks = nil
		}
		out.Blocks = append(out.Blocks, b)
	}
	return out
}

func cloneDoc(d item.Doc) item.Doc {
	out := item.Doc{Blocks: make([]item.Block, len(d.Blocks))}
	for i, b := range d.Blocks {
		b.Mentions = slices.Clone(b.Mentions)
		b.Marks = slices.Clone(b.Marks)
		out.Blocks[i] = b
	}
	return out
}

// Apply runs a client's operations. Locks are taken implicitly by lock,
// set and delete, and an inserted block is locked by its inserter. Any
// release commits the body as principal.
func (h *Hub) Apply(id item.ID, clientID, principal string, ops []Op) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	s := h.sessions[id]
	if s == nil {
		return ErrNotJoined
	}
	c := s.clients[clientID]
	if c == nil || c.principal != principal {
		return ErrNotJoined
	}
	if _, edit := h.b.Access(id, principal); !edit {
		return ErrForbidden
	}
	if len(ops) == 0 {
		return fmt.Errorf("%w: no operations", ErrInvalid)
	}

	// Validate against a copy, so a batch applies completely or not at all.
	work := make([]*sblock, len(s.blocks))
	for i, b := range s.blocks {
		cp := *b
		work[i] = &cp
	}
	find := func(bid string) int { return slices.IndexFunc(work, func(b *sblock) bool { return b.id == bid }) }
	now := h.now()
	var events []Event
	var released []string
	take := func(b *sblock) error {
		if b.holder != nil && b.holder != c {
			return fmt.Errorf("%w: %s is editing this block", ErrLocked, b.holder.name)
		}
		if b.holder == nil {
			events = append(events, Event{Kind: Lock, ID: b.id, Holder: holder(c)})
		}
		b.holder, b.touched = c, now
		return nil
	}
	check := func(b item.Block, current *item.Block) error {
		if b.Kind == item.Unsupported && (current == nil || !reflect.DeepEqual(normalBlock(b), normalBlock(*current))) {
			return fmt.Errorf("%w: imported unsupported blocks can be kept or deleted, not written", ErrInvalid)
		}
		return item.CheckBlock(b, id)
	}
	for n, op := range ops {
		at := -1
		if op.Op != "insert" {
			if at = find(op.ID); at < 0 {
				return fmt.Errorf("%w: operation %d: no block %q", ErrInvalid, n+1, op.ID)
			}
		}
		switch op.Op {
		case "lock":
			if err := take(work[at]); err != nil {
				return err
			}
		case "set":
			b := work[at]
			if err := take(b); err != nil {
				return err
			}
			if err := check(op.Block, &b.draft); err != nil {
				return err
			}
			b.draft = op.Block
			events = append(events, Event{Kind: Set, ID: b.id, Block: ptr(op.Block), Holder: holder(c)})
		case "insert":
			if !clientIDRE.MatchString(op.ID) || find(op.ID) >= 0 {
				return fmt.Errorf("%w: operation %d: block ID %q is malformed or taken", ErrInvalid, n+1, op.ID)
			}
			if len(work) >= MaxBlocks {
				return ErrBusy
			}
			pos := 0
			if op.After != "" {
				if pos = find(op.After) + 1; pos == 0 {
					return fmt.Errorf("%w: operation %d: no block %q", ErrInvalid, n+1, op.After)
				}
			}
			if err := check(op.Block, nil); err != nil {
				return err
			}
			b := &sblock{id: op.ID, draft: op.Block, holder: c, touched: now}
			work = slices.Insert(work, pos, b)
			events = append(events, Event{Kind: Insert, ID: b.id, After: op.After, Block: ptr(op.Block), Holder: holder(c)})
		case "delete":
			if err := take(work[at]); err != nil {
				return err
			}
			work = slices.Delete(work, at, at+1)
			events = append(events, Event{Kind: Delete, ID: op.ID})
			released = slices.DeleteFunc(released, func(r string) bool { return r == op.ID })
		case "release":
			if work[at].holder == c && !slices.Contains(released, op.ID) {
				released = append(released, op.ID)
			}
		default:
			return fmt.Errorf("%w: operation %d: unknown operation %q", ErrInvalid, n+1, op.Op)
		}
	}

	// Commit the batch to the session.
	deleted := slices.ContainsFunc(events, func(e Event) bool { return e.Kind == Delete })
	s.blocks = work
	for _, e := range events {
		if e.Kind == Insert || e.Kind == Delete {
			e.Order = s.order()
		}
		h.broadcast(s, e)
	}
	var rel []*sblock
	for _, r := range released {
		rel = append(rel, s.blocks[s.find(r)])
	}
	switch {
	case len(rel) > 0:
		return h.release(s, principal, rel, true)
	case deleted:
		return h.commit(s, principal)
	}
	return nil
}

func normalBlock(b item.Block) item.Block { return normal(item.Doc{Blocks: []item.Block{b}}).Blocks[0] }

// Tick releases locks idle for LockTTL, committing each as its holder.
func (h *Hub) Tick() {
	h.mu.Lock()
	defer h.mu.Unlock()
	cutoff := h.now().Add(-LockTTL)
	for _, s := range h.sessions {
		byClient := map[*client][]*sblock{}
		for _, b := range s.blocks {
			if b.holder != nil && b.touched.Before(cutoff) {
				byClient[b.holder] = append(byClient[b.holder], b)
			}
		}
		for c, blocks := range byClient {
			if err := h.release(s, c.principal, blocks, false); errors.Is(err, ErrDiverged) {
				break
			}
		}
		h.discardOrphans(s)
		h.gc(s)
	}
}

// discardOrphans reverts drafts that no client holds any more: their
// client was dropped without a chance to commit them.
func (h *Hub) discardOrphans(s *session) {
	for _, b := range s.blocks {
		if b.holder == nil && b.hasStable && !reflect.DeepEqual(b.draft, b.stable) {
			b.draft = b.stable
			h.broadcast(s, Event{Kind: Set, ID: b.id, Block: ptr(b.draft)})
		}
	}
	h.dropUnstable(s)
}

// Revalidate checks every client's access again, after permissions or the
// item changed: a session whose item is gone closes, clients that may no
// longer read are removed, and locks of clients that may no longer edit
// are discarded.
func (h *Hub) Revalidate() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for id, s := range h.sessions {
		alive := false
		for _, c := range s.clients {
			read, edit := h.b.Access(id, c.principal)
			if read {
				alive = true
			}
			if !edit {
				for _, b := range s.blocks {
					if b.holder == c {
						b.holder = nil
						h.broadcast(s, Event{Kind: Lock, ID: b.id})
					}
				}
			}
			if !read {
				h.send(s, c, Event{Kind: Closed, Reason: "you may no longer read this item"})
				h.drop(s, c)
			}
		}
		if !alive {
			if _, err := h.b.Body(id); err != nil || len(s.clients) == 0 {
				h.close(s, "the item is no longer available")
				continue
			}
		}
		h.discardOrphans(s)
		h.broadcast(s, Event{Kind: Presence, Participants: s.participants()})
	}
}

// Close ends an item's session at once, discarding drafts; used when the
// item's content is erased (ADR-0026), so no plaintext stays in memory.
func (h *Hub) Close(id item.ID, reason string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if s := h.sessions[id]; s != nil {
		h.close(s, reason)
	}
}

func (h *Hub) close(s *session, reason string) {
	for _, c := range s.clients {
		h.send(s, c, Event{Kind: Closed, Reason: reason})
		h.drop(s, c)
	}
	delete(h.sessions, s.id)
}

// Busy reports whether a session on the item holds locks, so a body
// written outside the session would collide with work in progress.
func (h *Hub) Busy(id item.ID) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	s := h.sessions[id]
	if s == nil {
		return false
	}
	return slices.ContainsFunc(s.blocks, func(b *sblock) bool { return b.holder != nil || !reflect.DeepEqual(b.draft, b.stable) })
}

// Sessions returns the items with an open session.
func (h *Hub) Sessions() []item.ID {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]item.ID, 0, len(h.sessions))
	for id := range h.sessions {
		out = append(out, id)
	}
	return out
}

// Shutdown ends every session for a server that is stopping: each
// client's drafts are committed as it, as if it had left, then its stream
// closes.
func (h *Hub) Shutdown() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, s := range h.sessions {
		for _, c := range s.clients {
			h.releaseAll(s, c)
		}
		h.close(s, "the server is shutting down")
	}
}
