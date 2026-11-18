package httpapi_test

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"qimeng.local/next/server/internal/model"
	"qimeng.local/next/server/internal/service"
	"qimeng.local/next/server/internal/store"
)

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
func TestCompleteBackupRestoresIdentityMetadataTrashAndPendingPrefix(t *testing.T) {
	f := setup(t)
	u, _ := f.upload("backup-live.png", 171, nil)
	a := *f.complete(u).Asset
	r := f.json("PUT", "/assets/"+a.ID+"/reaction", "backup-favorite", map[string]any{"favorite": true, "liked": true}, 200)
	decode(t, r, &a)
	u, _ = f.upload("backup-trash.png", 172, nil)
	trashed := *f.complete(u).Asset
	f.json("DELETE", "/assets/"+trashed.ID+"?expectedRevision=1", "backup-trash", struct{}{}, 200)
	r = f.json("POST", "/uploads", "backup-pending-upload", map[string]any{"libraryId": f.library, "fileName": "pending.png", "size": 10, "mimeType": "image/png", "mode": "ordinary"}, 201)
	var pending model.Upload
	decode(t, r, &pending)
	response := f.request("PUT", "/uploads/"+pending.ID+"/content", "", testSecret, []byte("part"), map[string]string{"Content-Type": "application/octet-stream", "Upload-Offset": "0"})
	if response.Code != 200 {
		t.Fatalf("partial upload: %s", response.Body.String())
	}
	r = f.json("POST", "/backups", "complete-backup-checkpoint", struct{}{}, 201)
	var b service.Backup
	decode(t, r, &b)
	f.json("POST", "/backups/"+b.ID+"/export", "complete-backup-export", struct{}{}, 202)
	b = awaitBackup(t, f, b.ID)
	if b.ExportState != "completed" || b.ArchiveHash == "" || b.TotalFiles != 3 {
		t.Fatalf("export: %+v", b)
	}
	source := filepath.Join(f.s.Config.DataDir, "backups", b.ID, "archive.qnext")
	if _, e := service.VerifyBackup(context.Background(), source, b.ArchiveHash); e != nil {
		t.Fatal(e)
	}
	destination := filepath.Join(t.TempDir(), "restored")
	report, e := service.RestoreBackup(context.Background(), service.RestoreOptions{ArchivePath: source, Destination: destination, ExpectedSHA256: b.ArchiveHash, ReserveBytes: 1})
	if e != nil {
		t.Fatal(e)
	}
	if report.InstanceID == f.s.InstanceID || report.AssetCount != 2 || report.UploadCount != 1 {
		t.Fatal("restore identity or counts incorrect")
	}
	restored, e := service.New(service.Config{DataDir: destination, AdminToken: strings.Repeat("r", 32), ReserveBytes: 1})
	if e != nil {
		t.Fatal(e)
	}
	defer restored.Close()
	actual, e := restored.Asset(context.Background(), a.ID)
	if e != nil {
		t.Fatal(e)
	}
	if !actual.Favorite || actual.Hash != a.Hash || actual.LikeCount != a.LikeCount {
		t.Fatal("restored metadata changed")
	}
	deleted, e := restored.Asset(context.Background(), trashed.ID)
	if e != nil || !deleted.Trashed {
		t.Fatal("trash identity lost")
	}
	prefix, e := restored.Root.Open("uploads/" + pending.ID + ".part")
	if e != nil {
		t.Fatal(e)
	}
	data, e := io.ReadAll(prefix)
	prefix.Close()
	if e != nil || string(data) != "part" {
		t.Fatal("committed upload prefix lost")
	}
	record, e := store.Get[model.Upload](context.Background(), restored.DB.DB, "SELECT data FROM uploads WHERE id=?", pending.ID)
	if e != nil || record.Offset != 4 || record.State != "receiving" {
		t.Fatal("upload continuation identity changed")
	}
	if _, e = service.RestoreBackup(context.Background(), service.RestoreOptions{ArchivePath: source, Destination: destination, ReserveBytes: 1}); e == nil {
		t.Fatal("existing instance overwritten")
	}
}
func TestBackupExportFailsWhenSnapshotOriginalWasReplaced(t *testing.T) {
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
	if b.ExportState != "failed" || b.ExportError != "backup_source_changed" {
		t.Fatalf("old source falsely protected: %+v", b)
	}
	if _, e := os.Stat(filepath.Join(f.s.Config.DataDir, "backups", b.ID, "archive.qnext")); !os.IsNotExist(e) {
		t.Fatal("failed archive was published")
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
