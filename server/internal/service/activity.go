package service

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"time"

	"qimeng.local/next/server/internal/model"
	"qimeng.local/next/server/internal/store"
)

// RecordEvent keeps append-only events and their daily projection in one transaction.
// Retries reuse eventId; repeated view/play in one browsing session do not count twice.
func (s *Service) RecordEvent(ctx context.Context, w *Write, id string, in model.Event) (model.Event, error) {
	if e := Authorize(w.Principal, "admin", ""); e != nil {
		return in, e
	}
	if len(in.EventID) < 8 || len(in.EventID) > 128 || in.PositionMs < 0 || math.IsNaN(in.Seconds) || math.IsInf(in.Seconds, 0) || in.Seconds < 0 || in.Seconds > 86400 {
		return in, Fail(400, "invalid_event", "浏览或播放事件无效。")
	}
	switch in.Kind {
	case "view", "play", "progress", "dwell":
	default:
		return in, Fail(400, "invalid_event", "事件类别无效。")
	}
	if in.SessionID == "" {
		in.SessionID = in.EventID
	}
	if len(in.SessionID) > 128 {
		return in, Fail(400, "invalid_event", "会话身份过长。")
	}
	in.Seconds = math.Trunc(in.Seconds)
	if in.Kind == "dwell" && in.Seconds < 1 {
		return in, Fail(400, "invalid_event", "不足一秒的停留不记录。")
	}
	old, e := store.Get[model.Event](ctx, s.DB.DB, "SELECT data FROM events WHERE event_id=?", in.EventID)
	if e == nil {
		if old.AssetID != id || old.Kind != in.Kind || old.ContentVersion != in.ContentVersion || old.PositionMs != in.PositionMs || old.Seconds != in.Seconds || old.SessionID != in.SessionID {
			return in, Fail(409, "event_conflict", "事件身份已被其他内容使用。")
		}
		return old, nil
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return in, e
	}
	a, e := s.Asset(ctx, id)
	if e != nil {
		return in, e
	}
	if a.Trashed {
		return in, Fail(409, "asset_trashed", "媒体已移入回收站。")
	}
	if a.ContentVersion != in.ContentVersion {
		return in, Fail(409, "content_changed", "内容已替换，旧记录不能覆盖新版进度。")
	}
	now := time.Now().UTC()
	occurred := now
	if in.OccurredAt != nil {
		occurred = in.OccurredAt.UTC()
		if occurred.After(now.Add(5 * time.Minute)) {
			return in, Fail(400, "event_time_invalid", "事件时间超前。")
		}
	}
	day := occurred.In(time.Local).Format("2006-01-02")
	if in.Kind == "view" || in.Kind == "play" {
		event, e := store.Get[model.Event](ctx, s.DB.DB, `SELECT e.data FROM event_sessions d JOIN events e ON e.event_id=d.event_id WHERE d.asset_id=? AND d.session_id=? AND d.kind=? AND d.day=?`, id, in.SessionID, in.Kind, day)
		if e == nil {
			return event, nil
		}
		if !errors.Is(e, sql.ErrNoRows) {
			return in, e
		}
	}
	in.AssetID = id
	in.FileName = a.FileName
	in.CreatedAt = occurred
	raw, e := store.Encode(in)
	if e != nil {
		return in, e
	}
	q, e := w.Q()
	if e != nil {
		return in, e
	}
	if _, e = q.ExecContext(ctx, "INSERT INTO events(event_id,asset_id,created_at,data) VALUES(?,?,?,?)", in.EventID, id, dateKey(occurred), raw); e != nil {
		return in, e
	}
	views, plays, seconds := int64(0), int64(0), int64(0)
	switch in.Kind {
	case "view":
		views = 1
	case "play":
		plays = 1
	case "dwell":
		seconds = int64(in.Seconds)
	}
	if views+plays > 0 {
		if _, e = q.ExecContext(ctx, "INSERT INTO event_sessions VALUES(?,?,?,?,?)", id, in.SessionID, in.Kind, day, in.EventID); e != nil {
			return in, e
		}
	}
	if views+plays+seconds > 0 {
		if _, e = q.ExecContext(ctx, `INSERT INTO asset_daily_stats(asset_id,day,views,plays,seconds) VALUES(?,?,?,?,?) ON CONFLICT(asset_id,day) DO UPDATE SET views=views+excluded.views,plays=plays+excluded.plays,seconds=seconds+excluded.seconds`, id, day, views, plays, seconds); e != nil {
			return in, e
		}
	}
	if views+seconds > 0 {
		if _, e = q.ExecContext(ctx, `INSERT INTO asset_hourly_stats(asset_id,day,hour,views,seconds) VALUES(?,?,?,?,?) ON CONFLICT(asset_id,day,hour) DO UPDATE SET views=views+excluded.views,seconds=seconds+excluded.seconds`, id, day, occurred.In(time.Local).Hour(), views, seconds); e != nil {
			return in, e
		}
	}
	if in.Kind == "progress" {
		_, e = q.ExecContext(ctx, `INSERT INTO asset_playback_positions(asset_id,content_version,position_ms,occurred_at)
			VALUES(?,?,?,?) ON CONFLICT(asset_id,content_version) DO UPDATE SET
			position_ms=excluded.position_ms,occurred_at=excluded.occurred_at
			WHERE excluded.occurred_at>=asset_playback_positions.occurred_at`, id, in.ContentVersion, in.PositionMs, dateKey(occurred))
		if e != nil {
			return in, e
		}
	}
	return in, nil
}

func (s *Service) History(ctx context.Context, cursor string, limit int) (model.Page[model.Event], error) {
	return s.FilteredHistory(ctx, AssetFilter{Cursor: cursor, Limit: limit})
}

func (s *Service) Stats(ctx context.Context) (map[string]int64, error) {
	// One scan with conditional aggregates replaces the five UNION ALL scans of
	// the assets view; authors, events and trash were never asset rows.
	var assets, images, videos, bytes, favorites, authors, events, trash int64
	e := s.DB.DB.QueryRowContext(ctx, `SELECT
 coalesce(sum(CASE WHEN trashed=0 THEN 1 ELSE 0 END),0),
 coalesce(sum(CASE WHEN trashed=0 AND media_type!='video' THEN 1 ELSE 0 END),0),
 coalesce(sum(CASE WHEN trashed=0 AND media_type='video' THEN 1 ELSE 0 END),0),
 coalesce(sum(CASE WHEN trashed=0 THEN size ELSE 0 END),0),
 coalesce(sum(CASE WHEN trashed=0 AND favorite=1 THEN 1 ELSE 0 END),0),
 (SELECT count(*) FROM authors WHERE id!='unknown'),
 (SELECT count(*) FROM events),
 (`+visibleTrashCount+`) FROM assets`).Scan(&assets, &images, &videos, &bytes, &favorites, &authors, &events, &trash)
	if e != nil {
		return nil, e
	}
	return map[string]int64{"assets": assets, "images": images, "videos": videos, "bytes": bytes,
		"favorites": favorites, "authors": authors, "events": events, "trash": trash}, nil
}
