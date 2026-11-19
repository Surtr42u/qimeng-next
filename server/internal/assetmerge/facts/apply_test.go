package facts_test

import (
	"database/sql"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"qimeng.local/next/server/internal/assetmerge/facts"
	"qimeng.local/next/server/internal/engagement"
	"qimeng.local/next/server/internal/service"
	"qimeng.local/next/server/internal/store"
)

func TestSelectedImagesKeepPhysicalEvidenceAndOneVisibleHistory(t *testing.T) {
	target, plan := fixture(t)
	db := target.DB
	// Even an inconsistent derived cache cannot add visits to the source events.
	execute(t, db, "UPDATE asset_daily_stats SET views=900 WHERE asset_id='right'")
	result, err := facts.Apply(t.Context(), db, plan)
	if err != nil || result != (facts.Result{Groups: 1, Members: 2, Donors: 1}) {
		t.Fatalf("apply: %+v, %v", result, err)
	}
	s := &service.Service{DB: target}
	rep, err := s.Asset(t.Context(), "left")
	if err != nil {
		t.Fatal(err)
	}
	if rep.FileName != "Reviewed.png" || rep.OriginalFileName != "left.png" || rep.RelativePath != "album/Reviewed.png" ||
		rep.ContentVersion != 1 || rep.ViewCount != 3 || rep.PlayCount != 1 || rep.LikeCount != 2 || !rep.Favorite ||
		rep.FavoritedAt == nil || rep.FavoritedAt.Format(time.RFC3339Nano) != "2025-03-09T06:00:01.123Z" {
		t.Fatalf("representative detail: %+v", rep)
	}
	if !slices.Equal(rep.AuthorIDs, []string{"artist-a", "artist-b"}) || !slices.Equal(rep.Tags, []string{"shared", "tag-left", "tag-right"}) ||
		!slices.Equal(rep.SourceIDs, []string{"world-a", "world-b"}) || !slices.Equal(rep.CharacterIDs, []string{"role-a", "role-b"}) {
		t.Fatalf("representative relationships: %+v", rep)
	}
	history, err := s.History(t.Context(), "", 10)
	if err != nil || len(history.Items) != 2 || history.Items[0].AssetID != "left" || history.Items[0].EventID != "visit-right" ||
		history.Items[0].FileName != "Reviewed.png" || history.Items[0].Asset == nil || history.Items[0].Asset.FileName != "Reviewed.png" {
		t.Fatalf("normal history: %+v, %v", history, err)
	}
	ranks, err := s.Rankings(t.Context(), "all", "", "", 10, "image")
	if err != nil || len(ranks.Items) != 3 || ranks.Items[0].Asset.ID != "left" || ranks.Items[0].Score != 6 {
		t.Fatalf("ranking: %+v, %v", ranks, err)
	}
	overview, err := s.StatsOverview(t.Context(), "all")
	if err != nil || overview.Inventory["assets"] != 3 || overview.Inventory["events"] != 6 ||
		overview.Totals != (service.ActivityTotals{Views: 4, Plays: 1, Seconds: 17}) {
		t.Fatalf("global activity: %+v, %v", overview, err)
	}
	value(t, db, "album/right.png", "SELECT relative_path FROM asset_locations WHERE asset_id='right' AND trashed=1 AND current_version=1")
	value(t, db, 4, "SELECT count(*) FROM asset_content_versions WHERE version=1 AND hash=?", plan.Groups[0].SHA256)
	value(t, db, 1, "SELECT count(*) FROM assets WHERE id='unselected' AND trashed=0 AND file_name='unselected.png'")
	value(t, db, "left", "SELECT asset_id FROM migration_media_aliases WHERE source_id=? AND source_asset_id='right'", plan.SourceID)
	value(t, db, "left/right", `SELECT a.asset_id||'/'||o.physical_asset_id FROM migration_media_aliases a JOIN selected_image_merge_aliases o
		USING(source_id,source_asset_id) WHERE a.source_id='another-provenance' AND a.source_asset_id='older-right'`)
	value(t, db, "album/right.png/artist-b/tag-right", `SELECT json_extract(original_asset,'$.relativePath')||'/'||
		json_extract(original_relations,'$.authors[0].id')||'/'||json_extract(original_asset,'$.tags[1]')
		FROM selected_image_merge_members WHERE physical_asset_id='right'`)
	value(t, db, "2025-01-02T03:04:05.000000101Z", `SELECT json_extract(original_asset,'$.contentMtime')
		FROM selected_image_merge_members WHERE physical_asset_id='right'`)
	value(t, db, 2, "SELECT count(*) FROM selected_image_merge_sources")
	value(t, db, "right", "SELECT physical_asset_id FROM selected_image_merge_source_members WHERE source_key='post-b'")
	value(t, db, 0, "SELECT count(*) FROM selected_image_merge_source_members WHERE source_key='post-a'")
	value(t, db, "2025-02-02T01:02:03+09:00", "SELECT occurred_at FROM selected_image_merge_sources WHERE source_key='post-a'")
	value(t, db, 6, "SELECT count(*) FROM migration_event_origins")
	value(t, db, "right", "SELECT json_extract(original,'$.asset_id') FROM migration_event_origins WHERE event_id='visit-right'")
	value(t, db, "right.png", `SELECT json_extract(m.original_asset,'$.fileName') FROM migration_event_origins o
		JOIN selected_image_merge_members m ON m.physical_asset_id=json_extract(o.original,'$.asset_id') WHERE o.event_id='visit-right'`)
	value(t, db, "2025-03-09T06:01:02.123456789Z", "SELECT created_at FROM events WHERE event_id='visit-left'")
	value(t, db, 5, "SELECT count(*) FROM events WHERE asset_id='left' AND json_extract(data,'$.assetId')='left' AND json_extract(data,'$.contentVersion')=0")
	value(t, db, "visit-left", "SELECT event_id FROM event_sessions WHERE asset_id='left' AND session_id='shared' AND kind='view' AND day='2025-03-09'")
	value(t, db, 0, "SELECT count(*) FROM event_sessions WHERE asset_id='left' AND day='2025-03-08'")
	value(t, db, 2, "SELECT views FROM asset_daily_stats WHERE asset_id='left' AND day='2025-03-09'")
	value(t, db, 1, "SELECT views FROM asset_hourly_stats WHERE asset_id='left' AND day='2025-03-08' AND hour=23")
	value(t, db, 17, "SELECT seconds FROM asset_hourly_stats WHERE asset_id='left' AND day='2025-03-09' AND hour=3")
	value(t, db, 0, "SELECT count(*) FROM asset_daily_stats WHERE asset_id='right'")
	value(t, db, 0, "SELECT count(*) FROM asset_sources WHERE asset_id='left' AND source_id='world-b'")
	value(t, db, 3, "SELECT count(*) FROM selected_image_merge_likes")
	value(t, db, "2025-03-09T07:59:59.123456789Z", "SELECT created_at FROM likes WHERE asset_id='left' AND day='2025-03-09'")
	value(t, db, 11, "SELECT count FROM recommendation_shown WHERE asset_id='left' AND day='2025-03-09'")
	value(t, db, 2, "SELECT count(*) FROM timeline_marks WHERE asset_id='left' AND position_ms=9 AND json_extract(data,'$.contentVersion')=0")
	value(t, db, "right", `SELECT json_extract(original,'$.asset_id') FROM migration_relation_origins WHERE input_id=? AND kind='cold26/timeline_tags' AND identity='mark-b'`, plan.SourceID)
	for _, query := range []string{
		"UPDATE asset_locations SET trashed=0 WHERE asset_id='right'",
		"DELETE FROM asset_records WHERE id='right'",
		"UPDATE selected_image_merge_members SET original_asset='{}' WHERE physical_asset_id='left'",
		"DELETE FROM selected_image_merge_likes",
	} {
		if _, err := db.Exec(query); err == nil {
			t.Fatalf("protected physical object or immutable evidence changed: %s", query)
		}
	}
}

