package httpapi_test

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"qimeng.local/next/server/internal/model"
	"qimeng.local/next/server/internal/service"
)

func TestUnchangedLibraryScanCountsEveryFileWithoutPerFileCatalogWrites(t *testing.T) {
	f := setup(t)
	root, err := f.s.Root.LibraryPath(f.library)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 140; i++ {
		if err = os.WriteFile(filepath.Join(root, fmt.Sprintf("indexed-%03d.png", i)), pngBytes(t, uint8(i+1)), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if initial := f.scan("index-synthetic-scan-pages", 45*time.Second); initial.Added != 140 || initial.IssueCount != 0 {
		t.Fatal("initial scan", initial)
	}
	for i := 0; i < 140; i++ {
		if err = os.WriteFile(filepath.Join(root, fmt.Sprintf("note-%03d.txt", i)), []byte("synthetic"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = f.s.DB.DB.Exec(`CREATE TABLE fixture_scan_writes(n INTEGER); INSERT INTO fixture_scan_writes VALUES(0);
  CREATE TRIGGER fixture_count_scan_write AFTER UPDATE ON scans BEGIN UPDATE fixture_scan_writes SET n=n+1; END;`); err != nil {
		t.Fatal(err)
	}
	job := f.scan("repeat-synthetic-scan-pages")
	if job.Processed != 280 || job.Skipped != 280 || job.Added+job.Moved+job.Changed+job.IssueCount != 0 {
		t.Fatal("unchanged scan lost or duplicated entries", job)
	}
	var writes int
	if err = f.s.DB.DB.QueryRow("SELECT n FROM fixture_scan_writes").Scan(&writes); err != nil || writes >= 30 {
		t.Fatalf("unchanged scan used %d progress updates: %v", writes, err)
	}
}

func (f *fixture) scan(key string, window ...time.Duration) service.ScanJob {
	f.t.Helper()
	r := f.json("POST", "/libraries/"+f.library+"/scans", key, map[string]any{"expectedRevision": 1}, 202)
	var job service.ScanJob
	decode(f.t, r, &job)
	interval := 10 * time.Second
	if len(window) > 0 {
		interval = window[0]
	}
	deadline := time.Now().Add(interval)
	for time.Now().Before(deadline) {
		r = f.json("GET", "/scans/"+job.ID, "", nil, 200)
		decode(f.t, r, &job)
		if job.State != "queued" && job.State != "running" {
			return job
		}
		time.Sleep(10 * time.Millisecond)
	}
	f.t.Fatal("synthetic scan did not finish")
	return job
}
func TestScanRegistersExistingFileAndConservesIdentityAfterExternalMove(t *testing.T) {
	f := setup(t)
	original := path.Join("libraries", f.library, "before.png")
	file, e := f.s.Root.OpenFile(original, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = file.Write(pngBytes(t, 210)); e != nil {
		t.Fatal(e)
	}
	if e = file.Close(); e != nil {
		t.Fatal(e)
	}
	job := f.scan("scan-existing-file")
	if job.State != "completed" || job.Added != 1 {
		t.Fatalf("first scan: %+v", job)
	}
	r := f.json("GET", "/assets", "", nil, 200)
	var assets model.Page[model.Asset]
	decode(t, r, &assets)
	if len(assets.Items) != 1 {
		t.Fatal("existing original not indexed")
	}
	a := assets.Items[0]
	r = f.json("PUT", "/assets/"+a.ID+"/tags", "scan-preserve-tags", map[string]any{"expectedRevision": a.TagsRevision, "tags": []string{"preserve"}}, 200)
	decode(t, r, &a)
	destination := path.Join("libraries", f.library, "after.png")
	if e = f.s.Root.Link(original, destination); e != nil {
		t.Fatal(e)
	}
	if e = f.s.Root.Remove(original); e != nil {
		t.Fatal(e)
	}
	job = f.scan("scan-external-move")
	if job.State != "completed" || job.Moved != 1 {
		t.Fatalf("move scan: %+v", job)
	}
	r = f.json("GET", "/assets/"+a.ID, "", nil, 200)
	decode(t, r, &a)
	if a.FileName != "after.png" || len(a.Tags) != 1 || a.Tags[0] != "preserve" {
		t.Fatal("external move lost identity or tags")
	}
	if e = f.s.Root.Remove(destination); e != nil {
		t.Fatal(e)
	}
	job = f.scan("scan-missing-original")
	if job.State != "partial" || job.Missing != 1 {
		t.Fatalf("missing original was hidden: %+v", job)
	}
	r = f.json("GET", "/assets/"+a.ID, "", nil, 200)
	decode(t, r, &a)
	if a.Trashed {
		t.Fatal("missing file was treated as authorized deletion")
	}
}
func TestScanSkipsUnchangedContentAndPrivateTrash(t *testing.T) {
	f := setup(t)
	u, _ := f.upload("indexed-upload.png", 211, nil)
	a := *f.complete(u).Asset
	job := f.scan("scan-no-change")
	if job.Added != 0 || job.Changed != 0 {
		t.Fatalf("unchanged original reprobed as new: %+v", job)
	}
	r := f.json("DELETE", "/assets/"+a.ID+"?expectedRevision="+strconv.FormatInt(a.Revision, 10), "scan-trash", struct{}{}, 200)
	decode(t, r, &a)
	job = f.scan("scan-skips-internal-trash")
	if job.Added != 0 {
		t.Fatal("private trash was indexed as a new asset")
	}
}
