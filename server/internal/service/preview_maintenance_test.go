package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"qimeng.local/next/server/internal/model"
)

func publishPreviewAsset(t *testing.T, s *Service, library, name string, shade byte) model.Asset {
	t.Helper()
	ctx := context.Background()
	upload := stagedUpload(t, s, library, shade, name)
	result := testMutation(t, s, "publish-"+name, func(w *Write) (any, int, error) {
		value, err := s.CompleteUpload(ctx, w, upload.ID)
		return value, 200, err
	})
	var published model.UploadResult
	if err := json.Unmarshal(result.Body, &published); err != nil || published.Asset == nil {
		t.Fatalf("publish %s: %v", name, err)
	}
	return *published.Asset
}

func createPreviewLibrary(t *testing.T, s *Service, name string) model.Library {
	t.Helper()
	ctx := context.Background()
	result := testMutation(t, s, "create-library-"+name, func(w *Write) (any, int, error) {
		library, err := s.CreateLibrary(ctx, w, name, "regular")
		return library, 201, err
	})
	var library model.Library
	if err := json.Unmarshal(result.Body, &library); err != nil {
		t.Fatal(err)
	}
	return library
}

func TestManagedPreviewFileAcceptsPolicyVersions(t *testing.T) {
	for _, name := range []string{
		"v1-1-512.jpg", "v2-12-256.jpg", "v6-3-1024.jpg",
		"v12-4-512.jpg", "v99-5-256.jpg",
	} {
		if !managedPreviewFile.MatchString(name) {
			t.Fatalf("managed preview rejected %q", name)
		}
	}
	for _, name := range []string{
		"v0-1-512.jpg", "v-1-1-512.jpg", "v5-0-512.jpg",
		"v5-1-128.jpg", "v5-1-512.png", "V5-1-512.jpg", "v5-1-512.jpg.tmp",
	} {
		if managedPreviewFile.MatchString(name) {
			t.Fatalf("unmanaged file accepted: %q", name)
		}
	}
}

func TestWarmPreviewPageContinuesPastUnavailableOriginals(t *testing.T) {
	s, run, _ := preparedScanFixture(t)
	ctx := t.Context()
	assets := make([]model.Asset, 0, 6)
	for i := 0; i < 6; i++ {
		assets = append(assets, publishPreviewAsset(t, s, run.library.ID, fmt.Sprintf("warm-page-%d.png", i), byte(90+i)))
	}
	offline := 2
	if err := s.Root.Remove(assetPath(assets[offline])); err != nil {
		t.Fatal(err)
	}
	if err := s.warmPreviews(ctx, previewScope{library: run.library.ID}); err != nil {
		t.Fatalf("one unavailable original aborted the page: %v", err)
	}
	for i, asset := range assets {
		for _, edge := range previewWarmEdges() {
			_, err := s.Root.Stat(previewPath(asset.ID, asset.ContentVersion, edge, false))
			if i == offline {
				if !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("offline asset published edge %d: %v", edge, err)
				}
				continue
			}
			if err != nil {
				t.Fatalf("healthy asset %d edge %d was not warmed: %v", i, edge, err)
			}
		}
	}
}

