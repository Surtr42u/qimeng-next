//go:build linux

package service

import (
	"os"
	"syscall"
)

func freeBytes(path string) (int64, error) {
	var st syscall.Statfs_t
	if e := syscall.Statfs(path, &st); e != nil {
		return 0, e
	}
	return int64(st.Bavail) * int64(st.Bsize), nil
}
func syncDirectory(f *os.File) error { return f.Sync() }
