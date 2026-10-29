package storage

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/dbuchs7/wikiwikibangbang/internal/item"
	"github.com/dbuchs7/wikiwikibangbang/internal/wal"
)

const (
	tokenName  = "WWBB_ENGINE_ATLAS_TOKEN"
	tokenValue = "atlas_secret_token_value_123"
)

func openOrFail(t *testing.T, dir string) *DB {
	t.Helper()
	db, _, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	return db
}

// credentialKeys are the keys of the credentials file in the keyring.
func credentialKeys(db *DB) []item.ID {
	var out []item.ID
	for id := range db.keys.keys {
		if isCredentialKey(id) {
			out = append(out, id)
		}
	}
	return out
}

func TestAStoredCredentialOutlivesARestartAndNeverReachesDiskInPlaintext(t *testing.T) {
	dir := t.TempDir()
	db := openOrFail(t, dir)
	if err := db.SetCredential(tokenName, tokenValue, "admin"); err != nil {
		t.Fatal(err)
	}
	e, ok := db.Credential(tokenName)
	if !ok || e.Value != tokenValue || e.By != "admin" || e.At.IsZero() {
		t.Fatalf("Credential = %+v, %v", e, ok)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range entries {
		if f.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, f.Name()))
		if err != nil {
			t.Fatal(err)
		}
		for _, plain := range []string{tokenValue, tokenName} {
			if bytes.Contains(data, []byte(plain)) {
				t.Errorf("%s holds %q in plaintext", f.Name(), plain)
			}
		}
	}

	db = openOrFail(t, dir)
	defer db.Close()
	if e, ok := db.Credential(tokenName); !ok || e.Value != tokenValue || e.By != "admin" {
		t.Fatalf("after reopening, Credential = %+v, %v", e, ok)
	}
	if keys := credentialKeys(db); len(keys) != 1 {
		t.Errorf("credential keys after reopening: %v, want exactly the one in use", keys)
	}
}

func TestEveryChangeTakesANewKeyAndDestroysTheOld(t *testing.T) {
	dir := t.TempDir()
	db := openOrFail(t, dir)
	if err := db.SetCredential(tokenName, "first", "admin"); err != nil {
		t.Fatal(err)
	}
	old := credentialKeys(db)
	before, err := os.ReadFile(filepath.Join(dir, CredentialsFile))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SetCredential(tokenName, "second", "other"); err != nil {
		t.Fatal(err)
	}
	now := credentialKeys(db)
	if len(old) != 1 || len(now) != 1 || old[0] == now[0] {
		t.Fatalf("keys before %v, after %v: want one each, different", old, now)
	}
	if !db.keys.shredded[old[0]] {
		t.Error("the old key is not destroyed")
	}
	if e, _ := db.Credential(tokenName); e.Value != "second" || e.By != "other" {
		t.Errorf("Credential = %+v", e)
	}
	db.Close()

	// A copy of the file from before the change can no longer be opened.
	if err := os.WriteFile(filepath.Join(dir, CredentialsFile), before, 0o600); err != nil {
		t.Fatal(err)
	}
	_, _, err = Open(dir)
	if err == nil || !strings.Contains(err.Error(), "remove the file") {
		t.Errorf("opening an old copy of the credentials: %v; want it refused, saying how to start without it", err)
	}
}

func TestRemovingTheLastCredentialLeavesNothingToOpen(t *testing.T) {
	dir := t.TempDir()
	db := openOrFail(t, dir)
	for _, name := range []string{tokenName, "WWBB_CONNECTOR_TICKETS_TOKEN"} {
		if err := db.SetCredential(name, tokenValue, "admin"); err != nil {
			t.Fatal(err)
		}
	}
	if gone, err := db.RemoveCredential(tokenName); err != nil || !gone {
		t.Fatalf("RemoveCredential = %v, %v", gone, err)
	}
	if _, ok := db.Credential(tokenName); ok {
		t.Error("a removed credential is still there")
	}
	if _, ok := db.Credential("WWBB_CONNECTOR_TICKETS_TOKEN"); !ok {
		t.Error("removing one credential removed another")
	}
	if gone, err := db.RemoveCredential(tokenName); err != nil || gone {
		t.Errorf("removing it again = %v, %v; want nothing removed", gone, err)
	}
	if gone, err := db.RemoveCredential("WWBB_CONNECTOR_TICKETS_TOKEN"); err != nil || !gone {
		t.Fatal(gone, err)
	}
	if keys := credentialKeys(db); len(keys) != 0 {
		t.Errorf("keys left with no credential: %v", keys)
	}
	db.Close()

	db = openOrFail(t, dir)
	defer db.Close()
	if _, ok := db.Credential("WWBB_CONNECTOR_TICKETS_TOKEN"); ok {
		t.Error("a removed credential came back after reopening")
	}
}

func TestAKeyNoCredentialUsesIsDestroyedAtTheNextStart(t *testing.T) {
	dir := t.TempDir()
	db := openOrFail(t, dir)
	if err := db.SetCredential(tokenName, tokenValue, "admin"); err != nil {
		t.Fatal(err)
	}
	inUse := credentialKeys(db)
	// A crash after a new key was made durable and before the file was
	// rewritten leaves that key behind.
	stray := newCredentialKey()
	if _, err := db.keys.Seal(stray, []byte("x"), nil); err != nil {
		t.Fatal(err)
	}
	if err := db.keys.persistPending(); err != nil {
		t.Fatal(err)
	}
	db.Close()

	db, rec, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if keys := credentialKeys(db); !slices.Equal(keys, inUse) {
		t.Errorf("credential keys after the start: %v, want only %v", keys, inUse)
	}
	if rec.Shredded != 1 {
		t.Errorf("Shredded = %d, want the stray key", rec.Shredded)
	}
	if e, ok := db.Credential(tokenName); !ok || e.Value != tokenValue {
		t.Errorf("the credential in use was lost: %+v, %v", e, ok)
	}
}

func TestDamagedCredentialsStopTheStartAndSayHowToGoOn(t *testing.T) {
	for _, c := range []struct {
		about  string
		record []byte
	}{
		{"not JSON", []byte("{")},
		{"another format", []byte(`{"v":2,"key":"~credentials~00","sealed":""}`)},
		{"an item's key", []byte(`{"v":1,"key":"someitem","sealed":""}`)},
		{"a key that never was", []byte(`{"v":1,"key":"~credentials~00","sealed":""}`)},
	} {
		t.Run(c.about, func(t *testing.T) {
			dir := t.TempDir()
			openOrFail(t, dir).Close()
			if err := wal.Rewrite(dir, CredentialsFile, [][]byte{c.record}); err != nil {
				t.Fatal(err)
			}
			if _, _, err := Open(dir); err == nil || !strings.Contains(err.Error(), "remove the file") {
				t.Errorf("Open = %v; want a refusal that says how to start without the credentials", err)
			}
		})
	}
	dir := t.TempDir()
	openOrFail(t, dir).Close()
	if err := os.WriteFile(filepath.Join(dir, CredentialsFile), []byte("not a log"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Open(dir); err == nil || !strings.Contains(err.Error(), "remove the file") {
		t.Errorf("a file that is no log: %v", err)
	}
}

func TestAnUnknownCredentialIsNotThere(t *testing.T) {
	db := openOrFail(t, t.TempDir())
	defer db.Close()
	if _, ok := db.Credential(tokenName); ok {
		t.Error("a credential nobody stored is reported")
	}
	if _, err := os.Stat(filepath.Join(db.Dir(), CredentialsFile)); !os.IsNotExist(err) {
		t.Errorf("the credentials file exists before anything was stored: %v", err)
	}
}
