package service

import (
	"context"
	"errors"
	"os"
	"path"

	"qimeng.local/next/server/internal/model"
	"qimeng.local/next/server/internal/store"
)

// Older instances could accept a cancellation with an ingest still prepared.
// Preserve the staging original and move any verified, uncommitted target out
// of the scanner's reach before releasing the reservation. Never publish it.
func (s *Service) retainCancelledPublication(ctx context.Context, j *fileIntent, u model.Upload) error {
	if err := s.verifyFileIdentity(j.Source, j.SourceObject); err != nil {
		return err
	}
	if err := s.verifyContext(ctx, j.Source, j.After.Hash, j.After.Size); err != nil {
		return err
	}
	hold := path.Join("libraries", j.After.LibraryID, ".qimeng-internal", "publications", j.ID+".cancelled")
	if _, err := s.Root.Lstat(j.Target); err == nil {
		if err = s.checkPath(ctx, j.After.LibraryID, j.After.RelativePath, ""); err != nil {
			return err
		}
		object := j.CopyObject
		if !j.CopyPublication {
			object, err = s.managedFileIdentity(j.Source)
			if err != nil {
				return err
			}
		}
		if object == "" {
			return Fail(409, "publication_target_changed", "缺少目标文件身份，已保留原件与目标供核对。")
		}
		if err = s.ensureParent(hold); err != nil {
			return err
		}
		if err = s.retireForCopy(j.Target, hold, object, j.After.Hash, j.After.Size); err != nil {
			return err
		}
		// A platform using link then unlink can be interrupted with both names.
		if err = s.removeFileObject(j.Target, object, j.After.Hash, j.After.Size); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if _, err := s.Root.Lstat(hold); err == nil {
		if err = s.syncPublicationMove(j.Target, hold); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	tx, err := s.DB.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if u.State != "cancelled" {
		u.State = "cancelled"
		if err = store.SaveUpload(ctx, tx, u); err != nil {
			return err
		}
	}
	j.State = "cancelled"
	data, err := store.Encode(j)
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE file_intents SET state=?,data=? WHERE id=?", j.State, data, j.ID); err != nil {
		return err
	}
	return tx.Commit()
}
