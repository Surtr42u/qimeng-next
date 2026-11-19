//go:build windows

package legacycold

import (
	"os"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/windows"
)

func unsafeFileInfo(info os.FileInfo) bool {
	attr, ok := info.Sys().(*syscall.Win32FileAttributeData)
	return info.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 || ok && attr.FileAttributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT != 0
}

func unsafeEntry(entry os.DirEntry) bool {
	info, err := entry.Info()
	return err != nil || unsafeFileInfo(info)
}

// The Windows converter also writes archives to removable volumes without
// hard links. MoveFileEx on the same volume with no REPLACE flag publishes a
// completed, flushed staging file atomically and fails if the name exists.
func publishSidecarOrArchive(out *readRoot, name string) error {
	if err := out.check(); err != nil {
		return err
	}
	from, err := windows.UTF16PtrFromString(filepath.Join(out.name, ".incomplete", name))
	if err != nil {
		return err
	}
	to, err := windows.UTF16PtrFromString(filepath.Join(out.name, name))
	if err != nil {
		return err
	}
	return windows.MoveFileEx(from, to, windows.MOVEFILE_WRITE_THROUGH)
}
