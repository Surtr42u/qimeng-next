package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	legacy "qimeng.local/next/server/internal/migration"
	"qimeng.local/next/server/internal/model"
	"qimeng.local/next/server/internal/store"
)

// Legacy event identities are already authoritative. Session deduplication is
// only for new reports; applying it to imported history would erase real rows.
func importV2Event(ctx context.Context, q store.Querier, sourceID string, input legacy.Event, a model.Asset) (bool, error) {
	occurred, e := time.Parse(time.RFC3339Nano, input.StartedAt)
	if e != nil {
		return false, e
	}
	kind := input.Kind
	if kind == "open" {
		kind = "view"
	}
	seconds := float64(0)
	if input.Seconds != nil {
		seconds = float64(*input.Seconds)
	}
	old, e := store.Get[model.Event](ctx, q, "SELECT data FROM events WHERE event_id=?", input.EventID)
	if e == nil {
		if old.AssetID != a.ID || old.Kind != kind || old.SessionID != input.SessionID || !old.CreatedAt.Equal(occurred) || old.Seconds != seconds {
			return false, Fail(409, "event_identity_conflict", "迁入事件身份与已存内容不一致；没有覆盖或重建事件。")
		}
		original, e := store.Get[legacy.Event](ctx, q, "SELECT original FROM migration_event_origins WHERE event_id=?", input.EventID)
		if e == nil {
			if (original.Seconds == nil) != (input.Seconds == nil) || original.Day != input.Day {
				return false, Fail(409, "event_identity_conflict", "相同事件身份的原始秒数或日键发生变化。")
			}
		} else if !errors.Is(e, sql.ErrNoRows) {
			return false, e
		} else {
			raw, e := json.Marshal(input)
			if e != nil {
				return false, e
			}
			if _, e = q.ExecContext(ctx, "INSERT INTO migration_event_origins(event_id,source_id,original) VALUES(?,?,?)", input.EventID, sourceID, string(raw)); e != nil {
				return false, e
			}
		}
		return false, nil
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return false, e
	}
	original, e := json.Marshal(input)
	if e != nil {
		return false, e
	}
	event := model.Event{EventID: input.EventID, AssetID: a.ID, FileName: a.FileName, Kind: kind, SessionID: input.SessionID, Seconds: seconds, OccurredAt: &occurred, ContentVersion: 0, CreatedAt: occurred.UTC()}
	raw, e := json.Marshal(struct {
		model.Event
		LegacyEvent json.RawMessage `json:"legacyEvent"`
		SourceID    string          `json:"migrationSourceId"`
	}{event, original, sourceID})
	if e != nil {
		return false, e
	}
	if _, e = q.ExecContext(ctx, "INSERT INTO events(event_id,asset_id,created_at,data) VALUES(?,?,?,?)", event.EventID, event.AssetID, dateKey(event.CreatedAt), string(raw)); e != nil {
		return false, e
	}
	if _, e = q.ExecContext(ctx, "INSERT INTO migration_event_origins(event_id,source_id,original) VALUES(?,?,?)", event.EventID, sourceID, string(original)); e != nil {
		return false, e
	}
	day := input.Day
	if day == "" {
		day = occurred.In(time.Local).Format("2006-01-02")
	}
	views, plays, dwell := int64(0), int64(0), int64(0)
	switch kind {
	case "view":
		views = 1
	case "play":
		plays = 1
	case "dwell":
		if input.Seconds != nil {
			dwell = *input.Seconds
		}
	}
	if (views+plays) > 0 && input.Day != "" {
		if _, e = q.ExecContext(ctx, "INSERT OR IGNORE INTO event_sessions(asset_id,session_id,kind,day,event_id) VALUES(?,?,?,?,?)", a.ID, input.SessionID, kind, input.Day, input.EventID); e != nil {
			return false, e
		}
	}
	if views+plays+dwell > 0 {
		if _, e = q.ExecContext(ctx, `INSERT INTO asset_daily_stats(asset_id,day,views,plays,seconds) VALUES(?,?,?,?,?) ON CONFLICT(asset_id,day) DO UPDATE SET views=views+excluded.views,plays=plays+excluded.plays,seconds=seconds+excluded.seconds`, a.ID, day, views, plays, dwell); e != nil {
			return false, e
		}
	}
	if views+dwell > 0 {
		if _, e = q.ExecContext(ctx, `INSERT INTO asset_hourly_stats(asset_id,day,hour,views,seconds) VALUES(?,?,?,?,?) ON CONFLICT(asset_id,day,hour) DO UPDATE SET views=views+excluded.views,seconds=seconds+excluded.seconds`, a.ID, day, occurred.In(time.Local).Hour(), views, dwell); e != nil {
			return false, e
		}
	}
	return true, nil
}

func preserveV2Relation(ctx context.Context, q store.Querier, inputID, kind, identity string, original any) error {
	raw, e := json.Marshal(original)
	if e != nil {
		return e
	}
	_, e = q.ExecContext(ctx, "INSERT OR IGNORE INTO migration_relation_origins(input_id,kind,identity,original) VALUES(?,?,?,?)", inputID, kind, identity, string(raw))
	return e
}

func importV2Mark(ctx context.Context, q store.Querier, sourceID string, input legacy.Timeline, a model.Asset) (bool, error) {
	if a.MediaType != "video" {
		return false, nil
	}
	var exists int
	if e := q.QueryRowContext(ctx, "SELECT COUNT(*) FROM timeline_marks WHERE asset_id=? AND position_ms=? AND json_extract(data,'$.label')=? AND COALESCE(json_extract(data,'$.color'),'')=? AND json_extract(data,'$.contentVersion')=0", a.ID, input.TimeMillis, input.Name, input.Color).Scan(&exists); e != nil {
		return false, e
	}
	if exists > 0 {
		return false, nil
	}
	created := time.Now().UTC()
	if input.CreatedAtMillis > 0 {
		created = time.UnixMilli(input.CreatedAtMillis).UTC()
	}
	identity, _ := json.Marshal(struct {
		Source, Asset string
		Position      int64
		Name, Color   string
	}{sourceID, a.ID, input.TimeMillis, input.Name, input.Color})
	mark := TimelineMark{ID: digest(identity)[:32], AssetID: a.ID, PositionMs: input.TimeMillis, Label: input.Name, Color: input.Color, ContentVersion: 0, Revision: 1, CreatedAt: created, UpdatedAt: created}
	raw, e := store.Encode(mark)
	if e != nil {
		return false, e
	}
	r, e := q.ExecContext(ctx, "INSERT OR IGNORE INTO timeline_marks(id,asset_id,position_ms,data) VALUES(?,?,?,?)", mark.ID, a.ID, mark.PositionMs, raw)
	if e != nil {
		return false, e
	}
	changed, e := r.RowsAffected()
	return changed > 0, e
}
