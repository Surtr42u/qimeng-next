//go:build windows

package legacycold

import (
	"path/filepath"

	"golang.org/x/sys/windows"
)

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
