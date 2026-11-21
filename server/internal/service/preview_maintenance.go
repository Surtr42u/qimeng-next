package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"
)

var previewAssetDirectory = regexp.MustCompile(`^[0-9a-f]{32}$`)

// Managed previews carry the preview policy prefix (previewPolicyVersion or
// its selected-image variant). Accept any positive policy generation so cleanup
// does not need editing on each bump, while still rejecting temp files and
// unrelated cache content.
var managedPreviewFile = regexp.MustCompile(`^v[1-9][0-9]*-[1-9][0-9]*-(256|512|1024)\.jpg$`)

const (
	// A page is warmed by a small worker pool. Actual decoding stays bounded by
	// previewWork.join, so this only hides SQL, stat and scheduling latency.
	previewWarmWorkers = 4
	// Keep aggregated failures bounded even when a whole page hits one problem.
	previewWarmFailureLimit = 8
)

// Preview eligibility is read from the versioned tables rather than the assets
// view: the view materialises the whole display document, so selecting its
// contentVersion or thumbnail marker rebuilt every per-asset JSON subquery for
// each scanned row. The columns below are the same facts the view exposes.
const previewVersionColumns = `v.version,
	EXISTS(SELECT 1 FROM selected_image_merge_members WHERE physical_asset_id=r.id)`
const previewCandidatesFrom = ` FROM asset_locations loc JOIN asset_records r ON r.id=loc.asset_id
	 JOIN asset_content_versions v ON v.asset_id=loc.asset_id AND v.version=loc.current_version
	 JOIN libraries lb ON lb.id=loc.library_id
	 WHERE loc.trashed=0 AND coalesce(json_extract(lb.data,'$.enabled'),1)=1
	 AND coalesce(json_extract(r.data,'$.thumbnailUrl'),'')!=''`

func previewWarmEdges() []int {
	if runtime.GOOS == "android" {
		// The phone client only reads the 512 grid size. Desktop and NAS keep
		// all three representations for the web and large previews.
		return []int{512}
	}
	return []int{512, 256, 1024}
}

func (s *Service) previewWorker() {
	defer s.workers.Done()
	tick := time.NewTicker(30 * time.Second)
	defer tick.Stop()
	fullDue := time.Now().Add(10 * time.Minute)
	var waitingFor int64
	for {
		select {
		case <-s.workCtx.Done():
			return
		case now := <-tick.C:
			force := !now.Before(fullDue)
			if force {
				fullDue = now.Add(10 * time.Minute)
			}
			waitingFor = s.runPreviewCycle(s.workCtx, force, waitingFor)
		}
	}
}

func (s *Service) runPreviewCycle(ctx context.Context, force bool, waitingFor int64) int64 {
	if waitingFor > 0 && s.checkPreviewSpace(waitingFor) != nil {
		if force {
			s.cleanupPreviews(ctx)
		}
		return waitingFor
	}
	full, scopes := s.previews.pending(force)
	if full {
		scopes = []previewScope{{}}
	}
	waitingFor = 0
	for i, scope := range scopes {
		err := s.warmPreviews(ctx, scope)
		if err == nil {
			continue
		}
		if ctx.Err() != nil {
			return waitingFor
		}
		var space *previewSpaceError
		if errors.As(err, &space) {
			for _, remaining := range scopes[i:] {
				s.previews.changed(remaining.library, remaining.asset)
			}
			waitingFor = space.required
			slog.Warn("thumbnail reconciliation paused at storage reserve")
			break
		}
		s.previews.changed(scope.library, scope.asset)
		slog.Warn("thumbnail reconciliation will retry")
	}
	if full {
		s.cleanupPreviews(ctx)
	}
	return waitingFor
}

func (s *Service) cleanupPreviews(ctx context.Context) {
	if err := s.prunePreviewDirectories(ctx); err != nil && ctx.Err() == nil {
		slog.Warn("old thumbnail cleanup deferred")
	}
}

type previewIdentity struct {
	id       string
	version  int64
	selected bool
}

