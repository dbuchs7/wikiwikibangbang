// Package notes keeps personal notes on passages of items (ADR-0107): a
// note belongs to one person and is shown to no one else. Notes live in a
// journal of their own, apart from the items' log, so no shared view of
// an item (history, versions, search, notifications, export, governance,
// the AI's tools) can show them. Their text and the passage they name are
// sealed with the item's key, so purging the item destroys them with it
// (ADR-0026).
package notes

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/dbuchs7/wikiwikibangbang/internal/item"
)

// Limits of notes, the shape of what a person types.
const (
	maxNoteText  = 4000
	maxNoteQuote = 1000
	maxNotesItem = 500
)

// ErrNotFound is a note that does not exist or is not the caller's.
var ErrNotFound = errors.New("notes: not found")

// ErrInvalid is a note that breaks a limit.
var ErrInvalid = errors.New("notes: invalid")

// Anchor names a passage by its text and a little of what surrounds it,
// as an inline comment's does (ADR-0047).
type Anchor struct {
	Quote  string `json:"quote"`
	Prefix string `json:"prefix,omitempty"`
	Suffix string `json:"suffix,omitempty"`
}

// Note is one personal note.
type Note struct {
	ID      string    `json:"id"`
	Item    item.ID   `json:"item"`
	Owner   string    `json:"-"`
	Anchor  Anchor    `json:"anchor"`
	Text    string    `json:"text"`
	Created time.Time `json:"created"`
	Updated time.Time `json:"updated"`
}

// record is one entry of the journal. Content holds the sealed anchor and
// text; a removal has none.
type record struct {
	V       int       `json:"v"`
	Kind    string    `json:"kind"`
	At      time.Time `json:"at"`
	Owner   string    `json:"owner"`
	Item    item.ID   `json:"item"`
	Note    string    `json:"note"`
	Content []byte    `json:"content,omitempty"`
}

type content struct {
	Anchor Anchor `json:"anchor"`
	Text   string `json:"text"`
}

const (
	noteSet     = "NoteSet"
	noteRemoved = "NoteRemoved"
)

// Notes is safe for concurrent use.
type Notes struct {
	mu      sync.Mutex
	journal func([]byte) error
	sealer  item.Sealer
	now     func() time.Time
	newID   func() string
	notes   map[string]*Note // by ID
}

// New rebuilds the notes from records and journals later changes. Notes
// whose item's key was destroyed are gone.
func New(records [][]byte, journal func([]byte) error, sealer item.Sealer, newID func() string) (*Notes, error) {
	n := &Notes{journal: journal, sealer: sealer, now: time.Now, newID: newID, notes: map[string]*Note{}}
	for i, data := range records {
		var r record
		if err := json.Unmarshal(data, &r); err != nil {
			return nil, fmt.Errorf("notes: record %d: %w", i+1, err)
		}
		if err := n.apply(r); err != nil {
			return nil, fmt.Errorf("notes: record %d: %w", i+1, err)
		}
	}
	return n, nil
}

func aad(r record) []byte {
	return []byte("wikiwikibangbang note " + r.Owner + " " + string(r.Item) + " " + r.Note)
}

func (n *Notes) apply(r record) error {
	if r.V != 1 {
		return fmt.Errorf("format version %d is not supported", r.V)
	}
	switch r.Kind {
	case noteSet:
		pt, err := n.sealer.Open(r.Item, r.Content, aad(r))
		if errors.Is(err, item.ErrKeyShredded) {
			delete(n.notes, r.Note)
			return nil
		}
		if err != nil {
			return err
		}
		var c content
		if err := json.Unmarshal(pt, &c); err != nil {
			return err
		}
		if old, ok := n.notes[r.Note]; ok {
			old.Anchor, old.Text, old.Updated = c.Anchor, c.Text, r.At
			return nil
		}
		n.notes[r.Note] = &Note{ID: r.Note, Item: r.Item, Owner: r.Owner, Anchor: c.Anchor, Text: c.Text, Created: r.At, Updated: r.At}
	case noteRemoved:
		delete(n.notes, r.Note)
	default:
		return fmt.Errorf("unknown kind %q", r.Kind)
	}
	return nil
}

