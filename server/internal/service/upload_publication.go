package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"

	"qimeng.local/next/server/internal/model"
	"qimeng.local/next/server/internal/store"
)

// The caller has a durable file intent reserving this upload and its target.
// No open SQLite transaction or other queued callbacks run during this IO.
func (w *Write) reservedUploadIO(work func() error) error {
	if w.tx != nil {
		return errors.New("reserved upload IO cannot run inside a catalog transaction")
	}
	w.s.mu.Unlock()
	defer w.s.mu.Lock()
	return work()
}

// An ingest has no previously visible asset at the new target. Replacement
// keeps its name switch linked to the old catalog version under the writer.
// Both operations can release the writer for post-commit verified cleanup.
func (s *Service) applyReservedUpload(ctx context.Context, w *Write, j *fileIntent) error {
	var verified string
	err := w.reservedUploadIO(func() error {
		return s.applyFileWithCheck(ctx, j, func() error {
			var err error
			verified, err = s.verifiedUploadTarget(ctx, *j)
			return err
		})
	})
	if err != nil {
		return err
	}
	principal, err := s.currentWritePrincipal(ctx, w.Principal)
	if err != nil {
		return err
	}
	upload, err := store.Get[model.Upload](ctx, s.DB.DB, "SELECT data FROM uploads WHERE id=?", j.UploadID)
	if err != nil {
		return err
	}
	if err = Authorize(principal, "ingest", upload.BatchID); err != nil {
		return err
	}
	if upload.State != "staged" || upload.Hash != j.After.Hash || upload.Size != j.After.Size {
		return Fail(409, "publication_changed", "上传版本已变化，请回读上传状态。")
	}
	batch, err := s.Batch(ctx, upload.BatchID)
	if err != nil {
		return err
	}
	if batch.State == "cancelled" {
		return Fail(409, "batch_cancelled", "批次已取消，已接收内容保留。")
	}
	library, err := s.library(ctx, upload.LibraryID)
	if err != nil {
		return err
	}
	if !library.Enabled {
		return Fail(409, "library_disabled", "库已停用。")
	}
	if err = s.checkPath(ctx, j.After.LibraryID, j.After.RelativePath, j.After.ID); err != nil {
		return err
	}
	stamp, err := s.publicationStamp(j.Target)
	if err != nil {
		return err
	}
	if verified != stamp {
		return Fail(409, "publication_target_changed", "已验证的目标文件发生变化，来源和发布意图已保留。")
	}
	// commitFile claims this same prepared intent in the catalog transaction.
	return s.validatePublicationBase(ctx, s.DB.DB, *j)
}

// Verify the actual installed object and return its content change stamp. The
// writer accepts this evidence only after rereading that same stamp at commit.
func (s *Service) verifiedUploadTarget(ctx context.Context, j fileIntent) (string, error) {
	if err := s.safe(j.Target); err != nil {
		return "", err
	}
	file, err := s.Root.Open(j.Target)
	if err != nil {
		return "", err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return "", err
	}
	object, err := fileObjectIdentity(file, info)
	if err != nil {
		return "", err
	}
	expected := j.SourceObject
	if j.CopyPublication {
		expected = j.CopyObject
	}
	if expected == "" || expected != digest([]byte(object)) || !info.Mode().IsRegular() || info.Size() != j.After.Size {
		return "", Fail(409, "publication_target_changed", "发布落点与已保留的原件不符。")
	}
	start, err := syncFileStamp(file, info)
	if err != nil {
		return "", err
	}
	hash := sha256.New()
	size, err := io.Copy(hash, scanReader{ctx: ctx, source: file})
	if err != nil {
		return "", err
	}
	info, err = file.Stat()
	if err != nil {
		return "", err
	}
	end, err := syncFileStamp(file, info)
	if err != nil {
		return "", err
	}
	if start != end || size != j.After.Size || hex.EncodeToString(hash.Sum(nil)) != j.After.Hash {
		return "", Fail(409, "content_mismatch", "发布目标的内容或版本已变化，原件已保留。")
	}
	return digest([]byte(end)), nil
}
