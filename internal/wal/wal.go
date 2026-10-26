// Package wal is the write-ahead log of the event store (ADR-0008, ADR-0025):
// one append-only file of checksummed records, synced to disk before an
// append returns.
//
// File format: an 8-byte header "WWBBLOG1", then records of
//
//	uint32 little-endian payload length
//	uint32 little-endian bitwise complement of the length
//	uint32 little-endian CRC-32C of the payload
//	payload
//
// A crash can leave the last record incomplete. Opening the log drops such a
// torn tail, because it was never acknowledged. Damage anywhere before the
// last record is not a crash artefact; Open refuses it instead of guessing.
// The complement guards the length: without it, a damaged length in the
// middle of the file would point past its end and read as a torn tail,
// silently dropping every record after it.
package wal

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
	"runtime"
)

// FileName is the name of the log file inside the data directory.
const FileName = "events.wal"

// MaxRecord bounds one record, so a damaged length field cannot make Open
// allocate gigabytes.
const MaxRecord = 64 << 20

var (
	header = []byte("WWBBLOG1")
	crcTab = crc32.MakeTable(crc32.Castagnoli)

	// ErrCorrupt reports damage that a crash during an append cannot cause.
	ErrCorrupt = errors.New("wal: log is corrupt")
	// ErrLocked reports that another process has the data directory open.
	ErrLocked = errors.New("wal: data directory is in use by another process")
	// ErrBroken reports that an earlier append failed and could not be
	// undone; the log refuses further appends until it is reopened.
	ErrBroken = errors.New("wal: log is unusable after a failed append")
)

// Scan is the result of reading a log file.
type Scan struct {
	Records [][]byte
	// Valid is the length of the file up to the end of the last complete record.
	Valid int64
	// Torn is the number of bytes after Valid: an append a crash interrupted.
	Torn int64
}

// Read reads the log file at path without changing it. A missing file reads
// as an empty log.
func Read(path string) (Scan, error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return Scan{}, nil
	}
	if err != nil {
		return Scan{}, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return Scan{}, err
	}
	return collect(bufio.NewReader(f), st.Size())
}

// Each passes the records of the log file at path to fn in order, one at a
// time, without holding them all, and without changing the file; it stops
// at the first error fn returns. A missing file has no records, and a torn
// tail, an append still in progress, is not passed.
func Each(path string, fn func(payload []byte) error) error {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return err
	}
	_, err = scan(bufio.NewReader(f), st.Size(), fn)
	return err
}

// ReadBytes reads a log held in memory, like Read: a copy taken while an
// append was in progress ends in a torn tail, which Valid excludes.
func ReadBytes(data []byte) (Scan, error) { return collect(bytes.NewReader(data), int64(len(data))) }

func collect(r io.Reader, size int64) (Scan, error) {
	var records [][]byte
	sc, err := scan(r, size, func(p []byte) error { records = append(records, p); return nil })
	sc.Records = records
	return sc, err
}

// scan checks the records of a log and passes each complete one to each.
func scan(r io.Reader, size int64, each func([]byte) error) (Scan, error) {
	var sc Scan
	if size == 0 {
		return sc, nil
	}
	h := make([]byte, len(header))
	if _, err := io.ReadFull(r, h); err != nil || string(h) != string(header) {
		if size < int64(len(header)) && string(h[:size]) == string(header[:size]) {
			sc.Torn = size // the very first write was interrupted
			return sc, nil
		}
		return sc, fmt.Errorf("%w: not a log file (bad header)", ErrCorrupt)
	}
	off := int64(len(header))
	sc.Valid = off
	var frame [12]byte
	for off < size {
		rest := size - off
		if rest < int64(len(frame)) {
			sc.Torn = rest
			return sc, nil
		}
		if _, err := io.ReadFull(r, frame[:]); err != nil {
			return sc, err
		}
		n32 := binary.LittleEndian.Uint32(frame[0:4])
		if binary.LittleEndian.Uint32(frame[4:8]) != ^n32 {
			return sc, fmt.Errorf("%w: damaged length in record at offset %d", ErrCorrupt, off)
		}
		n := int64(n32)
		sum := binary.LittleEndian.Uint32(frame[8:12])
		end := off + int64(len(frame)) + n
		if n > MaxRecord {
			return sc, fmt.Errorf("%w: record at offset %d claims %d bytes", ErrCorrupt, off, n)
		}
		if end > size {
			sc.Torn = rest // the payload was not completely written
			return sc, nil
		}
		payload := make([]byte, n)
		if _, err := io.ReadFull(r, payload); err != nil {
			return sc, err
		}
		if crc32.Checksum(payload, crcTab) != sum {
			if end == size {
				sc.Torn = rest // the last write reached the length but not all of the data
				return sc, nil
			}
			return sc, fmt.Errorf("%w: checksum mismatch in record at offset %d", ErrCorrupt, off)
		}
		if err := each(payload); err != nil {
			return sc, err
		}
		off = end
		sc.Valid = off
	}
	return sc, nil
}

// Log is an open, locked log, ready for appends. It is not safe for
// concurrent use; the store has a single writer (ADR-0008).
type Log struct {
	f      file
	lock   io.Closer
	size   int64
	broken bool
}

// file is the part of *os.File the log uses; tests substitute failures.
type file interface {
	WriteAt([]byte, int64) (int, error)
	Sync() error
	Truncate(int64) error
	Close() error
}

