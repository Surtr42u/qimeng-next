package service

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"log/slog"
	"os"
	"regexp"
	"strings"
	"time"
)

var previewAssetDirectory = regexp.MustCompile(`^[0-9a-f]{32}$`)
var managedPreviewFile = regexp.MustCompile(`^v[1234]-[1-9][0-9]*-(256|512|1024)\.jpg$`)

const previewVersionColumns = `json_extract(a.data,'$.contentVersion'),
	EXISTS(SELECT 1 FROM selected_image_merge_members WHERE physical_asset_id=a.id)`
const previewCandidatesFrom = ` FROM assets a JOIN libraries l ON l.id=a.library_id
	 WHERE a.trashed=0 AND coalesce(json_extract(l.data,'$.enabled'),1)=1
	 AND coalesce(json_extract(a.data,'$.thumbnailUrl'),'')!=''`

func (s *Service) previewWorker() {
	defer s.workers.Done()
	tick := time.NewTicker(30 * time.Second)
	defer tick.Stop()
	fullDue := time.Now().Add(10 * time.Minute)
	for {
		select {
		case <-s.workCtx.Done():
			return
		case now := <-tick.C:
			force := !now.Before(fullDue)
			if force {
				fullDue = now.Add(10 * time.Minute)
			}
			full, scopes := s.previews.pending(force)
			if full {
				scopes = []previewScope{{}}
			}
			for _, scope := range scopes {
				err := s.warmPreviews(s.workCtx, scope)
				if err == nil {
					continue
				}
				if s.workCtx.Err() != nil {
					return
				}
				var problem *Error
				if errors.As(err, &problem) && problem.Status == 507 {
					s.previews.changed("", "")
					slog.Warn("thumbnail reconciliation paused at storage reserve")
					break
				}
				s.previews.changed(scope.library, scope.asset)
				slog.Warn("thumbnail reconciliation will retry")
			}
			if full {
				if err := s.prunePreviewDirectories(s.workCtx); err != nil && s.workCtx.Err() == nil {
					slog.Warn("old thumbnail cleanup deferred")
				}
			}
		}
	}
}

type previewIdentity struct {
	id       string
	version  int64
	selected bool
}

func (s *Service) warmPreviews(ctx context.Context, scope previewScope) error {
	query := "SELECT a.id," + previewVersionColumns + previewCandidatesFrom + " AND a.id>?"
	if scope.library != "" {
		query += " AND a.library_id=?"
	}
	if scope.asset != "" {
		query += " AND a.id=?"
	}
	query += " ORDER BY a.id LIMIT 256"
	last := ""
	for {
		args := []any{last}
		if scope.library != "" {
			args = append(args, scope.library)
		}
		if scope.asset != "" {
			args = append(args, scope.asset)
		}
		rows, err := s.DB.DB.QueryContext(ctx, query, args...)
		if err != nil {
			return err
		}
		page := make([]previewIdentity, 0, 256)
		for rows.Next() {
			var item previewIdentity
			if err = rows.Scan(&item.id, &item.version, &item.selected); err != nil {
				rows.Close()
				return err
			}
			page = append(page, item)
			last = item.id
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		if err = s.warmPreviewPage(ctx, page); err != nil {
			return err
		}
		if len(page) < 256 {
			break
		}
	}
	if scope.asset != "" {
		return s.prunePreviewAsset(ctx, scope.asset)
	}
	return nil
}

func (s *Service) warmPreviewPage(ctx context.Context, page []previewIdentity) error {
	// Give the default gallery size priority across the page. Once an original
	// is unavailable, skip its other sizes until the next reconciliation.
	for _, edge := range [...]int{512, 256, 1024} {
		available := page[:0]
		for _, asset := range page {
			if err := ctx.Err(); err != nil {
				return err
			}
			err := s.warmPreviewSize(ctx, asset, edge)
			if err == nil {
				available = append(available, asset)
				continue
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if errors.Is(err, os.ErrNotExist) || errors.Is(err, context.DeadlineExceeded) {
				continue
			}
			var problem *Error
			if errors.As(err, &problem) {
				switch problem.Status {
				case 404, 409, 415, 422, 501:
					continue
				}
			}
			return err
		}
		page = available
	}
	return nil
}

func (s *Service) warmPreviewSize(ctx context.Context, asset previewIdentity, edge int) error {
	info, err := s.Root.Lstat(previewPath(asset.id, asset.version, edge, asset.selected))
	if err == nil && info.Mode().IsRegular() {
		return nil
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	file, _, err := s.thumbnailFor(ctx, asset.id, asset.version, edge, true)
	if err != nil {
		return err
	}
	return file.Close()
}

func (s *Service) prunePreviewDirectories(ctx context.Context) error {
	dir, err := s.Root.Open("cache")
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer dir.Close()
	for {
		if err = ctx.Err(); err != nil {
			return err
		}
		entries, readErr := dir.ReadDir(128)
		for _, entry := range entries {
			if entry.IsDir() && previewAssetDirectory.MatchString(entry.Name()) {
				if err = s.prunePreviewAsset(ctx, entry.Name()); err != nil {
					return err
				}
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return nil
			}
			return readErr
		}
	}
}
func (s *Service) prunePreviewAsset(ctx context.Context, id string) error {
	if !previewAssetDirectory.MatchString(id) {
		return nil
	}
	dirname := "cache/" + id
	if err := s.safe(dirname); err != nil {
		return err
	}
	dir, err := s.Root.Open(dirname)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer dir.Close()
	for {
		entries, readErr := dir.ReadDir(64)
		if len(entries) > 0 {
			if err = s.removeOldPreviews(ctx, id, dirname, entries); err != nil {
				return err
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return nil
			}
			return readErr
		}
	}
}
func (s *Service) removeOldPreviews(ctx context.Context, id, dirname string, entries []os.DirEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var version int64
	var selected bool
	err := s.DB.DB.QueryRowContext(ctx, "SELECT "+previewVersionColumns+" FROM assets a WHERE a.id=? AND a.trashed=0", id).Scan(&version, &selected)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	prefix := previewPrefix(version, selected)
	for _, entry := range entries {
		name := entry.Name()
		if !entry.Type().IsRegular() || !managedPreviewFile.MatchString(name) || strings.HasPrefix(name, prefix) {
			continue
		}
		if err = s.Root.Remove(dirname + "/" + name); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}
