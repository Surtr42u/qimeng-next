//go:build windows

package assetmerge

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"golang.org/x/sys/windows"
)

func linked(info os.FileInfo) bool {
	attr, ok := info.Sys().(*syscall.Win32FileAttributeData)
	return info.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 || ok && attr.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0
}

func singleLink(f *os.File) error {
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(windows.Handle(f.Fd()), &info); err != nil {
		return err
	}
	if info.NumberOfLinks != 1 || info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		return errors.New("expected an independent regular file")
	}
	return nil
}

func longName(name string) (*uint16, error) {
	switch {
	case strings.HasPrefix(name, `\\?\`):
	case strings.HasPrefix(name, `\\`):
		name = `\\?\UNC\` + strings.TrimPrefix(name, `\\`)
	default:
		name = `\\?\` + name
	}
	return windows.UTF16PtrFromString(name)
}

func fileIdentity(f *os.File) (string, error) {
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(windows.Handle(f.Fd()), &info); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x/%x/%x", info.VolumeSerialNumber, info.FileIndexHigh, info.FileIndexLow), nil
}

func renameExclusive(a *area, from, to string) error {
	source, err := longName(filepath.Join(a.name, filepath.FromSlash(from)))
	if err != nil {
		return err
	}
	target, err := longName(filepath.Join(a.name, filepath.FromSlash(to)))
	if err != nil {
		return err
	}
	// No REPLACE_EXISTING or COPY_ALLOWED: existing destinations and other
	// volumes must fail. WRITE_THROUGH is used for the directory handoff too.
	return windows.MoveFileEx(source, target, windows.MOVEFILE_WRITE_THROUGH)
}

// Windows does not support directory fsync. Files are flushed before handoff.
func syncDir(*os.File) error { return nil }
