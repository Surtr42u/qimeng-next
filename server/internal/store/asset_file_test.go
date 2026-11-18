package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

func TestAssetFileUpgradeRequiresCommittedEvidenceAndRetainsPendingLock(t *testing.T) {
	db, err := OpenDatabase(filepath.Join(t.TempDir(), "prior.sqlite"), CatalogDatabase)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := db.Exec(query, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(migrationLedgerSQL)
	steps, err := readSchemaMigrations(migrations)
	if err != nil {
		t.Fatal(err)
	}
	for _, step := range steps {
		if step.version >= 35 {
			break
		}
		if err = step.apply(db); err != nil {
			t.Fatal(err)
		}
	}
	exec("INSERT INTO libraries(id,name,kind,data) VALUES('library','Library','regular','{}')")
	exec("INSERT INTO authors(id,name,data) VALUES('owner','Owner','{}')")
	hash := func(s string) string { return fmt.Sprintf("%x", sha256.Sum256([]byte(s))) }
	proof := func(v int, body string) string {
		raw, err := Encode(map[string]any{"id": "asset", "hash": hash(body), "size": len(body), "contentVersion": v,
			"mediaType": "video", "mimeType": "video/mp4", "width": 160, "height": 90, "durationMs": 3000, "probeState": "ready"})
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	exec(`INSERT INTO assets(id,library_id,relative_path,file_name,media_type,author_id,hash,size,revision,created_at,updated_at,data)
		VALUES('asset','library','video.mp4','video.mp4','video','owner',?,5,7,'2026-01-01','2026-01-01',?)`, hash("sixth"), proof(6, "sixth"))
	exec("INSERT INTO asset_authors(asset_id,author_id) VALUES('asset','owner')")
	intent := func(id, state, side, record string) {
		exec("INSERT INTO file_intents(id,kind,state,data) VALUES(?,'replace',?,json_object(?,json(?)))", id, state, side, record)
	}
	intent("before", "done", "Before", proof(1, "first"))
	exec(`INSERT INTO uploads(id,batch_id,state,data) VALUES('up','batch','ingested',
		json_object('assetId','asset','publishedContentVersion',2,'hash',?,'size',6))`, hash("second"))
	intent("pending", "prepared", "After", proof(3, "unpublished"))
	intent("contradiction-one", "committed", "Before", proof(4, "different"))
	intent("contradiction-two", "done", "After", proof(4, "evidence"))
	// No evidence for v5. A current version number is insufficient to create it.
	if err = applySchema(db, migrations); err != nil {
		t.Fatal(err)
	}
	var versions string
	if err = db.QueryRow(`SELECT group_concat(version,',') FROM
		(SELECT version FROM asset_content_versions WHERE asset_id='asset' ORDER BY version)`).Scan(&versions); err != nil || versions != "1,2,6" {
		t.Fatal("migration lost evidence or invented content", versions, err)
	}
	var storedHash, path string
	var width sql.NullInt64
	if err = db.QueryRow("SELECT hash,width FROM asset_content_versions WHERE asset_id='asset' AND version=2").Scan(&storedHash, &width); err != nil || storedHash != hash("second") || width.Valid {
		t.Fatal("the old upload supplied bytes but did not measure dimensions", storedHash, width, err)
	}
	if err = db.QueryRow(`SELECT a.relative_path FROM assets a JOIN asset_authors aa ON aa.asset_id=a.id
		WHERE a.id='asset' AND aa.author_id='owner' AND json_extract(a.data,'$.contentVersion')=6`).Scan(&path); err != nil || path != "video.mp4" {
		t.Fatal("current original or author foreign key missing", path, err)
	}
	if _, err = db.Exec("UPDATE asset_records SET revision=revision+1 WHERE id='asset'"); err == nil || !strings.Contains(err.Error(), "qimeng_pending_asset") {
		t.Fatal("prepared publication lost its lock", err)
	}
	for _, statement := range []string{
		"UPDATE asset_content_versions SET size=size+1 WHERE asset_id='asset' AND version=1",
		"DELETE FROM asset_content_versions WHERE asset_id='asset' AND version=1",
		"UPDATE assets SET hash='' WHERE id='asset'",
	} {
		if _, err = db.Exec(statement); err == nil {
			t.Fatal("a second writer could alter immutable file facts", statement)
		}
	}
	if err = ValidateBackupSchema(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	exec("DELETE FROM asset_locations WHERE asset_id='asset'")
	if err = ValidateBackupSchema(context.Background(), db); err == nil {
		t.Fatal("a snapshot with no location for a known asset was accepted")
	}
}
