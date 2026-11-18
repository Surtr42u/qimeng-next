package service

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"io/fs"
	"os"
	"path"
	"runtime"
	"time"

	"qimeng.local/next/server/internal/domain"
)

const missingPathPage = 256

type indexedScanPath struct {
	rowID    int64
	assetID  string
	relative string
}

// Start with the root, then seek every other indexed parent, including ones
// no longer present in the walk. New uploads do not extend an in-flight scan.
func (s *Service) scanMissing(ctx context.Context, jobID, libraryID string) error {
	var through int64
	if err := s.DB.DB.QueryRowContext(ctx, "SELECT coalesce(max(rowid),0) FROM asset_locations").Scan(&through); err != nil || through == 0 {
		return err
	}
	parent := ""
	for {
		if err := s.reconcileScanDirectory(ctx, jobID, libraryID, parent, through); err != nil {
			return err
		}
		err := s.DB.DB.QueryRowContext(ctx, "SELECT "+assetParentDir+liveDirectoryRows+
			assetParentDir+">? AND rowid<=? ORDER BY "+assetParentDir+" LIMIT 1", libraryID, parent, through).Scan(&parent)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

func (s *Service) scanDirectoryPage(ctx context.Context, libraryID, parent string, after, through int64) ([]indexedScanPath, error) {
	rows, err := s.DB.DB.QueryContext(ctx, "SELECT rowid,asset_id,relative_path"+liveDirectoryRows+
		assetParentDir+"=? AND rowid>? AND rowid<=? ORDER BY rowid LIMIT ?", libraryID, parent, after, through, missingPathPage)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var page []indexedScanPath
	for rows.Next() {
		var item indexedScanPath
		if err = rows.Scan(&item.rowID, &item.assetID, &item.relative); err != nil {
			return nil, err
		}
		page = append(page, item)
	}
	return page, rows.Err()
}

// A directory listing proves only the regular names it actually contained.
// Other entries, unlisted names and changed directories require a rooted stat.
type scanDirectoryListing struct {
	stamp os.FileInfo
	names map[string]bool
}

func (s *Service) readScanDirectory(ctx context.Context, full string, budget int) (scanDirectoryListing, error) {
	var listing scanDirectoryListing
	f, err := s.Root.Open(full)
	if errors.Is(err, os.ErrNotExist) {
		return listing, nil
	}
	if err != nil {
		return listing, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return listing, err
	}
	if !info.IsDir() || syncPathLink(info) {
		return listing, fs.ErrInvalid
	}
	listing = scanDirectoryListing{stamp: info, names: make(map[string]bool)}
	var sample string
	for budget > 0 {
		if err = ctx.Err(); err != nil {
			return scanDirectoryListing{}, err
		}
		entries, readErr := f.ReadDir(min(budget, 128))
		for _, entry := range entries {
			if !entry.Type().IsRegular() {
				continue
			}
			if runtime.GOOS == "windows" {
				// Windows ReadDir supplies attributes, including reparse points.
				item, err := entry.Info()
				if errors.Is(err, os.ErrNotExist) {
					continue
				}
				if err != nil {
					return scanDirectoryListing{}, err
				}
				if syncPathLink(item) {
					continue
				}
			}
			listing.names[entry.Name()] = true
			if sample == "" && domain.ValidName(entry.Name()) {
				sample = entry.Name()
			}
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return scanDirectoryListing{}, readErr
		}
		budget -= len(entries)
	}
	// A directory may grant read permission (list names) but deny search
	// permission (resolve a child). Exercise a real child lookup before reuse.
	if sample != "" {
		missing, err := s.scanOriginalMissing(path.Join(full, sample))
		if err != nil || missing {
			return scanDirectoryListing{}, err
		}
	}
	return listing, nil
}

func (s *Service) scanOriginalMissing(full string) (bool, error) {
	info, err := s.Root.Lstat(full)
	if errors.Is(err, os.ErrNotExist) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	if syncPathLink(info) {
		return false, fs.ErrPermission
	}
	return false, nil
}

func (s *Service) reconcileScanDirectory(ctx context.Context, jobID, libraryID, parent string, through int64) error {
	if !domain.ValidMediaPath(parent) {
		return fs.ErrInvalid
	}
	full := path.Join("libraries", libraryID, parent)
	var listing scanDirectoryListing
	var after int64
	for {
		page, err := s.scanDirectoryPage(ctx, libraryID, parent, after, through)
		if err != nil || len(page) == 0 {
			return err
		}
		if after == 0 && len(page) >= 4 {
			// Bound both names retained and work in a directory full of unindexed entries.
			listing, err = s.readScanDirectory(ctx, full, min(4096, len(page)*16))
			if err != nil {
				return err
			}
		}
		if listing.stamp != nil {
			current, err := s.Root.Lstat(full)
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
			if err == nil && syncPathLink(current) {
				return fs.ErrPermission
			}
			if err != nil || !os.SameFile(listing.stamp, current) || !listing.stamp.ModTime().Equal(current.ModTime()) {
				listing = scanDirectoryListing{}
			}
		}
		var absent []indexedScanPath
		for _, item := range page {
			if err = ctx.Err(); err != nil {
				return err
			}
			if item.relative == "" || !domain.ValidMediaPath(item.relative) {
				return fs.ErrInvalid
			}
			name := path.Base(item.relative)
			if listing.names[name] && path.Join(parent, name) == item.relative {
				continue
			}
			missing, err := s.scanOriginalMissing(path.Join("libraries", libraryID, item.relative))
			if err != nil {
				return err
			}
			if missing {
				absent = append(absent, item)
			}
		}
		for len(absent) > 0 {
			n := min(len(absent), 32)
			if err = s.confirmMissingPaths(ctx, jobID, libraryID, absent[:n]); err != nil {
				return err
			}
			absent = absent[n:]
		}
		if len(page) < missingPathPage {
			return nil
		}
		after = page[len(page)-1].rowID
	}
}

func (s *Service) confirmMissingPaths(ctx context.Context, jobID, libraryID string, absent []indexedScanPath) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	j, err := s.Scan(ctx, jobID)
	if err != nil {
		return err
	}
	if j.State == "cancelled" {
		return context.Canceled
	}
	before := j.Missing
	for _, item := range absent {
		var current bool
		err = s.DB.DB.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM assets WHERE id=? AND library_id=? AND relative_path=? AND trashed=0)",
			item.assetID, libraryID, item.relative).Scan(&current)
		if err != nil {
			return err
		}
		if !current {
			continue
		}
		// A catalog move, trash or external recreation after enumeration is not a missing original.
		missing, err := s.scanOriginalMissing(path.Join("libraries", libraryID, item.relative))
		if err != nil {
			return err
		}
		if !missing {
			continue
		}
		j.Missing++
		j.Processed++
		scanIssue(&j, item.relative, "missing_original")
	}
	if j.Missing == before {
		return nil
	}
	j.UpdatedAt = time.Now().UTC()
	return saveScan(ctx, s.DB.DB, j)
}
