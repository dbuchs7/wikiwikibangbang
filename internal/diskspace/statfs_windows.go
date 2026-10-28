//go:build windows

package diskspace

import (
	"syscall"
	"unsafe"
)

var getDiskFreeSpaceEx = syscall.NewLazyDLL("kernel32.dll").NewProc("GetDiskFreeSpaceExW")

func of(path string) (Usage, error) {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return Usage{}, err
	}
	var free, total, all uint64
	r, _, err := getDiskFreeSpaceEx.Call(uintptr(unsafe.Pointer(p)), uintptr(unsafe.Pointer(&free)), uintptr(unsafe.Pointer(&total)), uintptr(unsafe.Pointer(&all)))
	if r == 0 {
		return Usage{}, err
	}
	return Usage{Free: free, Total: total}, nil
}
