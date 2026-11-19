package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
	"qimeng.local/next/server/internal/model"
)

func rewriteWatchedOriginal(t *testing.T, root string, asset model.Asset) []byte {
	t.Helper()
	name := filepath.Join(root, asset.RelativePath)
	for shade := range 256 {
		data := syntheticPNG(t, byte(shade))
		if int64(len(data)) != asset.Size || digest(data) == asset.Hash {
			continue
		}
		if err := os.WriteFile(name, data, 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(name, *asset.ContentMtime, *asset.ContentMtime); err != nil {
			t.Fatal(err)
		}
		return data
	}
	t.Fatal("no distinct equal-length PNG")
	return nil
}

func finishWatchBatch(t *testing.T, s *Service, run *scanRun, batch *watchBatch) ScanJob {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	testMutation(t, s, "release-scan-slot", func(w *Write) (any, int, error) {
		job, err := s.CancelScan(ctx, w, run.id)
		return job, 200, err
	})
	ready, err := batch.enqueue(time.Now().Add(watchQuiet), func(targets scanTargets) error {
		return s.enqueueMonitoredScan(ctx, run.library.ID, "watch", targets)
	})
	if !ready || err != nil {
		t.Fatal("watch submission", ready, err)
	}
	for {
		page, err := s.Scans(ctx, run.library.ID, "", "", 1)
		if err != nil || len(page.Items) != 1 || page.Items[0].Trigger != "watch" {
			t.Fatal("missing watch scan", page, err)
		}
		job := page.Items[0]
		switch job.State {
		case "completed":
			return job
		case "queued", "running":
			time.Sleep(5 * time.Millisecond)
		default:
			t.Fatal("watch scan did not finish cleanly", job)
		}
	}
}

func TestScanBatchMixedWatchChangesRetainWritesAcrossQueueConflict(t *testing.T) {
	ctx := context.Background()
	s, run, root := preparedScanFixture(t)
	old := existingScanOriginal(t, s, run, root, "edited.png")
	updated := rewriteWatchedOriginal(t, root, old)
	newName := "added.png"
	newBytes := syntheticPNG(t, 77)
	if err := os.WriteFile(filepath.Join(root, newName), newBytes, 0600); err != nil {
		t.Fatal(err)
	}
	var batch watchBatch
	batch.observe(fsnotify.Event{Op: fsnotify.Write}, old.RelativePath)
	// The fixture holds a live scan. Deliver a structural event while the
	// consumer is submitting the prior write, before it sees the real 409.
	ready, err := batch.enqueue(time.Now().Add(watchQuiet), func(targets scanTargets) error {
		batch.observe(fsnotify.Event{Op: fsnotify.Create | fsnotify.Write}, newName)
		return s.enqueueMonitoredScan(ctx, run.library.ID, "watch", targets)
	})
	var app *Error
	if !ready || !errors.As(err, &app) || app.Code != "scan_already_queued" {
		t.Fatal("fixture failed to hold the scan queue", ready, err)
	}
	if !batch.pending.inventory || batch.pending.verifyAll || len(batch.pending.writes) != 2 ||
		!batch.pending.writes[old.RelativePath] || !batch.pending.writes[newName] {
		t.Fatal("retry lost the inventory or widened the content checks", batch.pending)
	}
	job := finishWatchBatch(t, s, run, &batch)
	if job.Scope != "library" || job.Processed != 2 || job.Added != 1 || job.Changed != 1 {
		t.Fatal("mixed notification results", job)
	}
	assertUpdated := func(instance *Service) {
		t.Helper()
		a, err := instance.Asset(ctx, old.ID)
		if err != nil || a.ContentVersion != old.ContentVersion+1 || a.Hash != digest(updated) || a.Size != old.Size ||
			a.Note != old.Note || !slices.Equal(a.Tags, old.Tags) || !slices.Equal(a.AuthorIDs, old.AuthorIDs) {
			t.Fatal("changed content or human metadata differs", a, err)
		}
	}
	assertUpdated(s)

	checkpoint := testMutation(t, s, "watch-checkpoint", func(w *Write) (any, int, error) {
		b, err := s.CreateBackup(ctx, w)
		return b, 201, err
	})
	var backup Backup
	if err := json.Unmarshal(checkpoint.Body, &backup); err != nil {
		t.Fatal(err)
	}
	s.exportBackup(ctx, backup)
	backup, err = s.Backup(ctx, syncOwner(), backup.ID)
	if err != nil || backup.ExportState != "completed" || backup.ArchiveHash == "" {
		t.Fatal("updated originals could not be exported", backup, err)
	}
	into := filepath.Join(t.TempDir(), "restored")
	restored, err := RestoreBackup(ctx, RestoreOptions{Destination: into, ExpectedSHA256: backup.ArchiveHash,
		ArchivePath: filepath.Join(s.Config.DataDir, "backups", backup.ID, "archive.qnext"), ReserveBytes: 1})
	if err != nil || restored.AssetCount != 2 {
		t.Fatal("watch backup restore", restored, err)
	}
	config := s.Config
	config.DataDir = into
	instance, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	assertUpdated(instance)
	for name, want := range map[string][]byte{old.RelativePath: updated, newName: newBytes} {
		data, err := os.ReadFile(filepath.Join(into, "libraries", run.library.ID, name))
		if err != nil || !bytes.Equal(data, want) {
			t.Fatal("restored original differs", name, err)
		}
	}
}

func TestScanBatchWatchOverflowChecksContentOutsideRetainedNames(t *testing.T) {
	for _, cause := range []string{"path budget", "notification stream"} {
		t.Run(cause, func(t *testing.T) {
			s, run, root := preparedScanFixture(t)
			old := existingScanOriginal(t, s, run, root, "unnamed.png")
			updated := rewriteWatchedOriginal(t, root, old)
			var batch watchBatch
			if cause == "path budget" {
				for i := range maxWatchPaths + 1 {
					batch.observe(fsnotify.Event{Op: fsnotify.Write}, fmt.Sprintf("other-%d.png", i))
				}
			} else {
				stream := make(chan error, 1)
				stream <- fsnotify.ErrEventOverflow
				tree := watchTree{watcher: &fsnotify.Watcher{Errors: stream}}
				if err := tree.receive(context.Background(), &batch, nil); !errors.Is(err, fsnotify.ErrEventOverflow) {
					t.Fatal(err)
				}
			}
			if !batch.pending.inventory || !batch.pending.verifyAll || len(batch.pending.writes) != 0 {
				t.Fatal("overflow must discard names and verify the library", batch.pending)
			}
			job := finishWatchBatch(t, s, run, &batch)
			a, err := s.Asset(context.Background(), old.ID)
			if job.Scope != "library" || job.Changed != 1 || job.Processed != 1 || err != nil ||
				a.ContentVersion != old.ContentVersion+1 || a.Hash != digest(updated) {
				t.Fatal("unnamed original missed after overflow", job, a, err)
			}
		})
	}
}