func (n *Notes) record(r record, c *content) error {
	r.V, r.At = 1, n.now().UTC()
	if c != nil {
		pt, err := json.Marshal(c)
		if err != nil {
			return err
		}
		if r.Content, err = n.sealer.Seal(r.Item, pt, aad(r)); err != nil {
			return err
		}
	}
	data, err := json.Marshal(r)
	if err != nil {
		return err
	}
	if err := n.journal(data); err != nil {
		return err
	}
	if c != nil {
		// Applied from the plain content: the record was sealed and made
		// durable above.
		if old, ok := n.notes[r.Note]; ok {
			old.Anchor, old.Text, old.Updated = c.Anchor, c.Text, r.At
		} else {
			n.notes[r.Note] = &Note{ID: r.Note, Item: r.Item, Owner: r.Owner, Anchor: c.Anchor, Text: c.Text, Created: r.At, Updated: r.At}
		}
		return nil
	}
	delete(n.notes, r.Note)
	return nil
}

func check(text string, a *Anchor) error {
	switch {
	case strings.TrimSpace(text) == "":
		return fmt.Errorf("%w: a note needs a text", ErrInvalid)
	case utf8.RuneCountInString(text) > maxNoteText:
		return fmt.Errorf("%w: a note has at most %d characters", ErrInvalid, maxNoteText)
	case a != nil && strings.TrimSpace(a.Quote) == "":
		return fmt.Errorf("%w: a note names a passage of the text", ErrInvalid)
	case a != nil && (utf8.RuneCountInString(a.Quote) > maxNoteQuote || utf8.RuneCountInString(a.Prefix) > maxNoteQuote || utf8.RuneCountInString(a.Suffix) > maxNoteQuote):
		return fmt.Errorf("%w: the passage is at most %d characters", ErrInvalid, maxNoteQuote)
	}
	return nil
}

// Add makes a note of owner on a passage of an item. The caller has
// checked that owner may read the item.
func (n *Notes) Add(owner string, id item.ID, a Anchor, text string) (Note, error) {
	if err := check(text, &a); err != nil {
		return Note{}, err
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	count := 0
	for _, x := range n.notes {
		if x.Owner == owner && x.Item == id {
			count++
		}
	}
	if count >= maxNotesItem {
		return Note{}, fmt.Errorf("%w: at most %d notes on one item", ErrInvalid, maxNotesItem)
	}
	r := record{Kind: noteSet, Owner: owner, Item: id, Note: n.newID()}
	if err := n.record(r, &content{Anchor: a, Text: text}); err != nil {
		return Note{}, err
	}
	return *n.notes[r.Note], nil
}

// Edit changes the text of one of owner's notes.
func (n *Notes) Edit(owner, note, text string) (Note, error) {
	if err := check(text, nil); err != nil {
		return Note{}, err
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	x, ok := n.notes[note]
	if !ok || x.Owner != owner {
		return Note{}, ErrNotFound
	}
	if x.Text == text {
		return *x, nil
	}
	if err := n.record(record{Kind: noteSet, Owner: owner, Item: x.Item, Note: note}, &content{Anchor: x.Anchor, Text: text}); err != nil {
		return Note{}, err
	}
	return *n.notes[note], nil
}

// Remove deletes one of owner's notes.
func (n *Notes) Remove(owner, note string) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	x, ok := n.notes[note]
	if !ok || x.Owner != owner {
		return ErrNotFound
	}
	return n.record(record{Kind: noteRemoved, Owner: owner, Item: x.Item, Note: note}, nil)
}

// Get returns one of owner's notes.
func (n *Notes) Get(owner, note string) (Note, bool) {
	n.mu.Lock()
	defer n.mu.Unlock()
	x, ok := n.notes[note]
	if !ok || x.Owner != owner {
		return Note{}, false
	}
	return *x, true
}

// Of returns owner's notes, on item or, with item empty, on every item,
// oldest first.
func (n *Notes) Of(owner string, id item.ID) []Note {
	n.mu.Lock()
	defer n.mu.Unlock()
	out := []Note{}
	for _, x := range n.notes {
		if x.Owner == owner && (id == "" || x.Item == id) {
			out = append(out, *x)
		}
	}
	slices.SortFunc(out, func(a, b Note) int {
		if c := a.Created.Compare(b.Created); c != 0 {
			return c
		}
		return strings.Compare(a.ID, b.ID)
	})
	return out
}

// Forget drops the notes of items whose keys were just destroyed; their
// records can no longer be read anyway.
func (n *Notes) Forget(ids ...item.ID) {
	if len(ids) == 0 {
		return
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	for k, x := range n.notes {
		if slices.Contains(ids, x.Item) {
			delete(n.notes, k)
		}
	}
}
