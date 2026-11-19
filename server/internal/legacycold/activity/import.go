// Package activity projects the activity in a sealed legacy PC v26 database
// into an independently created Next catalog. It never opens a source file.
package activity

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

const timeKey = "2006-01-02T15:04:05.000000000Z"

var errInvalid = errors.New("invalid historical fact")
var errConflict = errors.New("conflicting identity or projection")

type importError struct {
	stage string
	cause error
}

// Driver conversion errors can quote private input. The operator sees the
// affected table and reason, while errors.Is still works for cancellation.
func (e *importError) Error() string {
	reason := "read or write failed"
	for _, known := range []error{errInvalid, errConflict, context.Canceled, context.DeadlineExceeded} {
		if errors.Is(e.cause, known) {
			reason = known.Error()
			break
		}
	}
	return "cold26 activity " + e.stage + ": " + reason
}

func (e *importError) Unwrap() error { return e.cause }

type importer struct {
	source   *sql.DB
	target   *sql.Tx
	sourceID string
	location *time.Location
}

// Import runs in the caller's private target transaction after the assets
// have been installed. A failed call must roll back that transaction. The old
// database stores UTC instants but does not contain its server's IANA zone.
func Import(ctx context.Context, source *sql.DB, target *sql.Tx, sourceID string, location *time.Location) error {
	if location == nil {
		return errors.New("cold26 activity requires the source time zone")
	}
	m := importer{source, target, sourceID, location}
	var recordedZone string
	err := target.QueryRowContext(ctx, `SELECT json_extract(original,'$.source_time_zone') FROM migration_relation_origins
		WHERE input_id=? AND kind='cold26/activity' AND identity='v1'`, sourceID).Scan(&recordedZone)
	if err == nil {
		if recordedZone != location.String() {
			return &importError{"source time zone", errConflict}
		}
		// The receipt commits with all facts. Reopening the same sealed source
		// cannot resurrect a vote, mark or favorite subsequently edited in Next.
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return &importError{"receipt", err}
	}
	for _, step := range []struct {
		name string
		run  func(context.Context) error
	}{
		{"view_events", m.events},
		{"likes", m.likes},
		{"favorites", m.favorites},
		{"timeline_tags", m.marks},
		{"daily_shown", m.impressions},
		{"last_position_seconds", m.positions},
		{"asset_daily_stats", m.sourceStats},
	} {
		if err := step.run(ctx); err != nil {
			return &importError{step.name, err}
		}
	}
	_, err = target.ExecContext(ctx, `INSERT INTO migration_relation_origins(input_id,kind,identity,original)
		VALUES(?,'cold26/activity','v1',json_object('source_time_zone',?))`, sourceID, location.String())
	if err != nil {
		return &importError{"receipt", err}
	}
	return nil
}

func (m importer) rows(ctx context.Context, query string, visit func(*sql.Rows) error) error {
	rows, err := m.source.QueryContext(ctx, query)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		if err := visit(rows); err != nil {
			return err
		}
	}
	return rows.Err()
}

func (m importer) origin(ctx context.Context, kind, identity, raw string) error {
	_, err := m.target.ExecContext(ctx, `INSERT INTO migration_relation_origins(input_id,kind,identity,original) VALUES(?,?,?,?)`,
		m.sourceID, "cold26/"+kind, identity, raw)
	return err
}

func timestamp(value string) (time.Time, error) {
	at, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, errInvalid
	}
	return at, nil
}

func validDay(value string) bool {
	_, err := time.Parse(time.DateOnly, value)
	return err == nil
}
