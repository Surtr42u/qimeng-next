package service

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"qimeng.local/next/server/internal/store"
)

func statisticsFixture(t *testing.T) (*Service, func(string, ...any)) {
	t.Helper()
	db, e := store.Open(filepath.Join(t.TempDir(), "statistics.sqlite"))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { db.DB.Close() })
	exec := func(query string, args ...any) {
		t.Helper()
		if _, e := db.DB.Exec(query, args...); e != nil {
			t.Fatalf("%s: %v", query, e)
		}
	}
	return &Service{DB: db}, exec
}

// TestStatsKeepsOnePassAggregateValues locks the eight inventory keys to their
// previous definitions. In particular, a trashed location that is a selected
// image merge donor stays out of the visible trash count.
func TestStatsKeepsOnePassAggregateValues(t *testing.T) {
	s, exec := statisticsFixture(t)
	exec(`INSERT INTO libraries(id,name,kind,data) VALUES('lib','Synthetic','regular','{}')`)
	exec(`INSERT INTO authors(id,name,following,data) VALUES
		('au','Author',0,'{"kind":"regular"}'),('cos-au','COS author',0,'{"kind":"cos"}'),('unknown','未知',0,'{}')`)
	for _, asset := range []struct {
		id, mediaType string
		size          int64
		favorite      int
		trashed       int
	}{
		{"live-image", "image", 100, 1, 0},
		{"live-video", "video", 200, 0, 0},
		{"live-animated", "animated_image", 50, 0, 0},
		{"live-cos", "image", 60, 0, 0},
		{"merge-representative", "image", 70, 0, 0},
		{"visible-trash", "image", 10, 0, 1},
		{"merge-donor", "image", 30, 0, 1},
	} {
		exec(`INSERT INTO asset_records(id,author_id,revision,created_at,updated_at,favorite,data)
			VALUES(?,'au',1,'2025-01-01T00:00:00Z','2025-01-01T00:00:00Z',?,'{}')`, asset.id, asset.favorite)
		exec(`INSERT INTO asset_content_versions(asset_id,version,hash,size,media_type,mime_type)
			VALUES(?,1,NULL,?,?,'image/png')`, asset.id, asset.size, asset.mediaType)
		exec(`INSERT INTO asset_locations(asset_id,library_id,relative_path,file_name,current_version,trashed,storage_layout)
			VALUES(?,'lib',?,?,1,?,2)`, asset.id, asset.id+".png", asset.id+".png", asset.trashed)
	}
	exec(`INSERT INTO selected_image_merge_groups(representative_id,source_id,baseline_sha256,decision,applied_at)
		VALUES('merge-representative','synthetic',?,'{}','2025-01-01T00:00:00Z')`, strings.Repeat("b", 64))
	exec(`INSERT INTO selected_image_merge_members(physical_asset_id,representative_id,original_asset,original_relations)
		VALUES('merge-representative','merge-representative','{}','{}'),('merge-donor','merge-representative','{}','{}')`)
	for _, link := range []struct{ asset, author string }{
		{"live-image", "au"}, {"live-video", "au"}, {"live-animated", "au"},
		{"live-cos", "cos-au"}, {"merge-representative", "cos-au"},
	} {
		exec(`INSERT INTO asset_authors(asset_id,author_id,ordinal) VALUES(?,?,0)`, link.asset, link.author)
	}
	exec(`INSERT INTO tags(id,name,revision,deleted,created_at,updated_at)
		VALUES('tag-shared','shared',1,0,'2025-01-01T00:00:00Z','2025-01-01T00:00:00Z')`)
	exec(`INSERT INTO asset_tags(asset_id,tag_id,ordinal) VALUES
		('live-image','tag-shared',0),('live-video','tag-shared',1),('visible-trash','tag-shared',0)`)
	exec(`INSERT INTO events(event_id,asset_id,created_at,data) VALUES
		('event-live','live-image','2025-01-01T00:00:00Z','{}'),('event-donor','merge-donor','2025-01-01T00:00:00Z','{}')`)

	want := map[string]int64{"assets": 5, "images": 4, "videos": 1, "bytes": 480,
		"favorites": 1, "authors": 2, "events": 2, "trash": 1}
	if got, e := s.Stats(t.Context()); e != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("stats = %v, %v; want %v", got, e, want)
	}
}

