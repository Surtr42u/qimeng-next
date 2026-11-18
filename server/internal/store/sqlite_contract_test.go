package store

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func TestConnectionProfilesPreserveDurabilityAndReadOnlySnapshots(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "database #1.sqlite")
	catalog, err := Open(filename)
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.DB.Close()
	var mode string
	var foreignKeys, synchronous, timeout int
	if err := catalog.DB.QueryRow(`SELECT (SELECT journal_mode FROM pragma_journal_mode), (SELECT foreign_keys FROM pragma_foreign_keys), (SELECT synchronous FROM pragma_synchronous), (SELECT timeout FROM pragma_busy_timeout)`).Scan(&mode, &foreignKeys, &synchronous, &timeout); err != nil {
		t.Fatal(err)
	}
	if mode != "wal" || foreignKeys != 1 || synchronous != 2 || timeout != 5000 {
		t.Fatalf("unexpected catalog connection profile: %s %d %d %d", mode, foreignKeys, synchronous, timeout)
	}
	var matched, excluded int
	var nullTime sql.NullString
	if err := catalog.DB.QueryRow(`SELECT qimeng_search_token('作品 11.png','11'), qimeng_search_token('作品 110.png','11'), qimeng_time_key(NULL)`).Scan(&matched, &excluded, &nullTime); err != nil || matched != 1 || excluded != 0 || nullTime.Valid {
		t.Fatalf("shared browse functions differ: %v", err)
	}
	snapshot, err := OpenDatabase(filename, SnapshotDatabase)
	if err != nil {
		t.Fatal(err)
	}
	defer snapshot.Close()
	var trust int
	if err := snapshot.QueryRow(`PRAGMA trusted_schema`).Scan(&trust); err != nil || trust != 0 {
		t.Fatalf("snapshot schema trust changed: %v", err)
	}
	if _, err := snapshot.Exec(`CREATE TABLE forbidden(id)`); err == nil {
		t.Fatal("snapshot connection accepted a write")
	}
}