// Open locks the data directory, reads the log and prepares it for appends,
// creating directory and file if needed. A torn tail is truncated; the
// returned Scan reports how many bytes were dropped.
func Open(dir string) (*Log, Scan, error) {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, Scan{}, err
	}
	lock, err := lockDir(dir)
	if err != nil {
		return nil, Scan{}, err
	}
	l, sc, err := OpenFile(dir, FileName)
	if err != nil {
		lock.Close()
		return nil, Scan{}, err
	}
	l.lock = lock
	return l, sc, nil
}

// Lock takes the data directory's lock, for callers that open several log
// files with OpenFile. Close releases it.
func Lock(dir string) (io.Closer, error) {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, err
	}
	return lockDir(dir)
}

// OpenFile opens the log file name in dir like Open, without taking the
// directory lock; the caller holds it (Lock).
func OpenFile(dir, name string) (*Log, Scan, error) {
	path := filepath.Join(dir, name)
	sc, err := Read(path)
	if err != nil {
		return nil, sc, err
	}
	_, statErr := os.Stat(path)
	created := errors.Is(statErr, os.ErrNotExist)
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o640)
	if err != nil {
		return nil, sc, err
	}
	l := &Log{f: f, size: sc.Valid}
	if sc.Torn > 0 {
		if err := l.truncate(sc.Valid); err != nil {
			f.Close()
			return nil, sc, fmt.Errorf("wal: dropping torn tail: %w", err)
		}
	}
	if l.size == 0 {
		if err := l.write(header); err != nil {
			f.Close()
			return nil, sc, err
		}
	}
	if created {
		if err := syncDir(dir); err != nil {
			f.Close()
			return nil, sc, err
		}
	}
	return l, sc, nil
}

// Append writes one record and syncs it to disk. When it returns nil, the
// record survives a crash. On error the record is not in the log.
func (l *Log) Append(payload []byte) error {
	if l.broken {
		return ErrBroken
	}
	if len(payload) > MaxRecord {
		return fmt.Errorf("wal: record of %d bytes exceeds the limit of %d", len(payload), MaxRecord)
	}
	buf := make([]byte, 12+len(payload))
	n := uint32(len(payload))
	binary.LittleEndian.PutUint32(buf[0:4], n)
	binary.LittleEndian.PutUint32(buf[4:8], ^n)
	binary.LittleEndian.PutUint32(buf[8:12], crc32.Checksum(payload, crcTab))
	copy(buf[12:], payload)
	return l.write(buf)
}

// write appends and syncs. A failed write is cut back, so a later append does
// not follow a half-written record. A failed sync breaks the log: after it,
// the kernel may have dropped the dirty pages and cleared the error, so
// neither the data nor a retried sync can be trusted (the "fsyncgate"
// lesson); only reopening, which re-reads the file, can.
func (l *Log) write(b []byte) error {
	before := l.size
	if _, err := l.f.WriteAt(b, before); err != nil {
		return l.undo(before, err)
	}
	if err := l.f.Sync(); err != nil {
		l.broken = true
		return fmt.Errorf("%w: sync failed: %v", ErrBroken, err)
	}
	l.size = before + int64(len(b))
	return nil
}

func (l *Log) undo(size int64, cause error) error {
	if err := l.truncate(size); err != nil {
		l.broken = true
		return fmt.Errorf("%w: %v (undo failed: %v)", ErrBroken, cause, err)
	}
	return fmt.Errorf("wal: append failed: %w", cause)
}

func (l *Log) truncate(size int64) error {
	if err := l.f.Truncate(size); err != nil {
		return err
	}
	if err := l.f.Sync(); err != nil {
		return err
	}
	l.size = size
	return nil
}

// Size is the length of the log up to the end of its last durable record:
// a copy of that many bytes is a complete log (ADR-0016).
func (l *Log) Size() int64 { return l.size }

// Close closes the log and releases the directory lock if Open took it.
func (l *Log) Close() error {
	err := l.f.Close()
	if l.lock != nil {
		if lerr := l.lock.Close(); err == nil {
			err = lerr
		}
	}
	return err
}

// Rewrite atomically replaces the log file name in dir with one holding
// exactly records: it writes a temporary file, syncs it, renames it over the
// old one and syncs the directory. A crash leaves either the old or the new
// file, never a mixture. An open Log of that file must be reopened after.
//
// Rewrite is how data leaves an append-only file, e.g. a destroyed key
// (ADR-0026). The old file's blocks are released to the file system, not
// overwritten; on SSDs and copy-on-write file systems they can survive until
// reused.
func Rewrite(dir, name string, records [][]byte) error {
	tmp := filepath.Join(dir, name+".tmp")
	f, err := os.OpenFile(tmp, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o640)
	if err != nil {
		return err
	}
	l := &Log{f: f}
	err = l.write(header)
	for _, r := range records {
		if err == nil {
			err = l.Append(r)
		}
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp, filepath.Join(dir, name))
	}
	if err == nil {
		err = syncDir(dir)
	}
	if err != nil {
		os.Remove(tmp)
		return fmt.Errorf("wal: rewriting %s: %w", name, err)
	}
	return nil
}

// syncDir makes a newly created file's directory entry durable. Windows
// cannot sync a directory and does not need to.
func syncDir(dir string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}
