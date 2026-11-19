package activity

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"time"

	"qimeng.local/next/server/internal/model"
)

type sourceEvent struct {
	ID            int64   `json:"id"`
	AssetID       string  `json:"asset_id"`
	Kind          string  `json:"kind"`
	SessionID     string  `json:"session_id"`
	StartedAt     string  `json:"started_at"`
	Seconds       *int64  `json:"seconds"`
	ClientEventID *string `json:"client_event_id"`
	Day           *string `json:"day"`
	PortableID    *string `json:"portable_stable_id"`
}

func (e sourceEvent) identity(sourceID string) (id, basis string) {
	if e.ClientEventID != nil && *e.ClientEventID != "" {
		return *e.ClientEventID, "client_event_id"
	}
	if e.PortableID != nil && *e.PortableID != "" {
		return *e.PortableID, "portable_event_identity"
	}
	// A normal v26 row already has one of the above. If the side row is
	// absent too, retain 0019's portable:<16 bytes in lowercase hex> shape,
	// using this immutable snapshot and its append-only row number as proof.
	sum := sha256.Sum256([]byte(sourceID + "\x00view_events\x00" + strconv.FormatInt(e.ID, 10)))
	return "portable:" + hex.EncodeToString(sum[:16]), "cold26_sha256_row_v1"
}

func (m importer) events(ctx context.Context) error {
	return m.rows(ctx, `SELECT v.id,v.asset_id,v.kind,v.session_id,v.started_at,v.seconds,v.client_event_id,v.day,p.stable_id,
		coalesce(a.file_name,'') FROM view_events v LEFT JOIN portable_event_identity p ON p.event_id=v.id
		LEFT JOIN assets a ON a.asset_id=v.asset_id ORDER BY v.id`, func(rows *sql.Rows) error {
		var e sourceEvent
		var fileName string
		if err := rows.Scan(&e.ID, &e.AssetID, &e.Kind, &e.SessionID, &e.StartedAt, &e.Seconds, &e.ClientEventID, &e.Day, &e.PortableID, &fileName); err != nil {
			return err
		}
		return m.event(ctx, e, fileName)
	})
}

func (m importer) event(ctx context.Context, e sourceEvent, fileName string) error {
	at, err := timestamp(e.StartedAt)
	if err != nil {
		return err
	}
	if e.AssetID == "" || (e.Seconds != nil && *e.Seconds < 0) {
		return errInvalid
	}
	kind := e.Kind
	switch kind {
	case "open":
		kind = "view"
	case "play", "dwell":
	default:
		return errInvalid
	}
	local := at.In(m.location)
	day := local.Format(time.DateOnly)
	if e.Day != nil {
		if !validDay(*e.Day) {
			return errInvalid
		}
		day = *e.Day
	}
	id, basis := e.identity(m.sourceID)
	at = at.UTC()
	// Retain the original integer in JSON, including explicit zero; the
	// public Event reads seconds as float64 but SQL statistics use int64.
	native, err := json.Marshal(struct {
		model.Event
		Seconds *int64 `json:"seconds,omitempty"`
	}{model.Event{EventID: id, AssetID: e.AssetID, FileName: fileName, Kind: kind, SessionID: e.SessionID,
		ContentVersion: 0, OccurredAt: &at, CreatedAt: at}, e.Seconds})
	if err != nil {
		return err
	}
	result, err := m.target.ExecContext(ctx, `INSERT INTO events(event_id,asset_id,created_at,data)
		VALUES(?,?,?,?) ON CONFLICT(event_id) DO NOTHING`, id, e.AssetID, at.Format(timeKey), string(native))
	if err != nil {
		return err
	}
	inserted, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if inserted != 1 {
		// Client keys and side-table keys live in separate unique indexes in
		// v26. A collision across indexes must not erase either source row.
		return errConflict
	}
	original, err := json.Marshal(struct {
		sourceEvent
		IdentityBasis string `json:"identity_basis"`
	}{e, basis})
	if err != nil {
		return err
	}
	if _, err = m.target.ExecContext(ctx, `INSERT INTO migration_event_origins(event_id,source_id,original) VALUES(?,?,?)`,
		id, m.sourceID, string(original)); err != nil {
		return err
	}
	if kind != "dwell" && e.Day != nil {
		// A NULL source day is deliberately outside 0014's session key. Its
		// repeated same-day visits are independent facts, including on replay.
		if _, err = m.target.ExecContext(ctx, `INSERT INTO event_sessions(asset_id,session_id,kind,day,event_id)
			VALUES(?,?,?,?,?)`, e.AssetID, e.SessionID, kind, day, id); err != nil {
			return err
		}
	}
	var views, plays, seconds int64
	switch kind {
	case "view":
		views = 1
	case "play":
		plays = 1
	case "dwell":
		if e.Seconds != nil {
			seconds = *e.Seconds
		}
	}
	if views+plays+seconds > 0 {
		if _, err = m.target.ExecContext(ctx, `INSERT INTO asset_daily_stats(asset_id,day,views,plays,seconds) VALUES(?,?,?,?,?)
			ON CONFLICT(asset_id,day) DO UPDATE SET views=views+excluded.views,plays=plays+excluded.plays,seconds=seconds+excluded.seconds`,
			e.AssetID, day, views, plays, seconds); err != nil {
			return err
		}
	}
	if views+seconds > 0 {
		_, err = m.target.ExecContext(ctx, `INSERT INTO asset_hourly_stats(asset_id,day,hour,views,seconds) VALUES(?,?,?,?,?)
			ON CONFLICT(asset_id,day,hour) DO UPDATE SET views=views+excluded.views,seconds=seconds+excluded.seconds`,
			e.AssetID, day, local.Hour(), views, seconds)
	}
	return err
}
