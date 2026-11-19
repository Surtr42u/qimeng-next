package service

import (
	"context"
	"database/sql"
	"errors"
	"os"
)

const devicePreviewCandidates = previewCandidatesFrom + " AND a.media_type!='animated_image'"

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
		if err = tx.QueryRowContext(ctx, "SELECT count(*)"+devicePreviewCandidates).Scan(&total); err != nil {
			return page, err
		}
		page.TotalMatched = &total
	}
	rows, err := tx.QueryContext(ctx, "SELECT a.id,json_extract(a.data,'$.contentVersion')"+devicePreviewCandidates+
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

func (s *Service) ThumbnailProgress(ctx context.Context) (ThumbnailProgress, error) {
	out := ThumbnailProgress{InstanceID: s.InstanceID}
	select {
	case s.previews.progress <- struct{}{}:
		defer func() { <-s.previews.progress }()
	default:
		return out, Fail(429, "thumbnail_progress_busy", "正在统计缩略图生成进度，请稍后刷新。")
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
	return out, rows.Err()
}
