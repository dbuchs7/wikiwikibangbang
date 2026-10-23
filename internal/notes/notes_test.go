package notes

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/dbuchs7/wikiwikibangbang/internal/item"
)

// sealer marks what it seals and refuses to open what it did not, or an
// item whose key was destroyed.
type sealer struct{ shredded map[item.ID]bool }

func (s sealer) Seal(id item.ID, pt, aad []byte) ([]byte, error) {
	return append([]byte("sealed:"+string(aad)+":"), pt...), nil
}

func (s sealer) Open(id item.ID, ct, aad []byte) ([]byte, error) {
	if s.shredded[id] {
		return nil, item.ErrKeyShredded
	}
	prefix := []byte("sealed:" + string(aad) + ":")
	if !bytes.HasPrefix(ct, prefix) {
		return nil, errors.New("does not authenticate")
	}
	return ct[len(prefix):], nil
}

func ids() func() string {
	n := 0
	return func() string { n++; return fmt.Sprintf("n%03d", n) }
}

// TestNotesAreKeptPrivateAndReplayed: a note belongs to its owner alone,
// is sealed in the journal, survives a restart, and goes with its item's
// key (ADR-0107).
func TestNotesAreKeptPrivateAndReplayed(t *testing.T) {
	var records [][]byte
	j := func(r []byte) error { records = append(records, r); return nil }
	s := sealer{shredded: map[item.ID]bool{}}
	n, err := New(nil, j, s, ids())
	if err != nil {
		t.Fatal(err)
	}
	a, err := n.Add("anna", "page1", Anchor{Quote: "budget", Prefix: "The "}, "Ask Paul about the secret figure")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := n.Add("bert", "page1", Anchor{Quote: "budget"}, "Mine"); err != nil {
		t.Fatal(err)
	}
	other, _ := n.Add("anna", "page2", Anchor{Quote: "x"}, "Elsewhere")
	if got := n.Of("anna", "page1"); len(got) != 1 || got[0].ID != a.ID || got[0].Text != "Ask Paul about the secret figure" {
		t.Fatalf("anna's notes on page1: %+v", got)
	}
	if got := n.Of("anna", ""); len(got) != 2 {
		t.Errorf("all of anna's notes: %+v", got)
	}
	if _, err := n.Edit("bert", a.ID, "taken"); !errors.Is(err, ErrNotFound) {
		t.Errorf("another person edits a note: %v", err)
	}
	if err := n.Remove("bert", a.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("another person removes a note: %v", err)
	}
	if _, ok := n.Get("bert", a.ID); ok {
		t.Error("another person reads a note")
	}
	if _, err := n.Edit("anna", a.ID, "Ask Paul"); err != nil {
		t.Fatal(err)
	}
	for _, r := range records {
		if strings.Contains(string(r), "budget") || strings.Contains(string(r), `"text"`) && !strings.Contains(string(r), "sealed:") {
			t.Fatalf("the journal holds plain content: %s", r)
		}
	}

	r, err := New(records, j, s, ids())
	if err != nil {
		t.Fatal(err)
	}
	if got := r.Of("anna", "page1"); len(got) != 1 || got[0].Text != "Ask Paul" || got[0].Anchor.Quote != "budget" || !got[0].Updated.After(got[0].Created) && !got[0].Updated.Equal(got[0].Created) {
		t.Fatalf("replayed: %+v", got)
	}
	if err := r.Remove("anna", other.ID); err != nil {
		t.Fatal(err)
	}
	s.shredded["page1"] = true
	r2, err := New(records, j, s, ids())
	if err != nil {
		t.Fatal(err)
	}
	if got := r2.Of("anna", ""); len(got) != 0 {
		t.Errorf("notes outlive their item's key, or a removed one came back: %+v", got)
	}
	r.Forget("page1")
	if got := r.Of("bert", ""); len(got) != 0 {
		t.Errorf("forgotten: %+v", got)
	}
}

func TestNotesRefuseWhatBreaksTheirLimits(t *testing.T) {
	n, _ := New(nil, func([]byte) error { return nil }, sealer{}, ids())
	for _, c := range []struct {
		a    Anchor
		text string
	}{
		{Anchor{Quote: "x"}, " "},
		{Anchor{Quote: " "}, "text"},
		{Anchor{Quote: "x"}, strings.Repeat("a", maxNoteText+1)},
		{Anchor{Quote: strings.Repeat("q", maxNoteQuote+1)}, "text"},
	} {
		if _, err := n.Add("anna", "p", c.a, c.text); !errors.Is(err, ErrInvalid) {
			t.Errorf("%q / %q: %v", c.a.Quote, c.text, err)
		}
	}
	for i := 0; i < maxNotesItem; i++ {
		if _, err := n.Add("anna", "p", Anchor{Quote: "x"}, "t"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := n.Add("anna", "p", Anchor{Quote: "x"}, "t"); !errors.Is(err, ErrInvalid) {
		t.Errorf("one note too many: %v", err)
	}
	if _, err := n.Edit("anna", "nope", "t"); !errors.Is(err, ErrNotFound) {
		t.Errorf("edit unknown: %v", err)
	}
	if _, err := New([][]byte{[]byte(`{"v":2}`)}, nil, sealer{}, ids()); err == nil {
		t.Error("a newer format is read")
	}
	if _, err := New([][]byte{[]byte(`{"v":1,"kind":"Other"}`)}, nil, sealer{}, ids()); err == nil {
		t.Error("an unknown kind is read")
	}
	if _, err := New([][]byte{[]byte(`{`)}, nil, sealer{}, ids()); err == nil {
		t.Error("broken JSON is read")
	}
	if _, err := New([][]byte{[]byte(`{"v":1,"kind":"NoteSet","owner":"a","item":"p","note":"n","content":"Zm9v"}`)}, nil, sealer{}, ids()); err == nil {
		t.Error("content that does not authenticate is read")
	}
}

func TestNotesNotKeptWhenTheJournalFails(t *testing.T) {
	fail := errors.New("disk full")
	ok := true
	n, _ := New(nil, func([]byte) error {
		if ok {
			return nil
		}
		return fail
	}, sealer{}, ids())
	a, err := n.Add("anna", "p", Anchor{Quote: "x"}, "first")
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := n.Edit("anna", a.ID, "first"); got.Text != "first" {
		t.Errorf("an unchanged text: %+v", got)
	}
	if got, found := n.Get("anna", a.ID); !found || got.Text != "first" {
		t.Errorf("get: %+v %v", got, found)
	}
	ok = false
	if _, err := n.Add("anna", "p", Anchor{Quote: "y"}, "second"); !errors.Is(err, fail) {
		t.Errorf("add: %v", err)
	}
	if _, err := n.Edit("anna", a.ID, "changed"); !errors.Is(err, fail) {
		t.Errorf("edit: %v", err)
	}
	if err := n.Remove("anna", a.ID); !errors.Is(err, fail) {
		t.Errorf("remove: %v", err)
	}
	if got := n.Of("anna", "p"); len(got) != 1 || got[0].Text != "first" {
		t.Errorf("a change that was not journaled took effect: %+v", got)
	}
}
