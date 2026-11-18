package service

import (
	"context"
	"database/sql"
	"errors"
	"time"

	legacy "qimeng.local/next/server/internal/migration"
	"qimeng.local/next/server/internal/model"
	"qimeng.local/next/server/internal/store"
)

type legacyLikeBaseline struct {
	SourceID, SourceKey, LastDay, ObservedDay string
	SourceCount, Unattributed                 int64
}

func likeSourceKey(media legacy.Media) string {
	if media.AssetID != "" {
		return "asset:" + media.AssetID
	}
	return "record:" + media.RecordKey
}
func legacyLikeDay(value legacy.Like) string {
	if value.LastLikeDate == nil {
		return ""
	}
	return *value.LastLikeDate
}
func decideLegacyLikes(ctx context.Context, q store.Querier, assetID, sourceID, sourceKey string, in legacy.Like) (string, string, error) {
	if in.LikeCount == 0 {
		return "keep", "", nil
	}
	day := legacyLikeDay(in)
	if in.LikeCount == 1 && day != "" {
		var exists int
		if e := q.QueryRowContext(ctx, "SELECT count(*) FROM likes WHERE asset_id=? AND day=?", assetID, day).Scan(&exists); e != nil {
			return "", "", e
		}
		if exists > 0 {
			return "keep", "", nil
		}
	}
	var baseline legacyLikeBaseline
	e := q.QueryRowContext(ctx, "SELECT source_id,source_key,source_count,unattributed_count,last_day,observed_day FROM like_baselines WHERE asset_id=?", assetID).Scan(&baseline.SourceID, &baseline.SourceKey, &baseline.SourceCount, &baseline.Unattributed, &baseline.LastDay, &baseline.ObservedDay)
	if e == nil {
		if baseline.SourceID == sourceID && baseline.SourceKey == sourceKey && baseline.SourceCount == in.LikeCount && baseline.LastDay == day {
			return "keep", "", nil
		}
		return "conflict", "legacy_like_overlap_unknown", nil
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return "", "", e
	}
	// A single dated vote has an exact daily identity and is safely deduplicated.
	if in.LikeCount == 1 && day != "" {
		return "apply", "", nil
	}
	var overlap int
	if day == "" {
		e = q.QueryRowContext(ctx, "SELECT count(*) FROM likes WHERE asset_id=?", assetID).Scan(&overlap)
	} else {
		e = q.QueryRowContext(ctx, "SELECT count(*) FROM likes WHERE asset_id=? AND day<?", assetID, day).Scan(&overlap)
	}
	if e != nil {
		return "", "", e
	}
	if overlap > 0 {
		return "conflict", "legacy_like_overlap_unknown", nil
	}
	var later int64
	if e = q.QueryRowContext(ctx, "SELECT count(*) FROM likes WHERE asset_id=? AND day>?", assetID, day).Scan(&later); e != nil {
		return "", "", e
	}
	if in.LikeCount > int64(1<<63-1)-later {
		return "", "", Fail(400, "invalid_like_count", "源累计与明确较晚的票数之和超出可表达范围。")
	}
	return "apply", "", nil
}
func importLegacyLikes(ctx context.Context, q store.Querier, metadata V2Import, media legacy.Media, in legacy.Like, asset model.Asset, action string) (model.Asset, bool, error) {
	actual, _, e := decideLegacyLikes(ctx, q, asset.ID, metadata.SourceID, likeSourceKey(media), in)
	if e != nil {
		return asset, false, e
	}
	if actual != action {
		return asset, false, Fail(409, "migration_like_state_changed", "点赞累计状态在计划后变化，请重新生成迁移计划。")
	}
	if actual != "apply" {
		return asset, false, nil
	}
	day := legacyLikeDay(in)
	unattributed := in.LikeCount
	if day != "" {
		if _, e = q.ExecContext(ctx, "INSERT OR IGNORE INTO likes(asset_id,day,created_at) VALUES(?,?,?)", asset.ID, day, dateKey(time.Now())); e != nil {
			return asset, false, e
		}
		unattributed--
	}
	if unattributed > 0 {
		if _, e = q.ExecContext(ctx, "INSERT INTO like_baselines(asset_id,source_id,source_key,source_count,unattributed_count,last_day,observed_day,import_id) VALUES(?,?,?,?,?,?,?,?)", asset.ID, metadata.SourceID, likeSourceKey(media), in.LikeCount, unattributed, day, time.Now().Format("2006-01-02"), metadata.ID); e != nil {
			return asset, false, e
		}
	}
	asset.Revision++
	asset.UpdatedAt = time.Now().UTC()
	if e = q.QueryRowContext(ctx, "SELECT (SELECT count(*) FROM likes WHERE asset_id=?)+COALESCE((SELECT unattributed_count FROM like_baselines WHERE asset_id=?),0)", asset.ID, asset.ID).Scan(&asset.LikeCount); e != nil {
		return asset, false, e
	}
	return asset, true, store.SaveAsset(ctx, q, asset)
}
