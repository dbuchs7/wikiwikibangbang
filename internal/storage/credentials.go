package storage

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/dbuchs7/wikiwikibangbang/internal/credential"
	"github.com/dbuchs7/wikiwikibangbang/internal/item"
	"github.com/dbuchs7/wikiwikibangbang/internal/wal"
)

// CredentialsFile holds the credentials administrators stored for the
// process engines and the connectors (ADR-0151): one record, rewritten by
// every change, that seals them under a key of their own in the keys file.
// A backup leaves it out.
const CredentialsFile = "credentials.wal"

// credentialKeyPrefix begins the IDs of the credentials' keys in the keys
// file; no item ID does.
const credentialKeyPrefix = "~credentials~"

func isCredentialKey(id item.ID) bool { return strings.HasPrefix(string(id), credentialKeyPrefix) }

// newCredentialKey is the ID of a new key for the credentials.
func newCredentialKey() item.ID {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err) // crypto/rand does not fail on supported systems
	}
	return item.ID(credentialKeyPrefix + hex.EncodeToString(b))
}

// credentialsRecord is the record of the credentials file: the key the
// credentials are sealed under, and the sealed credentials.
type credentialsRecord struct {
	V      int     `json:"v"`
	Key    item.ID `json:"key"`
	Sealed []byte  `json:"sealed"`
}

// storedCredential is one credential as sealed.
type storedCredential struct {
	Value string    `json:"value"`
	At    time.Time `json:"at"`
	By    string    `json:"by"`
}

// credentials are the stored credentials in memory, and the key the file
// seals them under.
type credentials struct {
	mu   sync.RWMutex
	key  item.ID
	vals map[string]storedCredential
}

// credentialsAAD binds the sealed credentials to their file.
var credentialsAAD = []byte(CredentialsFile)

// loadCredentials reads the credentials file, if there is one.
func (db *DB) loadCredentials() error {
	db.creds.vals = map[string]storedCredential{}
	data, err := os.ReadFile(filepath.Join(db.dir, CredentialsFile))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	sc, err := wal.ReadBytes(data)
	if err != nil {
		return credentialsDamaged(err)
	}
	if len(sc.Records) == 0 {
		return nil // the last credential was removed
	}
	var r credentialsRecord
	if err := json.Unmarshal(sc.Records[len(sc.Records)-1], &r); err != nil {
		return credentialsDamaged(err)
	}
	if r.V != 1 || !isCredentialKey(r.Key) {
		return credentialsDamaged(fmt.Errorf("format version %d, key %q", r.V, r.Key))
	}
	pt, err := db.keys.Open(r.Key, r.Sealed, credentialsAAD)
	if err != nil {
		return credentialsDamaged(err)
	}
	if err := json.Unmarshal(pt, &db.creds.vals); err != nil {
		return credentialsDamaged(err)
	}
	db.creds.key = r.Key
	return nil
}

func credentialsDamaged(err error) error {
	return fmt.Errorf("storage: the stored credentials in %s cannot be opened (%v); remove the file to start without them, and store them again", CredentialsFile, err)
}

// strayCredentialKeys are the credentials' keys that the file does not
// seal under: left by a crash between making a new key durable and
// rewriting the file, or before destroying the old one.
func (db *DB) strayCredentialKeys() []item.ID {
	var out []item.ID
	for id := range db.keys.keys {
		if isCredentialKey(id) && id != db.creds.key {
			out = append(out, id)
		}
	}
	return out
}

// Credential returns a stored credential (ADR-0151).
func (db *DB) Credential(name string) (credential.Entry, bool) {
	db.creds.mu.RLock()
	defer db.creds.mu.RUnlock()
	c, ok := db.creds.vals[name]
	return credential.Entry{Value: c.Value, At: c.At, By: c.By}, ok
}

// SetCredential stores a credential, or replaces the one of that name. The
// caller checks the name and cleans the value.
func (db *DB) SetCredential(name, value, by string) error {
	db.mu.Lock()
	defer db.mu.Unlock()
	db.creds.mu.RLock()
	next := maps.Clone(db.creds.vals)
	db.creds.mu.RUnlock()
	next[name] = storedCredential{Value: value, At: time.Now().UTC(), By: by}
	return db.writeCredentials(next)
}

// RemoveCredential removes a stored credential and reports whether there
// was one.
func (db *DB) RemoveCredential(name string) (bool, error) {
	db.mu.Lock()
	defer db.mu.Unlock()
	db.creds.mu.RLock()
	_, ok := db.creds.vals[name]
	next := maps.Clone(db.creds.vals)
	db.creds.mu.RUnlock()
	if !ok {
		return false, nil
	}
	delete(next, name)
	return true, db.writeCredentials(next)
}

// writeCredentials makes next the stored credentials: sealed under a new
// key made durable first, the file rewritten, then the old key destroyed,
// so that an older copy of the file cannot be opened. A crash between the
// steps leaves a key the next start destroys. The caller holds db.mu, the
// lock of the keys.
func (db *DB) writeCredentials(next map[string]storedCredential) error {
	var records [][]byte
	var key item.ID
	if len(next) > 0 {
		pt, err := json.Marshal(next)
		if err != nil {
			return err
		}
		key = newCredentialKey()
		ct, err := db.keys.Seal(key, pt, credentialsAAD)
		if err == nil {
			err = db.keys.persistPending()
		}
		if err != nil {
			return err
		}
		rec, err := json.Marshal(credentialsRecord{V: 1, Key: key, Sealed: ct})
		if err != nil {
			return err
		}
		records = [][]byte{rec}
	}
	if err := wal.Rewrite(db.dir, CredentialsFile, records); err != nil {
		if key != "" {
			_ = db.keys.shred(key) // never used; a failure is retried at the next start
		}
		return err
	}
	db.creds.mu.Lock()
	old := db.creds.key
	db.creds.key, db.creds.vals = key, next
	db.creds.mu.Unlock()
	if old != "" {
		// The new credentials are durable; an old key that cannot be
		// destroyed now is destroyed by the next change or the next start.
		_ = db.keys.shred(old)
	}
	return nil
}
