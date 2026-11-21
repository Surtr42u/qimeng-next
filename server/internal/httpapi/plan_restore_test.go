package httpapi_test

import (
	"context"
	"os"
	"path"
	"path/filepath"
	"testing"

	"qimeng.local/next/server/internal/httpapi"
	"qimeng.local/next/server/internal/model"
	"qimeng.local/next/server/internal/service"
)

func TestPartialPlanAdvancesWithSameKeyOnOriginalAndRestoredInstance(t *testing.T) {
	f := setup(t)
	var batch model.Batch
	decode(t, f.json("POST", "/batches", "durable-plan-original-selection", map[string]any{"libraryId": f.library, "mode": "intelligent", "label": "Synthetic recoverable plan"}, 201), &batch)
	ids := f.declareInputs(batch.ID, "first.png", "second.png")
	items := []map[string]any{}
	for index, name := range []string{"first.png", "second.png"} {
		u, _ := f.upload(name, uint8(151+index), map[string]any{"batchId": batch.ID, "inputId": ids[name], "mode": "intelligent"})
		if out := f.complete(u); out.Upload.State != "staged" {
			t.Fatal("missing staged input")
		}
		items = append(items, map[string]any{"uploadId": u.ID, "family": []string{"First work", "Second work"}[index]})
	}
	var detail model.BatchDetail
	decode(t, f.json("GET", "/batches/"+batch.ID, "", nil, 200), &detail)
	var plan model.Plan
	decode(t, f.json("POST", "/batches/"+batch.ID+"/plans", "prepare-planned-publication", map[string]any{"expectedRevision": detail.Batch.Revision, "items": items}, 201), &plan)
	_ = importList(t, f, "declared-planned-first-name", "author.txt", "1  Declared creator\n作品\n"+plan.Items[0].FileName+"\n", 0)
	obstacle := path.Join("libraries", f.library, plan.Items[1].RelativePath)
	file, e := f.s.Root.OpenFile(obstacle, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		t.Fatal(e)
	}
	file.Close()
	url := "/batches/" + batch.ID + "/plans/" + plan.ID + "/commit"
	key := "original-operation-after-recovery"
	body := map[string]any{"fingerprint": plan.Fingerprint}
	f.json("POST", url, "bad", body, 400)
	var count int
	if e = f.s.DB.DB.QueryRow("SELECT count(*) FROM assets").Scan(&count); e != nil || count != 0 {
		t.Fatal("invalid operation key published media")
	}
	decode(t, f.json("POST", url, key, body, 200), &detail)
	if detail.Batch.State != "partial" || detail.Items[0].Upload.State != "ingested" || detail.Items[1].Upload.State != "staged" || detail.Items[1].Upload.ErrorCode != "path_occupied" {
		t.Fatalf("independent partial result lost: %+v", detail)
	}
	firstID := detail.Items[0].Upload.AssetID
	var backup service.Backup
	decode(t, f.json("POST", "/backups", "checkpoint-partially-applied-plan", struct{}{}, 201), &backup)
	// The interrupted plan must resume its committed upload prefix. That byte
	// exists only in a full archive, so build the internal full archive instead
	// of the user-facing records export.
	_, archiveHash, e := f.s.WriteFullBackupArchive(context.Background(), backup.ID, "backups/"+backup.ID+"/archive.full.qnext")
	if e != nil {
		t.Fatal(e)
	}
	destination := filepath.Join(t.TempDir(), "resumed")
	_, e = service.RestoreBackup(context.Background(), service.RestoreOptions{ArchivePath: filepath.Join(f.s.Config.DataDir, "backups", backup.ID, "archive.full.qnext"), ExpectedSHA256: archiveHash, Destination: destination, ReserveBytes: 1})
	if e != nil {
		t.Fatal(e)
	}
	restored, e := service.New(service.Config{DataDir: destination, AdminToken: testSecret, ReserveBytes: 1})
	if e != nil {
		t.Fatal(e)
	}
	defer restored.Close()
	if e = f.s.Root.Remove(obstacle); e != nil {
		t.Fatal(e)
	}
	copyFixture := &fixture{t: t, s: restored, h: httpapi.New(restored), library: f.library}
	for _, instance := range []*fixture{f, copyFixture} {
		var resumed model.BatchDetail
		decode(t, instance.json("POST", url, key, body, 200), &resumed)
		if resumed.Batch.State != "completed" || len(resumed.Items) != 2 || resumed.Items[0].Upload.AssetID != firstID || resumed.Items[1].Upload.State != "ingested" {
			t.Fatalf("same operation did not recover remaining item: %+v", resumed)
		}
		var got model.Asset
		decode(t, instance.json("GET", "/assets/"+firstID, "", nil, 200), &got)
		if got.AuthorName != "Declared creator" {
			t.Fatal("recovery discarded published derived authors")
		}
	}
	var second service.Backup
	decode(t, copyFixture.json("POST", "/backups", "second-generation-checkpoint", struct{}{}, 201), &second)
	_, secondHash, e := restored.WriteFullBackupArchive(context.Background(), second.ID, "backups/"+second.ID+"/archive.full.qnext")
	if e != nil {
		t.Fatal(e)
	}
	next := filepath.Join(t.TempDir(), "second-restore")
	report, e := service.RestoreBackup(context.Background(), service.RestoreOptions{ArchivePath: filepath.Join(destination, "backups", second.ID, "archive.full.qnext"), ExpectedSHA256: secondHash, Destination: next, ReserveBytes: 1})
	if e != nil || report.AssetCount != 2 {
		t.Fatal("second generation lost committed results", report, e)
	}
	third, e := service.New(service.Config{DataDir: next, AdminToken: testSecret, ReserveBytes: 1})
	if e != nil {
		t.Fatal(e)
	}
	defer third.Close()
	state, e := third.Asset(context.Background(), firstID)
	if e != nil || state.AuthorName != "Declared creator" {
		t.Fatal("source creator identity lost across two restores", e)
	}
}
