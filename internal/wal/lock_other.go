//go:build !(darwin || dragonfly || freebsd || linux || netbsd || openbsd || windows)

package wal

import "io"

// lockDir does not lock on platforms outside the release targets
// (ADR-0024); running two processes on one data directory there is not
// detected.
func lockDir(string) (io.Closer, error) { return nopCloser{}, nil }

type nopCloser struct{}

func (nopCloser) Close() error { return nil }
