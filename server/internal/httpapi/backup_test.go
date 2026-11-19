package httpapi_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"qimeng.local/next/server/internal/backup"
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
	defer file.Close()
	payloads := map[string][]byte{}
	base, err := backup.Read(ctx, file, func(entry backup.File, reader io.Reader) error {
		body, err := io.ReadAll(reader)
		payloads[entry.StoragePath] = body
		return err
	})
	if err != nil {
		t.Fatal(err)
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
func TestCompleteBackupRestoresIdentityMetadataTrashAndPendingPrefix(t *testing.T) {
	f := setup(t)
	u, _ := f.upload("backup-live.png", 171, nil)
	a := *f.complete(u).Asset
	f.json("PUT", "/assets/"+a.ID+"/favorite", "backup-favorite", map[string]any{"favorite": true}, 200)
	r := f.json("PUT", "/assets/"+a.ID+"/like/today", "backup-today-like", map[string]any{"liked": true}, 200)
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