func TestThumbnailProgressUsesTTLSnapshotAndSingleScan(t *testing.T) {
	s, run, _ := preparedScanFixture(t)
	ctx := t.Context()
	asset := publishPreviewAsset(t, s, run.library.ID, "progress.png", 71)
	first, err := s.ThumbnailProgress(ctx)
	if err != nil || first.TotalAssets != 1 || first.ThumbsOnDisk != 0 {
		t.Fatalf("initial progress: %+v, %v", first, err)
	}
	cache := previewPath(asset.ID, asset.ContentVersion, 512, false)
	file, err := s.Root.OpenFile(cache, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = file.Write([]byte("synthetic preview")); err != nil {
		t.Fatal(err)
	}
	if err = file.Close(); err != nil {
		t.Fatal(err)
	}
	// The snapshot shields a poll burst from the per-asset stat walk.
	if cached, err := s.ThumbnailProgress(ctx); err != nil || cached.ThumbsOnDisk != 0 {
		t.Fatalf("TTL snapshot was not reused: %+v, %v", cached, err)
	}
	expireThumbnailProgress(t, s)
	if updated, err := s.ThumbnailProgress(ctx); err != nil || updated.ThumbsOnDisk != 1 {
		t.Fatalf("expired snapshot was not refreshed: %+v, %v", updated, err)
	}

	s.previews.progress <- struct{}{}
	busy, err := s.ThumbnailProgress(ctx)
	<-s.previews.progress
	if err != nil || busy.ThumbsOnDisk != 1 {
		t.Fatalf("concurrent poll did not share the stored snapshot: %+v, %v", busy, err)
	}

	s.invalidateThumbnailProgress()
	s.previews.progress <- struct{}{}
	_, err = s.ThumbnailProgress(ctx)
	<-s.previews.progress
	var problem *Error
	if !errors.As(err, &problem) || problem.Status != 429 {
		t.Fatalf("cold concurrent poll did not report busy: %v", err)
	}
}

func expireThumbnailProgress(t *testing.T, s *Service) {
	t.Helper()
	key := s.thumbnailProgressKey()
	thumbnailProgressCache.Lock()
	defer thumbnailProgressCache.Unlock()
	snapshot, found := thumbnailProgressCache.entries[key]
	if !found {
		t.Fatal("missing thumbnail progress snapshot")
	}
	snapshot.at = time.Now().Add(-thumbnailProgressTTL - time.Second)
	thumbnailProgressCache.entries[key] = snapshot
}

func assertNoManagedPreviews(t *testing.T, s *Service, assetID string) {
	t.Helper()
	dir, err := s.Root.Open("cache/" + assetID)
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	entries, readErr := dir.ReadDir(-1)
	closeErr := dir.Close()
	if readErr != nil {
		t.Fatal(readErr)
	}
	if closeErr != nil {
		t.Fatal(closeErr)
	}
	for _, entry := range entries {
		if managedPreviewFile.MatchString(entry.Name()) {
			t.Fatalf("deleted asset retained managed preview %q", entry.Name())
		}
	}
}

func TestPurgeRemovesPreviewCacheImmediately(t *testing.T) {
	s, run, _ := preparedScanFixture(t)
	ctx := t.Context()
	asset := publishPreviewAsset(t, s, run.library.ID, "purge-preview.png", 33)
	if err := s.warmPreviews(ctx, previewScope{asset: asset.ID}); err != nil {
		t.Fatal(err)
	}
	if info, err := s.Root.Stat(previewPath(asset.ID, asset.ContentVersion, 512, false)); err != nil || info.Size() == 0 {
		t.Fatalf("fixture preview was not warmed: %v", err)
	}
	result := testMutation(t, s, "trash-purge-preview", func(w *Write) (any, int, error) {
		value, err := s.Trash(ctx, w, asset.ID, asset.Revision)
		return value, 200, err
	})
	var trashed model.Asset
	if err := json.Unmarshal(result.Body, &trashed); err != nil {
		t.Fatal(err)
	}
	testMutation(t, s, "purge-preview-asset", func(w *Write) (any, int, error) {
		return nil, 204, s.Purge(ctx, w, asset.ID, trashed.Revision)
	})
	for _, edge := range previewWarmEdges() {
		if _, err := s.Root.Stat(previewPath(asset.ID, asset.ContentVersion, edge, false)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("purge retained preview edge %d: %v", edge, err)
		}
	}
	assertNoManagedPreviews(t, s, asset.ID)
}

func TestDeleteLibraryRemovesPreviewCaches(t *testing.T) {
	s, err := New(Config{DataDir: t.TempDir(), AdminToken: "synthetic-preview-delete-owner-token", ReserveBytes: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	library := createPreviewLibrary(t, s, "Detached previews")
	asset := publishPreviewAsset(t, s, library.ID, "library-preview.png", 54)
	if err = s.warmPreviews(ctx, previewScope{library: library.ID}); err != nil {
		t.Fatal(err)
	}
	if info, err := s.Root.Stat(previewPath(asset.ID, asset.ContentVersion, 512, false)); err != nil || info.Size() == 0 {
		t.Fatalf("fixture preview was not warmed: %v", err)
	}
	testMutation(t, s, "delete-preview-library", func(w *Write) (any, int, error) {
		return nil, 204, s.DeleteLibrary(ctx, w, library.ID, library.Revision)
	})
	for _, edge := range previewWarmEdges() {
		if _, err := s.Root.Stat(previewPath(asset.ID, asset.ContentVersion, edge, false)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("library removal retained preview edge %d: %v", edge, err)
		}
	}
	assertNoManagedPreviews(t, s, asset.ID)
}
