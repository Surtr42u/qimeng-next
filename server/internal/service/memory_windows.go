package service

import (
	"golang.org/x/sys/windows"
	"unsafe"
)

var globalMemoryStatus = windows.NewLazySystemDLL("kernel32.dll").NewProc("GlobalMemoryStatusEx")

func availableMemory() uint64 {
	var status struct {
		Length, Load                                                                                                          uint32
		TotalPhysical, AvailablePhysical, TotalPageFile, AvailablePageFile, TotalVirtual, AvailableVirtual, AvailableExtended uint64
	}
	status.Length = uint32(unsafe.Sizeof(status))
	ok, _, _ := globalMemoryStatus.Call(uintptr(unsafe.Pointer(&status)))
	if ok == 0 {
		return 0
	}
	return status.AvailablePhysical
}
