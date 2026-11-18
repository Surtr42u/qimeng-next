package service

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"log/slog"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var previewAssetDirectory = regexp.MustCompile(`^[0-9a-f]{32}$`)
var managedPreviewFile = regexp.MustCompile(`^v[123]-[1-9][0-9]*-(256|512|1024)\.jpg$`)

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
				if err := s.warmPreviews(s.workCtx, scope); err != nil && s.workCtx.Err() == nil {
					s.previews.changed(scope.library, scope.asset)
					slog.Warn("thumbnail reconciliation will retry")
				}
			}
			if full {
				if err := s.prunePreviewDirectories(s.workCtx); err != nil && s.workCtx.Err() == nil {
					slog.Warn("old thumbnail cleanup deferred")
				}
			}
		}
	}
}

// Select keyset pages of inexpensive cache identities. Reading and decoding
// originals is deferred until a current medium preview is actually missing.
func (s *Service) warmPreviews(ctx context.Context, scope previewScope) error {
	query := `SELECT a.id,json_extract(a.data,'$.contentVersion') FROM assets a JOIN libraries l ON l.id=a.library_id
	 WHERE a.id>? AND a.trashed=0 AND coalesce(json_extract(l.data,'$.enabled'),1)=1
	 AND coalesce(json_extract(a.data,'$.thumbnailUrl'),'')!=''`
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
		type identity struct {
			id      string
			version int64
		}
		page := make([]identity, 0, 256)
		for rows.Next() {
			var item identity
			if err = rows.Scan(&item.id, &item.version); err != nil {
				rows.Close()
				return err
			}
			page = append(page, item)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		for _, a := range page {
			if err = ctx.Err(); err != nil {
				return err
			}
			last = a.id
			cache := previewPath(a.id, a.version, 512)
			info, err := s.Root.Lstat(cache)
			if err == nil && info.Mode().IsRegular() {
				continue
			}
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
			file, _, err := s.thumbnailFor(ctx, a.id, a.version, 512, true)
			if file != nil {
				closeErr := file.Close()
				if err == nil {
					err = closeErr
				}
			}
			if err != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				// A changed, offline or unsupported original must not starve
				// independent works. Reconcile it again on its next change.
				var problem *Error
				if errors.Is(err, os.ErrNotExist) || errors.As(err, &problem) && (problem.Status == 404 || problem.Status == 409 || problem.Status == 415 || problem.Status == 422 || problem.Status == 501) {
					continue
				}
				return err
			}
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
	err := s.DB.DB.QueryRowContext(ctx, "SELECT json_extract(data,'$.contentVersion') FROM assets WHERE id=? AND trashed=0", id).Scan(&version)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	prefix := "v3-" + strconv.FormatInt(version, 10) + "-"
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