func TestReplayAfterSnapshotAndOwnerChangesKeepsOriginalReceipt(t *testing.T) {
	target, plan := fixture(t)
	db := target.DB
	if _, err := facts.Apply(t.Context(), db, plan); err != nil {
		t.Fatal(err)
	}
	s := &service.Service{DB: target}
	rep, err := s.Asset(t.Context(), "left")
	if err != nil {
		t.Fatal(err)
	}
	tx, err := db.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	at := time.Date(2025, 3, 9, 9, 0, 0, 0, time.UTC)
	rep, err = engagement.SetFavorite(t.Context(), tx, rep, false, at)
	if err != nil {
		t.Fatal(err)
	}
	rep.Liked = true // The fixture contains a vote on the simulated owner's day.
	if _, err = engagement.SetTodayLike(t.Context(), tx, rep, false, at); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	snapshotPath := filepath.Join(t.TempDir(), "snapshot.sqlite")
	execute(t, db, "VACUUM INTO ?", snapshotPath)
	copied, err := store.Open(snapshotPath)
	if err != nil {
		t.Fatal(err)
	}
	defer copied.DB.Close()
	if err := store.ValidateBackupSchema(t.Context(), copied.DB); err != nil {
		t.Fatal(err)
	}
	var before int
	if err := copied.DB.QueryRow("SELECT revision FROM backup_business_state").Scan(&before); err != nil {
		t.Fatal(err)
	}
	result, err := facts.Apply(t.Context(), copied.DB, plan)
	if err != nil || !result.AlreadyApplied {
		t.Fatalf("snapshot replay: %+v %v", result, err)
	}
	value(t, copied.DB, before, "SELECT revision FROM backup_business_state")
	value(t, copied.DB, 0, "SELECT favorite FROM asset_records WHERE id='left'")
	value(t, copied.DB, 1, "SELECT count(*) FROM likes WHERE asset_id='left'")
	value(t, copied.DB, 3, "SELECT count(*) FROM selected_image_merge_likes")
	value(t, copied.DB, 6, "SELECT count(*) FROM events")
	plan.Groups[0].Sources[0].Caption = "Changed decision"
	if _, err := facts.Apply(t.Context(), copied.DB, plan); err == nil {
		t.Fatal("changed provenance reused the old receipt")
	}
}

