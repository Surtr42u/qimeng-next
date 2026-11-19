package legacycold

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"qimeng.local/next/server/internal/legacycold/catalog"
	"qimeng.local/next/server/internal/store"
)

func syntheticConversion(t *testing.T) Options {
	t.Helper()
	fixture := os.Getenv("QIMENG_COLD_SYNTHETIC_FIXTURE")
	if fixture == "" {
		t.Skip("the cloud conversion workflow supplies the synthetic v26 database")
	}
	source := filepath.Join(fixture, "sealed")
	output := filepath.Join(t.TempDir(), "attempt")
	return Options{SourceDB: filepath.Join(source, "old-data", "v26.sqlite"), Roots: filepath.Join(source, "roots.json"),
		TrashRoot: filepath.Join(source, "old-data", "trash"), Output: output, SourceTimezone: "Asia/Shanghai", ReserveBytes: 1}
}

func TestCancelAfterFirstArchivePayloadKeepsOnlyFailureCoverage(t *testing.T) {
	opt := syntheticConversion(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var callbacks, streamed int
	checkPrepared := func(ctx context.Context, _ *sql.DB, tx *sql.Tx, sourceID string) error {
		var count int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM migration_media_aliases m JOIN asset_content_versions v
			ON v.asset_id=m.asset_id AND v.version=1 WHERE m.source_id=? AND m.source_asset_id=v.asset_id AND length(v.hash)=64`, sourceID).Scan(&count); err != nil {
			return err
		}
		if count != 3 {
			return errors.New("projection did not receive all three original identities")
		}
		callbacks++
		return nil
	}
	prepare := func(ctx context.Context, source *sql.DB, tx *sql.Tx, sourceID string) (func() error, error) {
		finish, err := catalog.Prepare(ctx, source, tx, sourceID)
		if err != nil {
			return nil, err
		}
		return func() error {
			if err := finish(); err != nil {
				return err
			}
			return checkPrepared(ctx, source, tx, sourceID)
		}, nil
	}
	opt.Progress = func(stage string, files int, _ int64) {
		if stage == "archiving_originals" && files == 1 {
			streamed++
			cancel()
		}
	}
	_, err := Convert(ctx, opt, prepare, checkPrepared)
	raw, readErr := os.ReadFile(filepath.Join(opt.Output, "coverage.json"))
	var report Report
	if readErr != nil || json.Unmarshal(raw, &report) != nil {
		t.Fatalf("private failure report missing: %v, conversion %v", readErr, err)
	}
	if callbacks != 2 || streamed != 1 || err == nil || err.Error() != "cancelled" || report.Problem.Code != "cancelled" {
		t.Fatalf("unexpected stop: %v, stage %s, reason %+v", err, report.Stage, report.Problem)
	}
	names, err := os.ReadDir(opt.Output)
	if err != nil || len(names) != 1 || names[0].Name() != "coverage.json" {
		t.Fatalf("incomplete output published: %v", err)
	}
}

func TestCatalogMetadataFailsBeforeOriginalFileAccess(t *testing.T) {
	for _, tc := range []struct {
		name, problem, section string
	}{
		{"valid metadata still requires every original", "source_file_missing", ""},
		{"invalid TXT source", "catalog_projection_failed", "kv_settings"},
		{"invalid structured author profile", "catalog_projection_failed", "creator_profiles"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			opt := syntheticConversion(t)
			original, err := os.ReadFile(opt.SourceDB)
			if err != nil {
				t.Fatal(err)
			}
			opt.SourceDB = filepath.Join(t.TempDir(), "sealed.sqlite")
			if err := os.WriteFile(opt.SourceDB, original, 0600); err != nil {
				t.Fatal(err)
			}
			db, err := store.OpenDatabase(opt.SourceDB, store.CatalogDatabase)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { db.Close() })
			if _, err = db.Exec("PRAGMA journal_mode=DELETE"); err != nil {
				t.Fatal(err)
			}
			var library string
			if err = db.QueryRow("SELECT id FROM libraries").Scan(&library); err != nil {
				t.Fatal(err)
			}
			switch tc.section {
			case "kv_settings":
				_, err = db.Exec(`INSERT OR REPLACE INTO kv_settings VALUES('imported_txt_sources',
					'[{"filename":"test.txt","content":"TXT: access_token=fixture"}]','2024-01-02T00:00:00Z')`)
			case "creator_profiles":
				_, err = db.Exec(`INSERT OR REPLACE INTO kv_settings SELECT 'creator_profile:'||id,
					json_object('authorId',id,'status','candidate','revision',1,'homepageUrl','https://artist.example/?sig=fixture'),
					created_at FROM authors`)
			}
			if err != nil {
				t.Fatal(err)
			}
			if err = db.Close(); err != nil {
				t.Fatal(err)
			}
			// Any access to the first recorded original must fail. This makes
			// the order observable independently of progress callbacks.
			selection, err := json.Marshal(map[string]string{library: t.TempDir()})
			if err != nil {
				t.Fatal(err)
			}
			opt.Roots = filepath.Join(t.TempDir(), "roots.json")
			if err := os.WriteFile(opt.Roots, selection, 0600); err != nil {
				t.Fatal(err)
			}
			var originals bool
			opt.Progress = func(stage string, _ int, _ int64) { originals = originals || stage == "checking_originals" }
			unreachable := func(context.Context, *sql.DB, *sql.Tx, string) error {
				t.Fatal("activity was reached with missing original files")
				return nil
			}
			_, err = Convert(t.Context(), opt, catalog.Prepare, unreachable)
			var reason *issue
			if !errors.As(err, &reason) || reason.Code != tc.problem || reason.Section != tc.section ||
				(tc.section != "" && reason.Reason != "nonportable_business_value") || originals != (tc.section == "") {
				t.Fatalf("unexpected conversion order: %v %+v originals=%v", err, reason, originals)
			}
			names, err := os.ReadDir(opt.Output)
			if err != nil || len(names) != 1 || names[0].Name() != "coverage.json" {
				t.Fatal("incomplete conversion published a business archive")
			}
		})
	}
}
