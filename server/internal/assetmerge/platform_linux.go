//go:build linux

package assetmerge

import (
	"errors"
	"fmt"
	"os"
	"path"
	"syscall"

	"golang.org/x/sys/unix"
)

func linked(info os.FileInfo) bool { return info.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 }

func singleLink(f *os.File) error {
	info, err := f.Stat()
	if err != nil {
		return err
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || st.Nlink != 1 {
		return errors.New("expected an independent regular file")
	}
	return nil
}

func renameExclusive(a *area, from, to string) error {
	source, err := a.root.Open(path.Dir(from))
	if err != nil {
		return err
	}
	defer source.Close()
	target, err := a.root.Open(path.Dir(to))
	if err != nil {
		return err
	}
	defer target.Close()
	return unix.Renameat2(int(source.Fd()), path.Base(from), int(target.Fd()), path.Base(to), unix.RENAME_NOREPLACE)
}

func fileIdentity(f *os.File) (string, error) {
	info, err := f.Stat()
	if err != nil {
		return "", err
	}
	st := info.Sys().(*syscall.Stat_t)
	return fmt.Sprintf("%x/%x", st.Dev, st.Ino), nil
}

func syncDir(dir *os.File) error { return dir.Sync() }

func lockHandoff(f *os.File) error {
	return unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
}
