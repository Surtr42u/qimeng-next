package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"qimeng.local/next/server/internal/model"
	"qimeng.local/next/server/internal/store"
)

func TestPreviewWaiterCanLeaveWithoutCancellingTheSharedDecode(t *testing.T) {
	s := &Service{workCtx: context.Background()}
	s.previews.init()
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	finish := make(chan struct{})
	result := make(chan error, 1)
	var count atomic.Int64
	generate := func(work context.Context) error {
		count.Add(1)
		close(started)
		select {
		case <-finish:
			return nil
		case <-work.Done():
			return work.Err()
		}
	}
	go func() { result <- s.previews.join(ctx, s, "one", true, generate) }()
	<-started
	cancel()
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled caller kept waiting", err)
	}
	cancelled, drop := context.WithCancel(context.Background())
	drop()
	if err := s.previews.join(cancelled, s, "one", false, generate); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	// Observe an independent join before allowing the only decode to finish.
	waiter, abandon := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer abandon()
	if err := s.previews.join(waiter, s, "one", false, generate); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	close(finish)
	s.workers.Wait()
	if count.Load() != 1 {
		t.Fatal("one cache identity triggered multiple decodes")
	}
}

func TestThumbnailWarmupResumeOnlyMissingSizesAcrossReserveAndTrash(t *testing.T) {
	s, scan, _ := preparedScanFixture(t)
	ctx := t.Context()
	config := s.Config
	create := func(name string, shade byte) model.Asset {
		t.Helper()
		u := stagedUpload(t, s, scan.library.ID, shade, name)
		result := testMutation(t, s, "publish-"+name, func(w *Write) (any, int, error) {
			v, err := s.CompleteUpload(ctx, w, u.ID)
			return v, 200, err
		})
		var v model.UploadResult
		if err := json.Unmarshal(result.Body, &v); err != nil || v.Asset == nil {
			t.Fatal("publish warmup image", err)
		}
		return *v.Asset
	}
	asset := create("resume.png", 22)
	offline := create("offline.png", 23)
	if err := s.Root.Remove(assetPath(offline)); err != nil {
		t.Fatal(err)
	}
	file, _, err := s.ThumbnailSize(ctx, asset.ID, asset.ContentVersion, "md")
	if err != nil {
		t.Fatal(err)
	}
	if err = file.Close(); err != nil {
		t.Fatal(err)
	}
	cache := func(edge int) string { return previewPath(asset.ID, asset.ContentVersion, edge, false) }
	stamp := time.Unix(1577836800, 0)
	if err = s.Root.Chtimes(cache(512), stamp, stamp); err != nil {
		t.Fatal(err)
	}
	restart := func(reserve int64) {
		t.Helper()
		if err := s.Close(); err != nil {
			t.Fatal(err)
		}
		config.ReserveBytes = reserve
		s, err = New(config)
		if err != nil {
			t.Fatal(err)
		}
	}
	defer func() {
		if s != nil && s.workCtx.Err() == nil {
			s.Close()
		}
	}()
	restart(math.MaxInt64)
	var problem *Error
	if err = s.warmPreviews(ctx, previewScope{}); !errors.As(err, &problem) || problem.Status != 507 {
		t.Fatal("missing size did not respect disk reserve", err)
	}
	for _, edge := range []int{256, 1024} {
		if _, err = s.Root.Stat(cache(edge)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("size %d was published below the reserve: %v", edge, err)
		}
	}
	restart(1)
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if err = s.warmPreviews(cancelled, previewScope{}); !errors.Is(err, context.Canceled) {
		t.Fatal("warmup ignored cancellation", err)
	}
	if err = s.warmPreviews(ctx, previewScope{}); err != nil {
		t.Fatal(err)
	}
	for _, edge := range []int{512, 256, 1024} {
		info, err := s.Root.Stat(cache(edge))
		if err != nil || info.Size() == 0 || edge == 512 && !info.ModTime().Equal(stamp) {
			t.Fatalf("size %d after restart: %v, %v", edge, info, err)
		}
		if _, err = s.Root.Stat(previewPath(offline.ID, offline.ContentVersion, edge, false)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("offline original published size %d: %v", edge, err)
		}
	}
	result := testMutation(t, s, "trash-warmed-image", func(w *Write) (any, int, error) {
		v, err := s.Trash(ctx, w, asset.ID, asset.Revision)
		return v, 200, err
	})
	var trashed model.Asset
	if err = json.Unmarshal(result.Body, &trashed); err != nil {
		t.Fatal(err)
	}
	scope := previewScope{asset: asset.ID}
	if err = s.warmPreviews(ctx, scope); err != nil {
		t.Fatal(err)
	}
	for _, edge := range []int{512, 256, 1024} {
		if _, err = s.Root.Stat(cache(edge)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("trash retained size %d: %v", edge, err)
		}
	}
	testMutation(t, s, "restore-warmed-image", func(w *Write) (any, int, error) {
		v, err := s.Restore(ctx, w, asset.ID, trashed.Revision)
		return v, 200, err
	})
	if err = s.warmPreviews(ctx, scope); err != nil {
		t.Fatal(err)
	}
	for _, edge := range []int{512, 256, 1024} {
		if info, err := s.Root.Stat(cache(edge)); err != nil || info.Size() == 0 {
			t.Fatalf("restore did not regenerate size %d: %v", edge, err)
		}
	}
	if err = s.verify(assetPath(asset), asset.Hash, asset.Size); err != nil {
		t.Fatal("original changed", err)
	}
}

