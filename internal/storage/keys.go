package storage

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"

	"github.com/dbuchs7/wikiwikibangbang/internal/item"
	"github.com/dbuchs7/wikiwikibangbang/internal/wal"
)

// KeysFile holds one AES-256 key per item (ADR-0026). It is kept apart from
// the event log, because it is the one file data must be able to leave:
// destroying an item's key here makes that item's content in the event log
// unrecoverable.
const KeysFile = "keys.wal"

// keyRecord is one record of the keys file: keys added, or items whose keys
// were destroyed. Destroyed IDs are kept (they are not secret) so that a
// missing key can be told apart from a lost one.
type keyRecord struct {
	Add      map[item.ID][]byte `json:"add,omitempty"`
	Shredded []item.ID          `json:"shredded,omitempty"`
}

// keyring is the item.Sealer of a data directory.
type keyring struct {
	dir      string
	log      *wal.Log
	keys     map[item.ID][]byte
	shredded map[item.ID]bool
	pending  map[item.ID][]byte // created for the batch being sealed, not yet durable
	unshred  []item.ID          // destroyed in memory, still to be removed from disk
}

func openKeyring(dir string) (*keyring, error) {
	l, sc, err := wal.OpenFile(dir, KeysFile)
	if err != nil {
		return nil, err
	}
	kr := &keyring{dir: dir, log: l, keys: map[item.ID][]byte{}, shredded: map[item.ID]bool{}, pending: map[item.ID][]byte{}}
	if err := kr.load(sc.Records); err != nil {
		l.Close()
		return nil, err
	}
	return kr, nil
}

func (kr *keyring) load(records [][]byte) error {
	for i, data := range records {
		var r keyRecord
		if err := json.Unmarshal(data, &r); err != nil {
			return fmt.Errorf("storage: keys record %d: %w", i+1, err)
		}
		for id, k := range r.Add {
			if len(k) != 32 {
				return fmt.Errorf("storage: keys record %d: key of %s has %d bytes", i+1, id, len(k))
			}
			kr.keys[id] = k
		}
		for _, id := range r.Shredded {
			delete(kr.keys, id)
			kr.shredded[id] = true
		}
	}
	return nil
}

func (kr *keyring) close() error { return kr.log.Close() }

func (kr *keyring) key(id item.ID) []byte {
	if k, ok := kr.keys[id]; ok {
		return k
	}
	return kr.pending[id]
}

// Seal encrypts with AES-256-GCM under the item's key, creating the key on
// the item's first content. The nonce is random and prefixed.
func (kr *keyring) Seal(id item.ID, pt, aad []byte) ([]byte, error) {
	if kr.shredded[id] {
		return nil, fmt.Errorf("storage: the key of %s was destroyed", id)
	}
	k := kr.key(id)
	if k == nil {
		k = make([]byte, 32)
		if _, err := rand.Read(k); err != nil {
			return nil, err
		}
		kr.pending[id] = k
	}
	gcm, err := newGCM(k)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize(), gcm.NonceSize()+len(pt)+gcm.Overhead())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return gcm.Seal(nonce, nonce, pt, aad), nil
}

// Open decrypts. A destroyed key is item.ErrKeyShredded; a key that is
// missing without having been destroyed is damage, not erasure, and fails.
func (kr *keyring) Open(id item.ID, ct, aad []byte) ([]byte, error) {
	k := kr.key(id)
	if k == nil {
		if kr.shredded[id] {
			return nil, item.ErrKeyShredded
		}
		return nil, fmt.Errorf("storage: no key for %s, and it was never destroyed: the keys file is incomplete", id)
	}
	gcm, err := newGCM(k)
	if err != nil {
		return nil, err
	}
	if len(ct) < gcm.NonceSize() {
		return nil, errors.New("storage: sealed content is too short")
	}
	pt, err := gcm.Open(nil, ct[:gcm.NonceSize()], ct[gcm.NonceSize():], aad)
	if err != nil {
		return nil, fmt.Errorf("storage: content of %s does not authenticate: %w", id, err)
	}
	return pt, nil
}

func newGCM(k []byte) (cipher.AEAD, error) {
	b, err := aes.NewCipher(k)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(b)
}

// persistPending makes the keys created while sealing a batch durable. It
// runs before the batch is appended, so no event on disk can lack its key.
func (kr *keyring) persistPending() error {
	if len(kr.pending) == 0 {
		return nil
	}
	data, err := json.Marshal(keyRecord{Add: kr.pending})
	if err == nil {
		err = kr.log.Append(data)
	}
	if err != nil {
		kr.pending = map[item.ID][]byte{}
		return err
	}
	maps.Copy(kr.keys, kr.pending)
	kr.pending = map[item.ID][]byte{}
	return nil
}

// passagesOf returns the keys of the protected passages of items
// (ADR-0088).
func (kr *keyring) passagesOf(items []item.ID) []item.ID {
	if len(items) == 0 {
		return nil
	}
	gone := map[item.ID]bool{}
	for _, id := range items {
		gone[id] = true
	}
	var out []item.ID
	for id := range kr.keys {
		if of, ok := item.SecretKeyItem(id); ok && gone[of] {
			out = append(out, id)
		}
	}
	return out
}

// shred destroys the keys of ids: at once in memory, and on disk by
// rewriting the keys file without them. If the rewrite fails, the IDs are
// remembered and the next shred, or the next start, retries.
func (kr *keyring) shred(ids ...item.ID) error {
	for _, id := range ids {
		if _, ok := kr.keys[id]; ok || !kr.shredded[id] {
			delete(kr.keys, id)
			kr.shredded[id] = true
			kr.unshred = append(kr.unshred, id)
		}
	}
	if len(kr.unshred) == 0 {
		return nil
	}
	shredded := slices.Sorted(maps.Keys(kr.shredded))
	data, err := json.Marshal(keyRecord{Add: kr.keys, Shredded: shredded})
	if err != nil {
		return err
	}
	if err := kr.log.Close(); err != nil {
		return err
	}
	rerr := wal.Rewrite(kr.dir, KeysFile, [][]byte{data})
	l, _, err := wal.OpenFile(kr.dir, KeysFile)
	if err != nil {
		return errors.Join(rerr, err) // the keyring cannot write until reopened
	}
	kr.log = l
	if rerr != nil {
		return rerr
	}
	kr.unshred = nil
	return nil
}
