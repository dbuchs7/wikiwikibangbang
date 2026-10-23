package storage

import (
	"bytes"
	"testing"

	"github.com/dbuchs7/wikiwikibangbang/internal/item"
	"github.com/dbuchs7/wikiwikibangbang/internal/notes"
)

// TestNotesAreSealedAndGoWithTheirItem: a personal note is sealed with its
// item's key in a journal of its own, survives a restart, and is gone
// once the item is purged (ADR-0107).
func TestNotesAreSealedAndGoWithTheirItem(t *testing.T) {
	dir := t.TempDir()
	db, _, err := Open(dir)
	noErr(t, err)
	it := secretItem(t, db.Store)
	var id string
	noErr(t, db.Write(func(_ *item.Store) error {
		n, err := db.Notes.Add("anna", it.ID, notes.Anchor{Quote: "body"}, "note "+secret)
		id = n.ID
		return err
	}))
	noErr(t, db.Close())
	if bytes.Contains(readFile(t, dir, NotesFile), []byte(secret)) {
		t.Fatal("the notes journal holds the note in plain text")
	}

	db, _, err = Open(dir)
	noErr(t, err)
	if n, ok := db.Notes.Get("anna", id); !ok || n.Text != "note "+secret {
		t.Fatalf("after a restart: %+v %v", n, ok)
	}
	noErr(t, db.Store.Trash("anna", it.ID))
	noErr(t, db.Store.Purge("admin", it.ID))
	if _, ok := db.Notes.Get("anna", id); ok {
		t.Error("a note outlives the purge of its item")
	}
	noErr(t, db.Close())
	db, _, err = Open(dir)
	noErr(t, err)
	defer db.Close()
	if got := db.Notes.Of("anna", ""); len(got) != 0 {
		t.Errorf("after the purge and a restart: %+v", got)
	}
}
