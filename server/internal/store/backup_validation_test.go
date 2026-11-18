package store

import (
	"context"
	"path/filepath"
	"testing"
)

func TestBackupSchemaRejectsExtraTriggerWithAuthenticMigrationLedger(t *testing.T) {
	db, e := Open(filepath.Join(t.TempDir(), "synthetic.sqlite"))
	if e != nil {
		t.Fatal(e)
	}
	defer db.DB.Close()
	if e = ValidateBackupSchema(context.Background(), db.DB); e != nil {
		t.Fatal("fresh schema did not validate", e)
	}
	if _, e = db.DB.Exec("CREATE TRIGGER hidden_restore_write AFTER DELETE ON credentials BEGIN UPDATE settings SET value='changed' WHERE key='instance_id'; END"); e != nil {
		t.Fatal(e)
	}
	if e = ValidateBackupSchema(context.Background(), db.DB); e == nil {
		t.Fatal("a valid migration ledger authorized an extra trigger")
	}
}

func TestBackupFromExistingLedgerStillValidates(t *testing.T) {
	db, err := OpenDatabase(filepath.Join(t.TempDir(), "previous.sqlite"), CatalogDatabase)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	// Preserve the literal stored by the previous program, independently of
	// the DDL in this version. SQLite records that text in the backup.
	if _, err := db.Exec("CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY,name TEXT NOT NULL,sha256 TEXT NOT NULL)"); err != nil {
		t.Fatal(err)
	}
	if err := applySchema(db, migrations); err != nil {
		t.Fatal(err)
	}
	if err := ValidateBackupSchema(context.Background(), db); err != nil {
		t.Fatal(err)
	}
}
