//go:build windows

package service

import (
	"os"
	"syscall"
	"unsafe"
)

func freeBytes(path string) (int64, error) {
	p, e := syscall.UTF16PtrFromString(path)
	if e != nil {
		return 0, e
	}
	var available, total, free uint64
	fn := syscall.NewLazyDLL("kernel32.dll").NewProc("GetDiskFreeSpaceExW")
	ok, _, err := fn.Call(uintptr(unsafe.Pointer(p)), uintptr(unsafe.Pointer(&available)), uintptr(unsafe.Pointer(&total)), uintptr(unsafe.Pointer(&free)))
	if ok == 0 {
		return 0, err
	}
	return int64(available), nil
}

// Windows does not provide fsync on directory handles. File bytes are flushed;
// the persistent intent provides recovery without claiming power-loss atomicity.
func syncDirectory(f *os.File) error { return nil }
