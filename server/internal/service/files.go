package service

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"runtime"
	"strings"
	"time"

	"qimeng.local/next/server/internal/domain"
	"qimeng.local/next/server/internal/model"
	"qimeng.local/next/server/internal/store"
)

type fileIntent struct {
	ID, Kind, Source, Target, Retired, UploadID string
	PrincipalID, Operation, Key, InputHash      string
	Before                                      *model.Asset
	After                                       model.Asset
	COSAuthor                                   *model.Author  `json:"cosAuthor,omitempty"`
	COSWork                                     *model.CosWork `json:"cosWork,omitempty"`
	State                                       string
	SourceObject                                string                `json:"sourceObject,omitempty"`
	OldObject                                   string                `json:"oldObject,omitempty"`
	CopyPublication                             bool                  `json:"copyPublication,omitempty"`
	CopyObject                                  string                `json:"copyObject,omitempty"`
	CopyStamp                                   string                `json:"copyStamp,omitempty"`
	SourceStamp                                 string                `json:"sourceStamp,omitempty"`
	SpellingOnly                                bool                  `json:"spellingOnly,omitempty"`
	MaintenanceTask                             *AssetMaintenanceTask `json:"assetMaintenanceTask,omitempty"`
}

func assetPath(a model.Asset) string {
	if a.Trashed {
		if a.StorageLayoutVersion >= 2 {
			return "library-trash/" + a.LibraryID + "/" + a.ID + "/" + a.FileName
		}
		return "trash/" + a.ID + "/" + a.FileName
	}
	return "libraries/" + a.LibraryID + "/" + a.RelativePath
}
func (s *Service) safe(pathname string) error {
	if !domain.ValidRelative(pathname) {
		return Fail(400, "invalid_path", "路径不合法。")
	}
	parts := strings.Split(pathname, "/")
	for i := range parts {
		info, e := s.Root.Lstat(strings.Join(parts[:i+1], "/"))
		if errors.Is(e, os.ErrNotExist) {
			return nil
		}
		if e != nil {
			return e
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return Fail(400, "symlink_denied", "受管文件路径不能包含链接。")
		}
	}
	return nil
}
func (s *Service) ensureParent(name string) error {
	if e := s.safe(name); e != nil {
		return e
	}
	return s.Root.MkdirAll(path.Dir(name), 0700)
}
func (s *Service) hashFile(name string) (string, int64, error) {
	return s.hashFileContext(context.Background(), name)
}
func (s *Service) hashFileContext(ctx context.Context, name string) (string, int64, error) {
	if e := s.safe(name); e != nil {
		return "", 0, e
	}
	f, e := s.Root.Open(name)
	if e != nil {
		return "", 0, e
	}
	defer f.Close()
	var input io.Reader = f
	if ctx.Done() != nil {
		input = scanReader{ctx: ctx, source: f}
	}
	h := sha256.New()
	n, e := io.Copy(h, input)
	if e != nil {
		return "", n, e
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}
func (s *Service) verify(name, hash string, size int64) error {
	return s.verifyContext(context.Background(), name, hash, size)
}

func (s *Service) verifyContext(ctx context.Context, name, hash string, size int64) error {
	actual, n, e := s.hashFileContext(ctx, name)
	if e != nil {
		return e
	}
	if actual != hash || n != size {
		return Fail(409, "content_mismatch", "文件内容发生变化，已保留文件供恢复核对。")
	}
	return nil
}
func (s *Service) linkNoReplace(source, target string) error {
	if e := s.safe(source); e != nil {
		return e
	}
	if e := s.ensureParent(target); e != nil {
		return e
	}
	if e := s.Root.Link(source, target); e != nil {
		if !errors.Is(e, os.ErrExist) {
			return e
		}
		a, ae := s.Root.Stat(source)
		b, be := s.Root.Stat(target)
		if ae != nil {
			return ae
		}
		if be != nil {
			return be
		}
		if !os.SameFile(a, b) {
			return Fail(409, "path_occupied", "目标已有其他文件，不会覆盖。")
		}
	}
	return s.syncParent(target)
}
func (s *Service) syncParent(name string) error {
	f, e := s.Root.OpenParent(name)
	if e != nil {
		return e
	}
	defer f.Close()
	return syncDirectory(f)
}
func (s *Service) removeVerified(name, hash string, size int64) error {
	if _, e := s.Root.Lstat(name); errors.Is(e, os.ErrNotExist) {
		return nil
	} else if e != nil {
		return e
	}
	if e := s.verify(name, hash, size); e != nil {
		return e
	}
	if e := s.Root.Remove(name); e != nil {
		return e
	}
	return s.syncParent(name)
}
func (s *Service) saveIntent(ctx context.Context, j fileIntent) error {
	raw, e := store.Encode(j)
	if e != nil {
		return e
	}
	_, e = s.DB.DB.ExecContext(ctx, "INSERT INTO file_intents(id,kind,state,data) VALUES(?,?,?,?) ON CONFLICT(id) DO UPDATE SET state=excluded.state,data=excluded.data", j.ID, j.Kind, j.State, raw)
	return e
}
func (s *Service) prepareFile(ctx context.Context, w *Write, j fileIntent) (fileIntent, error) {
	if w.tx != nil {
		return j, errors.New("file intent must precede transaction")
	}
	old, e := store.Get[fileIntent](ctx, s.DB.DB, "SELECT data FROM file_intents WHERE id=?", j.ID)
	if e == nil {
		if old.Kind != j.Kind || old.UploadID != j.UploadID || old.Operation != w.operation {
			return j, Fail(409, "publication_pending", "此输入已有不同发布操作，须先回读原操作并恢复，不能用新计划覆盖。")
		}
		return old, nil
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return j, e
	}
	if e = s.reservePublication(ctx, j); e != nil {
		return j, e
	}
	if j.Target != "" && j.Kind != "move" {
		_, e = s.Root.Lstat(j.Target)
		permitted := j.Kind == "replace" && j.Before != nil && assetPath(*j.Before) == j.Target
		if e == nil && !permitted {
			return j, Fail(409, "path_occupied", "目标已有文件，未开始发布。")
		}
		if e != nil && !errors.Is(e, os.ErrNotExist) {
			return j, e
		}
	}
	// Validate the exact author/list transaction before the first external write.
	// A malformed declaration or a 21st matching creator must not leave a file
	// intent that can never be recovered on restart.
	if j.Kind != "trash" {
		var applies bool
		if e = s.DB.DB.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM author_lists)").Scan(&applies); e != nil {
			return j, e
		}
		hasCreator := false
		for _, id := range j.After.AuthorIDs {
			if id != "" && id != "unknown" {
				hasCreator = true
				break
			}
		}
		if applies || hasCreator {
			tx, e := s.DB.DB.BeginTx(ctx, nil)
			if e != nil {
				return j, e
			}
			preview := j
			e = saveCOSPlacement(ctx, tx, j.COSAuthor, j.COSWork)
			if e == nil && j.Kind == "purge" {
				e = removeAssetAuthorWorks(ctx, tx, j.After)
			} else if e == nil {
				e = store.SaveAsset(ctx, tx, j.After)
				if e == nil {
					e = s.finalizeAuthorTextForFile(ctx, tx, &preview)
				}
			}
			rollbackErr := tx.Rollback()
			if e != nil {
				return j, e
			}
			if rollbackErr != nil {
				return j, rollbackErr
			}
		}
	}
	if j.Kind == "move" {
		j.SpellingOnly, e = s.classifyMove(j.Source, j.Target)
		if e != nil {
			return j, e
		}
	}
	if j.Source != "" {
		j.SourceObject, e = s.managedFileIdentity(j.Source)
		if e != nil {
			return j, e
		}
	}
	if j.Kind == "replace" && j.Before != nil {
		j.OldObject, e = s.managedFileIdentity(assetPath(*j.Before))
		if e != nil {
			return j, e
		}
	}
	j.CopyPublication = j.CopyPublication || runtime.GOOS == "android" && j.Kind != "purge"
	j.State = "prepared"
	j.PrincipalID = w.Principal.ID
	j.Operation = w.operation
	j.Key = w.key
	j.InputHash = w.inputHash
	return j, s.saveIntent(ctx, j)
}
func (s *Service) applyFile(ctx context.Context, intent *fileIntent) error {
	return s.applyFileWithCheck(ctx, intent, nil)
}

