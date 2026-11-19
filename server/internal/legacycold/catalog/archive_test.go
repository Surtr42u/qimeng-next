package catalog

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"qimeng.local/next/server/internal/backup"
	"qimeng.local/next/server/internal/service"
	"qimeng.local/next/server/internal/store"
)

func TestColdCatalogNativeArchiveRestoresRawDecisionsAndClearedFields(t *testing.T) {
	f := newFixture(t)
	f.author("writer", "Writer", "regular", 1)
	f.setting("creator_profile:writer", `{"authorId":"writer","revision":6,"status":"empty","biography":"","sourceInteger":9007199254740993}`)
	f.setting("imported_txt_sources", `[{"filename":"owner.txt","content":"1  Writer\r\n作品\r\nabsent.png\r\n"}]`)
	f.setting("imported_txt_upload_entries", `{"owner.txt":[{"authorId":"writer","displayName":"Writer","works":["absent.png"],"sources":[]}]}`)
	f.native("INSERT INTO settings(key,value) VALUES('instance_id','synthetic-native')")
	if err := f.run(); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	snapshot := filepath.Join(dir, "checkpoint.sqlite")
	f.native("VACUUM INTO ?", snapshot)
	body, err := os.ReadFile(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(body)
	archivePath := filepath.Join(dir, "synthetic.qnext")
	out, err := os.Create(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	m := backup.Manifest{Format: backup.Format, Version: backup.Version, BackupID: "synthetic-backup",
		InstanceID: "synthetic-native", CreatedAt: time.Now().UTC(), Files: []backup.File{},
		Snapshot: backup.File{Kind: "snapshot", StoragePath: "qimeng.sqlite", Size: int64(len(body)), SHA256: hex.EncodeToString(digest[:])}}
	err = backup.Write(t.Context(), out, m, func(backup.File) (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(body)), nil }, nil)
	if closeErr := out.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	if err != nil {
		t.Fatal(err)
	}
	verified, err := service.VerifyBackup(t.Context(), archivePath, "")
	if err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(dir, "isolated")
	restored, err := service.RestoreBackup(t.Context(), service.RestoreOptions{
		ArchivePath: archivePath, ExpectedSHA256: verified.ArchiveHash, Destination: destination})
	if err != nil || restored.InstanceID == m.InstanceID || restored.State != "restored" {
		t.Fatalf("restore=%+v err=%v", restored, err)
	}
	native, err := store.Open(filepath.Join(destination, "qimeng.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer native.DB.Close()
	after := &fixture{t: t, target: native}
	after.expect("9007199254740993", "SELECT json_extract(original,'$.value.sourceInteger') FROM migration_relation_origins WHERE input_id=? AND kind='cold26/author-profile'", sampleSource)
	after.expect("1", "SELECT count(*) FROM migration_relation_origins WHERE input_id=? AND kind='cold26/unbound-protected-work'", sampleSource)
	after.expect("text", "SELECT json_type(data,'$.values.summary') FROM author_profile_blocks WHERE author_id='writer' AND block='basic'")
	after.expect("1  Writer\r\n作品\r\nabsent.png\r\n", "SELECT content FROM author_txt_fragments WHERE source_id=? AND name='owner.txt'", sampleSource)
	after.expect("writer", "SELECT author_id FROM migration_author_aliases WHERE source_id=? AND source_author_id='writer'", sampleSource)
}
