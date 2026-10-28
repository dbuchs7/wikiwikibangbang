//go:build !(linux || darwin || freebsd || windows)

package diskspace

func of(string) (Usage, error) { return Usage{}, ErrUnsupported }
