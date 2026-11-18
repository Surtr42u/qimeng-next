package store

import (
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

func TestMigrationHistoryIsValidatedBeforePendingSteps(t *testing.T) {
	for _, scenario := range []struct {
		name      string
		laterFile string
		laterSQL  string
		wantErr   string
	}{
		{"newer version", "0099_future.sql", "SELECT 1", "requires a newer server"},
		{"changed history", "0003_existing.sql", "SELECT 2", "checksum changed"},
		{"renamed step", "0003_previous_name.sql", "SELECT 1", "filename changed"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			db, err := OpenDatabase(filepath.Join(t.TempDir(), "history.sqlite"), CatalogDatabase)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			base := &fstest.MapFile{Data: []byte("CREATE TABLE library(id INTEGER)")}
			original := fstest.MapFS{
				"migrations/0001_base.sql":         base,
				"migrations/" + scenario.laterFile: {Data: []byte(scenario.laterSQL)},
			}
			if err := applySchema(db, original); err != nil {
				t.Fatal(err)
			}
			candidate := fstest.MapFS{
				"migrations/0001_base.sql":     base,
				"migrations/0002_pending.sql":  {Data: []byte("CREATE TABLE pending(id INTEGER)")},
				"migrations/0003_existing.sql": {Data: []byte("SELECT 1")},
			}
			if err := applySchema(db, candidate); err == nil || !strings.Contains(err.Error(), scenario.wantErr) {
				t.Fatalf("expected %q, got %v", scenario.wantErr, err)
			}
			var changed int
			err = db.QueryRow(`SELECT
				(SELECT COUNT(*) FROM sqlite_master WHERE name='pending') +
				(SELECT COUNT(*) FROM schema_migrations WHERE version=2)`).Scan(&changed)
			if err != nil || changed != 0 {
				t.Fatalf("rejected startup changed the database: %d, %v", changed, err)
			}
		})
	}
}
