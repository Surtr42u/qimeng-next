package httpapi_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"qimeng.local/next/server/internal/backup"
	"qimeng.local/next/server/internal/httpapi"
	"qimeng.local/next/server/internal/model"
	"qimeng.local/next/server/internal/service"
	"qimeng.local/next/server/internal/store"
)

func TestNativeBackupChecksRecordedFileTimes(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	modified := time.Date(2024, time.January, 8, 16, 0, 0, 0, time.UTC)
	u, _ := f.upload("dated-original.png", 166, map[string]any{"sourceMtime": modified})
	known := f.complete(u).Asset
	u, _ = f.upload("historical-original.png", 167, nil)
	historical := f.complete(u).Asset
	if _, err := f.s.DB.DB.Exec("UPDATE asset_locations SET content_mtime=NULL WHERE asset_id=?", historical.ID); err != nil {
		t.Fatal(err)
	}
	var checkpoint service.Backup
	decode(t, f.json("POST", "/backups", "mtime-checkpoint", struct{}{}, 201), &checkpoint)
	f.json("POST", "/backups/"+checkpoint.ID+"/export", "mtime-export", struct{}{}, 202)
	if exported := awaitBackup(t, f, checkpoint.ID); exported.ExportState != "completed" {
		t.Fatalf("export: %+v", exported)
	}
	file, err := os.Open(filepath.Join(f.s.Config.DataDir, "backups", checkpoint.ID, "archive.qnext"))
	if err != nil {
		t.Fatal(err)
	}
	payloads := map[string][]byte{}
	base, err := backup.Read(ctx, file, func(entry backup.File, reader io.Reader) error {
		body, err := io.ReadAll(reader)
		payloads[entry.StoragePath] = body
		return err
	})
	file.Close()
	if err != nil {
		t.Fatal(err)
	}
	// The portable export is records-only. Build a legacy full manifest from
	// the same checkpoint to keep the original-time compatibility test alive.
	storagePath := func(a *model.Asset) string { return "libraries/" + a.LibraryID + "/" + a.RelativePath }
	original := func(a *model.Asset) []byte {
		input, e := f.s.Root.Open(storagePath(a))
		if e != nil {
			t.Fatal(e)
		}
		defer input.Close()
		body, e := io.ReadAll(input)
		if e != nil {
			t.Fatal(e)
		}
		return body
	}
	payloads[storagePath(known)] = original(known)
	payloads[storagePath(historical)] = original(historical)
	base.Scope = ""
	base.Files = []backup.File{
		{Kind: "asset", ObjectID: known.ID, ContentVersion: known.ContentVersion, StoragePath: storagePath(known), SHA256: known.Hash, Size: known.Size},
		{Kind: "asset", ObjectID: historical.ID, ContentVersion: historical.ContentVersion, StoragePath: storagePath(historical), SHA256: historical.Hash, Size: historical.Size},
	}
	withOffset := modified.In(time.FixedZone("fixture", 8*60*60))
	conflicting := modified.Add(time.Minute)
	for _, test := range []struct {
		name       string
		manifestAt *time.Time
		valid      bool
	}{
		{"equivalent offset", &withOffset, true},
		{"absent in manifest", nil, true},
		{"conflicting instant", &conflicting, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			manifest := base
			manifest.Files = slices.Clone(base.Files)
			for i, entry := range manifest.Files {
				switch entry.ObjectID {
				case known.ID:
					manifest.Files[i].ModifiedAt = test.manifestAt
				case historical.ID:
					manifest.Files[i].ModifiedAt = &modified
				}
			}
			var raw bytes.Buffer
			if err := backup.Write(ctx, &raw, manifest, func(entry backup.File) (io.ReadCloser, error) {
				return io.NopCloser(bytes.NewReader(payloads[entry.StoragePath])), nil
			}, nil); err != nil {
				t.Fatal(err)
			}
			hash := sha256.Sum256(raw.Bytes())
			expected := hex.EncodeToString(hash[:])
			root := t.TempDir()
			source, destination := filepath.Join(root, "fixture.qnext"), filepath.Join(root, "restored")
			if err := os.WriteFile(source, raw.Bytes(), 0600); err != nil {
				t.Fatal(err)
			}
			verified, verifyErr := service.VerifyBackup(ctx, source, expected)
			restored, restoreErr := service.RestoreBackup(ctx, service.RestoreOptions{ArchivePath: source, Destination: destination, ExpectedSHA256: expected, ReserveBytes: 1})
			if !test.valid {
				for _, err := range []error{verifyErr, restoreErr} {
					if err == nil || !strings.Contains(err.Error(), "asset modification time does not match snapshot") {
						t.Fatalf("expected timestamp disagreement with valid archive SHA-256, got %v", err)
					}
				}
				if _, err := os.Stat(filepath.Join(destination, "qimeng.sqlite")); !os.IsNotExist(err) {
					t.Fatalf("conflicting archive published a database: %v", err)
				}
				if _, err := os.Stat(filepath.Join(destination, "RESTORE_INCOMPLETE")); err != nil {
					t.Fatal(err)
				}
				return
			}
			if verifyErr != nil || restoreErr != nil || verified.ArchiveHash != expected || restored.ArchiveHash != expected {
				t.Fatalf("compatible archive failed: verify %v, restore %v", verifyErr, restoreErr)
			}
			for _, entry := range manifest.Files {
				info, err := os.Stat(filepath.Join(destination, filepath.FromSlash(entry.StoragePath)))
				if err != nil {
					t.Fatal(err)
				}
				if !info.ModTime().Equal(modified) {
					t.Fatalf("original modification time was %s, want %s", info.ModTime(), modified)
				}
			}
		})
	}
}

