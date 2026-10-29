package storage

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/dbuchs7/wikiwikibangbang/internal/engineconn"
	"github.com/dbuchs7/wikiwikibangbang/internal/item"
)

// TestConnectionsAreSealedAndTheirKeysGoWithThem: a connection's refresh
// token is sealed with a key of its own in the keys file, never on disk
// in plaintext; the key survives a restart, which destroys only the keys
// of connections that are gone; ending a connection destroys its key, so
// its records can no longer be read (ADR-0151).
func TestConnectionsAreSealedAndTheirKeysGoWithThem(t *testing.T) {
	const token = "atlasor_0123456789abcdef"
	dir := t.TempDir()
	db, _, err := Open(dir)
	noErr(t, err)
	p := engineconn.Profile{Subject: "u-17", Username: "anna", Email: "anna@example.org"}
	_, err = db.Connections.Connect("bert", "atlas", p, token)
	noErr(t, err)
	_, err = db.Connections.Connect("vera", "atlas", p, token+"-vera")
	noErr(t, err)
	// An item is written alongside, as the server does.
	secretItem(t, db.Store)
	noErr(t, db.Close())
	entries, err := os.ReadDir(dir)
	noErr(t, err)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if raw, err := os.ReadFile(filepath.Join(dir, e.Name())); err == nil && bytes.Contains(raw, []byte(token)) {
			t.Fatalf("%s holds a refresh token in plaintext", e.Name())
		}
	}

	db, rec, err := Open(dir)
	noErr(t, err)
	if rec.Shredded != 0 {
		t.Errorf("a restart destroyed %d keys of connections that are there", rec.Shredded)
	}
	got, err := db.Connections.Refresh("bert", "atlas")
	if err != nil || got != token {
		t.Fatalf("after a restart, Refresh = %q, %v", got, err)
	}
	keys := db.Connections.Keys()
	noErr(t, db.Connections.End("bert", "atlas", "disconnected"))
	if got := db.Connections.Count(); got != 1 {
		t.Fatalf("%d connections left, want vera's", got)
	}
	destroyed := 0
	for _, k := range keys {
		if _, err := db.keys.Open(item.ID(connectionKey+k), []byte("x"), nil); errors.Is(err, item.ErrKeyShredded) {
			destroyed++
		}
	}
	if destroyed != 1 {
		t.Errorf("%d keys destroyed, want the ended connection's", destroyed)
	}
	if got, err := db.Connections.Refresh("vera", "atlas"); err != nil || got != token+"-vera" {
		t.Errorf("the other connection: %q, %v", got, err)
	}
	noErr(t, db.Close())

	// A key without a connection, as a crash between the key and its
	// record leaves, is destroyed on the next start.
	db, _, err = Open(dir)
	noErr(t, err)
	_, err = db.keys.Seal(item.ID(connectionKey+"orphan"), []byte("x"), nil)
	noErr(t, err)
	noErr(t, db.keys.persistPending())
	noErr(t, db.Close())
	db, rec, err = Open(dir)
	noErr(t, err)
	defer db.Close()
	if rec.Shredded != 1 {
		t.Errorf("destroyed %d keys at the start, want the orphan", rec.Shredded)
	}
	if db.Connections.Count() != 1 {
		t.Errorf("connections after the start: %d", db.Connections.Count())
	}
	if _, ok := NotBackedUp[ConnectionsFile]; !ok {
		t.Error("people's grants would be backed up")
	}
}
