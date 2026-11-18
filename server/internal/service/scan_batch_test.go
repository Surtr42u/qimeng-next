package service

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"qimeng.local/next/server/internal/model"
)

func preparedScanFixture(t *testing.T, names ...string) (*Service, *scanRun, string) {
	t.Helper()
	ctx := context.Background()
	s, err := New(Config{DataDir: t.TempDir(), AdminToken: "synthetic-scan-batch-test-owner-token", ReserveBytes: 1})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if s.workCtx.Err() == nil {
			s.Close()
		}
	})
	var library model.Library
	result := testMutation(t, s, "batch-test-library", func(w *Write) (any, int, error) {
		l, e := s.CreateLibrary(ctx, w, "Synthetic", "regular")
		return l, 201, e
	})
	if err = json.Unmarshal(result.Body, &library); err != nil {
		t.Fatal(err)
	}
	root, err := s.Root.LibraryPath(library.ID)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	job := ScanJob{ID: ID(), LibraryID: library.ID, Trigger: "manual", State: "running", Issues: []ScanIssue{}, CreatedAt: now, UpdatedAt: now}
	if err = saveScan(ctx, s.DB.DB, job); err != nil {
		t.Fatal(err)
	}
	run := &scanRun{id: job.ID, library: library}
	for i, name := range names {
		data := syntheticPNG(t, byte(i+1))
		if err = os.WriteFile(filepath.Join(root, name), data, 0600); err != nil {
			t.Fatal(err)
		}
		full := "libraries/" + library.ID + "/" + name
		initial, err := s.Root.Lstat(full)
		if err != nil {
			t.Fatal(err)
		}
		probe := model.Asset{ID: "scan", MimeType: "image/png"}
		if err = s.probe(ctx, full, &probe); err != nil {
			t.Fatal(err)
		}
		run.prepared = append(run.prepared, scannedFile{relative: name, full: full, kind: "image",
			hash: digest(data), size: int64(len(data)), initial: initial, probe: probe})
	}
	return s, run, root
}

func TestScanBatchFailureRetainsOnlyFinishedAssetsAndTheirCounts(t *testing.T) {
	ctx := context.Background()
	s, run, _ := preparedScanFixture(t, "ready.png", "failure.png", "later.png")
	// Fail after the asset insert, during relation persistence.
	_, err := s.DB.DB.Exec(`CREATE TRIGGER scan_relation_fault BEFORE INSERT ON asset_authors
		WHEN (SELECT file_name FROM assets WHERE id=NEW.asset_id)='failure.png'
		BEGIN SELECT RAISE(ABORT,'synthetic relation fault'); END`)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.flushPreparedScans(ctx, run); err == nil {
		t.Fatal("relation persistence error was lost")
	}
	job, err := s.Scan(ctx, run.id)
	if err != nil || job.Processed != 1 || job.Added != 1 {
		t.Fatal("partial completion counters", job, err)
	}
	var count, readyWithAuthor int
	err = s.DB.DB.QueryRow(`SELECT count(*), coalesce(sum(file_name='ready.png' AND EXISTS(
		SELECT 1 FROM asset_authors x WHERE x.asset_id=assets.id AND x.author_id='unknown')),0) FROM assets`).Scan(&count, &readyWithAuthor)
	if err != nil || count != 1 || readyWithAuthor != 1 {
		t.Fatal("incomplete asset or missing committed relation", count, readyWithAuthor, err)
	}
}

