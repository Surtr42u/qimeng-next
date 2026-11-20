package store

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func thumbnailRevision(t *testing.T, db *sql.DB) int64 {
	t.Helper()
	var revision int64
	if err := db.QueryRow("SELECT revision FROM thumbnail_revision WHERE id=1").Scan(&revision); err != nil {
		t.Fatal(err)
	}
	return revision
}

func execThumbnailTest(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := db.Exec(query, args...); err != nil {
		t.Fatal(err)
	}
}

func TestThumbnailRevisionTracksOnlyPreviewFacts(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "thumbnail-revision.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.DB.Close()

	expect := func(want int64, step string) {
		t.Helper()
		if got := thumbnailRevision(t, db.DB); got != want {
			t.Fatalf("%s: revision %d want %d", step, got, want)
		}
	}

	expect(1, "initial")

	execThumbnailTest(t, db.DB, `INSERT INTO libraries(id,name,kind,data) VALUES('lib-a','Alpha','regular','{}')`)
	expect(2, "library insert")
	execThumbnailTest(t, db.DB, `INSERT INTO libraries(id,name,kind,data) VALUES('lib-b','Beta','regular','{}')`)
	expect(3, "second library insert")
	execThumbnailTest(t, db.DB, `INSERT INTO authors(id,name,data) VALUES('author','Author','{}')`)
	expect(3, "author insert")

	execThumbnailTest(t, db.DB, `INSERT INTO asset_records(id,author_id,revision,created_at,updated_at,favorite,data)
		VALUES('asset-a','author',1,'2026-01-01T00:00:00Z','2026-01-01T00:00:00Z',0,'{}')`)
	expect(4, "asset insert")
	execThumbnailTest(t, db.DB, `INSERT INTO asset_content_versions(asset_id,version,size,media_type) VALUES('asset-a',1,4,'image')`)
	expect(5, "content version insert")
	execThumbnailTest(t, db.DB, `INSERT INTO asset_locations(asset_id,library_id,relative_path,file_name,current_version,trashed,storage_layout)
		VALUES('asset-a','lib-a','asset-a.png','asset-a.png',1,0,1)`)
	expect(6, "location insert")

	// Browser activity may create rows and derived counters, but it does not
	// change which preview bytes the library resolves.
	execThumbnailTest(t, db.DB, `INSERT INTO events(event_id,asset_id,created_at,data) VALUES('event-view-1','asset-a','2026-01-01T01:00:00Z','{}')`)
	expect(6, "event insert")

	// A pure rename keeps the cache key and content; it must not defeat skipping.
	execThumbnailTest(t, db.DB, `UPDATE asset_locations SET relative_path='renamed.png' WHERE asset_id='asset-a'`)
	expect(6, "path-only location update")
	execThumbnailTest(t, db.DB, `UPDATE asset_locations SET trashed=1 WHERE asset_id='asset-a'`)
	expect(7, "trash update")
	execThumbnailTest(t, db.DB, `UPDATE asset_locations SET trashed=1 WHERE asset_id='asset-a'`)
	expect(7, "repeated trash update")
	execThumbnailTest(t, db.DB, `UPDATE asset_locations SET library_id='lib-b' WHERE asset_id='asset-a'`)
	expect(8, "library membership update")
	execThumbnailTest(t, db.DB, `UPDATE asset_records SET data='{"note":"updated"}' WHERE id='asset-a'`)
	expect(9, "asset data update")
	execThumbnailTest(t, db.DB, `UPDATE libraries SET data='{"name":"Renamed"}' WHERE id='lib-b'`)
	expect(10, "library data update")

	// The cascade also deletes the location row, whose delete trigger advances
	// the counter again. Multiple opaque steps per logical change are harmless.
	execThumbnailTest(t, db.DB, `DELETE FROM asset_records WHERE id='asset-a'`)
	expect(12, "asset delete")

	// Selected-image membership switches the preview through a representative,
	// so both its insert and the (currently prohibited) delete are watched.
	execThumbnailTest(t, db.DB, `INSERT INTO asset_records(id,author_id,revision,created_at,updated_at,favorite,data)
		VALUES('asset-b','author',1,'2026-01-01T00:00:00Z','2026-01-01T00:00:00Z',0,'{}')`)
	expect(13, "merge representative insert")
	execThumbnailTest(t, db.DB, `INSERT INTO asset_content_versions(asset_id,version,size,media_type) VALUES('asset-b',1,4,'image')`)
	expect(14, "merge representative content")
	execThumbnailTest(t, db.DB, `INSERT INTO asset_locations(asset_id,library_id,relative_path,file_name,current_version,trashed,storage_layout)
		VALUES('asset-b','lib-a','asset-b.png','asset-b.png',1,0,1)`)
	expect(15, "merge representative location")
	execThumbnailTest(t, db.DB, `INSERT INTO selected_image_merge_groups(representative_id,source_id,baseline_sha256,decision,applied_at)
		VALUES('asset-b','source','baseline','{}','2026-01-01T00:00:00Z')`)
	expect(15, "merge group insert")
	execThumbnailTest(t, db.DB, `INSERT INTO selected_image_merge_members(physical_asset_id,representative_id,original_asset,original_relations)
		VALUES('asset-b','asset-b','{}','{}')`)
	expect(16, "merge member insert")
	if _, err = db.DB.Exec(`DELETE FROM selected_image_merge_members WHERE physical_asset_id='asset-b'`); err == nil {
		t.Fatal("immutable selected merge member was deleted")
	}
	expect(16, "blocked merge member delete")

	execThumbnailTest(t, db.DB, `DELETE FROM libraries WHERE id='lib-b'`)
	expect(17, "library delete")
}