func (s *Service) applyFileWithCheck(ctx context.Context, intent *fileIntent, checkedTarget func() error) error {
	if intent.CopyPublication || runtime.GOOS == "android" && intent.Kind != "purge" {
		if err := s.applyCopiedFile(ctx, intent); err != nil {
			return err
		}
		if checkedTarget != nil {
			return checkedTarget()
		}
		return nil
	}
	j := *intent
	if j.SpellingOnly {
		return s.publishSpelling(j)
	}
	if j.Kind == "purge" {
		return s.removeFileObject(j.Source, j.SourceObject, j.After.Hash, j.After.Size)
	}
	if e := s.verifyFileIdentity(j.Source, j.SourceObject); e != nil {
		return e
	}
	if e := s.verifyContext(ctx, j.Source, j.After.Hash, j.After.Size); e != nil {
		return e
	}
	if j.Kind == "replace" && j.Before != nil {
		oldPath := assetPath(*j.Before)
		if info, e := s.Root.Stat(oldPath); e == nil {
			sourceInfo, se := s.Root.Stat(j.Source)
			if se != nil {
				return se
			}
			if !os.SameFile(info, sourceInfo) {
				if e = s.verifyFileIdentity(oldPath, j.OldObject); e != nil {
					return e
				}
				if e = s.verify(oldPath, j.Before.Hash, j.Before.Size); e != nil {
					return e
				}
				if e = s.linkNoReplace(oldPath, j.Retired); e != nil {
					return e
				}
				sameEntry := oldPath == j.Target
				if !sameEntry && strings.EqualFold(oldPath, j.Target) && path.Dir(oldPath) == path.Dir(j.Target) {
					if targetInfo, err := s.Root.Lstat(j.Target); err == nil && os.SameFile(info, targetInfo) {
						oldName, newName, err := s.exactNames(path.Dir(oldPath), path.Base(oldPath), path.Base(j.Target))
						if err != nil {
							return err
						}
						sameEntry = oldName && !newName
					} else if err != nil && !errors.Is(err, os.ErrNotExist) {
						return err
					}
				}
				if sameEntry {
					if e = s.removeFileObject(oldPath, j.OldObject, j.Before.Hash, j.Before.Size); e != nil {
						return e
					}
				}
			}
		} else if !errors.Is(e, os.ErrNotExist) {
			return e
		} else if e = s.verify(j.Retired, j.Before.Hash, j.Before.Size); e != nil {
			return e
		} else if e = s.verifyFileIdentity(j.Retired, j.OldObject); e != nil {
			return e
		}
	}
	if e := ctx.Err(); e != nil {
		return e
	}
	if e := s.linkNoReplace(j.Source, j.Target); e != nil {
		return e
	}
	if checkedTarget != nil {
		return checkedTarget()
	}
	return s.verify(j.Target, j.After.Hash, j.After.Size)
}
func (s *Service) commitFile(ctx context.Context, q store.Querier, j *fileIntent) error {
	status := 200
	claimed, e := q.ExecContext(ctx, "UPDATE file_intents SET state='committing' WHERE id=? AND state='prepared'", j.ID)
	if e != nil {
		return e
	}
	changed, e := claimed.RowsAffected()
	if e != nil {
		return e
	}
	if changed != 1 {
		return Fail(409, "publication_changed", "文件操作不再处于可提交状态，请回读已有回执。")
	}
	if e := saveCOSPlacement(ctx, q, j.COSAuthor, j.COSWork); e != nil {
		return e
	}
	if j.Kind == "purge" {
		if e := removeAssetAuthorWorks(ctx, q, j.After); e != nil {
			return e
		}
		if _, e := q.ExecContext(ctx, "DELETE FROM asset_records WHERE id=?", j.After.ID); e != nil {
			return e
		}
	} else {
		if e := store.SaveAsset(ctx, q, j.After); e != nil {
			return e
		}
		if j.Kind != "trash" {
			if j.Kind == "ingest" && j.UploadID != "" {
				u, e := store.Get[model.Upload](ctx, q, "SELECT data FROM uploads WHERE id=?", j.UploadID)
				if e != nil {
					return e
				}
				if u.ExplicitTaxonomy {
					if e = protectHumanField(ctx, q, j.After.ID, "taxonomy"); e != nil {
						return e
					}
				}
			}
			changed, e := s.applySourceRules(ctx, q, &j.After)
			if e != nil {
				return e
			}
			if changed {
				if e = store.SaveAsset(ctx, q, j.After); e != nil {
					return e
				}
			}
		}
		if e := s.finalizeAuthorTextForFile(ctx, q, j); e != nil {
			return e
		}
	}
	if j.Kind == "move" {
		if j.MaintenanceTask != nil {
			if e := saveAssetMaintenance(ctx, q, *j.MaintenanceTask); e != nil {
				return e
			}
		} else if e := protectHumanField(ctx, q, j.After.ID, "rename"); e != nil {
			return e
		}
	}
	var receipt any = j.After
	if j.UploadID != "" {
		u, e := store.Get[model.Upload](ctx, q, "SELECT data FROM uploads WHERE id=?", j.UploadID)
		if e != nil {
			return e
		}
		u.AssetID = j.After.ID
		u.PublishedContentVersion = j.After.ContentVersion
		u.State = "ingested"
		u.ErrorCode = ""
		u.ErrorMessage = ""
		if e = store.SaveUpload(ctx, q, u); e != nil {
			return e
		}
		if u.BatchID != "" {
			if e = s.refreshBatch(ctx, q, u.BatchID); e != nil {
				return e
			}
		}
		if e = savePublishedPlanItem(ctx, q, *j); e != nil {
			return e
		}
		receipt = model.UploadResult{Upload: u, Asset: &j.After}
	}
	if j.Kind == "purge" {
		status = 204
	}
	if j.Key != "" {
		body := []byte{}
		if status != 204 {
			raw, e := store.Encode(receipt)
			if e != nil {
				return e
			}
			body = []byte(raw)
		}
		if _, e := q.ExecContext(ctx, "INSERT OR IGNORE INTO idempotency(principal_id,operation,key,input_hash,response,status) VALUES(?,?,?,?,?,?)", j.PrincipalID, j.Operation, j.Key, j.InputHash, body, status); e != nil {
			return e
		}
	}
	j.State = "committed"
	raw, e := store.Encode(j)
	if e != nil {
		return e
	}
	_, e = q.ExecContext(ctx, "UPDATE file_intents SET state=?,data=? WHERE id=?", j.State, raw, j.ID)
	return e
}
func (s *Service) finishFile(ctx context.Context, j fileIntent) error {
	if j.CopyPublication {
		if e := s.finishCopiedFile(ctx, &j); e != nil {
			return e
		}
	} else if j.SpellingOnly {
		if e := s.publishSpelling(j); e != nil {
			return e
		}
		if e := s.verifyFileIdentity(j.Target, j.SourceObject); e != nil {
			return e
		}
		if e := s.removeFileObject(spellingHold(j), j.SourceObject, j.After.Hash, j.After.Size); e != nil {
			return e
		}
	} else if j.Kind != "purge" && j.Source != j.Target {
		if e := s.finishPublishedLink(j); e != nil {
			return e
		}
	}
	if !j.CopyPublication && j.Kind == "replace" && j.Before != nil {
		old := assetPath(*j.Before)
		cleanOld := old != j.Target
		if cleanOld && strings.EqualFold(old, j.Target) && path.Dir(old) == path.Dir(j.Target) {
			oldName, newName, e := s.exactNames(path.Dir(old), path.Base(old), path.Base(j.Target))
			if e != nil {
				return e
			}
			if !oldName && newName {
				cleanOld = false
			}
		}
		if cleanOld {
			original, e := s.Root.Lstat(old)
			if e != nil && !errors.Is(e, os.ErrNotExist) {
				return e
			}
			if e == nil {
				hold, e := s.Root.Lstat(j.Retired)
				if e != nil {
					return e
				}
				if !os.SameFile(original, hold) {
					return Fail(409, "original_object_changed", "旧路径不再指向被替换的原件。")
				}
				if e = s.removeFileObject(old, j.OldObject, j.Before.Hash, j.Before.Size); e != nil {
					return e
				}
			}
		}
		if e := s.removeFileObject(j.Retired, j.OldObject, j.Before.Hash, j.Before.Size); e != nil {
			return e
		}
	}
	j.State = "done"
	if e := s.saveIntent(ctx, j); e != nil {
		return e
	}
	s.previews.changed(j.After.LibraryID, j.After.ID)
	return nil
}
func (s *Service) runFile(ctx context.Context, w *Write, j fileIntent) (model.Asset, error) {
	j.After.FileDate = j.After.EffectiveFileDate()
	if j.Kind == "ingest" || j.Kind == "move" || j.Kind == "restore" {
		var e error
		j.COSAuthor, j.COSWork, e = s.planCOSPlacement(ctx, s.DB.DB, &j.After)
		if e != nil {
			return j.After, e
		}
	}
	if j.Kind == "ingest" && j.After.COSDirectoryAuthorID != "" {
		for _, id := range j.After.AuthorIDs {
			if id == "unknown" || id == j.After.COSDirectoryAuthorID {
				continue
			}
			author, e := store.Get[model.Author](ctx, s.DB.DB, "SELECT data FROM authors WHERE id=?", id)
			if e != nil {
				return j.After, e
			}
			if author.Kind == "cos" {
				return j.After, Fail(409, "cos_work_author_conflict", "该 COS 作者不属于最终发布目录。")
			}
		}
		if len(j.After.AuthorIDs) > 20 {
			return j.After, Fail(400, "too_many_authors", "加上固定目录作者后最多关联20人。")
		}
	}
	j, e := s.prepareFile(ctx, w, j)
	if e != nil {
		return j.After, publicationError("prepare", e)
	}
	if j.State == "done" {
		return j.After, nil
	}
	if j.State == "committed" {
		return j.After, s.finishFile(ctx, j)
	}
	if e = s.validatePublicationBase(ctx, s.DB.DB, j); e != nil {
		return j.After, e
	}
	reservedUpload := w.preparedUpload != "" && w.preparedUpload == j.UploadID
	if reservedUpload && j.Kind == "ingest" {
		e = s.applyReservedUpload(ctx, w, &j)
	} else {
		e = s.applyFile(ctx, &j)
	}
	if e != nil {
		return j.After, publicationError("install_file", e)
	}
	q, e := w.Q()
	if e != nil {
		return j.After, publicationError("catalog_commit", e)
	}
	if e = s.commitFile(ctx, q, &j); e != nil {
		return j.After, publicationError("catalog_commit", Normalize(e))
	}
	w.after = append(w.after, func() error {
		finish := func() error { return s.finishFile(context.Background(), j) }
		if reservedUpload {
			return w.reservedUploadIO(finish)
		}
		return finish()
	})
	return j.After, nil
}
func (s *Service) recoverIntents(ctx context.Context) error {
	for {
		intents, e := store.List[fileIntent](ctx, s.DB.DB, "SELECT data FROM file_intents WHERE state!='done' ORDER BY id LIMIT 64")
		if e != nil {
			return e
		}
		if len(intents) == 0 {
			return nil
		}
		for _, j := range intents {
			j, e = s.recoverMoveSpelling(ctx, j)
			if e != nil {
				return e
			}
			if j.State == "prepared" {
				if e = s.validatePublicationBase(ctx, s.DB.DB, j); e != nil {
					return e
				}
				if e = s.applyFile(ctx, &j); e != nil {
					return fmt.Errorf("intent %s: %w", j.ID, e)
				}
				tx, e := s.DB.DB.BeginTx(ctx, nil)
				if e != nil {
					return e
				}
				if e = s.commitFile(ctx, tx, &j); e != nil {
					tx.Rollback()
					return e
				}
				if e = tx.Commit(); e != nil {
					return e
				}
			}
			if e = s.finishFile(ctx, j); e != nil {
				return fmt.Errorf("intent %s cleanup: %w", j.ID, e)
			}
		}
	}
}
func (s *Service) Move(ctx context.Context, w *Write, id, target string, rev int64) (model.Asset, error) {
	if e := Authorize(w.Principal, "admin", ""); e != nil {
		return model.Asset{}, e
	}
	a, e := s.Asset(ctx, id)
	if e != nil {
		return a, e
	}
	return s.moveAsset(ctx, w, a, target, rev, nil)
}
func (s *Service) moveAsset(ctx context.Context, w *Write, a model.Asset, target string, rev int64, task *AssetMaintenanceTask) (model.Asset, error) {
	if a.Trashed {
		return a, Fail(409, "asset_trashed", "请先恢复媒体。")
	}
	if a.Revision != rev {
		return a, Conflict(a.Revision)
	}
	if !domain.ValidMediaPath(target) || target == "" {
		return a, Fail(400, "invalid_path", "目标相对路径无效。")
	}
	_, mime, ok := domain.MediaType(target)
	if !ok || mime != a.MimeType {
		return a, Fail(400, "extension_mismatch", "改名不能改变媒体格式扩展名。")
	}
	if target == a.RelativePath {
		return a, nil
	}
	if e := s.checkPath(ctx, a.LibraryID, target, a.ID); e != nil {
		return a, e
	}
	old := a
	a.RelativePath = target
	a.FileName = path.Base(target)
	a.Revision++
	a.UpdatedAt = time.Now().UTC()
	if task != nil {
		task.ExpectedRevision = a.Revision
		task.UsedUpdates++
	}
	return s.runFile(ctx, w, fileIntent{ID: w.ID("file"), Kind: "move", Source: assetPath(old), Target: assetPath(a), Before: &old, After: a, MaintenanceTask: task})
}
func (s *Service) Trash(ctx context.Context, w *Write, id string, rev int64) (model.Asset, error) {
	if e := Authorize(w.Principal, "admin", ""); e != nil {
		return model.Asset{}, e
	}
	a, e := s.Asset(ctx, id)
	if e != nil {
		return a, e
	}
	if a.Revision != rev {
		return a, Conflict(a.Revision)
	}
	if a.Trashed {
		return a, nil
	}
	old := a
	a.Trashed = true
	a.StorageLayoutVersion = 2
	a.Revision++
	a.UpdatedAt = time.Now().UTC()
	return s.runFile(ctx, w, fileIntent{ID: w.ID("file"), Kind: "trash", Source: assetPath(old), Target: assetPath(a), Before: &old, After: a})
}
func (s *Service) Restore(ctx context.Context, w *Write, id string, rev int64) (model.Asset, error) {
	if e := Authorize(w.Principal, "admin", ""); e != nil {
		return model.Asset{}, e
	}
	a, e := s.Asset(ctx, id)
	if e != nil {
		return a, e
	}
	if a.Revision != rev {
		return a, Conflict(a.Revision)
	}
	if !a.Trashed {
		return a, nil
	}
	old := a
	a.Trashed = false
	target, e := s.availablePath(ctx, a.LibraryID, a.RelativePath)
	if e != nil {
		return a, e
	}
	a.RelativePath = target
	a.FileName = path.Base(target)
	a.Revision++
	a.UpdatedAt = time.Now().UTC()
	return s.runFile(ctx, w, fileIntent{ID: w.ID("file"), Kind: "restore", Source: assetPath(old), Target: assetPath(a), Before: &old, After: a})
}
func (s *Service) Purge(ctx context.Context, w *Write, id string, rev int64) error {
	if e := Authorize(w.Principal, "admin", ""); e != nil {
		return e
	}
	a, e := s.Asset(ctx, id)
	if e != nil {
		return e
	}
	if !a.Trashed {
		return Fail(409, "trash_required", "永久删除只能操作回收站内的资产。")
	}
	if a.Revision != rev {
		return Conflict(a.Revision)
	}
	_, e = s.runFile(ctx, w, fileIntent{ID: w.ID("file"), Kind: "purge", Source: assetPath(a), After: a})
	return e
}
func (s *Service) checkPath(ctx context.Context, library, relative, except string) error {
	var count int
	if e := s.DB.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM assets WHERE library_id=? AND relative_path=? AND trashed=0 AND id!=?", library, relative, except).Scan(&count); e != nil {
		return e
	}
	if count > 0 {
		return Fail(409, "path_occupied", "目标名称已被占用。")
	}
	return nil
}
func (s *Service) availablePath(ctx context.Context, library, relative string) (string, error) {
	ext := path.Ext(relative)
	stem := strings.TrimSuffix(relative, ext)
	for n := 1; n < 100000; n++ {
		candidate := relative
		if n > 1 {
			candidate = fmt.Sprintf("%s (%d)%s", stem, n, ext)
		}
		e := s.checkPath(ctx, library, candidate, "")
		if e != nil {
			var ae *Error
			if errors.As(e, &ae) && ae.Code == "path_occupied" {
				continue
			}
			return "", e
		}
		_, e = s.Root.Lstat("libraries/" + library + "/" + candidate)
		if errors.Is(e, os.ErrNotExist) {
			return candidate, nil
		}
		if e != nil {
			return "", e
		}
	}
	return "", Fail(409, "name_exhausted", "无法分配可用文件名。")
}
func (s *Service) Content(ctx context.Context, id string, versions ...int64) (*os.File, model.Asset, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, e := s.Asset(ctx, id)
	if e != nil {
		return nil, a, e
	}
	if len(versions) > 0 && versions[0] > 0 && versions[0] != a.ContentVersion {
		return nil, a, Fail(409, "content_changed", "媒体内容版本已变化，请刷新条目。")
	}
	if a.Trashed {
		return nil, a, Fail(404, "not_found", "媒体位于回收站。")
	}
	name := assetPath(a)
	if e = s.safe(name); e != nil {
		return nil, a, e
	}
	f, e := s.Root.Open(name)
	return f, a, e
}
