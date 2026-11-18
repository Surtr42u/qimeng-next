package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"qimeng.local/next/server/internal/model"
)

func TestCancellationCannotErasePreparedPublication(t *testing.T) {
	ctx := context.Background()
	s, e := New(Config{DataDir: t.TempDir(), AdminToken: "synthetic-pending-cancellation-00000000000", ReserveBytes: 1})
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	r := testMutation(t, s, "cancel-test-library", func(w *Write) (any, int, error) {
		v, e := s.CreateLibrary(ctx, w, "Synthetic cancellation", "regular")
		return v, 201, e
	})
	var library model.Library
	if e = json.Unmarshal(r.Body, &library); e != nil {
		t.Fatal(e)
	}
	u := stagedUpload(t, s, library.ID, 12, "pending.png")
	now := time.Now().UTC()
	asset := model.Asset{ID: digest([]byte("asset:" + u.ID))[:32], LibraryID: library.ID, FileName: u.FileName, OriginalFileName: u.FileName, RelativePath: u.FileName, MediaType: "image", MimeType: "image/png", Size: u.Size, Hash: u.Hash, Revision: 1, TagsRevision: 1, AuthorsRevision: 1, ContentVersion: 1, AuthorID: "unknown", AuthorIDs: []string{"unknown"}, Tags: []string{}, CreatedAt: now, UpdatedAt: now}
	owner := model.Principal{ID: "owner", Scopes: []string{"admin"}}
	write := &Write{s: s, ctx: ctx, Principal: owner, operation: "publish-pending", key: "publish-pending-key", inputHash: digest(nil)}
	_, e = s.prepareFile(ctx, write, fileIntent{ID: digest([]byte("publish:" + u.ID))[:32], Kind: "ingest", Source: uploadPath(u.ID), Target: assetPath(asset), UploadID: u.ID, After: asset})
	if e != nil {
		t.Fatal(e)
	}
	_, e = s.Run(ctx, owner, "cancel-pending", "cancel-pending-key", nil, func(w *Write) (any, int, error) { return nil, 204, s.CancelUpload(ctx, w, u.ID) })
	var problem *Error
	if !errors.As(e, &problem) || problem.Code != "publication_pending" {
		t.Fatalf("prepared publication was cancelled: %v", e)
	}
	preserved, e := s.Upload(ctx, owner, u.ID)
	if e != nil || preserved.State != "staged" {
		t.Fatal("failed cancellation changed the original input")
	}
	batch, e := s.Batch(ctx, u.BatchID)
	if e != nil {
		t.Fatal(e)
	}
	_, e = s.Run(ctx, owner, "cancel-pending-batch", "cancel-pending-batch-key", nil, func(w *Write) (any, int, error) {
		b, e := s.CancelBatch(ctx, w, batch.ID, batch.Revision)
		return b, 200, e
	})
	if !errors.As(e, &problem) || problem.Code != "publication_pending" {
		t.Fatalf("batch cancellation stranded its prepared publication: %v", e)
	}
	still, e := s.Batch(ctx, batch.ID)
	if e != nil || still.State != batch.State || still.Revision != batch.Revision {
		t.Fatal("rejected batch cancellation changed its manifest", e)
	}
}