func TestNewGroupsRollBackTogetherOnConflict(t *testing.T) {
	for _, tc := range []struct {
		name    string
		prepare func(*facts.Group, *sql.DB)
		want    string
	}{
		{"occupied as case variant", func(g *facts.Group, _ *sql.DB) { g.RelativePath = "album/LEFT.png" }, "occupied"},
		{"missing cold identity", func(_ *facts.Group, db *sql.DB) {
			execute(t, db, "DELETE FROM migration_media_aliases WHERE asset_id='fourth'")
		}, "identity"},
		{"different reviewed SHA", func(g *facts.Group, _ *sql.DB) { g.SHA256 = fixtureDigest("unexpected bytes") }, "original"},
		{"different directory", func(_ *facts.Group, db *sql.DB) {
			execute(t, db, "UPDATE asset_locations SET relative_path='other/fourth.png' WHERE asset_id='fourth'")
		}, "directory"},
		{"native-only playback", func(_ *facts.Group, db *sql.DB) {
			execute(t, db, "INSERT INTO asset_playback_positions VALUES('fourth',1,0,'2025-01-01T00:00:00Z')")
		}, "asset_playback_positions"},
		{"unproven event", func(_ *facts.Group, db *sql.DB) {
			execute(t, db, "DELETE FROM migration_event_origins WHERE event_id='visit-outside'")
		}, "event"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			target, plan := fixture(t)
			g := facts.Group{LibraryID: "pictures", RepresentativeID: "unselected", RelativePath: "album/Another.png",
				SHA256: plan.Groups[0].SHA256, Members: []facts.Member{{AssetID: "unselected"}, {AssetID: "fourth"}}}
			tc.prepare(&g, target.DB)
			plan.Groups = append(plan.Groups, g)
			_, err := facts.Apply(t.Context(), target.DB, plan)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("wanted diagnostic %q, got %v", tc.want, err)
			}
			value(t, target.DB, 0, "SELECT count(*) FROM selected_image_merge_groups")
			value(t, target.DB, 0, "SELECT count(*) FROM asset_locations WHERE trashed=1")
			value(t, target.DB, 3, "SELECT count(*) FROM likes")
			value(t, target.DB, "right", "SELECT asset_id FROM events WHERE event_id='visit-right'")
		})
	}
}
