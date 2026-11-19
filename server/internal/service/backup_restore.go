package service

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	archive "qimeng.local/next/server/internal/backup"
	"qimeng.local/next/server/internal/domain"
	"qimeng.local/next/server/internal/model"
	"qimeng.local/next/server/internal/storage"
	"qimeng.local/next/server/internal/store"
)

type RestoreOptions struct {
	ArchivePath, Destination, ExpectedSHA256 string
	ReserveBytes                             int64
}
type RestoreResult struct {
	BackupID         string `json:"backupId"`
	SourceInstanceID string `json:"sourceInstanceId"`
	InstanceID       string `json:"instanceId"`
	AssetCount       int    `json:"assetCount"`
	UploadCount      int    `json:"uploadCount"`
	ArchiveHash      string `json:"archiveHash"`
	State            string `json:"state"`
}

// RestoreBackup writes only a newly created destination. The manifest cannot
// select host roots: every restored library is isolated below this data folder.
// Failed extraction leaves a marked recovery directory, never a runnable DB.
func RestoreBackup(ctx context.Context, options RestoreOptions) (result RestoreResult, err error) {
	if options.Destination == "" || options.ArchivePath == "" {
		return result, errors.New("archive and a new destination are required")
	}
	if options.ReserveBytes <= 0 {
		options.ReserveBytes = 512 << 20
	}
	destination, e := filepath.Abs(options.Destination)
	if e != nil {
		return result, e
	}
	if e = os.Mkdir(destination, 0700); e != nil {
		return result, errors.New("restore destination must not already exist")
	}
	root, e := storage.Open(destination, nil)
	if e != nil {
		return result, e
	}
	defer root.Close()
	marker, e := root.OpenFile("RESTORE_INCOMPLETE", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return result, e
	}
	if _, e = marker.WriteString("Restore did not finish. Do not start a service in this directory.\n"); e == nil {
		e = marker.Sync()
	}
	closeErr := marker.Close()
	if e == nil {
		e = closeErr
	}
	if e != nil {
		return result, e
	}
	input, e := os.Open(options.ArchivePath)
	if e != nil {
		return result, e
	}
	defer input.Close()
	info, e := input.Stat()
	if e != nil {
		return result, e
	}
	if !info.Mode().IsRegular() {
		return result, errors.New("backup input must be a regular archive file")
	}
	hash := sha256.New()
	var snapshot *sql.DB
	defer func() {
		if snapshot != nil {
			snapshot.Close()
		}
	}()
	seenAssets, seenUploads := map[string]bool{}, map[string]bool{}
	output := func(file archive.File, reader io.Reader) error {
		target := "restore.sqlite"
		modifiedAt := file.ModifiedAt
		if file.Kind != "snapshot" {
			if snapshot == nil {
				return errors.New("backup snapshot must precede media")
			}
			var e error
			if modifiedAt, e = validateBackupPayload(ctx, snapshot, file); e != nil {
				return e
			}
			target = file.StoragePath
			if file.Kind == "asset" {
				seenAssets[file.ObjectID] = true
				result.AssetCount++
			} else {
				seenUploads[file.ObjectID] = true
				result.UploadCount++
			}
		}
		if e := root.MkdirAll(path.Dir(target), 0700); e != nil {
			return e
		}
		fileOut, e := root.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if e != nil {
			return e
		}
		writer := &restoreWriter{ctx: ctx, root: destination, reserve: options.ReserveBytes, file: fileOut}
		_, e = io.Copy(writer, reader)
		if e == nil {
			e = fileOut.Sync()
		}
		closeErr := fileOut.Close()
		if e == nil {
			e = closeErr
		}
		if e != nil {
			return e
		}
		if modifiedAt != nil {
			if e = root.Chtimes(target, *modifiedAt, *modifiedAt); e != nil {
				return e
			}
		}
		if file.Kind == "snapshot" {
			snapshot, e = store.OpenDatabase(filepath.Join(destination, "restore.sqlite"), store.SnapshotDatabase)
			if e != nil {
				return e
			}
			if e = store.ValidateBackupSchema(ctx, snapshot); e != nil {
				return e
			}
			libraries, e := store.List[model.Library](ctx, snapshot, "SELECT data FROM libraries")
			if e != nil {
				return e
			}
			for _, library := range libraries {
				if !safeBackupID(library.ID) {
					return errors.New("invalid library identity in backup")
				}
				if e = root.RegisterLibrary(library.ID, "data", ""); e != nil {
					return e
				}
				if e = root.MkdirAll("libraries/"+library.ID, 0700); e != nil {
					return e
				}
			}
			uploads, e := snapshot.QueryContext(ctx, "SELECT u.id,json_extract(u.data,'$.libraryId'),l.id IS NOT NULL FROM uploads u LEFT JOIN libraries l ON l.id=json_extract(u.data,'$.libraryId')")
			if e != nil {
				return e
			}
			for uploads.Next() {
				var id, libraryID string
				var active bool
				if e = uploads.Scan(&id, &libraryID, &active); e != nil {
					uploads.Close()
					return e
				}
				if !safeBackupID(id) || !safeBackupID(libraryID) {
					uploads.Close()
					return errors.New("invalid upload identity in backup")
				}
				if active {
					root.RegisterUpload(id, libraryID)
				}
			}
			e = uploads.Err()
			uploads.Close()
			if e != nil {
				return e
			}
		}
		return nil
	}
	manifest, e := archive.Read(ctx, io.TeeReader(input, hash), output)
	if e != nil {
		return result, e
	}
	result.ArchiveHash = hex.EncodeToString(hash.Sum(nil))
	if options.ExpectedSHA256 != "" && strings.ToLower(options.ExpectedSHA256) != result.ArchiveHash {
		return result, errors.New("archive checksum differs from expected backup")
	}
	if e = validateBackupCoverage(ctx, snapshot, manifest, len(seenAssets), len(seenUploads)); e != nil {
		return result, e
	}
	if e = snapshot.Close(); e != nil {
		return result, e
	}
	snapshot = nil
	db, e := store.Open(filepath.Join(destination, "restore.sqlite"))
	if e != nil {
		return result, e
	}
	defer db.DB.Close()
	tx, e := db.DB.BeginTx(ctx, nil)
	if e != nil {
		return result, e
	}
	defer tx.Rollback()
	result.InstanceID = ID()
	// Runtime capabilities cannot survive a cross-instance restore. Business task
	// reports remain, but no previous session or delegated writer can claim them.
	for _, statement := range []string{
		"DELETE FROM credentials", "DELETE FROM idempotency", "DELETE FROM file_intents", "DELETE FROM delegated_tasks",
		"DELETE FROM author_list_mirror", "DELETE FROM client_diagnostics",
		"DELETE FROM detached_upload_roots",
		"DELETE FROM device_apply_chunks",
		"DELETE FROM device_apply_requests",
		"UPDATE device_apply_tasks SET state='cancelled',data=json_set(data,'$.state','cancelled','$.finishedAt',qimeng_time_key(strftime('%Y-%m-%dT%H:%M:%fZ','now')),'$.revision',json_extract(data,'$.revision')+1) WHERE state IN ('pending','claimed')",
		"UPDATE ai_delegations SET state='revoked',data=json_set(data,'$.state','revoked')",
		"UPDATE research_provider_calls SET state='interrupted',data=json_set(data,'$.state','interrupted','$.finishedAt',qimeng_time_key(strftime('%Y-%m-%dT%H:%M:%fZ','now'))) WHERE state='pending'",
		"UPDATE author_tasks SET state='cancelled',data=json_set(data,'$.state','cancelled') WHERE state='authorized'",
		"UPDATE asset_maintenance_tasks SET data=json_set(data,'$.state','revoked') WHERE coalesce(json_extract(data,'$.state'),'authorized')!='revoked'",
		"UPDATE libraries SET data=json_set(data,'$.storageId','data','$.rootRelativePath','')",
		"UPDATE scans SET state='interrupted',data=json_set(data,'$.state','interrupted') WHERE state IN ('queued','running')",
		"UPDATE backups SET data=json_set(json_remove(data,'$.archiveHash','$.archiveBytes','$.exportedAt'),'$.state','snapshot_not_included','$.exportState','unavailable','$.exportError','backup_snapshot_not_included')",
		"UPDATE batches SET data=json_set(data,'$.backupState','not_covered')",
		"UPDATE batch_protections SET state='historical',data=json_set(data,'$.state','historical')",
		"UPDATE source_cleanup_permits SET state='revoked',data=json_set(data,'$.state','revoked') WHERE state='issued'",
	} {
		if _, e = tx.ExecContext(ctx, statement); e != nil {
			return result, e
		}
	}
	if _, e = tx.ExecContext(ctx, "UPDATE settings SET value=? WHERE key='instance_id'", result.InstanceID); e != nil {
		return result, e
	}
	if e = tx.Commit(); e != nil {
		return result, e
	}
	if _, e = db.DB.ExecContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)"); e != nil {
		return result, e
	}
	if e = db.DB.Close(); e != nil {
		return result, e
	}
	if e = root.PublishTemp("restore.sqlite", "qimeng.sqlite"); e != nil {
		return result, e
	}
	if e = root.Remove("RESTORE_INCOMPLETE"); e != nil {
		return result, e
	}
	parent, e := root.OpenParent("qimeng.sqlite")
	if e != nil {
		return result, e
	}
	e = syncDirectory(parent)
	parent.Close()
	if e != nil {
		return result, e
	}
	result.BackupID = manifest.BackupID
	result.SourceInstanceID = manifest.InstanceID
	result.State = "restored"
	return result, nil
}
func safeBackupID(id string) bool {
	return id != "" && len(id) <= 256 && !strings.ContainsAny(id, "/\\:\x00") && id != "." && id != ".."
}

