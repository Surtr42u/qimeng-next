//go:build linux

package storage

import (
	"errors"
	"os"
	"path"
	"runtime"

	"golang.org/x/sys/unix"
)

// RENAME_NOREPLACE is available to a regular Android app on its private
// storage. Open directory handles keep both names anchored to the trusted root.
func PublishTemp(root *os.Root, from, to string) error {
	source, err := root.Open(path.Dir(from))
	if err != nil { return err }
	defer source.Close()
	target, err := root.Open(path.Dir(to))
	if err != nil { return err }
	defer target.Close()
	err=unix.Renameat2(int(source.Fd()),path.Base(from),int(target.Fd()),path.Base(to),unix.RENAME_NOREPLACE)
	if runtime.GOOS!="android" && (errors.Is(err,unix.ENOSYS)||errors.Is(err,unix.EOPNOTSUPP)||errors.Is(err,unix.EINVAL)) {
		return linkAndRemoveStage(root,from,to)
	}
	return err
}
