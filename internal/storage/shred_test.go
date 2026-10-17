package storage

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dbuchs7/wikiwikibangbang/internal/item"
	"github.com/dbuchs7/wikiwikibangbang/internal/wal"
)

const secret = "TOP-SECRET-7731"

func readFile(t *testing.T, dir, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, name))
	noErr(t, err)
	return data
}

// secretItem creates an item whose title, property and body hold the secret.
func secretItem(t *testing.T, s *item.Store) item.Item {
	t.Helper()
	noErr(t, s.DefineType("admin", item.TypeDef{Name: "page", Body: true, Fields: []item.FieldDef{{Name: "note", Kind: item.KindText}}}))
	noErr(t, s.CreateSpace("admin", "PLAT", "Platform"))
	it, err := s.Create("anna", item.NewItem{Space: "PLAT", Type: "page", Title: "Runbook " + secret, Properties: item.Props{"note": secret}})
	noErr(t, err)
	noErr(t, s.CommitBody("anna", it.ID, item.Doc{Blocks: []item.Block{{Kind: item.Paragraph, Text: "body " + secret}}}))
	return it
}

func keyRecords(t *testing.T, dir string) []keyRecord {
	t.Helper()
	sc, err := wal.Read(filepath.Join(dir, KeysFile))
	noErr(t, err)
	var out []keyRecord
	for _, data := range sc.Records {
		var r keyRecord
		noErr(t, json.Unmarshal(data, &r))
		out = append(out, r)
	}
	return out
}

func hasKey(t *testing.T, dir string, id item.ID) bool {
	t.Helper()
	found := false
	for _, r := range keyRecords(t, dir) {
		if _, ok := r.Add[id]; ok {
			found = true
		}
	}
	return found
}

func TestContentIsNeverWrittenInPlaintext(t *testing.T) {
	dir := t.TempDir()
	db, _ := mustOpen(t, dir)
	secretItem(t, db.Store)
	noErr(t, db.Close())
	if bytes.Contains(readFile(t, dir, wal.FileName), []byte(secret)) {
		t.Fatal("the event log holds item content in plaintext")
	}
}

func TestErasureDestroysTheKey(t *testing.T) {
	for _, how := range []string{"purge", "redact"} {
		t.Run(how, func(t *testing.T) {
			dir := t.TempDir()
			db, _ := mustOpen(t, dir)
			it := secretItem(t, db.Store)
			if !hasKey(t, dir, it.ID) {
				t.Fatal("no key was stored for the item")
			}
			switch how {
			case "purge":
				noErr(t, db.Store.Trash("anna", it.ID))
				noErr(t, db.Store.Purge("anna", it.ID))
			case "redact":
				noErr(t, db.Store.GrantSystemRole("admin", "carla", item.RoleErasure))
				noErr(t, db.Store.Redact("carla", it.ID, "personal data"))
			}
			if hasKey(t, dir, it.ID) {
				t.Fatal("the key survived the erasure on disk")
			}
			noErr(t, db.Close())

			db, rec := mustOpen(t, dir)
			defer db.Close()
			if rec.Erased != 2 {
				t.Errorf("recovered %d erased events, want 2 (the create and the body)", rec.Erased)
			}
			for _, e := range db.Store.History(it.ID) {
				if strings.Contains(fmt.Sprintf("%s %+v", e.Title, e.Body), secret) || e.Props["note"] != nil {
					t.Errorf("event %d still shows content after a restart", e.Seq)
				}
			}
			got, _ := db.Store.Get(it.ID)
			if got.Presence != item.Purged {
				t.Errorf("presence %s, want purged", got.Presence)
			}
		})
	}
}

func TestACrashBeforeTheKeyIsDestroyedHealsOnStart(t *testing.T) {
	dir := t.TempDir()
	db, _ := mustOpen(t, dir)
	it := secretItem(t, db.Store)
	before := readFile(t, dir, KeysFile)
	noErr(t, db.Store.Trash("anna", it.ID))
	noErr(t, db.Store.Purge("anna", it.ID))
	noErr(t, db.Close())
	// The purge is on disk, but the keys file is as it was before it.
	noErr(t, os.WriteFile(filepath.Join(dir, KeysFile), before, 0o640))

	db, rec := mustOpen(t, dir)
	defer db.Close()
	if rec.Shredded != 1 || hasKey(t, dir, it.ID) {
		t.Fatalf("shredded %d on start; key still on disk: %v", rec.Shredded, hasKey(t, dir, it.ID))
	}
	for _, e := range db.Store.History(it.ID) {
		if strings.Contains(e.Title, secret) {
			t.Fatal("content visible after healing")
		}
	}
}

