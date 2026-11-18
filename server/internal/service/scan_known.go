package service

import (
	"context"
	"errors"
	"os"
	"path"

	"qimeng.local/next/server/internal/domain"
)

func (signature scanSignature) sameSizeAndTime(info os.FileInfo) bool {
	return signature.RelativePath != "" && signature.Hash != "" && signature.Mtime != nil && signature.Size == info.Size() &&
		signature.Mtime.Equal(info.ModTime())
}

func sameScanDirectory(observed, current os.FileInfo) bool {
	return current.IsDir() && !syncPathLink(current) && os.SameFile(observed, current) &&
		observed.ModTime().Equal(current.ModTime())
}

// A child lookup on the pinned directory avoids resolving every ancestor for
// every known file. Only a fully verified page may contribute skip counts.
// Replaced directories fall back to current rooted paths, and changed files
// still go through normal hash, probe and publication checks.
func (s *Service) knownDirectoryEntries(ctx context.Context, run *scanRun, dir *os.File, relative string, entries []os.DirEntry, known map[string]scanSignature) ([]bool, error) {
	if !run.manualFull || run.library.Kind != "regular" || len(known) == 0 {
		return nil, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	full := path.Join("libraries", run.library.ID, relative)
	observed, err := dir.Stat()
	if err != nil {
		return nil, err
	}
	openCurrent := func() (*os.Root, error) {
		pinned, err := s.Root.OpenDirectoryRoot(full, syncPathLink)
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		info, err := pinned.Stat(".")
		if err != nil || !sameScanDirectory(observed, info) {
			pinned.Close()
			return nil, err
		}
		return pinned, nil
	}
	pinned, err := openCurrent()
	if err != nil || pinned == nil {
		return nil, err
	}
	defer pinned.Close()
	var unchanged []bool
	for index, entry := range entries {
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		child := path.Join(relative, entry.Name())
		prior, ok := known[child]
		if !ok || !entry.Type().IsRegular() || privateLibraryEntry(entry.Name()) ||
			!domain.ValidMediaPath(child) || !domain.ValidRelative(path.Join(full, entry.Name())) {
			continue
		}
		info, err := pinned.Lstat(entry.Name())
		if err != nil || !info.Mode().IsRegular() || syncPathLink(info) || !prior.sameSizeAndTime(info) {
			continue // The ordinary per-path lookup reports inaccessible or changed entries.
		}
		if unchanged == nil {
			unchanged = make([]bool, len(entries))
		}
		unchanged[index] = true
	}
	if unchanged == nil {
		return nil, nil
	}
	current, err := openCurrent()
	if err != nil || current == nil {
		return nil, err
	}
	return unchanged, current.Close()
}
