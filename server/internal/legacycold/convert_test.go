package legacycold

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestCancelAfterFirstArchivePayloadKeepsOnlyFailureCoverage(t *testing.T) {
	fixture := os.Getenv("QIMENG_COLD_SYNTHETIC_FIXTURE")
	if fixture == "" {
		t.Skip("the cloud conversion workflow supplies the synthetic v26 database")
	}
	source := filepath.Join(fixture, "sealed")
	output := filepath.Join(t.TempDir(), "attempt")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	opt := Options{SourceDB: filepath.Join(source, "old-data", "v26.sqlite"), Roots: filepath.Join(source, "roots.json"),
		TrashRoot: filepath.Join(source, "old-data", "trash"), Output: output, SourceTimezone: "Asia/Shanghai", ReserveBytes: 1}
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
	opt.Progress = func(stage string, files int, _ int64) {
		if stage == "archiving_originals" && files == 1 {
			streamed++
			cancel()
		}
	}
	_, err := Convert(ctx, opt, checkPrepared, checkPrepared)
	raw, readErr := os.ReadFile(filepath.Join(output, "coverage.json"))
	var report Report
	if readErr != nil || json.Unmarshal(raw, &report) != nil {
		t.Fatalf("private failure report missing: %v, conversion %v", readErr, err)
	}
	if callbacks != 2 || streamed != 1 || err == nil || err.Error() != "cancelled" || report.Problem.Code != "cancelled" {
		t.Fatalf("unexpected stop: %v, stage %s, reason %+v", err, report.Stage, report.Problem)
	}
	names, err := os.ReadDir(output)
	if err != nil || len(names) != 1 || names[0].Name() != "coverage.json" {
		t.Fatalf("incomplete output published: %v", err)
	}
}
