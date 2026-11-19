package activity_test

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestSealedActivityKeepsIndependentFactsAndReplaysOnce(t *testing.T) {
	source, id := syntheticSource(t, "")
	_, db := syntheticTarget(t)
	zone := sourceLocation(t)
	if err := transfer(t.Context(), source, db, id, zone); err != nil {
		t.Fatal(err)
	}
	checkValue(t, db, 13, "SELECT count(*) FROM events")
	checkValue(t, db, 13, "SELECT count(*) FROM migration_event_origins WHERE source_id=?", id)
	checkValue(t, db, 13, "SELECT count(*) FROM events WHERE json_extract(data,'$.contentVersion')=0")
	checkValue(t, db, "view", "SELECT json_extract(data,'$.kind') FROM events WHERE event_id='client-original'")
	checkValue(t, db, "", "SELECT json_extract(data,'$.fileName') FROM events WHERE event_id='deleted-original-event'")
	checkValue(t, db, 0, "SELECT count(*) FROM asset_records WHERE id='deleted-original'")
	checkValue(t, db, 4, "SELECT count(*) FROM event_sessions")
	checkValue(t, db, "legacy:transferred", `SELECT event_id FROM event_sessions WHERE asset_id='clip-a' AND kind='view' AND session_id='same' AND day='2025-03-09'`)
	checkValue(t, db, "null", `SELECT json_type(original,'$.seconds') FROM migration_event_origins WHERE event_id='client-null-dwell'`)
	checkValue(t, db, "integer", `SELECT json_type(original,'$.seconds') FROM migration_event_origins WHERE event_id='client-zero-dwell'`)
	checkValue(t, db, "portable_event_identity", `SELECT json_extract(original,'$.identity_basis') FROM migration_event_origins WHERE event_id='portable:1234567890abcdef1234567890abcdef'`)
	checkValue(t, db, "null", `SELECT json_type(original,'$.day') FROM migration_event_origins WHERE event_id='portable:1234567890abcdef1234567890abcdef'`)
	for _, row := range []int{11, 12} {
		// Two indistinguishable payloads have different append-only row IDs.
		sum := sha256.Sum256(fmt.Appendf(nil, "%s\x00view_events\x00%d", id, row))
		checkValue(t, db, fmt.Sprintf("portable:%x", sum[:16]), `SELECT event_id FROM migration_event_origins
			WHERE json_extract(original,'$.id')=? AND json_extract(original,'$.identity_basis')='cold26_sha256_row_v1'`, row)
	}
	checkValue(t, db, 1, "SELECT views FROM asset_daily_stats WHERE asset_id='clip-a' AND day='2025-03-08'")
	checkValue(t, db, 3, "SELECT views FROM asset_daily_stats WHERE asset_id='clip-a' AND day='2025-03-09'")
	checkValue(t, db, 2, "SELECT seconds FROM asset_daily_stats WHERE asset_id='clip-a' AND day='2025-03-10'")
	checkValue(t, db, 900, `SELECT json_extract(original,'$.view_count') FROM migration_relation_origins WHERE input_id=? AND kind='cold26/asset_daily_stats'`, id)
	checkValue(t, db, "2025-03-09T04:55:11.456Z", "SELECT created_at FROM likes WHERE asset_id='clip-a' AND day='2025-03-08'")
	checkValue(t, db, "2025-03-09T05:01:22.789Z", "SELECT created_at FROM likes WHERE asset_id='clip-a' AND day='2025-03-09'")
	checkValue(t, db, "2025-03-09T05:00:00.123000000Z", "SELECT favorited_at FROM asset_records WHERE id='clip-a'")
	checkValue(t, db, 5, "SELECT count FROM recommendation_shown WHERE asset_id='clip-a' AND day='2025-03-09'")
	checkValue(t, db, 0, "SELECT count FROM recommendation_shown WHERE asset_id='clip-b'")
	checkValue(t, db, 0, "SELECT count(*) FROM asset_playback_positions")
	const positions = " FROM migration_relation_origins WHERE input_id=? AND kind='cold26/last_position_seconds'"
	checkValue(t, db, 2, "SELECT count(*)"+positions, id)
	checkValue(t, db, 12.3456789, "SELECT json_extract(original,'$.last_position_seconds')"+positions+" AND identity='clip-a'", id)
	checkValue(t, db, 0.0, "SELECT json_extract(original,'$.last_position_seconds')"+positions+" AND identity='clip-b'", id)
	checkValue(t, db, "2025-03-09T07:08:00.000Z", "SELECT json_extract(original,'$.last_event_at')"+positions+" AND identity='clip-a'", id)
	checkValue(t, db, "2025-03-09T05:00:00.000Z", "SELECT json_extract(original,'$.last_opened_at')"+positions+" AND identity='clip-a'", id)
	checkValue(t, db, 0, "SELECT count(*) FROM migration_inputs")

	// Future ordinary edits belong to Next. The same sealed source cannot
	// revive removed votes/marks or reset an updated impression counter.
	_, err := db.Exec(`DELETE FROM likes WHERE asset_id='clip-a' AND day='2025-03-08';
		DELETE FROM timeline_marks WHERE id='mark-first';
		UPDATE asset_records SET favorite=0,favorited_at=NULL WHERE id='clip-a';
		UPDATE recommendation_shown SET count=count+2 WHERE asset_id='clip-a' AND day='2025-03-09'`)
	if err != nil {
		t.Fatal(err)
	}
	if err = transfer(t.Context(), source, db, id, zone); err != nil {
		t.Fatal(err)
	}
	checkValue(t, db, 13, "SELECT count(*) FROM events")
	checkValue(t, db, 6, "SELECT sum(views) FROM asset_daily_stats")
	checkValue(t, db, 27, "SELECT sum(seconds) FROM asset_hourly_stats")
	checkValue(t, db, 1, "SELECT count(*) FROM likes")
	checkValue(t, db, 2, "SELECT count(*) FROM timeline_marks")
	checkValue(t, db, 0, "SELECT favorite FROM asset_records WHERE id='clip-a'")
	checkValue(t, db, 7, "SELECT count FROM recommendation_shown WHERE asset_id='clip-a' AND day='2025-03-09'")
	if err = transfer(t.Context(), source, db, id, time.UTC); err == nil {
		t.Fatal("the same snapshot replayed using another time zone")
	}
}

func TestSealedActivityRollsBackAmbiguousKeysOrUnreadableFacts(t *testing.T) {
	for _, example := range []struct {
		name, change, stage string
	}{
		{"key shared across old indexes", "UPDATE portable_event_identity SET stable_id='client-original' WHERE event_id=3", "view_events"},
		{"unreadable stored date", "UPDATE favorites SET created_at='sensitive-fixture-value'", "favorites"},
	} {
		t.Run(example.name, func(t *testing.T) {
			source, id := syntheticSource(t, example.change)
			_, target := syntheticTarget(t)
			err := transfer(t.Context(), source, target, id, sourceLocation(t))
			if err == nil || !strings.Contains(err.Error(), example.stage) || strings.Contains(err.Error(), "sensitive-fixture-value") {
				t.Fatalf("expected redacted table-specific error, got %v", err)
			}
			checkValue(t, target, 0, "SELECT count(*) FROM events")
			checkValue(t, target, 0, "SELECT count(*) FROM migration_event_origins")
			checkValue(t, target, 0, "SELECT count(*) FROM asset_daily_stats")
			checkValue(t, target, 0, "SELECT count(*) FROM likes")
			checkValue(t, target, 0, "SELECT count(*) FROM migration_relation_origins")
		})
	}
}