func (s *Service) warmPreviews(ctx context.Context, scope previewScope) error {
	query := "SELECT r.id," + previewVersionColumns + previewCandidatesFrom + " AND loc.asset_id>?"
	if scope.library != "" {
		query += " AND loc.library_id=?"
	}
	if scope.asset != "" {
		query += " AND loc.asset_id=?"
	}
	query += " ORDER BY loc.asset_id LIMIT 256"
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
	// Workers are bounded but a bad asset only removes itself: failures are
	// aggregated so healthy assets on the page still warm every size.
	var failures []error
	failed := 0
	for _, edge := range previewWarmEdges() {
		if err := ctx.Err(); err != nil {
			return err
		}
		results := make([]error, len(page))
		jobs := make(chan int)
		var group sync.WaitGroup
		for range min(previewWarmWorkers, len(page)) {
			group.Add(1)
			go func() {
				defer group.Done()
				for i := range jobs {
					results[i] = s.warmPreviewSize(ctx, page[i], edge)
				}
			}()
		}
	feed:
		for i := range page {
			select {
			case jobs <- i:
			case <-ctx.Done():
				break feed
			}
		}
		close(jobs)
		group.Wait()
		if err := ctx.Err(); err != nil {
			return err
		}
		available := page[:0]
		for i, asset := range page {
			err := results[i]
			if err == nil {
				available = append(available, asset)
				continue
			}
			if previewWarmSkippable(err) {
				continue
			}
			failed++
			if len(failures) < previewWarmFailureLimit {
				failures = append(failures, fmt.Errorf("asset %s edge %d: %w", asset.id, edge, err))
			}
		}
		page = available
		if len(page) == 0 {
			break
		}
	}
	if failed > len(failures) {
		failures = append(failures, fmt.Errorf("%d more preview failures", failed-len(failures)))
	}
	return errors.Join(failures...)
}

// previewWarmSkippable reports per-asset conditions: the original is gone,
// the decode cannot fit the reservation or the asset rejects previews. These
// only skip that asset/edge; the rest of the page continues.
func previewWarmSkippable(err error) bool {
	if errors.Is(err, os.ErrNotExist) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var problem *Error
	if errors.As(err, &problem) {
		switch problem.Status {
		case 404, 409, 415, 422, 501:
			return true
		}
	}
	return false
}

func (s *Service) warmPreviewSize(ctx context.Context, asset previewIdentity, edge int) error {
	info, err := s.Root.Lstat(previewPath(asset.id, asset.version, edge, asset.selected))
	if err == nil && info.Mode().IsRegular() && info.Size() > 0 {
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
	err := s.DB.DB.QueryRowContext(ctx, "SELECT "+previewVersionColumns+` FROM asset_locations loc
	 JOIN asset_records r ON r.id=loc.asset_id
	 JOIN asset_content_versions v ON v.asset_id=loc.asset_id AND v.version=loc.current_version
	 WHERE loc.asset_id=? AND loc.trashed=0`, id).Scan(&version, &selected)
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

// discardDeletedAssetPreviews immediately drops every generated preview for an
// asset whose catalog row is gone. Purge and library removal run in post-commit
// callbacks while s.mu is held, so this path never queries the catalog and
// never takes the write lock. Failures stay advisory: prunePreviewDirectories
// retries the same directory on the next reconciliation pass.
func (s *Service) discardDeletedAssetPreviews(id string) {
	if err := s.removePreviewAssetCache(id); err != nil {
		slog.Warn("deleted asset preview cleanup deferred", "assetId", id, "error", err)
	}
}

// removePreviewAssetCache deletes managed preview files under cache/<assetId>.
// Unmanaged files, temp files and the directory itself are left to the periodic
// sweep when they cannot be removed yet.
func (s *Service) removePreviewAssetCache(id string) error {
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
	var removeErr error
	for {
		entries, readErr := dir.ReadDir(64)
		for _, entry := range entries {
			name := entry.Name()
			if !entry.Type().IsRegular() || !managedPreviewFile.MatchString(name) {
				continue
			}
			if err := s.Root.Remove(dirname + "/" + name); err != nil && !errors.Is(err, os.ErrNotExist) && removeErr == nil {
				removeErr = err
			}
		}
		if readErr != nil {
			if !errors.Is(readErr, io.EOF) && removeErr == nil {
				removeErr = readErr
			}
			break
		}
	}
	if err := dir.Close(); err != nil && removeErr == nil {
		removeErr = err
	}
	// Succeeds only when every file was managed; otherwise the directory stays
	// for the sweep that also knows how to handle leftover files.
	_ = s.Root.Remove(dirname)
	return removeErr
}
