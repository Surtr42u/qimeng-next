package activity

import (
	"context"
	"database/sql"
	"encoding/json"
)

func (m importer) likes(ctx context.Context) error {
	return m.rows(ctx, `SELECT asset_id,day,created_at FROM likes ORDER BY asset_id,day`, func(rows *sql.Rows) error {
		var asset, day, created string
		if err := rows.Scan(&asset, &day, &created); err != nil {
			return err
		}
		if !validDay(day) {
			return errInvalid
		}
		if _, err := timestamp(created); err != nil {
			return err
		}
		// All three v26 columns fit the native vote row verbatim. Neither a
		// synthetic event nor an unattributed lifetime baseline is necessary.
		result, err := m.target.ExecContext(ctx, `INSERT INTO likes(asset_id,day,created_at)
			SELECT id,?,? FROM asset_records WHERE id=?`, day, created, asset)
		return requireOne(result, err)
	})
}

func (m importer) favorites(ctx context.Context) error {
	return m.rows(ctx, `SELECT asset_id,created_at,json_object('asset_id',asset_id,'created_at',created_at) FROM favorites`, func(rows *sql.Rows) error {
		var asset, created, raw string
		if err := rows.Scan(&asset, &created, &raw); err != nil {
			return err
		}
		at, err := timestamp(created)
		if err != nil {
			return err
		}
		// Next sorts favorites by a fixed-width UTC key. This state change has
		// its own timestamp; it must not change the original asset's mtime.
		key := at.UTC().Format(timeKey)
		result, err := m.target.ExecContext(ctx, `UPDATE asset_records SET favorite=1,favorited_at=?,favorite_updated_at=?
			WHERE id=? AND favorite=0 AND favorite_updated_at IS NULL`, key, key, asset)
		if err = requireOne(result, err); err != nil {
			return err
		}
		return m.origin(ctx, "favorites", asset, raw)
	})
}

func requireOne(result sql.Result, err error) error {
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return errConflict
	}
	return nil
}

func (m importer) marks(ctx context.Context) error {
	return m.rows(ctx, `SELECT id,asset_id,time_millis,name,created_at,color,
		json_object('id',id,'asset_id',asset_id,'time_millis',time_millis,'name',name,'created_at',created_at,'color',color)
		FROM timeline_tags ORDER BY id`, func(rows *sql.Rows) error {
		var id, asset, label, created, color, raw string
		var position int64
		if err := rows.Scan(&id, &asset, &position, &label, &created, &color, &raw); err != nil {
			return err
		}
		if id == "" || position < 0 {
			return errInvalid
		}
		if _, err := timestamp(created); err != nil {
			return err
		}
		// Zero is the native unknown-version marker. v26 records only creation,
		// which initializes revision one. A later edit owns its own timestamp.
		_, err := m.target.ExecContext(ctx, `INSERT INTO timeline_marks(id,asset_id,position_ms,data)
			VALUES(?1,?2,?3,json_object('id',?1,'assetId',?2,'positionMs',?3,'label',?4,'color',?5,
			'contentVersion',0,'revision',1,'createdAt',?6,'updatedAt',?6))`, id, asset, position, label, color, created)
		if err != nil {
			return err
		}
		return m.origin(ctx, "timeline_tags", id, raw)
	})
}

func (m importer) impressions(ctx context.Context) error {
	return m.rows(ctx, `SELECT asset_id,day,count,json_array(asset_id,day),json_object('asset_id',asset_id,'day',day,'count',count)
		FROM daily_shown ORDER BY asset_id,day`, func(rows *sql.Rows) error {
		var asset, day, identity, raw string
		var count int64
		if err := rows.Scan(&asset, &day, &count, &identity, &raw); err != nil {
			return err
		}
		if count < 0 || !validDay(day) {
			return errInvalid
		}
		result, err := m.target.ExecContext(ctx, `INSERT INTO recommendation_shown(asset_id,day,count)
			SELECT id,?,? FROM asset_records WHERE id=?`, day, count, asset)
		if err = requireOne(result, err); err != nil {
			return err
		}
		return m.origin(ctx, "daily_shown", identity, raw)
	})
}

func (m importer) positions(ctx context.Context) error {
	return m.rows(ctx, `SELECT a.asset_id,a.last_position_seconds,
		(SELECT max(started_at) FROM view_events WHERE asset_id=a.asset_id),
		(SELECT max(started_at) FROM view_events WHERE asset_id=a.asset_id AND kind='open')
		FROM assets a WHERE a.last_position_seconds IS NOT NULL ORDER BY a.asset_id`, func(rows *sql.Rows) error {
		var p struct {
			AssetID             string  `json:"asset_id"`
			LastPositionSeconds float64 `json:"last_position_seconds"`
			LastEventAt         *string `json:"last_event_at"`
			LastOpenedAt        *string `json:"last_opened_at"`
		}
		if err := rows.Scan(&p.AssetID, &p.LastPositionSeconds, &p.LastEventAt, &p.LastOpenedAt); err != nil {
			return err
		}
		// v26 progress updates deliberately did not update assets.updated_at.
		// Last interaction and last open are real event times, not seek times.
		// Neither the content version nor the seek time can be recovered from
		// this state, including an explicit rewind to 0 seconds.
		raw, err := json.Marshal(p)
		if err != nil {
			return err
		}
		return m.origin(ctx, "last_position_seconds", p.AssetID, string(raw))
	})
}

func (m importer) sourceStats(ctx context.Context) error {
	// The v26 daily cache can omit deleted files, contain rows of zeros, or
	// disagree with the immutable event stream. Preserve the observed cache
	// separately so none of these numbers become additional Next activity.
	return m.rows(ctx, `SELECT json_array(asset_id,day),json_object('asset_id',asset_id,'day',day,'view_count',view_count,
		'play_count',play_count,'browse_seconds',browse_seconds) FROM asset_daily_stats ORDER BY asset_id,day`, func(rows *sql.Rows) error {
		var identity, raw string
		if err := rows.Scan(&identity, &raw); err != nil {
			return err
		}
		return m.origin(ctx, "asset_daily_stats", identity, raw)
	})
}
