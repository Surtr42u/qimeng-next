package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
)

func TestScanKnownDirectoryRejectsSwappedAndCancelledPages(t *testing.T) {
	s, run, library := preparedScanFixture(t)
	run.manualFull = true
	folder := filepath.Join(library, "album")
	if err := os.Mkdir(folder, 0700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(folder, "known.png")
	data := syntheticPNG(t, 11)
	if err := os.WriteFile(file, data, 0600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(file)
	if err != nil {
		t.Fatal(err)
	}
	stamp := info.ModTime()
	known := map[string]scanSignature{"album/known.png": {RelativePath: "album/known.png", Size: info.Size(), Mtime: &stamp}}
	dir, err := s.Root.Open("libraries/" + run.library.ID + "/album")
	if err != nil {
		t.Fatal(err)
	}
	defer dir.Close()
	entries, err := dir.ReadDir(-1)
	if err != nil {
		t.Fatal(err)
	}
	skips, err := s.knownDirectoryEntries(context.Background(), run, dir, "album", entries, known)
	if err != nil || len(skips) != 1 || !skips[0] {
		t.Fatal("stable known file not recognized", skips, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if skips, err = s.knownDirectoryEntries(ctx, run, dir, "album", entries, known); !errors.Is(err, context.Canceled) || len(skips) != 0 {
		t.Fatal("cancelled page accepted", skips, err)
	}
	if err = os.Rename(folder, folder+"-old"); err != nil {
		if runtime.GOOS == "windows" && (errors.Is(err, os.ErrPermission) || errors.Is(err, syscall.Errno(32))) {
			t.Log("open Windows directory cannot be renamed on this volume")
			return
		}
		t.Fatal(err)
	}
	if err = os.Mkdir(folder, 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(file, data, 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.Chtimes(file, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	skips, err = s.knownDirectoryEntries(context.Background(), run, dir, "album", entries, known)
	if err != nil || len(skips) != 0 {
		t.Fatal("stale directory listing applied to a replaced path", skips, err)
	}
}
