package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"qimeng.local/next/server/internal/model"
)

func TestOwnerAliasesUpgradePreservesPreviousCollectionsAndSnapshot(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db, err := OpenDatabase(filepath.Join(dir, "before.sqlite"), CatalogDatabase)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	exec := func(statement string, args ...any) {
		t.Helper()
		if _, err := db.Exec(statement, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(migrationLedgerSQL)
	steps, err := readSchemaMigrations(migrations)
	if err != nil {
		t.Fatal(err)
	}
	for _, step := range steps {
		if step.version >= 39 {
			break
		}
		if err = step.apply(db); err != nil {
			t.Fatal(err)
		}
	}
	exec("INSERT INTO libraries(id,name,kind,data) VALUES('lib','Fixture','regular','{}')")
	c := model.Collection{ID: "series", LibraryID: "lib", Kind: "short_drama", Name: "Current",
		Aliases: []string{"旧称", `A"B`, `query%_\`}, Revision: 5, CreatedAt: time.Unix(1700000000, 0).UTC()}
	raw, err := Encode(c)
	if err != nil {
		t.Fatal(err)
	}
	exec("INSERT INTO collections(id,library_id,name,kind,data) VALUES(?,?,?,?,?)", c.ID, c.LibraryID, c.Name, c.Kind, raw)
	exec("INSERT INTO tags(id,name,revision,deleted,created_at,updated_at) VALUES('retired','Removed',4,1,'old','old')")
	exec("INSERT INTO retired_tag_names(name,revision) VALUES('Previous word',3)")

	exec(`INSERT INTO collections(id,library_id,name,kind,data) VALUES('invalid','lib','Malformed','image_set','{"aliases":{}}')`)
	if err = applySchema(db, migrations); err == nil {
		t.Fatal("an invalid old alias value was silently discarded")
	}
	var originalType string
	if err = db.QueryRow("SELECT json_type(data,'$.aliases') FROM collections WHERE id='invalid'").Scan(&originalType); err != nil || originalType != "object" {
		t.Fatal("failed migration changed the source value", originalType, err)
	}
	exec("DELETE FROM collections WHERE id='invalid'")
	var previousRevision int64
	if err = db.QueryRow("SELECT revision FROM backup_business_state WHERE id=1").Scan(&previousRevision); err != nil {
		t.Fatal(err)
	}
	if err = applySchema(db, migrations); err != nil {
		t.Fatal(err)
	}
	check := func(db *sql.DB) {
		t.Helper()
		got, err := Get[model.Collection](ctx, db, "SELECT data FROM collection_details WHERE id=?", c.ID)
		if err != nil || got.ID != c.ID || got.Revision != c.Revision || !slices.Equal(got.Aliases, c.Aliases) {
			t.Fatal("existing collection or its ordered aliases changed during upgrade or snapshot", got, err)
		}
		var namingRevision, businessRevision int64
		var oldCopy, activeWords, retiredSpellings int
		if err = db.QueryRow(`SELECT naming_revision,json_type(data,'$.aliases') IS NOT NULL,
			(SELECT revision FROM backup_business_state WHERE id=1),(SELECT count(*) FROM tag_details WHERE deleted=0),
			(SELECT count(*) FROM retired_tag_names WHERE name='Previous word' AND revision=3) FROM collections WHERE id=?`, c.ID).
			Scan(&namingRevision, &oldCopy, &businessRevision, &activeWords, &retiredSpellings); err != nil ||
			namingRevision != c.Revision || oldCopy != 0 || businessRevision != previousRevision || activeWords != 0 || retiredSpellings != 1 {
			t.Fatal("migration changed a naming scope, backup revision or tag tombstone, or kept a second alias list",
				namingRevision, oldCopy, businessRevision, activeWords, retiredSpellings, err)
		}
		if err = ValidateBackupSchema(ctx, db); err != nil {
			t.Fatal(err)
		}
	}
	check(db)
	snapshotFile := filepath.Join(dir, "alias-snapshot.sqlite")
	exec("VACUUM INTO ?", snapshotFile)
	snapshot, err := OpenDatabase(snapshotFile, SnapshotDatabase)
	if err != nil {
		t.Fatal(err)
	}
	defer snapshot.Close()
	check(snapshot)
}