func TestAKeyWithoutItsBatchIsDestroyedOnStart(t *testing.T) {
	dir := t.TempDir()
	db, _ := mustOpen(t, dir)
	secretItem(t, db.Store)
	data, err := json.Marshal(keyRecord{Add: map[item.ID][]byte{"never-logged": make([]byte, 32)}})
	noErr(t, err)
	noErr(t, db.keys.log.Append(data))
	noErr(t, db.Close())

	db, rec := mustOpen(t, dir)
	defer db.Close()
	if rec.Shredded != 1 || hasKey(t, dir, "never-logged") {
		t.Fatalf("orphan key: shredded %d", rec.Shredded)
	}
}

func TestALostKeyIsDamageNotErasure(t *testing.T) {
	dir := t.TempDir()
	db, _ := mustOpen(t, dir)
	secretItem(t, db.Store)
	noErr(t, db.Close())
	noErr(t, os.Remove(filepath.Join(dir, KeysFile)))
	if _, _, err := Open(dir); err == nil || !strings.Contains(err.Error(), "never destroyed") {
		t.Fatalf("Open with a lost keys file: err = %v", err)
	}
	if _, err := Inspect(dir); err == nil {
		t.Fatal("Inspect accepted a lost keys file")
	}
}

func TestUnreadableKeysAreRefused(t *testing.T) {
	cases := map[string][]byte{
		"not json":  []byte("{"),
		"short key": []byte(`{"add":{"x":"AAAA"}}`),
	}
	for name, rec := range cases {
		dir := t.TempDir()
		l, _, err := wal.OpenFile(dir, KeysFile)
		noErr(t, err)
		noErr(t, l.Append(rec))
		noErr(t, l.Close())
		if _, _, err := Open(dir); err == nil {
			t.Errorf("%s: Open succeeded", name)
		}
		if _, err := Inspect(dir); err == nil {
			t.Errorf("%s: Inspect succeeded", name)
		}
	}
}

func TestSealedContentIsAuthenticated(t *testing.T) {
	kr := &keyring{keys: map[item.ID][]byte{}, shredded: map[item.ID]bool{}, pending: map[item.ID][]byte{}}
	ct, err := kr.Seal("a", []byte("content"), []byte("event 1"))
	noErr(t, err)
	if pt, err := kr.Open("a", ct, []byte("event 1")); err != nil || string(pt) != "content" {
		t.Fatalf("round trip: %q, %v", pt, err)
	}
	if _, err := kr.Open("a", ct, []byte("event 2")); err == nil {
		t.Error("content moved to another event decrypted")
	}
	if _, err := kr.Open("a", ct[:5], []byte("event 1")); err == nil {
		t.Error("truncated content decrypted")
	}
	kr.keys["b"] = make([]byte, 32)
	if _, err := kr.Open("b", ct, []byte("event 1")); err == nil {
		t.Error("content decrypted with another item's key")
	}
	kr.shredded["c"] = true
	if _, err := kr.Seal("c", nil, nil); err == nil {
		t.Error("sealed with a destroyed key")
	}
}

func TestPlaintextDataDirectoriesStillOpen(t *testing.T) {
	// A directory written before ADR-0026: plaintext events, no keys file.
	dir := t.TempDir()
	l, _, err := wal.Open(dir)
	noErr(t, err)
	data, err := item.EncodeBatch([]item.Event{{Seq: 1, Kind: item.SpaceCreated, Space: "PLAT", SpaceName: "Platform"}})
	noErr(t, err)
	noErr(t, l.Append(data))
	noErr(t, l.Close())
	db, rec := mustOpen(t, dir)
	defer db.Close()
	if rec.Events != 1 || !db.Store.HasSpace("PLAT") {
		t.Fatalf("recovered %+v", rec)
	}
}
