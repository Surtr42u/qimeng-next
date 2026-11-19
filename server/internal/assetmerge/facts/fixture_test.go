package facts_test

import (
	"crypto/sha256"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
	_ "time/tzdata"

	"qimeng.local/next/server/internal/assetmerge/facts"
	"qimeng.local/next/server/internal/legacycold/activity"
	"qimeng.local/next/server/internal/model"
	"qimeng.local/next/server/internal/store"
)

func fixtureDigest(value string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(value)))
}

func execute(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := db.ExecContext(t.Context(), query, args...); err != nil {
		t.Fatal(err)
	}
}

func value[T comparable](t *testing.T, db *sql.DB, want T, query string, args ...any) {
	t.Helper()
	var got T
	if err := db.QueryRowContext(t.Context(), query, args...).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("%s: got %v, want %v", query, got, want)
	}
}

func fixture(t *testing.T) (*store.Store, facts.Plan) {
	t.Helper()
	target, err := store.Open(filepath.Join(t.TempDir(), "derived.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { target.DB.Close() })
	old, err := store.OpenDatabase(filepath.Join(t.TempDir(), "sealed-fixture.sqlite"), store.CatalogDatabase)
	if err != nil {
		t.Fatal(err)
	}
	defer old.Close()
	schema, err := os.ReadFile("../../legacycold/activity/testdata/activity-v26.sql")
	if err != nil {
		t.Fatal(err)
	}
	execute(t, old, string(schema))
	execute(t, old, "INSERT INTO libraries VALUES('pictures','Pictures','synthetic','2025-01-01T00:00:00Z')")
	db := target.DB
	execute(t, db, `INSERT INTO libraries(id,name,kind,data) VALUES ('pictures','Pictures','regular',
		'{"id":"pictures","name":"Pictures","kind":"regular","enabled":true,"storageId":"data","rootRelativePath":"libraries/pictures"}')`)
	for _, author := range []string{"artist-a", "artist-b", "unknown"} {
		execute(t, db, "INSERT INTO authors(id,name,data) VALUES(?1,?1,json_object('id',?1,'name',?1,'kind','regular'))", author)
	}
	execute(t, db, `INSERT INTO sources VALUES('world-a','World A','{}'),('world-b','World B','{}');
		INSERT INTO characters VALUES('role-a','world-a','A','{}'),('role-b','world-b','B','{}');`)
	source := "legacy-cold-v26:" + fixtureDigest("sealed-fixture")
	bytesHash := fixtureDigest("same synthetic image payload")
	for i, id := range []string{"left", "right", "unselected", "fourth"} {
		name := id + ".png"
		rel := "album/" + name
		at := time.Date(2025, 1, 2, 3, 4, 5, i+100, time.UTC)
		a := model.Asset{ID: id, LibraryID: "pictures", FileName: name, OriginalFileName: name, RelativePath: rel,
			MediaType: "image", MimeType: "image/png", Hash: bytesHash, Size: 28, MetadataSource: "migration",
			ContentVersion: 1, Revision: 1, StorageLayoutVersion: 2, SourceMtime: &at, ContentMtime: &at, CreatedAt: at, UpdatedAt: at,
			AuthorID: "artist-a", AuthorIDs: []string{"artist-a"}, AuthorStatus: "confirmed", AuthorsRevision: 1, TagsRevision: 1,
			TagSetSource: "cold26", TagSetUpdatedAt: &at, Tags: []string{"shared", "tag-" + id}}
		if id == "left" {
			a.SourceIDs, a.CharacterIDs = []string{"world-a"}, []string{"role-a"}
		}
		if id == "right" {
			a.AuthorID, a.AuthorIDs = "artist-b", []string{"artist-b"}
			a.CharacterIDs = []string{"role-b"}
		}
		if err := store.InsertAsset(t.Context(), db, a); err != nil {
			t.Fatal(err)
		}
		execute(t, db, "INSERT INTO migration_media_aliases VALUES(?,?,?)", source, id, id)
		execute(t, db, "INSERT INTO migration_relation_origins VALUES(?,'cold26/asset',?,json_object('rowId',?,'recordedMtime',?))", source, id, i+1, at.Format(time.RFC3339Nano))
		execute(t, db, "INSERT INTO asset_author_overrides VALUES(?,?,0,'')", id, a.AuthorID)
		execute(t, old, `INSERT INTO assets(asset_id,library_id,rel_path,file_name,media_type,size_bytes,mtime,created_at,updated_at)
			VALUES(?,'pictures',?,?,'image',28,?4,?4,?4)`, id, rel, name, at.Format(time.RFC3339Nano))
	}
	execute(t, db, "INSERT INTO migration_media_aliases VALUES('another-provenance','older-right','right')")
	execute(t, old, `INSERT INTO view_events(id,asset_id,kind,session_id,started_at,seconds,client_event_id,day) VALUES
		(1,'left','open','shared','2025-03-09T06:01:02.123456789Z',NULL,'visit-left','2025-03-09'),
		(2,'right','open','shared','2025-03-09T07:01:03Z',NULL,'visit-right','2025-03-09'),
		(3,'right','open','shared','2025-03-09T04:30:00Z',NULL,'visit-legacy',NULL),
		(4,'right','dwell','watch','2025-03-09T07:01:04Z',17,'dwell-right',NULL),
		(5,'left','play','shared','2025-03-09T06:05:00Z',NULL,'play-left','2025-03-09'),
		(6,'unselected','open','shared','2025-03-09T06:07:00Z',NULL,'visit-outside','2025-03-09');
		INSERT INTO likes VALUES('left','2025-03-09','2025-03-09T04:00:00-04:00'),
		('right','2025-03-09','2025-03-09T07:59:59.123456789Z'),('right','2025-03-08','2025-03-09T04:58:00Z');
		INSERT INTO favorites VALUES('left','2025-03-09T05:00:00Z'),('right','2025-03-09T06:00:01.123Z');
		INSERT INTO daily_shown VALUES('left','2025-03-09',4),('right','2025-03-09',7),('unselected','2025-03-09',3);
		INSERT INTO timeline_tags(id,asset_id,time_millis,name,created_at,color) VALUES
		('mark-a','left',9,'same','2025-03-09T05:02:01Z',''),('mark-b','right',9,'same','2025-03-09T05:02:02Z','')`)
	zone, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	tx, err := db.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err := activity.Import(t.Context(), old, tx, source, zone); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	size, version := int64(28), int64(1)
	plan := facts.Plan{BaselineSHA256: fixtureDigest("sealed native archive"), SourceID: source, Groups: []facts.Group{{
		LibraryID: "pictures", RepresentativeID: "left", RelativePath: "album/Reviewed.png", SHA256: bytesHash,
		Members: []facts.Member{{AssetID: "right", SourceKeys: []string{"post-b"}, ExpectedSize: &size}, {AssetID: "left", ExpectedRelativePath: "album/left.png", ExpectedVersion: &version}},
		Sources: []facts.Source{{Key: "post-a", URL: "https://gallery.example/post/a", At: "2025-02-02T01:02:03+09:00", Caption: "Group clue"},
			{Key: "post-b", URL: "https://gallery.example/post/b", At: "2025-02-03T00:01:02Z", MediaURL: "https://gallery.example/b.png"}},
	}}}
	return target, plan
}
