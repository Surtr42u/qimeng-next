package service

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"qimeng.local/next/server/internal/model"
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
	go func() { result <- s.previews.join(ctx, s, "one", false, generate) }()
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
