//go:build windows

package phonerelink

import (
	"os"
	"strings"
	"syscall"

	"golang.org/x/sys/windows"
)

func linked(info os.FileInfo) bool {
	attr, ok := info.Sys().(*syscall.Win32FileAttributeData)
	return info.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 || ok && attr.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0
}

func syncDir(string) error { return nil }

func publish(from, to string) error {
	long := func(name string) (*uint16, error) {
		if strings.HasPrefix(name, "\\\\") {
			name = "\\\\?\\UNC\\" + strings.TrimPrefix(name, "\\\\")
		} else {
			name = "\\\\?\\" + name
		}
		return windows.UTF16PtrFromString(name)
	}
	src, err := long(from)
	if err != nil {
		return err
	}
	dst, err := long(to)
	if err != nil {
		return err
	}
	return windows.MoveFileEx(src, dst, windows.MOVEFILE_WRITE_THROUGH)
}
