package privdrop

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
)

// ops are the system calls Drop makes, replaceable in tests.
type ops struct {
	geteuid   func() int
	lchown    func(path string, uid, gid int) error
	setgroups func([]int) error
	setgid    func(int) error
	setuid    func(int) error
}

var system = ops{
	geteuid: os.Geteuid, lchown: os.Lchown,
	setgroups: syscall.Setgroups, setgid: syscall.Setgid, setuid: syscall.Setuid,
}

// Drop does nothing unless the process runs as root. As root it creates
// dir if needed, gives everything in it that the account does not own to
// the account, and then becomes the account irreversibly. It reports
// whether it dropped root; on any error the caller must stop.
func Drop(dir string, to Account) (bool, error) { return drop(dir, to, system) }

func drop(dir string, to Account, o ops) (bool, error) {
	if o.geteuid() != 0 {
		return false, nil
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return false, fmt.Errorf("create the data directory: %w", err)
	}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if st, ok := info.Sys().(*syscall.Stat_t); ok && int(st.Uid) == to.UID && int(st.Gid) == to.GID {
			return nil
		}
		// Lchown: a symbolic link is handed over itself, never followed out
		// of the data directory.
		return o.lchown(p, to.UID, to.GID)
	})
	if err != nil {
		return false, fmt.Errorf("hand the data directory to %d:%d: %w", to.UID, to.GID, err)
	}
	// Supplementary groups and the group first: once the user is no longer
	// root, it can change neither.
	if err := o.setgroups([]int{}); err != nil {
		return false, fmt.Errorf("drop supplementary groups: %w", err)
	}
	if err := o.setgid(to.GID); err != nil {
		return false, fmt.Errorf("set group %d: %w", to.GID, err)
	}
	if err := o.setuid(to.UID); err != nil {
		return false, fmt.Errorf("set user %d: %w", to.UID, err)
	}
	if o.geteuid() == 0 {
		return false, fmt.Errorf("still root after setuid(%d)", to.UID)
	}
	return true, nil
}
