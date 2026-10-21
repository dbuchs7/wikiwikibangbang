package backup

import (
	"archive/tar"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/dbuchs7/wikiwikibangbang/internal/audit"
	"github.com/dbuchs7/wikiwikibangbang/internal/item"
	"github.com/dbuchs7/wikiwikibangbang/internal/storage"
)

func backupOn(t *testing.T, db *storage.DB, base *Manifest) ([]byte, Manifest) {
	t.Helper()
	files, err := db.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	m, err := WriteBackup(&buf, files, Options{Passphrase: testPass, Build: "test", Now: time.Now(), Base: base})
	if err != nil {
		t.Fatal(err)
	}
	return buf.Bytes(), m
}

// entries lists a backup's archive: each entry's name with the offset it
// continues its file from, and its size.
func entries(t *testing.T, data []byte) map[string][2]string {
	t.Helper()
	plain, err := decrypt(bytes.NewReader(data), testPass)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string][2]string{}
	tr := tar.NewReader(plain)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return out
		}
		if err != nil {
			t.Fatal(err)
		}
		out[hdr.Name] = [2]string{hdr.PAXRecords[fromRecord], string(rune('0' + min(hdr.Size, 9)))}
	}
}

func sources(backups ...[]byte) []Source {
	var out []Source
	for _, b := range backups {
		out = append(out, func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(b)), nil })
	}
	return out
}

