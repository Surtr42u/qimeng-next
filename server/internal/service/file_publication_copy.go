package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path"
	"syscall"
)

func publicationError(stage string, err error) error {
	if err == nil {
		return nil
	}
	var app *Error
	if errors.As(err, &app) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	details := map[string]any{"stage": stage}
	var errno syscall.Errno
	if errors.As(err, &errno) {
		details["systemCode"] = int64(errno)
	}
	return &Error{Code: "publication_" + stage + "_failed", Status: 500, Details: details,
		Message: "文件发布暂未完成（" + stage + "），请保留来源并使用同一操作身份重试。"}
}

func (s *Service) publicationStamp(name string) (string, error) {
	file, err := s.Root.Open(name)
	if err != nil {
		return "", err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return "", err
	}
	stamp, err := syncFileStamp(file, info)
	if err != nil {
		return "", err
	}
	return digest([]byte(stamp)), nil
}

func (s *Service) syncPublicationMove(from, to string) error {
	if err := s.syncParent(to); err != nil {
		return err
	}
	if path.Dir(from) != path.Dir(to) {
		return s.syncParent(from)
	}
	return nil
}

// Retire an existing name without overwriting a bystander's file. The source
// remains available under the private hold across a crash before publication.
func (s *Service) retireForCopy(from, to, object, hash string, size int64) error {
	if _, err := s.Root.Lstat(to); err == nil {
		if err = s.verifyFileIdentity(to, object); err != nil {
			return err
		}
		return s.verify(to, hash, size)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := s.verifyFileIdentity(from, object); err != nil {
		return err
	}
	if err := s.verify(from, hash, size); err != nil {
		return err
	}
	if err := s.Root.PublishTemp(from, to); err != nil {
		return err
	}
	return s.syncPublicationMove(from, to)
}

func (s *Service) copyTargetReady(j *fileIntent) (bool, error) {
	if j.CopyObject == "" {
		return false, nil
	}
	id, err := s.managedFileIdentity(j.Target)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if id != j.CopyObject {
		return false, nil
	}
	if j.CopyStamp != "" {
		stamp, err := s.publicationStamp(j.Target)
		if err != nil {
			return false, err
		}
		if stamp != j.CopyStamp {
			return false, Fail(409, "publication_target_changed", "目标文件对象已变化，原件与目标均保留。")
		}
	}
	return true, s.verify(j.Target, j.After.Hash, j.After.Size)
}

// The application owns a private copy on filesystems where it cannot create
// hard links. Persist the inode before writing it, then publish that complete
// file with no-replace rename. The original is retained until the catalog commits.
func (s *Service) stagePublicationCopy(ctx context.Context, j *fileIntent, source, stage string) error {
	if err := s.verifyFileIdentity(source, j.SourceObject); err != nil {
		return err
	}
	stamp, err := s.publicationStamp(source)
	if err != nil {
		return err
	}
	if j.SourceStamp != "" && j.SourceStamp != stamp {
		return Fail(409, "original_object_changed", "待发布来源的版本已改变，已保留供核对。")
	}
	if err = s.ensureParent(stage); err != nil {
		return err
	}
	out, err := s.Root.OpenFile(stage, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	newFile := err == nil
	if errors.Is(err, os.ErrExist) {
		out, err = s.Root.OpenFile(stage, os.O_RDWR, 0600)
	}
	if err != nil {
		return err
	}
	defer out.Close()
	info, err := out.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return Fail(409, "publication_stage_changed", "发布暂存对象不再是普通文件。")
	}
	id, err := fileObjectIdentity(out, info)
	if err != nil {
		return err
	}
	copyID := digest([]byte(id))
	if !newFile && (j.CopyObject != "" && j.CopyObject != copyID || j.CopyObject == "" && info.Size() != 0) {
		return Fail(409, "publication_stage_changed", "已有发布暂存与原操作不符，已保留文件。")
	}
	j.CopyObject = copyID
	j.SourceStamp = stamp
	j.CopyStamp = ""
	j.CopyPublication = true
	if err = s.saveIntent(ctx, *j); err != nil {
		return err
	}
	if err = s.syncParent(stage); err != nil {
		return err
	}
	in, err := s.Root.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	original, err := in.Stat()
	if err != nil {
		return err
	}
	if original.Size() != j.After.Size {
		return Fail(409, "content_mismatch", "来源长度已变化。")
	}
	location, err := s.Root.LibraryPath(j.After.LibraryID)
	if err != nil {
		return err
	}
	free, err := freeBytes(location)
	if err != nil {
		return err
	}
	if free-s.Config.ReserveBytes < j.After.Size-info.Size() {
		return Fail(507, "insufficient_storage", "存储余量不足以安全发布并保留原件。")
	}
	if err = out.Truncate(0); err != nil {
		return err
	}
	hash := sha256.New()
	_, err = io.CopyN(io.MultiWriter(&restoreWriter{ctx: ctx, root: location, reserve: s.Config.ReserveBytes, file: out}, hash), in, j.After.Size)
	if err != nil {
		return err
	}
	if hex.EncodeToString(hash.Sum(nil)) != j.After.Hash {
		return Fail(409, "content_mismatch", "来源字节与发布意图不一致。")
	}
	if err = s.Root.Chtimes(stage, original.ModTime(), original.ModTime()); err != nil {
		return err
	}
	if err = out.Sync(); err != nil {
		return err
	}
	finalStamp, err := s.publicationStamp(source)
	if err != nil {
		return err
	}
	if finalStamp != j.SourceStamp {
		return Fail(409, "original_object_changed", "来源在复制期间变化。")
	}
	return nil
}

func (s *Service) applyCopiedFile(ctx context.Context, j *fileIntent) error {
	ready, err := s.copyTargetReady(j)
	if err != nil {
		return err
	}
	if ready {
		if j.CopyStamp != "" {
			return nil
		}
		j.CopyStamp, err = s.publicationStamp(j.Target)
		if err != nil {
			return err
		}
		return s.saveIntent(ctx, *j)
	}
	source := j.Source
	if j.SpellingOnly {
		source = spellingHold(*j)
		if err = s.retireForCopy(j.Source, source, j.SourceObject, j.After.Hash, j.After.Size); err != nil {
			return publicationError("retain_source", err)
		}
	}
	var occupied bool
	if _, err = s.Root.Lstat(j.Target); err == nil {
		occupied = true
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	replaceTarget := j.Kind == "replace" && j.Before != nil && assetPath(*j.Before) == j.Target
	if occupied {
		if !replaceTarget {
			return Fail(409, "publication_target_changed", "目标已有其他文件，未覆盖或清理来源。")
		}
		if err = s.verifyFileIdentity(j.Target, j.OldObject); err != nil {
			return err
		}
	}
	stage := path.Join("libraries", j.After.LibraryID, ".qimeng-internal", "publications", j.ID+".part")
	if err = s.stagePublicationCopy(ctx, j, source, stage); err != nil {
		return publicationError("stage_copy", err)
	}
	if replaceTarget {
		if err = s.retireForCopy(j.Target, j.Retired, j.OldObject, j.Before.Hash, j.Before.Size); err != nil {
			return publicationError("retain_original", err)
		}
	}
	if err = s.Root.PublishTemp(stage, j.Target); err != nil {
		if errors.Is(err, os.ErrExist) {
			return Fail(409, "path_occupied", "目标名称已被占用，已保留来源和暂存。")
		}
		return publicationError("publish_name", err)
	}
	if err = s.syncPublicationMove(stage, j.Target); err != nil {
		return publicationError("sync_directories", err)
	}
	ready, err = s.copyTargetReady(j)
	if err != nil {
		return err
	}
	if !ready {
		return Fail(409, "publication_target_changed", "发布后的对象身份发生变化，已保留来源。")
	}
	j.CopyStamp, err = s.publicationStamp(j.Target)
	if err != nil {
		return err
	}
	return s.saveIntent(ctx, *j)
}

func (s *Service) finishCopiedFile(ctx context.Context, j *fileIntent) error {
	if err := s.applyCopiedFile(ctx, j); err != nil {
		return err
	}
	source := j.Source
	if j.SpellingOnly {
		source = spellingHold(*j)
	}
	if _, err := s.Root.Lstat(source); err == nil {
		stamp, err := s.publicationStamp(source)
		if err != nil {
			return err
		}
		if stamp != j.SourceStamp {
			return Fail(409, "original_object_changed", "源位置已变化，未清理源文件。")
		}
		if err = s.removeFileObject(source, j.SourceObject, j.After.Hash, j.After.Size); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if j.Kind == "replace" && j.Before != nil {
		old := assetPath(*j.Before)
		if old == j.Target {
			old = j.Retired
		}
		return s.removeFileObject(old, j.OldObject, j.Before.Hash, j.Before.Size)
	}
	return nil
}
