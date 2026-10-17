//go:build windows

package wal

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"syscall"
)

const errSharingViolation syscall.Errno = 32

// lockDir opens dir/LOCK without sharing it, so a second process cannot open
// it. Windows releases the handle when the process ends.
func lockDir(dir string) (io.Closer, error) {
	path := filepath.Join(dir, "LOCK")
	name, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	h, err := syscall.CreateFile(name, syscall.GENERIC_READ|syscall.GENERIC_WRITE, 0, nil,
		syscall.OPEN_ALWAYS, syscall.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		if errors.Is(err, errSharingViolation) {
			return nil, ErrLocked
		}
		return nil, err
	}
	return os.NewFile(uintptr(h), path), nil
}
