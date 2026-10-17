package backup

import (
	"bufio"
	"crypto/aes"
	"crypto/cipher"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// The encrypted stream (ADR-0034):
//
//	magic "WWBBBAK1" | iterations uint32 BE | salt [16] | nonce prefix [7]
//	then chunks of at most chunkSize plaintext bytes, each sealed with
//	AES-256-GCM under nonce = prefix | chunk counter uint32 BE | last flag,
//	with the header as additional data.
//
// The key is derived from the passphrase with PBKDF2-SHA256. The counter
// keeps chunks from being reordered, the last flag keeps the stream from
// being cut short, and the header in every chunk keeps its parameters from
// being changed.

const (
	magic      = "WWBBBAK1"
	headerSize = len(magic) + 4 + 16 + 7
	chunkSize  = 64 << 10
	// MinPassphrase is the shortest passphrase accepted for a backup.
	MinPassphrase = 12
	// maxIterations bounds what a header may demand from a reader.
	maxIterations = 10_000_000
)

// Iterations of PBKDF2 for new backups, as for passwords (ADR-0027). A
// backup records its own count, so lowering this, which tests do, does not
// affect reading older backups.
var Iterations = 600_000

// ErrPassphrase means the passphrase is wrong or the backup is damaged;
// authenticated encryption cannot tell the two apart.
var ErrPassphrase = errors.New("backup: wrong passphrase, or the backup is damaged")

func deriveKey(passphrase string, salt []byte, iter int) ([]byte, error) {
	return pbkdf2.Key(sha256.New, passphrase, salt, iter, 32)
}

type encWriter struct {
	w       io.Writer
	aead    cipher.AEAD
	header  []byte
	prefix  []byte
	counter uint32
	buf     []byte
	err     error
}

// encrypt returns a writer that encrypts into w; Close writes the last chunk.
func encrypt(w io.Writer, passphrase string, iter int) (io.WriteCloser, error) {
	if len([]rune(passphrase)) < MinPassphrase {
		return nil, fmt.Errorf("backup: a passphrase has at least %d characters", MinPassphrase)
	}
	header := make([]byte, headerSize)
	copy(header, magic)
	binary.BigEndian.PutUint32(header[8:12], uint32(iter))
	if _, err := rand.Read(header[12:]); err != nil {
		return nil, err
	}
	key, err := deriveKey(passphrase, header[12:28], iter)
	if err != nil {
		return nil, err
	}
	aead, err := newAEAD(key)
	if err != nil {
		return nil, err
	}
	if _, err := w.Write(header); err != nil {
		return nil, err
	}
	return &encWriter{w: w, aead: aead, header: header, prefix: header[28:], buf: make([]byte, 0, chunkSize)}, nil
}

func newAEAD(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func nonce(prefix []byte, counter uint32, last bool) []byte {
	n := make([]byte, 12)
	copy(n, prefix)
	binary.BigEndian.PutUint32(n[7:11], counter)
	if last {
		n[11] = 1
	}
	return n
}

func (e *encWriter) Write(p []byte) (int, error) {
	written := 0
	for len(p) > 0 && e.err == nil {
		// A full buffer is sealed only when more data follows, so the last
		// chunk is always sealed by Close with the last flag.
		if len(e.buf) == chunkSize {
			e.seal(false)
			continue
		}
		n := copy(e.buf[len(e.buf):chunkSize], p)
		e.buf = e.buf[:len(e.buf)+n]
		p = p[n:]
		written += n
	}
	return written, e.err
}

func (e *encWriter) seal(last bool) {
	if e.err != nil {
		return
	}
	if e.counter == ^uint32(0) {
		e.err = errors.New("backup: too large")
		return
	}
	ct := e.aead.Seal(nil, nonce(e.prefix, e.counter, last), e.buf, e.header)
	e.counter++
	e.buf = e.buf[:0]
	_, e.err = e.w.Write(ct)
}

func (e *encWriter) Close() error {
	e.seal(true)
	return e.err
}

type decReader struct {
	r       *bufio.Reader
	aead    cipher.AEAD
	header  []byte
	prefix  []byte
	counter uint32
	plain   []byte
	done    bool
}

// decrypt returns a reader of the plaintext of an encrypted backup. It
// fails with ErrPassphrase on a wrong passphrase or any damage, including a
// stream cut short.
func decrypt(r io.Reader, passphrase string) (io.Reader, error) {
	br := bufio.NewReaderSize(r, chunkSize+64)
	header := make([]byte, headerSize)
	if _, err := io.ReadFull(br, header); err != nil || string(header[:8]) != magic {
		return nil, errors.New("backup: not a wikiwikibangbang backup")
	}
	iter := binary.BigEndian.Uint32(header[8:12])
	if iter == 0 || iter > maxIterations {
		return nil, errors.New("backup: unsupported key derivation parameters")
	}
	key, err := deriveKey(passphrase, header[12:28], int(iter))
	if err != nil {
		return nil, err
	}
	aead, err := newAEAD(key)
	if err != nil {
		return nil, err
	}
	return &decReader{r: br, aead: aead, header: header, prefix: header[28:]}, nil
}

func (d *decReader) Read(p []byte) (int, error) {
	for len(d.plain) == 0 {
		if d.done {
			return 0, io.EOF
		}
		if err := d.next(); err != nil {
			return 0, err
		}
	}
	n := copy(p, d.plain)
	d.plain = d.plain[n:]
	return n, nil
}

func (d *decReader) next() error {
	ct := make([]byte, chunkSize+d.aead.Overhead())
	n, err := io.ReadFull(d.r, ct)
	switch {
	case errors.Is(err, io.EOF):
		return ErrPassphrase // the last chunk is missing
	case errors.Is(err, io.ErrUnexpectedEOF), err == nil:
	default:
		return err
	}
	ct = ct[:n]
	_, peekErr := d.r.Peek(1)
	last := errors.Is(peekErr, io.EOF)
	if peekErr != nil && !last {
		return peekErr
	}
	pt, err := d.aead.Open(nil, nonce(d.prefix, d.counter, last), ct, d.header)
	if err != nil {
		return ErrPassphrase
	}
	d.counter++
	d.plain = pt
	d.done = last
	return nil
}