// TestStatsOverviewDimensionsShareOneAggregate keeps the fixed key order and
// exact totals now that media types and sources are grouped together: activity
// follows live assets only, while Totals keep every historical daily row.
func TestStatsOverviewDimensionsShareOneAggregate(t *testing.T) {
	s, exec := statisticsFixture(t)
	exec(`INSERT INTO libraries(id,name,kind,data) VALUES('lib','Synthetic','regular','{}')`)
	exec(`INSERT INTO authors(id,name,following,data) VALUES
		('au','Author',0,'{"kind":"regular"}'),('cos-au','COS author',0,'{"kind":"cos"}')`)
	for _, asset := range []struct {
		id, mediaType string
		size          int64
		trashed       int
	}{
		{"live-image", "image", 100, 0},
		{"live-video", "video", 200, 0},
		{"live-animated", "animated_image", 50, 0},
		{"live-cos", "image", 60, 0},
		{"merge-representative", "image", 70, 0},
		{"visible-trash", "image", 10, 1},
		{"merge-donor", "image", 30, 1},
	} {
		exec(`INSERT INTO asset_records(id,author_id,revision,created_at,updated_at,data)
			VALUES(?,'au',1,'2025-01-01T00:00:00Z','2025-01-01T00:00:00Z','{}')`, asset.id)
		exec(`INSERT INTO asset_content_versions(asset_id,version,hash,size,media_type,mime_type)
			VALUES(?,1,NULL,?,?,'image/png')`, asset.id, asset.size, asset.mediaType)
		exec(`INSERT INTO asset_locations(asset_id,library_id,relative_path,file_name,current_version,trashed,storage_layout)
			VALUES(?,'lib',?,?,1,?,2)`, asset.id, asset.id+".png", asset.id+".png", asset.trashed)
	}
	exec(`INSERT INTO selected_image_merge_groups(representative_id,source_id,baseline_sha256,decision,applied_at)
		VALUES('merge-representative','synthetic',?,'{}','2025-01-01T00:00:00Z')`, strings.Repeat("b", 64))
	exec(`INSERT INTO selected_image_merge_members(physical_asset_id,representative_id,original_asset,original_relations)
		VALUES('merge-representative','merge-representative','{}','{}'),('merge-donor','merge-representative','{}','{}')`)
	for _, link := range []struct{ asset, author string }{
		{"live-image", "au"}, {"live-video", "au"}, {"live-animated", "au"},
		{"live-cos", "cos-au"}, {"merge-representative", "cos-au"},
	} {
		exec(`INSERT INTO asset_authors(asset_id,author_id,ordinal) VALUES(?,?,0)`, link.asset, link.author)
	}
	exec(`INSERT INTO tags(id,name,revision,deleted,created_at,updated_at)
		VALUES('tag-shared','shared',1,0,'2025-01-01T00:00:00Z','2025-01-01T00:00:00Z')`)
	exec(`INSERT INTO asset_tags(asset_id,tag_id,ordinal) VALUES
		('live-image','tag-shared',0),('live-video','tag-shared',1),('visible-trash','tag-shared',0)`)
	for _, row := range []struct {
		id              string
		views, plays, s int64
	}{
		{"live-image", 2, 1, 4},
		{"live-video", 3, 0, 8},
		{"merge-representative", 1, 0, 1},
		{"visible-trash", 5, 2, 16},
		{"merge-donor", 7, 0, 0},
	} {
		exec(`INSERT INTO asset_daily_stats(asset_id,day,views,plays,seconds) VALUES(?,'2025-01-01',?,?,?)`,
			row.id, row.views, row.plays, row.s)
	}

	overview, e := s.StatsOverview(t.Context(), "all")
	if e != nil {
		t.Fatal(e)
	}
	wantInventory := map[string]int64{"assets": 5, "images": 4, "videos": 1, "bytes": 480,
		"favorites": 0, "authors": 2, "events": 0, "trash": 1}
	if !reflect.DeepEqual(overview.Inventory, wantInventory) {
		t.Fatalf("inventory = %v, want %v", overview.Inventory, wantInventory)
	}
	if overview.Totals != (ActivityTotals{Views: 18, Plays: 3, Seconds: 29}) || overview.ActiveDays != 1 {
		t.Fatalf("historical totals changed: %+v", overview)
	}
	if overview.ViewedAssets != 3 || overview.ViewedAuthors != 2 || overview.ViewedTags != 1 ||
		overview.AverageViewsPerFile == nil || *overview.AverageViewsPerFile != 2 {
		t.Fatalf("viewed facts: %+v", overview)
	}
	wantMedia := []ActivityDimension{
		{Key: "image", Assets: 3, Bytes: 230, ActivityTotals: ActivityTotals{Views: 3, Plays: 1, Seconds: 5}},
		{Key: "animated_image", Assets: 1, Bytes: 50},
		{Key: "video", Assets: 1, Bytes: 200, ActivityTotals: ActivityTotals{Views: 3, Seconds: 8}},
	}
	wantSources := []ActivityDimension{
		{Key: "regular", Assets: 3, Bytes: 350, ActivityTotals: ActivityTotals{Views: 5, Plays: 1, Seconds: 12}},
		{Key: "cos", Assets: 2, Bytes: 130, ActivityTotals: ActivityTotals{Views: 1, Seconds: 1}},
	}
	if !reflect.DeepEqual(overview.MediaTypes, wantMedia) {
		t.Fatalf("media types = %+v, want %+v", overview.MediaTypes, wantMedia)
	}
	if !reflect.DeepEqual(overview.Sources, wantSources) {
		t.Fatalf("sources = %+v, want %+v", overview.Sources, wantSources)
	}
}

func TestActivityDimensionFoldingKeepsFixedKeys(t *testing.T) {
	values := map[string]ActivityDimension{}
	addActivityDimension(values, "image", ActivityDimension{Assets: 2, Bytes: 10, ActivityTotals: ActivityTotals{Views: 3, Plays: 1}})
	addActivityDimension(values, "image", ActivityDimension{Assets: 1, Bytes: 5, ActivityTotals: ActivityTotals{Views: 2, Seconds: 4}})
	got := fixedActivityDimensions(values, []string{"image", "video"})
	want := []ActivityDimension{
		{Key: "image", Assets: 3, Bytes: 15, ActivityTotals: ActivityTotals{Views: 5, Plays: 1, Seconds: 4}},
		{Key: "video"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("folded dimensions = %+v, want %+v", got, want)
	}
}
