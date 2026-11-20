package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"
)

type ThumbnailCandidate struct {
	AssetID        string `json:"assetId"`
	ContentVersion int64  `json:"contentVersion"`
}

type ThumbnailCandidatePage struct {
	InstanceID   string               `json:"instanceId"`
	Items        []ThumbnailCandidate `json:"items"`
	NextCursor   string               `json:"nextCursor,omitempty"`
	TotalMatched *int64               `json:"totalMatched,omitempty"`
}

func (s *Service) ThumbnailCandidates(ctx context.Context, cursor string, limit int) (ThumbnailCandidatePage, error) {
	const filter, order = "thumbnail-candidates", "id:asc"
	page := ThumbnailCandidatePage{InstanceID: s.InstanceID, Items: []ThumbnailCandidate{}}
	n, err := limited(limit, 50)
	if err != nil {
		return page, err
	}
	after, err := readSeek(cursor, filter, order)
	if err != nil {
		return page, err
	}
	tx, err := s.DB.DB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return page, err
	}
	defer tx.Rollback()
	if cursor == "" {
		var total int64
		if err = tx.QueryRowContext(ctx, "SELECT count(*)"+previewCandidatesFrom).Scan(&total); err != nil {
			return page, err
		}
		page.TotalMatched = &total
	}
	rows, err := tx.QueryContext(ctx, "SELECT a.id,json_extract(a.data,'$.contentVersion')"+previewCandidatesFrom+
		" AND a.id>? ORDER BY a.id LIMIT ?", after.ID, n+1)
	if err != nil {
		return page, err
	}
	defer rows.Close()
	for rows.Next() {
		var item ThumbnailCandidate
		if err = rows.Scan(&item.AssetID, &item.ContentVersion); err != nil {
			return page, err
		}
		page.Items = append(page.Items, item)
	}
	if err = rows.Err(); err != nil {
		return page, err
	}
	if len(page.Items) > n {
		page.Items = page.Items[:n]
		page.NextCursor = makeSeek(filter, order, "", page.Items[n-1].AssetID)
	}
	return page, nil
}

type ThumbnailProgress struct {
	InstanceID   string `json:"instanceId"`
	TotalAssets  int64  `json:"totalAssets"`
	ThumbsOnDisk int64  `json:"thumbsOnDisk"`
}

// thumbnailProgressTTL keeps a poll burst from walking every candidate asset.
// Generation, trash and library changes invalidate the entry immediately; a
// stale snapshot is also returned while a cold scan is already running.
const thumbnailProgressTTL = 30 * time.Second

type thumbnailProgressSnapshot struct {
	value ThumbnailProgress
	at    time.Time
}

type thumbnailProgressSnapshots struct {
	sync.Mutex
	entries map[string]thumbnailProgressSnapshot
}

var thumbnailProgressCache = thumbnailProgressSnapshots{entries: map[string]thumbnailProgressSnapshot{}}

func (s *Service) thumbnailProgressKey() string {
	if s.Config.DataDir == "" && s.InstanceID == "" {
		return fmt.Sprintf("%p", s)
	}
	return s.Config.DataDir + "\x00" + s.InstanceID
}

// cachedThumbnailProgress returns a stored snapshot. When fresh is false an
// expired snapshot is still returned so concurrent polls do not repeat the
// catalog walk; callers that need a new scan check freshness separately.
func (s *Service) cachedThumbnailProgress(fresh bool) (ThumbnailProgress, bool) {
	key := s.thumbnailProgressKey()
	thumbnailProgressCache.Lock()
	defer thumbnailProgressCache.Unlock()
	snapshot, found := thumbnailProgressCache.entries[key]
	if !found || fresh && time.Since(snapshot.at) >= thumbnailProgressTTL {
		return ThumbnailProgress{}, false
	}
	return snapshot.value, true
}

func (s *Service) rememberThumbnailProgress(value ThumbnailProgress) {
	thumbnailProgressCache.Lock()
	thumbnailProgressCache.entries[s.thumbnailProgressKey()] = thumbnailProgressSnapshot{value: value, at: time.Now()}
	thumbnailProgressCache.Unlock()
}

// invalidateThumbnailProgress makes the next poll rescan after a committed
// asset or library change. It is safe to call while s.mu is held.
func (s *Service) invalidateThumbnailProgress() {
	thumbnailProgressCache.Lock()
	delete(thumbnailProgressCache.entries, s.thumbnailProgressKey())
	thumbnailProgressCache.Unlock()
}

// InvalidateThumbnailProgressForTest drops the cached progress snapshot so the
// next poll recounts. Tests that generate, truncate or remove preview files
// outside the service use it; production keeps the 30-second TTL and only
// invalidates on committed asset or library changes.
func (s *Service) InvalidateThumbnailProgressForTest() {
	s.invalidateThumbnailProgress()
}

func (s *Service) ThumbnailProgress(ctx context.Context) (ThumbnailProgress, error) {
	if value, ok := s.cachedThumbnailProgress(true); ok {
		return value, nil
	}
	out := ThumbnailProgress{InstanceID: s.InstanceID}
	select {
	case s.previews.progress <- struct{}{}:
		defer func() { <-s.previews.progress }()
	case <-ctx.Done():
		return out, ctx.Err()
	default:
		// Another request is already scanning: reuse its last result rather
		// than walking the same catalog again.
		if value, ok := s.cachedThumbnailProgress(false); ok {
			return value, nil
		}
		return out, Fail(429, "thumbnail_progress_busy", "正在统计缩略图生成进度，请稍后刷新。")
	}
	if value, ok := s.cachedThumbnailProgress(true); ok {
		return value, nil
	}
	rows, err := s.DB.DB.QueryContext(ctx, "SELECT a.id,"+previewVersionColumns+previewCandidatesFrom)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var item previewIdentity
		if err = rows.Scan(&item.id, &item.version, &item.selected); err != nil {
			return out, err
		}
		out.TotalAssets++
		info, err := s.Root.Lstat(previewPath(item.id, item.version, 512, item.selected))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return out, err
		}
		if info.Mode().IsRegular() && info.Size() > 0 {
			out.ThumbsOnDisk++
		}
	}
	if err = rows.Err(); err != nil {
		return out, err
	}
	s.rememberThumbnailProgress(out)
	return out, nil
}
