package service

import (
	"context"
	"errors"
	"io"
	"os"

	"qimeng.local/next/server/internal/mediafile"
	"qimeng.local/next/server/internal/model"
)

type uploadInspection struct {
	file     *os.File
	info     os.FileInfo
	stamp    string
	hash     string
	metadata model.Asset
	probed   bool
}

func (in *uploadInspection) close() {
	if in != nil && in.file != nil {
		in.file.Close()
		in.file = nil
	}
}

func inspectUploadContent(ctx context.Context, source io.Reader, u model.Upload) (string, error) {
	hash, size, err := mediafile.Inspect(ctx, source, u.FileName, u.MimeType)
	if errors.Is(err, mediafile.ErrContainer) {
		return "", Fail(415, "mime_mismatch", "文件内容与声明格式不匹配。")
	}
	if err != nil {
		return "", err
	}
	if size != u.Size {
		return "", Fail(409, "length_mismatch", "接收长度与声明长度不一致。")
	}
	if u.ExpectedHash != "" && u.ExpectedHash != hash {
		return "", Fail(409, "source_hash_mismatch", "接收内容与源文件快照摘要不符，未入库。请保留源文件并以新上传会话重新发送。")
	}
	return hash, nil
}

// The admission slot keeps expensive completion reads bounded and prevents
// same-upload retries from probing in parallel. Unrelated catalog writes do
// not wait for it. Run still checks the current credential and canonical
// receipt before applying any inspected result or returning an inspection error.
func (s *Service) RunUploadCompletion(ctx context.Context, p model.Principal, id, operation, key string, input []byte) (Result, error) {
	if len(key) < 8 || len(key) > 128 {
		return Result{}, Fail(400, "invalid_idempotency_key", "幂等键长度应为 8–128。")
	}
	ctx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(s.workCtx, cancel)
	defer func() { stop(); cancel() }()
	select {
	case s.uploadVerify <- struct{}{}:
		defer func() { <-s.uploadVerify }()
	case <-ctx.Done():
		return Result{}, ctx.Err()
	case <-s.workCtx.Done():
		return Result{}, s.workCtx.Err()
	}
	p, err := s.currentWritePrincipal(ctx, p)
	if err != nil {
		return Result{}, err
	}
	prepared, inspectionErr := s.prepareUploadInspection(ctx, p, id)
	defer prepared.close()
	return s.Run(ctx, p, operation, key, input, func(w *Write) (any, int, error) {
		if prepared != nil {
			w.preparedUpload = id
		}
		result, err := s.completeUpload(ctx, w, id, prepared, inspectionErr)
		return result, 200, err
	})
}

func (s *Service) prepareUploadInspection(ctx context.Context, p model.Principal, id string) (*uploadInspection, error) {
	u, err := s.Upload(ctx, p, id)
	if err != nil {
		return nil, err
	}
	if err = Authorize(p, "ingest", u.BatchID); err != nil {
		return nil, err
	}
	if !syncPlatformSupported || (u.State != "receiving" && u.State != "staged") || u.Offset != u.Size ||
		(u.Mode == "intelligent" && u.Hash != "") || u.ErrorCode == "publication_pending" {
		return nil, nil
	}
	if err = s.uploadsAllowed(ctx); err != nil {
		return nil, err
	}
	batch, err := s.Batch(ctx, u.BatchID)
	if err != nil || batch.State == "cancelled" {
		return nil, err
	}
	name := uploadPath(id)
	if err = s.safe(name); err != nil {
		return nil, err
	}
	file, err := s.Root.Open(name)
	if err != nil {
		return nil, err
	}
	evidence, err := s.inspectOpenedUpload(ctx, file, u)
	if err != nil {
		file.Close()
		return nil, err
	}
	return evidence, nil
}

func (s *Service) inspectOpenedUpload(ctx context.Context, file *os.File, u model.Upload) (*uploadInspection, error) {
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() != u.Size {
		return nil, Fail(409, "length_mismatch", "接收长度与声明长度不一致。")
	}
	stamp, err := syncFileStamp(file, info)
	if err != nil {
		return nil, err
	}
	checked := &uploadInspection{file: file, info: info, stamp: stamp}
	checked.hash, err = inspectUploadContent(ctx, file, u)
	if err != nil {
		return nil, err
	}
	// Native image readers only need a section reader. On Linux and Android
	// ffprobe receives this very file descriptor. A platform without inherited
	// descriptors uses its existing protected path probe during publication.
	if u.Mode == "ordinary" && (inheritedMediaFD || builtinImage(u.MimeType)) {
		checked.metadata = model.Asset{ID: "inspection", MimeType: u.MimeType}
		if _, err = file.Seek(0, io.SeekStart); err != nil {
			return nil, err
		}
		if err = s.probeMedia(ctx, mediaInput{name: uploadPath(u.ID), file: file, size: u.Size}, &checked.metadata); err != nil {
			return nil, publicationError("probe", err)
		}
		checked.probed = true
	}
	return checked, nil
}

// Called with the writer held. The original descriptor remains open until the
// live pathname, inode and native change clock have all been rechecked. This
// also detects equal-size rewrites whose modification time was restored.
func (s *Service) checkUploadInspection(ctx context.Context, u model.Upload, checked *uploadInspection) error {
	library, err := s.library(ctx, u.LibraryID)
	if err != nil {
		return err
	}
	if !library.Enabled {
		return Fail(409, "library_disabled", "库已停用。")
	}
	name := uploadPath(u.ID)
	if err = s.safe(name); err != nil {
		return err
	}
	current, err := s.Root.Open(name)
	if err != nil {
		return err
	}
	defer current.Close()
	info, err := current.Stat()
	if err != nil {
		return err
	}
	stamp, err := syncFileStamp(current, info)
	if err != nil {
		return err
	}
	if info.Size() != u.Size || !os.SameFile(checked.info, info) || checked.stamp != stamp ||
		(u.Hash != "" && u.Hash != checked.hash) || (u.ExpectedHash != "" && u.ExpectedHash != checked.hash) {
		return Fail(409, "content_mismatch", "上传暂存的文件对象或内容在校验期间改变，原件已保留。")
	}
	checked.close()
	return nil
}
