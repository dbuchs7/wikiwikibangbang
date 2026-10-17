package backup

import (
	"bytes"
	"crypto/rand"
	"errors"
	"io"
	"testing"
)

const testPass = "correct horse battery"

func seal(t *testing.T, plain []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	w, err := encrypt(&buf, testPass, 1000)
	if err != nil {
		t.Fatal(err)
	}
	// Uneven writes, to cross chunk boundaries mid-write.
	for p := plain; len(p) > 0; {
		n := min(len(p), 7777)
		if _, err := w.Write(p[:n]); err != nil {
			t.Fatal(err)
		}
		p = p[n:]
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func open(data []byte, pass string) ([]byte, error) {
	r, err := decrypt(bytes.NewReader(data), pass)
	if err != nil {
		return nil, err
	}
	return io.ReadAll(r)
}

func TestEncryptionRoundTrips(t *testing.T) {
	for _, n := range []int{0, 1, chunkSize - 1, chunkSize, chunkSize + 1, 3*chunkSize + 17} {
		plain := make([]byte, n)
		rand.Read(plain)
		got, err := open(seal(t, plain), testPass)
		if err != nil || !bytes.Equal(got, plain) {
			t.Errorf("%d bytes: err %v, equal %v", n, err, bytes.Equal(got, plain))
		}
	}
}

func TestDamageAndWrongPassphrasesAreRefused(t *testing.T) {
	plain := make([]byte, 2*chunkSize+100)
	rand.Read(plain)
	data := seal(t, plain)
	if _, err := open(data, "not the passphrase"); !errors.Is(err, ErrPassphrase) {
		t.Errorf("wrong passphrase: %v", err)
	}
	full := headerSize + chunkSize + 16
	for name, cut := range map[string]int{
		"mid chunk":      headerSize + 100,
		"chunk boundary": full,
		"two chunks":     headerSize + 2*(chunkSize+16),
		"the last byte":  len(data) - 1,
	} {
		if _, err := open(data[:cut], testPass); !errors.Is(err, ErrPassphrase) {
			t.Errorf("cut at %s: %v", name, err)
		}
	}
	for _, at := range []int{9, 20, headerSize + 5, len(data) - 3} {
		bad := bytes.Clone(data)
		bad[at] ^= 1
		if _, err := open(bad, testPass); err == nil {
			t.Errorf("a flipped bit at %d went unnoticed", at)
		}
	}
	if _, err := open([]byte("PK\x03\x04 not ours"), testPass); err == nil {
		t.Error("a foreign file was accepted")
	}
	bad := bytes.Clone(data)
	copy(bad[8:12], []byte{0xff, 0xff, 0xff, 0xff}) // an absurd iteration count
	if _, err := open(bad, testPass); err == nil {
		t.Error("an absurd iteration count was accepted")
	}
}

func TestShortPassphrasesAreRefused(t *testing.T) {
	if _, err := encrypt(io.Discard, "short", 1000); err == nil {
		t.Fatal("accepted")
	}
}