func awaitBackup(t *testing.T, f *fixture, id string) service.Backup {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		r := f.json("GET", "/backups/"+id, "", nil, 200)
		var b service.Backup
		decode(t, r, &b)
		if b.ExportState == "completed" || b.ExportState == "failed" || b.ExportState == "interrupted" {
			return b
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("backup did not reach durable state")
	return service.Backup{}
}
func TestRecordsBackupRestoresCatalogueWithoutOriginals(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	u, _ := f.upload("records-live.png", 171, nil)
	live := *f.complete(u).Asset
	f.json("PUT", "/assets/"+live.ID+"/favorite", "records-favorite", map[string]any{"favorite": true}, 200)
	r := f.json("PUT", "/assets/"+live.ID+"/like/today", "records-today-like", map[string]any{"liked": true}, 200)
	decode(t, r, &live)
	u, _ = f.upload("records-trash.png", 172, nil)
	trashed := *f.complete(u).Asset
	f.json("DELETE", "/assets/"+trashed.ID+"?expectedRevision=1", "records-trash", struct{}{}, 200)
	r = f.json("POST", "/uploads", "records-pending-upload", map[string]any{"libraryId": f.library, "fileName": "pending.png", "size": 10, "mimeType": "image/png", "mode": "ordinary"}, 201)
	var pending model.Upload
	decode(t, r, &pending)
	if response := f.request("PUT", "/uploads/"+pending.ID+"/content", "", testSecret, []byte("part"), map[string]string{"Content-Type": "application/octet-stream", "Upload-Offset": "0"}); response.Code != 200 {
		t.Fatalf("partial upload: %s", response.Body.String())
	}
	r = f.json("GET", "/backups/export-estimate", "", nil, 200)
	var estimate service.BackupExportEstimate
	decode(t, r, &estimate)
	if estimate.Scope != "metadata" || estimate.OriginalBytes != 0 || estimate.OriginalFiles != 0 || estimate.SnapshotBytes <= 0 {
		t.Fatalf("records estimate: %+v", estimate)
	}
	r = f.json("POST", "/backups", "records-backup", struct{}{}, 201)
	var b service.Backup
	decode(t, r, &b)
	if b.Scope != "metadata" {
		t.Fatalf("checkpoint scope: %+v", b)
	}
	f.json("POST", "/backups/"+b.ID+"/export", "records-export", struct{}{}, 202)
	b = awaitBackup(t, f, b.ID)
	if b.ExportState != "completed" || b.Scope != "metadata" || b.TotalFiles != 0 || b.ArchiveBytes <= 0 {
		t.Fatalf("records export: %+v", b)
	}
	source := filepath.Join(f.s.Config.DataDir, "backups", b.ID, "archive.qnext")
	file, e := os.Open(source)
	if e != nil {
		t.Fatal(e)
	}
	manifest, e := backup.Read(ctx, file, nil)
	file.Close()
	if e != nil || manifest.Scope != backup.ScopeMetadata || len(manifest.Files) != 0 {
		t.Fatalf("records manifest: %v %+v", e, manifest)
	}
	verified, e := service.VerifyBackup(ctx, source, b.ArchiveHash)
	if e != nil || verified.OriginalsIncluded || verified.MissingOriginals != 3 || verified.AssetCount != 0 {
		t.Fatalf("records verify: %v %+v", e, verified)
	}
	destination := filepath.Join(t.TempDir(), "records-restored")
	report, e := service.RestoreBackup(ctx, service.RestoreOptions{ArchivePath: source, Destination: destination, ExpectedSHA256: b.ArchiveHash, ReserveBytes: 1})
	if e != nil || report.OriginalsIncluded || report.MissingOriginals != 3 || report.AssetCount != 0 {
		t.Fatalf("records restore: %v %+v", e, report)
	}
	restored, e := service.New(service.Config{DataDir: destination, AdminToken: strings.Repeat("r", 32), ReserveBytes: 1})
	if e != nil {
		t.Fatal(e)
	}
	defer restored.Close()
	actual, e := restored.Asset(ctx, live.ID)
	if e != nil || !actual.Favorite || actual.Hash != live.Hash || actual.LikeCount != live.LikeCount {
		t.Fatal("records restore changed metadata")
	}
	deleted, e := restored.Asset(ctx, trashed.ID)
	if e != nil || !deleted.Trashed {
		t.Fatal("records restore lost trash identity")
	}
	var active, cancelled int
	if e = restored.DB.DB.QueryRow("SELECT COUNT(*) FROM uploads WHERE state!='ingested' AND state!='cancelled'").Scan(&active); e != nil || active != 0 {
		t.Fatalf("records restore left a resumable upload row: %v %d", e, active)
	}
	if e = restored.DB.DB.QueryRow("SELECT COUNT(*) FROM uploads WHERE state='cancelled'").Scan(&cancelled); e != nil || cancelled != 1 {
		t.Fatalf("records restore did not mark the lost upload: %v %d", e, cancelled)
	}
	if _, e = os.Stat(filepath.Join(destination, "libraries", live.LibraryID, filepath.FromSlash(live.RelativePath))); !os.IsNotExist(e) {
		t.Fatalf("records restore wrote original media: %v", e)
	}
	if _, e = service.RestoreBackup(ctx, service.RestoreOptions{ArchivePath: source, Destination: destination, ReserveBytes: 1}); e == nil {
		t.Fatal("existing instance overwritten")
	}
}
func TestRecordsRestoreKeepsReferencedUploadRowsUnresumable(t *testing.T) {
	f := setup(t)
	var batch model.Batch
	decode(t, f.json("POST", "/batches", "records-upload-batch", map[string]any{"libraryId": f.library, "mode": "intelligent"}, 201), &batch)
	ids := f.declareInputs(batch.ID, "staged.png")
	upload, _ := f.upload("staged.png", 176, map[string]any{"batchId": batch.ID, "inputId": ids["staged.png"], "mode": "intelligent"})
	if out := f.complete(upload); out.Upload.State != "staged" {
		t.Fatalf("expected staged input: %+v", out.Upload)
	}
	r := f.json("POST", "/backups", "records-upload-checkpoint", struct{}{}, 201)
	var b service.Backup
	decode(t, r, &b)
	f.json("POST", "/backups/"+b.ID+"/export", "records-upload-export", struct{}{}, 202)
	b = awaitBackup(t, f, b.ID)
	if b.ExportState != "completed" {
		t.Fatalf("records export: %+v", b)
	}
	destination := filepath.Join(t.TempDir(), "records-upload-restored")
	report, e := service.RestoreBackup(context.Background(), service.RestoreOptions{ArchivePath: filepath.Join(f.s.Config.DataDir, "backups", b.ID, "archive.qnext"), Destination: destination, ExpectedSHA256: b.ArchiveHash, ReserveBytes: 1})
	if e != nil {
		t.Fatalf("records restore with referenced upload: %v", e)
	}
	restored, e := service.New(service.Config{DataDir: destination, AdminToken: testSecret, ReserveBytes: 1})
	if e != nil {
		t.Fatal(e)
	}
	defer restored.Close()
	var state string
	if e = restored.DB.DB.QueryRow("SELECT state FROM uploads WHERE id=?", upload.ID).Scan(&state); e != nil || state != "cancelled" {
		t.Fatalf("lost upload row state: %v %s", e, state)
	}
	var referenced int
	if e = restored.DB.DB.QueryRow("SELECT COUNT(*) FROM batch_inputs WHERE upload_id=?", upload.ID).Scan(&referenced); e != nil || referenced != 1 {
		t.Fatalf("referenced upload row disappeared: %v %d", e, referenced)
	}
	restoredFixture := &fixture{t: t, s: restored, h: httpapi.New(restored), library: f.library}
	response := restoredFixture.request("PUT", "/uploads/"+upload.ID+"/content", "", testSecret, []byte("more"), map[string]string{"Content-Type": "application/octet-stream", "Upload-Offset": "0"})
	if response.Code != 409 {
		t.Fatalf("cancelled restored upload accepted bytes: %d %s", response.Code, response.Body.String())
	}
	if report.MissingOriginals != 1 {
		t.Fatalf("records restore did not report the lost prefix: %+v", report)
	}
}
func TestRecordsArchiveScrubsCredentialsAndRequestBodies(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	u, _ := f.upload("scrub-original.png", 183, nil)
	asset := *f.complete(u).Asset
	if _, e := f.s.DB.DB.Exec("INSERT INTO credentials(id,token_hash,expires_at,data) VALUES('synthetic-credential','synthetic-token-hash','2099-01-01T00:00:00Z','{}')"); e != nil {
		t.Fatal(e)
	}
	if _, e := f.s.DB.DB.Exec("INSERT INTO idempotency(principal_id,operation,key,input_hash,response,status) VALUES('synthetic','POST /api/v1/credentials','synthetic-key','synthetic-hash','{\"token\":\"synthetic-secret\"}',201)"); e != nil {
		t.Fatal(e)
	}
	r := f.json("POST", "/backups", "scrub-checkpoint", struct{}{}, 201)
	var b service.Backup
	decode(t, r, &b)
	f.json("POST", "/backups/"+b.ID+"/export", "scrub-export", struct{}{}, 202)
	b = awaitBackup(t, f, b.ID)
	if b.ExportState != "completed" {
		t.Fatalf("scrub export: %+v", b)
	}
	var liveCredentials int
	if e := f.s.DB.DB.QueryRow("SELECT COUNT(*) FROM credentials").Scan(&liveCredentials); e != nil || liveCredentials != 1 {
		t.Fatalf("live credential was touched: %v %d", e, liveCredentials)
	}
	file, e := os.Open(filepath.Join(f.s.Config.DataDir, "backups", b.ID, "archive.qnext"))
	if e != nil {
		t.Fatal(e)
	}
	defer file.Close()
	snapshot := filepath.Join(t.TempDir(), "snapshot.sqlite")
	manifest, e := backup.Read(ctx, file, func(entry backup.File, reader io.Reader) error {
		if entry.Kind != "snapshot" {
			return nil
		}
		out, e := os.OpenFile(snapshot, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if e != nil {
			return e
		}
		_, copyErr := io.Copy(out, reader)
		closeErr := out.Close()
		return errors.Join(copyErr, closeErr)
	})
	if e != nil || manifest.Scope != backup.ScopeMetadata {
		t.Fatalf("scrub archive: %v %+v", e, manifest)
	}
	copyDB, e := store.OpenDatabase(snapshot, store.SnapshotDatabase)
	if e != nil {
		t.Fatal(e)
	}
	defer copyDB.Close()
	for _, table := range []string{"credentials", "idempotency"} {
		var count int
		if e = copyDB.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); e != nil || count != 0 {
			t.Fatalf("%s survived in the archive snapshot: %v %d", table, e, count)
		}
	}
	var assets int
	var instance string
	if e = copyDB.QueryRow("SELECT COUNT(*) FROM assets").Scan(&assets); e != nil || assets != 1 {
		t.Fatalf("archive snapshot lost its catalogue: %v %d", e, assets)
	}
	if e = copyDB.QueryRow("SELECT value FROM settings WHERE key='instance_id'").Scan(&instance); e != nil || instance != f.s.InstanceID {
		t.Fatalf("archive snapshot lost the instance identity: %v", e)
	}
	var assetID string
	if e = copyDB.QueryRow("SELECT id FROM assets WHERE id=?", asset.ID).Scan(&assetID); e != nil {
		t.Fatal("archive snapshot lost the asset row")
	}
}
func TestBackupExportRefusesToStartWithoutSpaceForTheArchive(t *testing.T) {
	f := setup(t)
	u, _ := f.upload("space-original.png", 174, nil)
	f.complete(u)
	r := f.json("POST", "/backups", "space-checkpoint", struct{}{}, 201)
	var b service.Backup
	decode(t, r, &b)
	f.s.Config.ReserveBytes = 1 << 62
	f.json("POST", "/backups/"+b.ID+"/export", "space-export", struct{}{}, 202)
	b = awaitBackup(t, f, b.ID)
	if b.ExportState != "failed" || b.ExportError != "insufficient_storage" {
		t.Fatalf("space preflight: %+v", b)
	}
	if !strings.Contains(b.ExportMessage, "约需") || !strings.Contains(b.ExportMessage, "当前可用") {
		t.Fatalf("space message: %q", b.ExportMessage)
	}
	directory := filepath.Join(f.s.Config.DataDir, "backups", b.ID)
	if _, e := os.Stat(filepath.Join(directory, "archive.partial")); !os.IsNotExist(e) {
		t.Fatalf("preflight created a partial: %v", e)
	}
	if _, e := os.Stat(filepath.Join(directory, "snapshot.sqlite")); e != nil {
		t.Fatalf("checkpoint was removed after a retryable failure: %v", e)
	}
}
func TestDiscardBackupReclaimsOnlyFailedDebris(t *testing.T) {
	f := setup(t)
	u, _ := f.upload("discard-original.png", 175, nil)
	f.complete(u)
	r := f.json("POST", "/backups", "discard-complete-checkpoint", struct{}{}, 201)
	var completed service.Backup
	decode(t, r, &completed)
	f.json("POST", "/backups/"+completed.ID+"/export", "discard-complete-export", struct{}{}, 202)
	completed = awaitBackup(t, f, completed.ID)
	if completed.ExportState != "completed" {
		t.Fatalf("completed export: %+v", completed)
	}
	if response := f.request("DELETE", "/backups/"+completed.ID, "discard-completed", testSecret, nil, nil); response.Code != 409 {
		t.Fatalf("published backup was discardable: %d %s", response.Code, response.Body.String())
	}
	r = f.json("POST", "/backups", "discard-failed-checkpoint", struct{}{}, 201)
	var failed service.Backup
	decode(t, r, &failed)
	f.s.Config.ReserveBytes = 1 << 62
	f.json("POST", "/backups/"+failed.ID+"/export", "discard-failed-export", struct{}{}, 202)
	failed = awaitBackup(t, f, failed.ID)
	if failed.ExportState != "failed" {
		t.Fatalf("failed export: %+v", failed)
	}
	r = f.json("GET", "/backups/"+failed.ID, "", nil, 200)
	var listed service.Backup
	decode(t, r, &listed)
	if listed.StorageBytes <= 0 {
		t.Fatalf("incomplete backup size was hidden: %+v", listed)
	}
	if response := f.request("DELETE", "/backups/"+failed.ID, "discard-failed", testSecret, nil, nil); response.Code != 204 {
		t.Fatalf("discard failed backup: %d %s", response.Code, response.Body.String())
	}
	if response := f.request("GET", "/backups/"+failed.ID, "", testSecret, nil, nil); response.Code != 404 {
		t.Fatalf("discarded record still exists: %d", response.Code)
	}
	if _, e := os.Stat(filepath.Join(f.s.Config.DataDir, "backups", failed.ID)); !os.IsNotExist(e) {
		t.Fatalf("discarded directory still exists: %v", e)
	}
	if _, e := os.Stat(filepath.Join(f.s.Config.DataDir, "backups", completed.ID, "archive.qnext")); e != nil {
		t.Fatalf("published archive was touched: %v", e)
	}
}
func TestStartupSweepKeepsPublishedArchivesAndReclaimsInterruptedExports(t *testing.T) {
	dir := t.TempDir()
	s, e := service.New(service.Config{DataDir: dir, AdminToken: testSecret, ReserveBytes: 1})
	if e != nil {
		t.Fatal(e)
	}
	f := &fixture{t: t, s: s, h: httpapi.New(s)}
	r := f.json("POST", "/libraries", "sweep-library", map[string]any{"name": "Sweep", "kind": "regular"}, 201)
	var library model.Library
	decode(t, r, &library)
	f.library = library.ID
	u, _ := f.upload("sweep-original.png", 176, nil)
	f.complete(u)
	r = f.json("POST", "/backups", "sweep-complete-checkpoint", struct{}{}, 201)
	var completed service.Backup
	decode(t, r, &completed)
	f.json("POST", "/backups/"+completed.ID+"/export", "sweep-complete-export", struct{}{}, 202)
	completed = awaitBackup(t, f, completed.ID)
	if completed.ExportState != "completed" {
		t.Fatalf("completed export: %+v", completed)
	}
	r = f.json("POST", "/backups", "sweep-failed-checkpoint", struct{}{}, 201)
	var failed service.Backup
	decode(t, r, &failed)
	s.Config.ReserveBytes = 1 << 62
	f.json("POST", "/backups/"+failed.ID+"/export", "sweep-failed-export", struct{}{}, 202)
	failed = awaitBackup(t, f, failed.ID)
	if failed.ExportState != "failed" {
		t.Fatalf("failed export: %+v", failed)
	}
	partial := filepath.Join(dir, "backups", failed.ID, "archive.partial")
	if e = os.WriteFile(partial, []byte("interrupted bytes"), 0600); e != nil {
		t.Fatal(e)
	}
	if e = s.Close(); e != nil {
		t.Fatal(e)
	}
	next, e := service.New(service.Config{DataDir: dir, AdminToken: testSecret, ReserveBytes: 1})
	if e != nil {
		t.Fatal(e)
	}
	defer next.Close()
	if _, e = os.Stat(filepath.Join(dir, "backups", completed.ID, "archive.qnext")); e != nil {
		t.Fatalf("sweep removed a published archive: %v", e)
	}
	if _, e = os.Stat(filepath.Join(dir, "backups", failed.ID)); !os.IsNotExist(e) {
		t.Fatalf("sweep kept an interrupted directory: %v", e)
	}
	record, e := next.Backup(context.Background(), model.Principal{Scopes: []string{"admin"}}, failed.ID)
	if e != nil || record.StorageBytes != 0 {
		t.Fatalf("swept record: %v %+v", e, record)
	}
}

func TestRecordsExportDoesNotReadReplacedOriginals(t *testing.T) {
	f := setup(t)
	u, _ := f.upload("snapshot-original.png", 181, nil)
	a := *f.complete(u).Asset
	r := f.json("POST", "/backups", "before-replacement-backup", struct{}{}, 201)
	var b service.Backup
	decode(t, r, &b)
	u, _ = f.upload("replacement.png", 182, map[string]any{"replaceAssetId": a.ID, "expectedContentVersion": a.ContentVersion})
	f.complete(u)
	f.json("POST", "/backups/"+b.ID+"/export", "after-replacement-export", struct{}{}, 202)
	b = awaitBackup(t, f, b.ID)
	if b.ExportState != "completed" || b.TotalFiles != 0 {
		t.Fatalf("records export read originals: %+v", b)
	}
	file, e := os.Open(filepath.Join(f.s.Config.DataDir, "backups", b.ID, "archive.qnext"))
	if e != nil {
		t.Fatal(e)
	}
	defer file.Close()
	manifest, e := backup.Read(context.Background(), file, nil)
	if e != nil || manifest.Scope != backup.ScopeMetadata || len(manifest.Files) != 0 {
		t.Fatalf("records archive carried originals: %v %+v", e, manifest)
	}
}
func TestIncompleteRestoreCannotStartAndWrongArchiveHashCannotPublish(t *testing.T) {
	f := setup(t)
	u, _ := f.upload("restore-checksum.png", 191, nil)
	f.complete(u)
	r := f.json("POST", "/backups", "restore-checksum-backup", struct{}{}, 201)
	var b service.Backup
	decode(t, r, &b)
	f.json("POST", "/backups/"+b.ID+"/export", "restore-checksum-export", struct{}{}, 202)
	b = awaitBackup(t, f, b.ID)
	if b.ExportState != "completed" {
		t.Fatal(b.ExportError)
	}
	destination := filepath.Join(t.TempDir(), "incomplete")
	_, e := service.RestoreBackup(context.Background(), service.RestoreOptions{ArchivePath: filepath.Join(f.s.Config.DataDir, "backups", b.ID, "archive.qnext"), Destination: destination, ExpectedSHA256: strings.Repeat("0", 64), ReserveBytes: 1})
	if e == nil {
		t.Fatal("wrong archive identity accepted")
	}
	if _, e = os.Stat(filepath.Join(destination, "qimeng.sqlite")); !os.IsNotExist(e) {
		t.Fatal("unverified database was published")
	}
	if _, e = service.New(service.Config{DataDir: destination, AdminToken: strings.Repeat("i", 32), ReserveBytes: 1}); e == nil {
		t.Fatal("incomplete destination started serving")
	}
}