func addPage(t *testing.T, db *storage.DB, title string) item.ID {
	t.Helper()
	var id item.ID
	err := db.Write(func(s *item.Store) error {
		it, err := s.Create("admin", item.NewItem{Space: "PLAT", Type: "page", Title: title})
		id = it.ID
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// A chain of a full backup and incremental ones restores to the state of
// the last one; each incremental backup holds only what changed.
func TestAChainRestoresToItsLastBackup(t *testing.T) {
	src := openDB(t, t.TempDir())
	defer src.Close()
	populate(t, src)
	full, m0 := backupOn(t, src, nil)
	if m0.Kind != Full || m0.Chain != m0.ID || m0.Base != "" || m0.Seq != 0 {
		t.Fatalf("full manifest %+v", m0)
	}

	page := addPage(t, src, "second")
	a, err := src.StoreAttachment(page, strings.NewReader("the diagram"), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	a.Name = "diagram.png"
	if err := src.Write(func(s *item.Store) error { a, err = s.AddAttachment("admin", page, a); return err }); err != nil {
		t.Fatal(err)
	}
	inc1, m1 := backupOn(t, src, &m0)
	if m1.Kind != Incremental || m1.Chain != m0.ID || m1.Base != m0.ID || m1.Seq != 1 {
		t.Fatalf("incremental manifest %+v", m1)
	}
	if e := entries(t, inc1); e["events.wal"][0] == "" {
		t.Errorf("the event log is not continued: %v", e)
	}
	afterOne := stateOf(src)

	addPage(t, src, "third")
	src.Audit.Record(audit.Entry{Event: audit.Login, Actor: "anna"})
	inc2, m2 := backupOn(t, src, &m1)
	e := entries(t, inc2)
	if _, ok := e["accounts.wal"]; ok {
		t.Error("an unchanged log is in the incremental backup")
	}
	for name := range e {
		if strings.HasPrefix(name, storage.AttachmentsDir+"/") {
			t.Errorf("an unchanged attachment is in the incremental backup: %s", name)
		}
	}
	if len(m2.Files) != len(m1.Files) {
		t.Errorf("the manifest does not list every file: %d, want %d", len(m2.Files), len(m1.Files))
	}
	want := stateOf(src)

	dir := filepath.Join(t.TempDir(), "r")
	got, err := RestoreChain(sources(full, inc1, inc2), testPass, dir)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != m2.ID {
		t.Fatalf("restored %+v", got)
	}
	dst := openDB(t, dir)
	if st := stateOf(dst); !reflect.DeepEqual(st, want) {
		t.Fatalf("restored state differs\n got %+v\nwant %+v", st, want)
	}
	r, err := dst.OpenAttachment(page, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	content, _ := io.ReadAll(r)
	r.Close()
	if string(content) != "the diagram" {
		t.Fatalf("attachment %q", content)
	}
	dst.Close()

	dir = filepath.Join(t.TempDir(), "r")
	if _, err := RestoreChain(sources(full, inc1), testPass, dir); err != nil {
		t.Fatal(err)
	}
	dst = openDB(t, dir)
	defer dst.Close()
	if st := stateOf(dst); !reflect.DeepEqual(st, afterOne) {
		t.Fatal("a shorter chain does not restore the state of its last backup")
	}

	for name, chain := range map[string][]Source{
		"a gap":                sources(full, inc2),
		"no full backup":       sources(inc1, inc2),
		"the wrong order":      sources(full, inc2, inc1),
		"a second full backup": sources(full, full),
		"nothing":              nil,
		"a source that fails":  {func() (io.ReadCloser, error) { return nil, errors.New("gone") }},
		"a wrong passphrase":   sources(full, inc1[:len(inc1)/2]),
	} {
		target := filepath.Join(t.TempDir(), "r")
		if _, err := RestoreChain(chain, testPass, target); err == nil {
			t.Errorf("%s was restored", name)
		}
		if _, err := os.Stat(target); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s left the target behind", name)
		}
	}
}

// A file whose start differs from the base's, as when a log is rewritten,
// goes into an incremental backup whole; a deleted file is removed again.
func TestIncrementalBackupsTakeRewrittenFilesWhole(t *testing.T) {
	sum := func(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }
	file := func(name, content string) storage.SnapshotFile {
		r := strings.NewReader(content)
		return storage.SnapshotFile{Name: name, Size: int64(len(content)), R: io.NopCloser(r), At: r}
	}
	base := &Manifest{Format: Format, ID: "b", Chain: "b", Files: []File{
		{Name: "events.wal", Size: 3, SHA256: sum("abc")},
		{Name: "keys", Size: 3, SHA256: sum("xyz")},
		{Name: "audit", Size: 9, SHA256: sum("123456789")},
		{Name: "site", Size: 3, SHA256: sum("abc")},
	}}
	var buf bytes.Buffer
	m, err := WriteBackup(&buf, []storage.SnapshotFile{file("events.wal", "abcdef"), file("keys", "uvwxyz"), file("audit", "12"), file("site", "abc")},
		Options{Passphrase: testPass, Now: time.Now(), Base: base})
	if err != nil {
		t.Fatal(err)
	}
	e := entries(t, buf.Bytes())
	if _, ok := e["site"]; ok || e["events.wal"][0] != "3" || e["keys"][0] != "" || e["audit"][0] != "" {
		t.Fatalf("entries %v", e)
	}
	if m.Files[0].SHA256 != sum("abcdef") || m.Files[1].SHA256 != sum("uvwxyz") || m.Files[3] != base.Files[3] {
		t.Fatalf("manifest %+v", m.Files)
	}
	if _, err := WriteBackup(io.Discard, nil, Options{Passphrase: testPass, Base: &Manifest{Format: 1}}); err == nil {
		t.Error("an incremental backup was built on a format 1 backup")
	}
}

// Backups of format 1, which were all full, still restore.
func TestFormatOneBackupsRestore(t *testing.T) {
	src := openDB(t, t.TempDir())
	defer src.Close()
	populate(t, src)
	files, err := src.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	enc, _ := encrypt(&buf, testPass, 1000)
	tw := tar.NewWriter(enc)
	m := Manifest{Format: 1, Build: "old"}
	for _, f := range files {
		data, _ := io.ReadAll(f.R)
		f.R.Close()
		tw.WriteHeader(&tar.Header{Name: f.Name, Size: int64(len(data)), Mode: 0o600, Typeflag: tar.TypeReg})
		tw.Write(data)
		h := sha256.Sum256(data)
		m.Files = append(m.Files, File{Name: f.Name, Size: int64(len(data)), SHA256: hex.EncodeToString(h[:])})
	}
	data, _ := jsonOf(m)
	tw.WriteHeader(&tar.Header{Name: manifestName, Size: int64(len(data)), Mode: 0o600, Typeflag: tar.TypeReg})
	tw.Write(data)
	tw.Close()
	enc.Close()
	got, err := Restore(&buf, testPass, filepath.Join(t.TempDir(), "r"))
	if err != nil {
		t.Fatal(err)
	}
	if !got.IsFull() || got.Build != "old" {
		t.Fatalf("manifest %+v", got)
	}
}

func jsonOf(m Manifest) ([]byte, error) { return json.Marshal(m) }

// Incremental backups that were not written by WriteBackup are refused.
func TestRestoreRefusesForgedIncrementals(t *testing.T) {
	src := openDB(t, t.TempDir())
	defer src.Close()
	populate(t, src)
	full, m0 := backupOn(t, src, nil)
	type entry struct {
		name, content string
		from          int64
		dir           bool
	}
	forge := func(m Manifest, entries ...entry) []byte {
		var buf bytes.Buffer
		enc, _ := encrypt(&buf, testPass, 1000)
		tw := tar.NewWriter(enc)
		for _, e := range entries {
			hdr := &tar.Header{Name: e.name, Size: int64(len(e.content)), Mode: 0o600, Typeflag: tar.TypeReg}
			if e.dir {
				hdr.Typeflag, hdr.Size = tar.TypeDir, 0
			}
			if e.from > 0 {
				hdr.Format, hdr.PAXRecords = tar.FormatPAX, map[string]string{fromRecord: strconv.FormatInt(e.from, 10)}
			}
			tw.WriteHeader(hdr)
			tw.Write([]byte(e.content))
		}
		data, _ := json.Marshal(m)
		tw.WriteHeader(&tar.Header{Name: manifestName, Size: int64(len(data)), Mode: 0o600, Typeflag: tar.TypeReg})
		tw.Write(data)
		tw.Close()
		enc.Close()
		return buf.Bytes()
	}
	inc := Manifest{Format: Format, ID: "next", Kind: Incremental, Chain: m0.Chain, Base: m0.ID, Seq: 1, Files: m0.Files}
	var walSize int64
	for _, f := range m0.Files {
		if f.Name == "events.wal" {
			walSize = f.Size
		}
	}
	lying := inc
	lying.Files = append([]File(nil), m0.Files...)
	for i := range lying.Files {
		if lying.Files[i].Name == "events.wal" {
			lying.Files[i].Size++
		}
	}
	for name, archive := range map[string][]byte{
		"a wrong offset":       forge(inc, entry{name: "events.wal", content: "x", from: walSize + 1}),
		"a bad offset":         forge(inc, entry{name: "events.wal", content: "x", from: -1}),
		"an offset of nothing": forge(inc, entry{name: "accounts.wal.new", content: "x", from: 3}),
		"a lying manifest":     forge(lying, entry{name: "events.wal", content: "x", from: walSize}),
		"a directory":          forge(inc, entry{name: "attachments", dir: true}),
		"a file twice":         forge(inc, entry{name: "audit.wal", content: "x"}, entry{name: "audit.wal", content: "x"}),
		"a missing file":       forge(Manifest{Format: Format, ID: "next", Kind: Incremental, Chain: m0.Chain, Base: m0.ID, Seq: 1}),
		"a file not listed":    forge(Manifest{Format: Format, ID: "next", Kind: Incremental, Chain: m0.Chain, Base: m0.ID, Seq: 1, Files: m0.Files[:1]}, entry{name: "audit.wal", content: "x"}),
	} {
		target := filepath.Join(t.TempDir(), "r")
		if _, err := RestoreChain(sources(full, archive), testPass, target); err == nil {
			t.Errorf("%s was restored", name)
		}
	}
	// A chain without the files every data directory has.
	bare := forge(Manifest{Format: Format, ID: "x", Kind: Full, Chain: "x"})
	if _, err := RestoreChain(sources(bare), testPass, filepath.Join(t.TempDir(), "r")); err == nil || !strings.Contains(err.Error(), "missing") {
		t.Errorf("an empty backup: %v", err)
	}
}
