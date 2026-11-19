package service

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"

	"qimeng.local/next/server/internal/model"
)

func TestCancellationAndLibraryDisableReachScanFileReads(t *testing.T) {
	ctx := context.Background()
	s, err := New(Config{DataDir: t.TempDir(), AdminToken: "synthetic-scan-cancellation-owner-token", ReserveBytes: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var library model.Library
	r := testMutation(t, s, "scan-cancel-library", func(w *Write) (any, int, error) {
		v, e := s.CreateLibrary(ctx, w, "Synthetic", "regular")
		return v, 201, e
	})
	if err = json.Unmarshal(r.Body, &library); err != nil {
		t.Fatal(err)
	}
	name := "libraries/" + library.ID + "/read.png"
	file, err := s.Root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, err = file.Write(syntheticPNG(t, 45))
	closeErr := file.Close()
	if err != nil || closeErr != nil {
		t.Fatal(err, closeErr)
	}
	// Fill the admission budget so both jobs are observably alive and no
	// synthetic timing delay or large media fixture is needed.
	s.mu.Lock()
	s.activeScans = 32
	s.mu.Unlock()
	workerContext := func(id string) context.Context {
		t.Helper()
		s.mu.Lock()
		worker, ok := s.scanWorkers[id]
		s.mu.Unlock()
		if !ok {
			t.Fatal("missing queued worker")
		}
		return worker.ctx
	}
	assertStopped := func(worker context.Context) {
		t.Helper()
		if _, _, err := s.hashFileContext(worker, name); !errors.Is(err, context.Canceled) {
			t.Fatal("scan read continued after cancellation", err)
		}
		if s.workCtx.Err() != nil {
			t.Fatal("cancelling an individual scan stopped the service")
		}
	}
	var manual ScanJob
	r = testMutation(t, s, "start-manual-queued", func(w *Write) (any, int, error) { v, e := s.StartScan(ctx, w, library.ID, 1); return v, 202, e })
	if err = json.Unmarshal(r.Body, &manual); err != nil {
		t.Fatal(err)
	}
	m := workerContext(manual.ID)
	testMutation(t, s, "cancel-one-scan", func(w *Write) (any, int, error) { v, e := s.CancelScan(ctx, w, manual.ID); return v, 200, e })
	assertStopped(m)
	if err = s.enqueueMonitoredScan(ctx, library.ID, "watch", scanTargets{inventory: true}); err != nil {
		t.Fatal(err)
	}
	page, err := s.Scans(ctx, library.ID, "queued", "", 10)
	if err != nil || len(page.Items) != 1 {
		t.Fatal("missing automatic scan", err)
	}
	auto := workerContext(page.Items[0].ID)
	off := false
	testMutation(t, s, "disable-scanned-library", func(w *Write) (any, int, error) {
		v, e := s.UpdateLibrary(ctx, w, library.ID, LibraryUpdate{ExpectedRevision: 1, Enabled: &off})
		return v, 200, e
	})
	assertStopped(auto)
}