func TestScanBatchRechecksPhysicalReplacementAndPendingPublication(t *testing.T) {
	ctx := context.Background()
	s, run, root := preparedScanFixture(t, "replaced.png", "pending.png", "ready.png")
	replaced := filepath.Join(root, "replaced.png")
	if err := os.Rename(replaced, replaced+".saved"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(replaced, syntheticPNG(t, 1), 0600); err != nil {
		t.Fatal(err)
	}
	stamp := run.prepared[0].initial.ModTime()
	if err := os.Chtimes(replaced, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	// The key and complete bytes match the staged file, but the file identity changed.
	if err := s.flushPreparedScans(ctx, &scanRun{id: run.id, library: run.library, prepared: run.prepared[:1]}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.DB.Exec(`INSERT INTO file_intents(id,kind,state,data) VALUES('pending-fixture','move','prepared',
		json_object('Target',?))`, run.prepared[1].full); err != nil {
		t.Fatal(err)
	}
	run.prepared = run.prepared[1:]
	if err := s.flushPreparedScans(ctx, run); err != nil {
		t.Fatal(err)
	}
	job, err := s.Scan(ctx, run.id)
	if err != nil || job.Processed != 3 || job.Added != 1 || job.Skipped != 2 || len(job.Issues) != 2 ||
		job.Issues[0].Code != "source_changed" || job.Issues[1].Code != "publication_pending" {
		t.Fatal(job, err)
	}
	var name string
	if err = s.DB.DB.QueryRow("SELECT file_name FROM assets").Scan(&name); err != nil || name != "ready.png" {
		t.Fatal(name, err)
	}
}

func TestScanBatchCancellationDiscardsPreparedAssets(t *testing.T) {
	ctx := context.Background()
	s, run, _ := preparedScanFixture(t, "queued.png")
	testMutation(t, s, "cancel-staged-scan", func(w *Write) (any, int, error) {
		job, e := s.CancelScan(ctx, w, run.id)
		return job, 200, e
	})
	if err := s.flushPreparedScans(ctx, run); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled batch attempted publication", err)
	}
	job, err := s.Scan(ctx, run.id)
	if err != nil || job.State != "cancelled" || job.Added != 0 || job.Processed != 0 {
		t.Fatal(job, err)
	}
	var count int
	if err = s.DB.DB.QueryRow("SELECT count(*) FROM assets").Scan(&count); err != nil || count != 0 {
		t.Fatal(count, err)
	}
}

func TestScanBatchUnrelatedFileIntentKeepsIndependentWorkInBatch(t *testing.T) {
	ctx := context.Background()
	s, run, _ := preparedScanFixture(t, "one.png", "two.png")
	if _, err := s.DB.DB.Exec(`INSERT INTO file_intents(id,kind,state,data) VALUES('unrelated','move','prepared',
		json_object('Target',?,'Before',json_object('libraryId',?,'relativePath','elsewhere.png')))`,
		"libraries/"+run.library.ID+"/other.png", run.library.ID); err != nil {
		t.Fatal(err)
	}
	committed, individual, err := s.commitFreshScanPrefix(ctx, run, run.prepared)
	if err != nil || committed < 1 || individual != 0 {
		t.Fatal("independent images lost their batch", committed, individual, err)
	}
	run.prepared = run.prepared[committed:]
	if err = s.flushPreparedScans(ctx, run); err != nil {
		t.Fatal(err)
	}
	job, err := s.Scan(ctx, run.id)
	if err != nil || job.Added != 2 || job.Processed != 2 || job.IssueCount != 0 {
		t.Fatal(job, err)
	}
}

func TestScanBatchRestartResumesFromDurableAssetsAndCounts(t *testing.T) {
	ctx := context.Background()
	s, run, _ := preparedScanFixture(t, "committed.png", "in-memory.png")
	prefix := &scanRun{id: run.id, library: run.library, prepared: run.prepared[:1]}
	if err := s.flushPreparedScans(ctx, prefix); err != nil {
		t.Fatal(err)
	}
	var retainedID string
	if err := s.DB.DB.QueryRow("SELECT id FROM assets WHERE file_name='committed.png'").Scan(&retainedID); err != nil {
		t.Fatal(err)
	}
	cfg := s.Config
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	restarted, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	job, err := restarted.Scan(ctx, run.id)
	if err != nil || job.State != "interrupted" || job.Processed != 1 || job.Added != 1 {
		t.Fatal("restart exposed unpublished progress", job, err)
	}
	result := testMutation(t, restarted, "restart-batch-rescan", func(w *Write) (any, int, error) {
		job, e := restarted.StartScan(ctx, w, run.library.ID, run.library.Revision)
		return job, 202, e
	})
	if err = json.Unmarshal(result.Body, &job); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for job.State == "queued" || job.State == "running" {
		if time.Now().After(deadline) {
			t.Fatal("new scan did not finish")
		}
		time.Sleep(5 * time.Millisecond)
		if job, err = restarted.Scan(ctx, job.ID); err != nil {
			t.Fatal(err)
		}
	}
	if job.State != "completed" || job.Added != 1 || job.Skipped != 1 || job.Processed != 2 {
		t.Fatal(job)
	}
	asset, err := restarted.Asset(ctx, retainedID)
	if err != nil || asset.FileName != "committed.png" || asset.ContentVersion != 1 {
		t.Fatal("rescan recreated an already committed asset", asset, err)
	}
}
