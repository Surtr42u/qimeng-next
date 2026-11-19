package facts

import (
	"database/sql"
	"errors"
	"path"
	"time"
)

func (p projection) activity() error {
	for _, table := range []string{"asset_daily_stats", "asset_hourly_stats", "event_sessions"} {
		if err := p.exec("DELETE FROM "+table+" WHERE asset_id IN "+groupIDs, p.group.RepresentativeID); err != nil {
			return err
		}
	}
	if err := p.rebuildEvents(); err != nil {
		return err
	}
	rep := p.group.RepresentativeID
	// Native history cards display the event filename. Its old spelling was
	// supplied by the cold member and remains addressable through the origin.
	if err := p.exec("UPDATE events SET asset_id=?1,data=json_set(data,'$.assetId',?1,'$.fileName',?2) WHERE asset_id IN "+groupIDs,
		rep, path.Base(p.group.RelativePath)); err != nil {
		return err
	}
	var unknownMark bool
	if err := p.tx.QueryRowContext(p.ctx, `SELECT EXISTS(SELECT 1 FROM timeline_marks m WHERE m.asset_id IN `+groupIDs+`
		AND (json_extract(m.data,'$.contentVersion') IS NOT 0 OR NOT EXISTS(
		SELECT 1 FROM migration_relation_origins o WHERE o.input_id=?2 AND o.kind='cold26/timeline_tags' AND o.identity=m.id
		AND json_extract(o.original,'$.asset_id')=m.asset_id)))`, rep, p.source).Scan(&unknownMark); err != nil {
		return err
	}
	if unknownMark {
		return errors.New("timeline mark lacks its original v26 binding")
	}
	for _, query := range []string{
		"UPDATE timeline_marks SET asset_id=?1,data=json_set(data,'$.assetId',?1) WHERE asset_id IN " + groupIDs + " AND asset_id!=?1",
		// Current votes have one identity per logical asset and day. Keep the
		// first actual vote on a shared day; archive every member's original.
		"DELETE FROM likes WHERE asset_id IN " + groupIDs,
		`INSERT INTO likes(asset_id,day,created_at) SELECT ?1,day,created_at FROM (
			SELECT day,created_at,row_number() OVER(PARTITION BY day ORDER BY qimeng_time_key(created_at),physical_asset_id) AS n
			FROM selected_image_merge_likes WHERE physical_asset_id IN ` + groupIDs + ") WHERE n=1",
		// Current favorite ordering uses the most recent actual favorite. All
		// member states and timestamps survive independently in original_asset.
		`UPDATE asset_records SET (favorite,favorited_at,favorite_updated_at)=(SELECT
			max(json_extract(original_asset,'$.favorite')),max(json_extract(original_asset,'$.favoritedAt')),
			max(json_extract(original_asset,'$.favoriteUpdatedAt')) FROM selected_image_merge_members WHERE representative_id=?1)
			WHERE id=?1`,
		"UPDATE asset_records SET favorite=0,favorited_at=NULL,favorite_updated_at=NULL WHERE id IN " + groupIDs + " AND id!=?1 AND favorite!=0",
		`INSERT INTO recommendation_shown(day,asset_id,count)
			SELECT day,?1,sum(count) FROM recommendation_shown WHERE asset_id IN ` + groupIDs + `
			GROUP BY day ON CONFLICT(day,asset_id) DO UPDATE SET count=excluded.count`,
		"DELETE FROM recommendation_shown WHERE asset_id IN " + groupIDs + " AND asset_id!=?1",
	} {
		if err := p.exec(query, rep); err != nil {
			return err
		}
	}
	return nil
}

const timeKey = "2006-01-02T15:04:05.000000000Z"

type historicalEvent struct {
	id, nativeTime, started, kind, session string
	day                                    sql.NullString
	seconds                                sql.NullInt64
}

