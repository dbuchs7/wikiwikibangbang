//go:build !linux

package privdrop

// Drop does nothing outside Linux: the container image is Linux only.
func Drop(string, Account) (bool, error) { return false, nil }
