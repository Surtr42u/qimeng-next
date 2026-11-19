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
		var err error
		verified, err = s.installVerifiedUpload(ctx, j)
		return err
	})
	if err != nil {
		return err
	}
	return s.validateReservedUpload(ctx, w.Principal, j, verified)
}

func (s *Service) installVerifiedUpload(ctx context.Context, j *fileIntent) (string, error) {
	var verified string
	err := s.applyFileWithCheck(ctx, j, func() error {
		var err error
		verified, err = s.verifiedUploadTarget(ctx, *j)
		return err
	})
	return verified, err
}

func (s *Service) validateReservedUpload(ctx context.Context, p model.Principal, j *fileIntent, verified string) error {
	upload, err := store.Get[model.Upload](ctx, s.DB.DB, "SELECT data FROM uploads WHERE id=?", j.UploadID)
	if err != nil {
		return err
	}
	if err = s.checkUploadPublication(ctx, p, upload, j); err != nil {
		return err
	}
	return s.validateUploadInstallation(ctx, j, verified)
}

func (s *Service) checkUploadPublication(ctx context.Context, p model.Principal, upload model.Upload, j *fileIntent) error {
	if upload.LibraryID != j.After.LibraryID || upload.Hash != j.After.Hash || upload.Size != j.After.Size {
		return Fail(409, "publication_changed", "上传版本已变化，请回读上传状态。")
	}
	if upload.State == "cancelled" {
		return Fail(409, "upload_cancelled", "上传已取消。")
	}
	batch, err := s.Batch(ctx, upload.BatchID)
	if err != nil {
		return err
	}
	if batch.State == "cancelled" {
		return Fail(409, "batch_cancelled", "批次已取消，已接收内容保留。")
	}
	principal, err := s.currentWritePrincipal(ctx, p)
	if err != nil {
		return err
	}
	if err = Authorize(principal, "ingest", upload.BatchID); err != nil {
		return err
	}
	if upload.State != "staged" || upload.Offset != upload.Size {
		return Fail(409, "publication_changed", "上传不再处于可发布状态，请回读上传状态。")
	}
	if err = s.uploadsAllowed(ctx); err != nil {
		return err
	}
	if err = s.checkUploadLibrary(ctx, upload.LibraryID); err != nil {
		return err
	}
	if err = s.checkPath(ctx, j.After.LibraryID, j.After.RelativePath, j.After.ID); err != nil {
		return err
	}
	if upload.Mode == "intelligent" {
		return s.checkPlannedPublication(ctx, principal, upload, j)
	}
	return nil
}

func (s *Service) checkUploadLibrary(ctx context.Context, id string) error {
	library, err := s.library(ctx, id)
	if err != nil {
		return err
	}
	if !library.Enabled {
		return Fail(409, "library_disabled", "库已停用。")
	}
	return nil
}

func (s *Service) validateUploadInstallation(ctx context.Context, j *fileIntent, verified string) error {
	if err := s.verifyFileIdentity(j.Source, j.SourceObject); err != nil {
		return err
	}
	if j.CopyPublication {
		sourceStamp, err := s.publicationStamp(j.Source)
		if err != nil {
			return err
		}
		if sourceStamp != j.SourceStamp {
			return Fail(409, "original_object_changed", "来源在复制后发生变化，已保留供核对。")
		}
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

// Startup has no authenticated requester. Resume an ingest only while
// the recorded credential and its lease can still authorize that staged input.
// Policy holds preserve the installed object, staging file and reservation.
func (s *Service) installRecoveringFile(ctx context.Context, j *fileIntent) (bool, error) {
	if j.Kind != "ingest" || j.UploadID == "" {
		return true, s.applyFile(ctx, j)
	}
	u, err := store.Get[model.Upload](ctx, s.DB.DB, "SELECT data FROM uploads WHERE id=?", j.UploadID)
	if err != nil {
		return false, err
	}
	p := model.Principal{ID: j.PrincipalID, Generation: j.PrincipalGeneration}
	if p.ID == "owner" {
		p.Scopes = []string{"admin"}
	}
	if err = s.checkUploadPublication(ctx, p, u, j); err == nil {
		if j.SourceObject == "" {
			// Older intents used byte verification and no-replace installation.
			err = s.applyFile(ctx, j)
		} else {
			var verified string
			verified, err = s.installVerifiedUpload(ctx, j)
			if err == nil {
				err = s.validateUploadInstallation(ctx, j, verified)
			}
		}
		if err != nil {
			return false, err
		}
		// A time-limited credential can expire even during exclusive startup.
		err = s.checkUploadPublication(ctx, p, u, j)
	}
	var problem *Error
	if errors.As(err, &problem) {
		switch problem.Code {
		case "upload_cancelled", "batch_cancelled":
			return false, s.retainCancelledPublication(ctx, j, u)
		case "library_disabled", "uploads_paused", "revoked", "expired", "lease_changed", "delegation_inactive", "scope_denied", "batch_scope_denied":
			return false, nil
		case "namespace_changed", "revision_conflict", "plan_result_changed", "plan_identity_changed", "plan_input_conflict", "plan_content_changed", "research_conflict", "research_author_unverified", "human_field_protected", "path_occupied":
			if u.Mode == "intelligent" {
				return false, nil
			}
		}
	}
	return err == nil, err
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
	if expected == "" || expected != digest([]byte(object)) || !info.Mode().IsRegular() || info.Size() != j.After.Size || j.After.ContentMtime != nil && !info.ModTime().Equal(*j.After.ContentMtime) {
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
