package service

import (
	"context"
	"errors"
	"io"
	"os"
	"path"
	"slices"

	"qimeng.local/next/server/internal/domain"
	"qimeng.local/next/server/internal/model"
	"qimeng.local/next/server/internal/store"
)

type Directory struct {
	RelativePath string `json:"relativePath"`
	Name         string `json:"name"`
	FileCount    int64  `json:"fileCount"`
	HasChildren  *bool  `json:"hasChildren,omitempty"`
}
type DirectoryPage struct {
	model.Page[Directory]
	DirectFileCount int64 `json:"directFileCount"`
}

const assetParentDir = "substr(relative_path,1,max(0,length(relative_path)-length(file_name)-1))"
const liveDirectoryRows = " FROM asset_locations INDEXED BY assets_live_directory WHERE library_id=? AND trashed=0 AND "

func (s *Service) Directories(ctx context.Context, libraryID, parent, cursor string, limit int) (DirectoryPage, error) {
	page := DirectoryPage{Page: model.Page[Directory]{Items: []Directory{}}}
	if _, e := s.library(ctx, libraryID); e != nil {
		return page, e
	}
	if !domain.ValidMediaPath(parent) {
		return page, Fail(400, "invalid_path", "目录路径无效。")
	}
	n, e := limited(limit, 50)
	if e != nil {
		return page, e
	}
	key := "directories:" + libraryID + ":" + parent
	seek, e := readSeek(cursor, key, "name")
	if e != nil {
		return page, e
	}
	full := path.Join("libraries", libraryID, parent)
	if e = s.safe(full); e != nil {
		return page, e
	}
	f, e := s.Root.Open(full)
	if errors.Is(e, os.ErrNotExist) {
		return page, Fail(404, "directory_missing", "目录已移除。")
	}
	if e != nil {
		return page, e
	}
	defer f.Close()
	// ReadDir is filesystem order. Keep only the next page in lexical order so
	// a later insertion before the cursor cannot duplicate or skip older rows.
	names := []string{}
	for {
		if e = ctx.Err(); e != nil {
			return page, e
		}
		entries, readErr := f.ReadDir(128)
		for _, entry := range entries {
			name := entry.Name()
			if name <= seek.Value || entry.Type()&os.ModeSymlink != 0 || !entry.IsDir() || !domain.ValidMediaPath(path.Join(parent, name)) {
				continue
			}
			names = append(names, name)
		}
		slices.Sort(names)
		if len(names) > n+1 {
			names = names[:n+1]
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return page, readErr
		}
	}
	if len(names) > n {
		names = names[:n]
		page.NextCursor = makeSeek(key, "name", names[n-1], names[n-1])
	}
	paths := []string{parent}
	for _, name := range names {
		paths = append(paths, path.Join(parent, name))
	}
	raw, e := store.Encode(paths)
	if e != nil {
		return page, e
	}
	rows, e := s.DB.DB.QueryContext(ctx, "SELECT "+assetParentDir+",count(*)"+liveDirectoryRows+assetParentDir+" IN (SELECT value FROM json_each(?)) GROUP BY "+assetParentDir, libraryID, raw)
	if e != nil {
		return page, e
	}
	defer rows.Close()
	counts := map[string]int64{}
	for rows.Next() {
		var dir string
		var count int64
		if e = rows.Scan(&dir, &count); e != nil {
			return page, e
		}
		counts[dir] = count
	}
	if e = rows.Err(); e != nil {
		return page, e
	}
	budget := 2048
	page.DirectFileCount = counts[parent]
	for _, dir := range paths[1:] {
		children, e := s.inspectSubdirectories(ctx, libraryID, dir, &budget)
		if e != nil {
			return page, e
		}
		page.Items = append(page.Items, Directory{RelativePath: dir, Name: path.Base(dir), FileCount: counts[dir], HasChildren: children})
	}
	return page, nil
}

func (s *Service) inspectSubdirectories(ctx context.Context, libraryID, relative string, budget *int) (*bool, error) {
	if *budget == 0 {
		return nil, nil
	}
	folder, e := s.Root.Open(path.Join("libraries", libraryID, relative))
	if errors.Is(e, os.ErrNotExist) || errors.Is(e, os.ErrPermission) {
		return nil, nil
	}
	if e != nil {
		return nil, e
	}
	defer folder.Close()
	for *budget > 0 {
		if e = ctx.Err(); e != nil {
			return nil, e
		}
		entries, readErr := folder.ReadDir(min(*budget, 64))
		*budget -= len(entries)
		for _, entry := range entries {
			if entry.IsDir() && entry.Type()&os.ModeSymlink == 0 && domain.ValidMediaPath(path.Join(relative, entry.Name())) {
				found := true
				return &found, nil
			}
		}
		if errors.Is(readErr, io.EOF) {
			found := false
			return &found, nil
		}
		if errors.Is(readErr, os.ErrPermission) || errors.Is(readErr, os.ErrNotExist) {
			return nil, nil
		}
		if readErr != nil {
			return nil, readErr
		}
	}
	return nil, nil
}
