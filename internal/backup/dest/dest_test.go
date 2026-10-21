package dest

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestAddresses(t *testing.T) {
	dir := t.TempDir()
	for _, ok := range []string{dir, "file://" + filepath.ToSlash(dir), " " + dir + " "} {
		if err := Check(ok); err != nil {
			t.Errorf("%q: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "relative/dir", "ftp://host/dir", "https://user:secret@host/dav", "file://elsewhere/dir", "s3://bucket"} {
		if err := Check(bad); !errors.Is(err, ErrInvalid) {
			t.Errorf("%q: %v", bad, err)
		}
	}
	if err := Check("https://user:secret@host/dav"); !strings.Contains(err.Error(), "password") {
		t.Errorf("a password in the address: %v", err)
	}
	if _, err := Open("relative", nil); err == nil {
		t.Error("a relative path opened")
	}
}

func TestLocalDirectory(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	tg, err := Open(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := tg.Put(ctx, "a.wwbb", strings.NewReader("content")); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, "left.part"), nil, 0o600)
	os.Mkdir(filepath.Join(dir, "sub"), 0o700)
	names, err := tg.List(ctx)
	if err != nil || !slices.Equal(names, []string{"a.wwbb"}) {
		t.Fatalf("list %v %v", names, err)
	}
	r, err := tg.Open(ctx, "a.wwbb")
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(r)
	r.Close()
	if string(data) != "content" {
		t.Fatalf("read %q", data)
	}
	if err := tg.Delete(ctx, "a.wwbb"); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"../x", "a/b", "", ".hidden", "x.part"} {
		if tg.Put(ctx, bad, strings.NewReader("")) == nil {
			t.Errorf("stored %q", bad)
		}
		if _, err := tg.Open(ctx, bad); err == nil {
			t.Errorf("opened %q", bad)
		}
		if tg.Delete(ctx, bad) == nil {
			t.Errorf("deleted %q", bad)
		}
	}

	// A failed write leaves nothing under the name, nor a part.
	failing := io.MultiReader(strings.NewReader("half"), errReader{})
	if err := tg.Put(ctx, "b.wwbb", failing); err == nil {
		t.Fatal("a failing write was stored")
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if err := tg.Put(cancelled, "c.wwbb", strings.NewReader("x")); err == nil {
		t.Fatal("a cancelled write was stored")
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if e.Name() != "left.part" && e.Name() != "sub" {
			t.Errorf("left behind: %s", e.Name())
		}
	}

	// A missing directory, e.g. an unmounted share, fails rather than
	// being created.
	gone, _ := Open(filepath.Join(dir, "missing"), nil)
	if err := gone.Put(ctx, "a.wwbb", strings.NewReader("x")); err == nil {
		t.Error("stored into a missing directory")
	}
	if _, err := gone.List(ctx); err == nil {
		t.Error("listed a missing directory")
	}
	file, _ := Open(filepath.Join(dir, "left.part"), nil)
	if _, err := file.List(ctx); err == nil {
		t.Error("listed a file as a directory")
	}
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("broken") }
