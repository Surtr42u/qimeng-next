package service

import (
	"encoding/json"
	"errors"
	"os"
	"sync/atomic"
	"testing"

	"qimeng.local/next/server/internal/model"
)

func TestPreviewCyclePreservesScopeAndWaitsForMeasuredOutputSpace(t *testing.T) {
	s, run, _ := preparedScanFixture(t)
	ctx := t.Context()
	u := stagedUpload(t, s, run.library.ID, 231, "reserve-window.png")
	result := testMutation(t, s, "reserve-window-publish", func(w *Write) (any, int, error) {
		out, err := s.CompleteUpload(ctx, w, u.ID)
		return out, 200, err
	})
	var published model.UploadResult
	if err := json.Unmarshal(result.Body, &published); err != nil || published.Asset == nil {
		t.Fatal("failed to publish synthetic image", err)
	}
	a := *published.Asset
	s.previews.pending(false)
	s.previews.changed(a.LibraryID, a.ID)

	var free, checks atomic.Int64
	free.Store(s.Config.ReserveBytes + previewFileAllowance + 1)
	s.previews.mu.Lock()
	s.previews.available = func(string) (int64, error) {
		checks.Add(1)
		return free.Load(), nil
	}
	s.previews.mu.Unlock()

	needed := s.runPreviewCycle(ctx, false, 0)
	if needed <= 1 || checks.Load() != 2 {
		t.Fatalf("first decode did not report measured output: need=%d, storage checks=%d", needed, checks.Load())
	}
	s.previews.mu.Lock()
	full, pending := s.previews.full, len(s.previews.scopes)
	retained := s.previews.scopes[previewScope{library: a.LibraryID, asset: a.ID}]
	s.previews.mu.Unlock()
	if full || pending != 1 || !retained {
		t.Fatal("the failed asset scope was replaced by broader work")
	}
	if next := s.runPreviewCycle(ctx, false, needed); next != needed || checks.Load() != 3 {
		t.Fatalf("second tick retried decoding without new space: need=%d, storage checks=%d", next, checks.Load())
	}
	for _, edge := range []int{512, 256, 1024} {
		if _, err := s.Root.Stat(previewPath(a.ID, a.ContentVersion, edge, false)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("preview %d was published under reserve: %v", edge, err)
		}
	}

	free.Store(s.Config.ReserveBytes + 8<<20)
	if waiting := s.runPreviewCycle(ctx, false, needed); waiting != 0 {
		t.Fatalf("preview worker did not recover: %d", waiting)
	}
	for _, edge := range []int{512, 256, 1024} {
		if info, err := s.Root.Stat(previewPath(a.ID, a.ContentVersion, edge, false)); err != nil || info.Size() == 0 {
			t.Fatalf("preview %d was not recovered: %v", edge, err)
		}
	}
	if err := s.verify(assetPath(a), a.Hash, a.Size); err != nil {
		t.Fatal("preview retries changed the source", err)
	}
}
