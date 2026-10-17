package wal

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func records(n int) [][]byte {
	var out [][]byte
	for i := range n {
		out = append(out, bytes.Repeat([]byte{byte('a' + i)}, 10+i))
	}
	return out
}

func mustOpen(t *testing.T, dir string) (*Log, Scan) {
	t.Helper()
	l, sc, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return l, sc
}

func write(t *testing.T, dir string, recs [][]byte) []byte {
	t.Helper()
	l, _ := mustOpen(t, dir)
	for _, r := range recs {
		if err := l.Append(r); err != nil {
			t.Fatal(err)
		}
	}
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, FileName))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestAppendAndReopen(t *testing.T) {
	dir := t.TempDir()
	want := records(3)
	write(t, dir, want)
	l, sc := mustOpen(t, dir)
	defer l.Close()
	if fmt.Sprint(sc.Records) != fmt.Sprint(want) || sc.Torn != 0 {
		t.Fatalf("reopened: %d records, torn %d", len(sc.Records), sc.Torn)
	}
	if err := l.Append([]byte("more")); err != nil {
		t.Fatal(err)
	}
}

func TestEmptyAndMissing(t *testing.T) {
	sc, err := Read(filepath.Join(t.TempDir(), FileName))
	if err != nil || len(sc.Records) != 0 {
		t.Fatalf("missing file: %v, %d records", err, len(sc.Records))
	}
	dir := t.TempDir()
	write(t, dir, nil)
	sc, err = Read(filepath.Join(dir, FileName))
	if err != nil || len(sc.Records) != 0 || sc.Valid != int64(len(header)) {
		t.Fatalf("empty log: %v, %+v", err, sc)
	}
	if err := l0(t, dir).Append(nil); err != nil {
		t.Fatalf("empty record: %v", err)
	}
}

func l0(t *testing.T, dir string) *Log {
	l, _ := mustOpen(t, dir)
	t.Cleanup(func() { l.Close() })
	return l
}

// TestEveryCrashPointRecovers cuts a log at every byte, as a crash during an
// append could, and checks that Open keeps exactly the complete records,
// drops the rest, and leaves a log that accepts appends and reopens cleanly.
func TestEveryCrashPointRecovers(t *testing.T) {
	recs := records(3)
	full := write(t, t.TempDir(), recs)

	var ends []int // file length after each complete record
	off := len(header)
	for _, r := range recs {
		off += 12 + len(r)
		ends = append(ends, off)
	}
	for cut := 0; cut <= len(full); cut++ {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, FileName), full[:cut], 0o640); err != nil {
			t.Fatal(err)
		}
		complete := 0
		for _, e := range ends {
			if cut >= e {
				complete++
			}
		}
		l, sc, err := Open(dir)
		if err != nil {
			t.Fatalf("cut at %d: %v", cut, err)
		}
		if len(sc.Records) != complete {
			t.Fatalf("cut at %d: %d records, want %d", cut, len(sc.Records), complete)
		}
		if err := l.Append([]byte("after")); err != nil {
			t.Fatalf("cut at %d: append: %v", cut, err)
		}
		l.Close()
		sc, err = Read(filepath.Join(dir, FileName))
		if err != nil || len(sc.Records) != complete+1 || sc.Torn != 0 {
			t.Fatalf("cut at %d: after recovery: %v, %d records, torn %d", cut, err, len(sc.Records), sc.Torn)
		}
	}
}

func TestDamageBeforeTheTailIsRefused(t *testing.T) {
	recs := records(3)
	full := write(t, t.TempDir(), recs)
	first := len(header)
	cases := map[string]int{
		"length of the first record":     first,
		"complement of the first record": first + 4,
		"checksum of the first record":   first + 8,
		"payload of the first record":    first + 12,
		"header":                         0,
	}
	for name, at := range cases {
		dir := t.TempDir()
		damaged := bytes.Clone(full)
		damaged[at] ^= 0x01
		if err := os.WriteFile(filepath.Join(dir, FileName), damaged, 0o640); err != nil {
			t.Fatal(err)
		}
		if _, _, err := Open(dir); !errors.Is(err, ErrCorrupt) {
			t.Errorf("%s: err = %v, want ErrCorrupt", name, err)
		}
	}
}

func TestDamagedLastPayloadIsATornTail(t *testing.T) {
	recs := records(3)
	full := write(t, t.TempDir(), recs)
	dir := t.TempDir()
	damaged := bytes.Clone(full)
	damaged[len(damaged)-1] ^= 0x01
	if err := os.WriteFile(filepath.Join(dir, FileName), damaged, 0o640); err != nil {
		t.Fatal(err)
	}
	l, sc := mustOpen(t, dir)
	defer l.Close()
	if len(sc.Records) != 2 || sc.Torn == 0 {
		t.Fatalf("%d records, torn %d; want 2 and a torn tail", len(sc.Records), sc.Torn)
	}
}

func TestOversizedRecords(t *testing.T) {
	l := l0(t, t.TempDir())
	if err := l.Append(make([]byte, MaxRecord+1)); err == nil {
		t.Fatal("an oversized record was accepted")
	}
	// A length beyond the limit with a matching complement is damage, not a
	// torn tail.
	dir := t.TempDir()
	frame := make([]byte, 12)
	n := uint32(MaxRecord + 1)
	frame[0], frame[1], frame[2], frame[3] = byte(n), byte(n>>8), byte(n>>16), byte(n>>24)
	c := ^n
	frame[4], frame[5], frame[6], frame[7] = byte(c), byte(c>>8), byte(c>>16), byte(c>>24)
	if err := os.WriteFile(filepath.Join(dir, FileName), append(bytes.Clone(header), frame...), 0o640); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Open(dir); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("err = %v, want ErrCorrupt", err)
	}
}