func TestThumbnailWarmupKeepsCurrentBytesAndOnlyPrunesObsoleteRepresentations(t *testing.T) {
	s, err := New(Config{DataDir: t.TempDir(), AdminToken: "synthetic-preview-maintenance-credential", ReserveBytes: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	result := testMutation(t, s, "warm-library", func(w *Write) (any, int, error) {
		l, e := s.CreateLibrary(ctx, w, "Synthetic images", "regular")
		return l, 201, e
	})
	var lib model.Library
	if err = json.Unmarshal(result.Body, &lib); err != nil {
		t.Fatal(err)
	}
	u := stagedUpload(t, s, lib.ID, 129, "warm-source.png")
	result = testMutation(t, s, "warm-complete", func(w *Write) (any, int, error) { v, e := s.CompleteUpload(ctx, w, u.ID); return v, 200, e })
	var committed model.UploadResult
	if err = json.Unmarshal(result.Body, &committed); err != nil || committed.Asset == nil {
		t.Fatal("missing fixture asset", err)
	}
	a := committed.Asset
	dir := "cache/" + a.ID
	for _, name := range []string{dir + "/v2-1-512.jpg", dir + "/leave-alone.txt"} {
		f, e := s.Root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if e != nil {
			t.Fatal(e)
		}
		if e = f.Close(); e != nil {
			t.Fatal(e)
		}
	}
	if err = s.warmPreviews(ctx, previewScope{library: lib.ID, asset: a.ID}); err != nil {
		t.Fatal(err)
	}
	if info, err := s.Root.Stat(previewPath(a.ID, 1, 512, false)); err != nil || info.Size() == 0 {
		t.Fatal("expected warmed medium preview", err)
	}
	if _, err = s.Root.Stat(dir + "/v2-1-512.jpg"); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("obsolete representation remained")
	}
	if _, err = s.Root.Stat(dir + "/leave-alone.txt"); err != nil {
		t.Fatal("unmanaged file was touched", err)
	}
	if err = s.verify(assetPath(*a), a.Hash, a.Size); err != nil {
		t.Fatal("thumbnail maintenance changed original", err)
	}
}

func TestThumbnailWarmupPassesAFullPageOfUnavailableOriginals(t *testing.T) {
	s, run, _ := preparedScanFixture(t)
	ctx := t.Context()
	data := syntheticPNG(t, 39)
	tx, err := s.DB.DB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	var last model.Asset
	for i := 1; i <= 257; i++ {
		id := fmt.Sprintf("%032x", i)
		name := fmt.Sprintf("work-%03d.png", i)
		last = model.Asset{ID: id, LibraryID: run.library.ID, RelativePath: name, FileName: name, Revision: 1, ContentVersion: 1,
			MediaType: "image", MimeType: "image/png", Size: int64(len(data)), Hash: digest(data), Width: 2, Height: 2,
			ThumbnailURL: "/api/v1/assets/" + id + "/thumbnail", AuthorID: "unknown", AuthorIDs: []string{"unknown"}}
		if err = store.SaveAsset(ctx, tx, last); err != nil {
			t.Fatal(err)
		}
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	file, err := s.Root.OpenFile(assetPath(last), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, err = file.Write(data)
	if err = errors.Join(err, file.Close()); err != nil {
		t.Fatal(err)
	}
	if err = s.warmPreviews(ctx, previewScope{library: run.library.ID}); err != nil {
		t.Fatal(err)
	}
	for _, edge := range []int{512, 256, 1024} {
		if info, err := s.Root.Stat(previewPath(last.ID, 1, edge, false)); err != nil || info.Size() == 0 {
			t.Fatalf("missing originals on the first page blocked size %d on the next: %v", edge, err)
		}
	}
}