type restoreWriter struct {
	ctx     context.Context
	root    string
	reserve int64
	file    *os.File
	since   int64
}

func (w *restoreWriter) Write(p []byte) (int, error) {
	if e := w.ctx.Err(); e != nil {
		return 0, e
	}
	if w.since == 0 || w.since >= 8<<20 {
		free, e := freeBytes(w.root)
		if e != nil {
			return 0, e
		}
		if free-w.reserve < int64(len(p))+(8<<20) {
			return 0, errors.New("restore storage reserve exhausted")
		}
		w.since = 0
	}
	n, e := w.file.Write(p)
	w.since += int64(n)
	return n, e
}

// validateBackupPayload checks the snapshot and returns the recorded time to restore.
func validateBackupPayload(ctx context.Context, db *sql.DB, file archive.File) (*time.Time, error) {
	if !domain.ValidRelative(file.StoragePath) || !safeBackupID(file.ObjectID) {
		return nil, errors.New("invalid backup storage path")
	}
	switch file.Kind {
	case "asset":
		a, e := store.Get[model.Asset](ctx, db, "SELECT data FROM assets WHERE id=?", file.ObjectID)
		if e != nil {
			return nil, e
		}
		if !safeBackupID(a.LibraryID) || !domain.ValidName(a.FileName) || !domain.ValidMediaPath(a.RelativePath) || a.Hash != file.SHA256 || a.Size != file.Size || a.ContentVersion != file.ContentVersion || assetPath(a) != file.StoragePath {
			return nil, errors.New("asset payload does not match snapshot")
		}
		if a.ContentMtime != nil {
			if file.ModifiedAt != nil && !a.ContentMtime.Equal(*file.ModifiedAt) {
				return nil, errors.New("asset modification time does not match snapshot")
			}
			return a.ContentMtime, nil
		}
	case "upload":
		u, e := store.Get[model.Upload](ctx, db, "SELECT data FROM uploads WHERE id=? AND state!='ingested'", file.ObjectID)
		if e != nil {
			return nil, e
		}
		if !safeBackupID(u.LibraryID) || u.Offset < 0 || u.Offset > u.Size || u.Offset != file.Size || uploadPath(u.ID) != file.StoragePath || u.Hash != "" && u.Hash != file.SHA256 {
			return nil, errors.New("upload payload does not match snapshot")
		}
	default:
		return nil, errors.New("unsupported backup payload")
	}
	return file.ModifiedAt, nil
}
func validateBackupCoverage(ctx context.Context, db *sql.DB, m archive.Manifest, assetCount, uploadCount int) error {
	var assets, uploads int
	if e := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM assets").Scan(&assets); e != nil {
		return e
	}
	if e := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM uploads WHERE state!='ingested'").Scan(&uploads); e != nil {
		return e
	}
	if assets != assetCount || uploads != uploadCount {
		return errors.New("backup omits snapshot payloads")
	}
	var original string
	if e := db.QueryRowContext(ctx, "SELECT value FROM settings WHERE key='instance_id'").Scan(&original); e != nil {
		return e
	}
	if original != m.InstanceID {
		return errors.New("backup instance identity mismatch")
	}
	var pending int
	if e := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM file_intents WHERE state='prepared'").Scan(&pending); e != nil {
		return e
	}
	if pending != 0 {
		return errors.New("backup contains unfinished publication")
	}
	return nil
}
func VerifyBackup(ctx context.Context, filename, expected string) (RestoreResult, error) {
	result := RestoreResult{}
	file, e := os.Open(filename)
	if e != nil {
		return result, e
	}
	defer file.Close()
	temporary, e := os.MkdirTemp("", "qimeng-backup-verify-")
	if e != nil {
		return result, e
	}
	defer os.RemoveAll(temporary)
	var db *sql.DB
	defer func() {
		if db != nil {
			db.Close()
		}
	}()
	hash := sha256.New()
	consume := func(entry archive.File, reader io.Reader) error {
		if entry.Kind == "snapshot" {
			name := filepath.Join(temporary, "snapshot.sqlite")
			out, e := os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
			if e != nil {
				return e
			}
			_, e = io.Copy(&restoreWriter{ctx: ctx, root: temporary, reserve: 512 << 20, file: out}, reader)
			closeErr := out.Close()
			if e == nil {
				e = closeErr
			}
			if e != nil {
				return e
			}
			db, e = store.OpenDatabase(name, store.SnapshotDatabase)
			if e != nil {
				return e
			}
			return store.ValidateBackupSchema(ctx, db)
		}
		if db == nil {
			return errors.New("missing snapshot")
		}
		if _, e := validateBackupPayload(ctx, db, entry); e != nil {
			return e
		}
		if entry.Kind == "asset" {
			result.AssetCount++
		} else {
			result.UploadCount++
		}
		_, e := io.Copy(io.Discard, reader)
		return e
	}
	manifest, e := archive.Read(ctx, io.TeeReader(file, hash), consume)
	if e != nil {
		return result, e
	}
	if e = validateBackupCoverage(ctx, db, manifest, result.AssetCount, result.UploadCount); e != nil {
		return result, e
	}
	result.ArchiveHash = hex.EncodeToString(hash.Sum(nil))
	if expected != "" && strings.ToLower(expected) != result.ArchiveHash {
		return result, errors.New("archive checksum differs from expected backup")
	}
	result.BackupID = manifest.BackupID
	result.SourceInstanceID = manifest.InstanceID
	result.State = "verified"
	return result, nil
}