// Rebuild from each original event, including visits which predate a session
// day. The time zone belongs to the sealed source, independent of the host.
func (p projection) rebuildEvents() error {
	rows, err := p.tx.QueryContext(p.ctx, `SELECT e.event_id,e.created_at,
		coalesce(json_extract(o.original,'$.started_at'),''),coalesce(json_extract(o.original,'$.kind'),''),
		coalesce(json_extract(o.original,'$.session_id'),''),json_extract(o.original,'$.day'),json_extract(o.original,'$.seconds'),
		(o.event_id IS NOT NULL AND json_extract(o.original,'$.asset_id') IS e.asset_id
		AND json_extract(e.data,'$.eventId') IS e.event_id AND json_extract(e.data,'$.assetId') IS e.asset_id
		AND json_extract(e.data,'$.fileName') IS json_extract(m.original_asset,'$.fileName')
		AND json_extract(e.data,'$.contentVersion') IS 0
		AND json_extract(e.data,'$.kind') IS CASE json_extract(o.original,'$.kind') WHEN 'open' THEN 'view' ELSE json_extract(o.original,'$.kind') END
		AND coalesce(json_extract(e.data,'$.sessionId'),'') IS json_extract(o.original,'$.session_id')
		AND json_extract(e.data,'$.seconds') IS json_extract(o.original,'$.seconds')
		AND qimeng_time_key(json_extract(e.data,'$.createdAt')) IS e.created_at
		AND qimeng_time_key(json_extract(e.data,'$.occurredAt')) IS e.created_at)
		FROM selected_image_merge_members m JOIN events e ON e.asset_id=m.physical_asset_id
		LEFT JOIN migration_event_origins o ON o.event_id=e.event_id AND o.source_id=?2
		WHERE m.representative_id=?1 ORDER BY e.created_at,e.event_id`, p.group.RepresentativeID, p.source)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var ev historicalEvent
		var valid bool
		if err := rows.Scan(&ev.id, &ev.nativeTime, &ev.started, &ev.kind, &ev.session, &ev.day, &ev.seconds, &valid); err != nil {
			return err
		}
		if !valid {
			return errors.New("event lacks matching native and original v26 facts")
		}
		if err := p.countEvent(ev); err != nil {
			return err
		}
	}
	return rows.Err()
}

func (p projection) countEvent(ev historicalEvent) error {
	at, err := time.Parse(time.RFC3339Nano, ev.started)
	if err != nil || at.UTC().Format(timeKey) != ev.nativeTime || ev.seconds.Int64 < 0 {
		return errors.New("invalid original event time or duration")
	}
	local := at.In(p.zone)
	day := local.Format(time.DateOnly)
	if ev.day.Valid {
		if _, err := time.Parse(time.DateOnly, ev.day.String); err != nil {
			return errors.New("invalid original event day")
		}
		day = ev.day.String
	}
	kind := ev.kind
	var views, plays, seconds int64
	switch kind {
	case "open":
		kind, views = "view", 1
	case "play":
		plays = 1
	case "dwell":
		seconds = ev.seconds.Int64
	default:
		return errors.New("unsupported original event kind")
	}
	rep := p.group.RepresentativeID
	if kind != "dwell" && ev.day.Valid {
		// The unique session key answers future retries. The two underlying
		// old visits from distinct members are both counted and retained.
		if err := p.exec(`INSERT INTO event_sessions(asset_id,session_id,kind,day,event_id) VALUES(?,?,?,?,?)
			ON CONFLICT(asset_id,session_id,kind,day) DO NOTHING`, rep, ev.session, kind, day, ev.id); err != nil {
			return err
		}
	}
	if views != 0 || plays != 0 || seconds != 0 {
		r, err := p.tx.ExecContext(p.ctx, `INSERT INTO asset_daily_stats(asset_id,day,views,plays,seconds) VALUES(?,?,?,?,?)
			ON CONFLICT(asset_id,day) DO UPDATE SET views=views+excluded.views,plays=plays+excluded.plays,seconds=seconds+excluded.seconds
			WHERE typeof(seconds+excluded.seconds)='integer'`, rep, day, views, plays, seconds)
		if err := checkStat(r, err); err != nil {
			return err
		}
	}
	if views != 0 || seconds != 0 {
		r, err := p.tx.ExecContext(p.ctx, `INSERT INTO asset_hourly_stats(asset_id,day,hour,views,seconds) VALUES(?,?,?,?,?)
			ON CONFLICT(asset_id,day,hour) DO UPDATE SET views=views+excluded.views,seconds=seconds+excluded.seconds
			WHERE typeof(seconds+excluded.seconds)='integer'`, rep, day, local.Hour(), views, seconds)
		return checkStat(r, err)
	}
	return nil
}

func checkStat(result sql.Result, err error) error {
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err == nil && n != 1 {
		return errors.New("combined activity exceeds the native integer range")
	}
	return err
}
