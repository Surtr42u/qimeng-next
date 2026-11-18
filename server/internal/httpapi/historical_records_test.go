package httpapi_test

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"qimeng.local/next/server/internal/service"
)

// Seed the recorded Next facts directly. A working legacy importer is not a
// prerequisite for testing todays votes or rankings on an upgraded database.
func seedRecordedLikes(t *testing.T, f *fixture, asset string, undated int64, lastDay string) {
	t.Helper()
	total := undated
	if lastDay != "" {
		total++
		if _, err := f.s.DB.DB.Exec("INSERT INTO likes(asset_id,day,created_at) VALUES(?,?,?)", asset, lastDay, lastDay+"T12:00:00Z"); err != nil { t.Fatal(err) }
	}
	if _, err := f.s.DB.DB.Exec("INSERT INTO like_baselines(asset_id,source_id,source_key,source_count,unattributed_count,last_day,observed_day,import_id) VALUES(?,?,?,?,?,?,?,?)", asset, "earlier-instance", "record", total, undated, lastDay, time.Now().Format(time.DateOnly), "archived-input"); err != nil { t.Fatal(err) }
}

func TestOrdinaryServiceRetiresV2WritesAndReadsEarlierRecords(t *testing.T) {
	f := setup(t)
	// Persisted in a previous Next database. The public reader works even when
	// the retired parser cannot interpret this synthetic document.
	archive := service.V2Import{ID: "archived-input", SourceID: "earlier-install", SHA256: fmt.Sprintf("%x", sha256.Sum256([]byte("preserved bytes"))), Bytes: 15, CreatedAt: time.Now().UTC()}
	plan := service.V2MigrationPlan{ID: "archived-plan", ImportID: archive.ID, Fingerprint: "historical-fingerprint", State: "committed", CreatedAt: archive.CreatedAt}
	rawInput, err := json.Marshal(archive)
	if err != nil { t.Fatal(err) }
	rawPlan, err := json.Marshal(plan)
	if err != nil { t.Fatal(err) }
	db := f.s.DB.DB
	if _, err = db.Exec("INSERT INTO migration_inputs(id,source_id,sha256,created_at,data,original) VALUES(?,?,?,?,?,?)", archive.ID, archive.SourceID, archive.SHA256, archive.CreatedAt.Format(time.RFC3339Nano), string(rawInput), []byte("preserved bytes")); err != nil { t.Fatal(err) }
	if _, err = db.Exec("INSERT INTO migration_plans(id,input_id,created_at,data) VALUES(?,?,?,?)", plan.ID, archive.ID, plan.CreatedAt.Format(time.RFC3339Nano), string(rawPlan)); err != nil { t.Fatal(err) }
	if got := f.request("GET", "/migration/v2/imports/"+archive.ID+"/document", "", testSecret, nil, nil); got.Code != 200 || got.Body.String() != "preserved bytes" { t.Fatal("historical document bytes changed", got.Code) }
	f.json("GET", "/migration/v2/imports/"+archive.ID, "", nil, 200)
	f.json("GET", "/migration/v2/plans/"+plan.ID, "", nil, 200)
	response := f.request("POST", "/migration/v2/imports?sourceId=synthetic-installation", "retired-stage", testSecret, []byte("{}"), map[string]string{"Content-Type": "application/octet-stream"})
	if response.Code != 410 {
		t.Fatalf("retired stage status = %d", response.Code)
	}
	f.json("POST", "/migration/v2/imports/"+archive.ID+"/plans", "retired-prepare", map[string]any{}, 410)
	f.json("POST", "/migration/v2/plans/"+plan.ID+"/commit", "retired-commit", map[string]any{"fingerprint": plan.Fingerprint, "acceptSkipped": false}, 410)
}
