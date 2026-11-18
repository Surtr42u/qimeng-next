//go:build windows

package service

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"golang.org/x/sys/windows"
	"io"
	"os"
	"syscall"
)

const syncReadFlags = 0
const syncPlatformSupported = true

func syncPathLink(info os.FileInfo) bool {
	attrs, ok := info.Sys().(*syscall.Win32FileAttributeData)
	return info.Mode()&os.ModeSymlink != 0 || ok && attrs.FileAttributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT != 0
}

func syncFileStamp(file *os.File, info os.FileInfo) (string, error) {
	var id syscall.ByHandleFileInformation
	if e := syscall.GetFileInformationByHandle(syscall.Handle(file.Fd()), &id); e != nil {
		return "", e
	}
	// FILE_BASIC_INFO contains four 64-bit timestamps followed by attributes.
	// ChangeTime also detects rewrites whose original last-write time was restored.
	var basic [40]byte
	if e := windows.GetFileInformationByHandleEx(windows.Handle(file.Fd()), windows.FileBasicInfo, &basic[0], uint32(len(basic))); e != nil || binary.LittleEndian.Uint64(basic[24:32]) == 0 {
		// Drivers which do not expose a reliable change clock still need to
		// distinguish same-length rewrites with a preserved last-write time.
		h := sha256.New()
		if _, e := io.CopyN(h, io.NewSectionReader(file, 0, info.Size()), info.Size()); e != nil {
			return "", e
		}
		return fmt.Sprintf("%d/%d/%d/%d/%d/%x", id.VolumeSerialNumber, id.FileIndexHigh, id.FileIndexLow, info.Size(), info.ModTime().UnixNano(), h.Sum(nil)), nil
	}
	return fmt.Sprintf("%d/%d/%d/%d/%d/%d", id.VolumeSerialNumber, id.FileIndexHigh, id.FileIndexLow, binary.LittleEndian.Uint64(basic[24:32]), info.Size(), info.ModTime().UnixNano()), nil
}
func fileObjectIdentity(file *os.File, _ os.FileInfo) (string, error) {
	var id syscall.ByHandleFileInformation
	if e := syscall.GetFileInformationByHandle(syscall.Handle(file.Fd()), &id); e != nil {
		return "", e
	}
	return fmt.Sprintf("%d/%d/%d/%d/%d", id.VolumeSerialNumber, id.FileIndexHigh, id.FileIndexLow, id.CreationTime.HighDateTime, id.CreationTime.LowDateTime), nil
}
