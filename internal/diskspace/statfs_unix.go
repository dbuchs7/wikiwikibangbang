//go:build linux || darwin || freebsd

package diskspace

import "syscall"

func of(path string) (Usage, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return Usage{}, err
	}
	// The field types differ by platform; the products fit in 64 bits.
	return Usage{Free: uint64(st.Bavail) * uint64(st.Bsize), Total: uint64(st.Blocks) * uint64(st.Bsize)}, nil
}