func TestOneProcessPerDataDirectory(t *testing.T) {
	dir := t.TempDir()
	l, _ := mustOpen(t, dir)
	if _, _, err := Open(dir); !errors.Is(err, ErrLocked) {
		t.Fatalf("second Open: err = %v, want ErrLocked", err)
	}
	l.Close()
	l2, _ := mustOpen(t, dir)
	l2.Close()
}

func TestOpenFailsOnAFileWhereTheDirectoryShouldBe(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data")
	if err := os.WriteFile(path, nil, 0o640); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Open(path); err == nil {
		t.Fatal("Open succeeded on a regular file")
	}
}

type faultyFile struct {
	file
	failWrite, failSync, failTruncate bool
}

func (f *faultyFile) WriteAt(b []byte, off int64) (int, error) {
	if f.failWrite {
		n, _ := f.file.WriteAt(b[:len(b)/2], off) // a partial write
		return n, errors.New("injected write failure")
	}
	return f.file.WriteAt(b, off)
}

func (f *faultyFile) Sync() error {
	if f.failSync {
		return errors.New("injected sync failure")
	}
	return f.file.Sync()
}

func (f *faultyFile) Truncate(n int64) error {
	if f.failTruncate {
		return errors.New("injected truncate failure")
	}
	return f.file.Truncate(n)
}

func TestAFailedWriteLeavesNoTrace(t *testing.T) {
	dir := t.TempDir()
	l, _ := mustOpen(t, dir)
	if err := l.Append([]byte("kept")); err != nil {
		t.Fatal(err)
	}
	ff := &faultyFile{file: l.f, failWrite: true}
	l.f = ff
	if err := l.Append([]byte("lost")); err == nil || errors.Is(err, ErrBroken) {
		t.Fatalf("err = %v, want a recoverable failure", err)
	}
	ff.failWrite = false
	if err := l.Append([]byte("next")); err != nil {
		t.Fatalf("append after a failure: %v", err)
	}
	l.Close()
	sc, err := Read(filepath.Join(dir, FileName))
	if err != nil || fmt.Sprintf("%s", sc.Records) != "[kept next]" {
		t.Fatalf("log holds %s (%v), want [kept next]", sc.Records, err)
	}
}

func TestAFailedSyncBreaksTheLogUntilReopened(t *testing.T) {
	dir := t.TempDir()
	l, _ := mustOpen(t, dir)
	ff := &faultyFile{file: l.f, failSync: true}
	l.f = ff
	if err := l.Append([]byte("x")); !errors.Is(err, ErrBroken) {
		t.Fatalf("err = %v, want ErrBroken", err)
	}
	ff.failSync = false
	if err := l.Append([]byte("y")); !errors.Is(err, ErrBroken) {
		t.Fatalf("append after a failed sync: err = %v, want ErrBroken", err)
	}
	l.Close()
	l2, _ := mustOpen(t, dir)
	defer l2.Close()
	if err := l2.Append([]byte("z")); err != nil {
		t.Fatalf("append after reopening: %v", err)
	}
}

func TestAnUndoableFailureBreaksTheLog(t *testing.T) {
	l := l0(t, t.TempDir())
	l.f = &faultyFile{file: l.f, failWrite: true, failTruncate: true}
	if err := l.Append([]byte("x")); !errors.Is(err, ErrBroken) {
		t.Fatalf("err = %v, want ErrBroken", err)
	}
	if err := l.Append([]byte("y")); !errors.Is(err, ErrBroken) {
		t.Fatalf("append on a broken log: err = %v, want ErrBroken", err)
	}
}

func TestRewriteReplacesTheFileAtomically(t *testing.T) {
	dir := t.TempDir()
	lock, err := Lock(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if _, err := Lock(dir); !errors.Is(err, ErrLocked) {
		t.Fatalf("second Lock: err = %v, want ErrLocked", err)
	}
	l, _, err := OpenFile(dir, "keys.wal")
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range records(3) {
		if err := l.Append(r); err != nil {
			t.Fatal(err)
		}
	}
	l.Close()
	if err := Rewrite(dir, "keys.wal", [][]byte{[]byte("only")}); err != nil {
		t.Fatal(err)
	}
	sc, err := Read(filepath.Join(dir, "keys.wal"))
	if err != nil || fmt.Sprintf("%s", sc.Records) != "[only]" {
		t.Fatalf("after rewrite: %s, %v", sc.Records, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "keys.wal.tmp")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("the temporary file was left behind")
	}
	if err := Rewrite(filepath.Join(dir, "missing"), "keys.wal", nil); err == nil {
		t.Fatal("Rewrite into a missing directory succeeded")
	}
	if err := Rewrite(dir, "big.wal", [][]byte{make([]byte, MaxRecord+1)}); err == nil {
		t.Fatal("Rewrite with an oversized record succeeded")
	}
	if _, err := os.Stat(filepath.Join(dir, "big.wal.tmp")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("a failed rewrite left its temporary file")
	}
}
