//go:build linux

package phonerelink

import (
	"os"
	"path"

	"golang.org/x/sys/unix"
)

func linked(info os.FileInfo) bool {
	return info.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0
}

func syncDir(name string) error {
	dir, err := os.Open(name)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

func publish(parent *os.Root, from, to string) error {
	source, err := parent.Open(path.Dir(from))
	if err != nil {
		return err
	}
	defer source.Close()
	target, err := parent.Open(".")
	if err != nil {
		return err
	}
	defer target.Close()
	return unix.Renameat2(int(source.Fd()), path.Base(from), int(target.Fd()), to, unix.RENAME_NOREPLACE)
}
