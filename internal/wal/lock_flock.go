//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd

package wal

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"syscall"
)

// lockDir takes an exclusive advisory lock on dir/LOCK. The kernel releases
// it when the process ends, so a crash leaves no stale lock.
func lockDir(dir string) (io.Closer, error) {
	f, err := os.OpenFile(filepath.Join(dir, "LOCK"), os.O_RDWR|os.O_CREATE, 0o640)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, ErrLocked
		}
		return nil, err
	}
	return f, nil
}
