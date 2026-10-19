//go:build linux

package privdrop

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// fake records what Drop would do to the system.
type fake struct {
	euid    int
	chowned []string
	calls   []string
	fail    string // the call that fails
}

func (f *fake) ops() ops {
	call := func(name string) error {
		f.calls = append(f.calls, name)
		if name == f.fail {
			return errors.New(name + " refused")
		}
		return nil
	}
	return ops{
		geteuid: func() int { return f.euid },
		lchown: func(p string, uid, gid int) error {
			f.chowned = append(f.chowned, filepath.Base(p))
			return call("lchown")
		},
		setgroups: func([]int) error { return call("setgroups") },
		setgid:    func(int) error { return call("setgid") },
		setuid: func(int) error {
			if err := call("setuid"); err != nil {
				return err
			}
			f.euid = 65532
			return nil
		},
	}
}

var svc = Account{UID: 65532, GID: 65532}

func TestParse(t *testing.T) {
	if a, err := Parse(" 65532:65532 "); err != nil || a != svc {
		t.Fatalf("%v, %v", a, err)
	}
	for _, bad := range []string{"", "65532", "0:0", "65532:0", "x:1", "1:y", "-1:5"} {
		if _, err := Parse(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func tree(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "data")
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"events.wal", "sub/keys.wal"} {
		if err := os.WriteFile(filepath.Join(dir, f), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestNotRootChangesNothing(t *testing.T) {
	f := &fake{euid: 1000}
	dropped, err := drop(tree(t), svc, f.ops())
	if dropped || err != nil || len(f.calls) != 0 {
		t.Fatalf("dropped %v, err %v, calls %v", dropped, err, f.calls)
	}
}

func TestRootHandsTheDataOverAndDrops(t *testing.T) {
	dir := tree(t)
	f := &fake{euid: 0}
	dropped, err := drop(dir, svc, f.ops())
	if !dropped || err != nil {
		t.Fatalf("dropped %v, err %v", dropped, err)
	}
	slices.Sort(f.chowned)
	if want := []string{"data", "events.wal", "keys.wal", "sub"}; !slices.Equal(f.chowned, want) {
		t.Errorf("chowned %v, want %v", f.chowned, want)
	}
	// Groups and group before the user: after setuid there is no way back.
	tail := f.calls[len(f.calls)-3:]
	if !slices.Equal(tail, []string{"setgroups", "setgid", "setuid"}) {
		t.Errorf("drop order %v", tail)
	}
}

func TestAMissingDataDirectoryIsCreated(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "new", "data")
	f := &fake{euid: 0}
	if _, err := drop(dir, svc, f.ops()); err != nil {
		t.Fatal(err)
	}
	if st, err := os.Stat(dir); err != nil || !st.IsDir() || st.Mode().Perm() != 0o700 {
		t.Fatalf("data directory %v, %v", st, err)
	}
}

func TestEveryFailureStopsAsRoot(t *testing.T) {
	for _, step := range []string{"lchown", "setgroups", "setgid", "setuid"} {
		f := &fake{euid: 0, fail: step}
		if _, err := drop(tree(t), svc, f.ops()); err == nil {
			t.Errorf("%s failed and drop went on", step)
		}
	}
	// A setuid that reports success but leaves root is caught too.
	f := &fake{euid: 0}
	o := f.ops()
	o.setuid = func(int) error { return nil }
	if _, err := drop(tree(t), svc, o); err == nil {
		t.Error("still root after the drop, and no error")
	}
}
