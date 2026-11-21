package service

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"time"

	archive "qimeng.local/next/server/internal/backup"
	"qimeng.local/next/server/internal/store"
)

type Backup struct {
	ID               string     `json:"id"`
	State            string     `json:"state"`
	CreatedAt        time.Time  `json:"createdAt"`
	SnapshotHash     string     `json:"snapshotHash"`
	BusinessRevision string     `json:"businessRevision,omitempty"`
	AssetCount       int        `json:"assetCount"`
	ManifestHash     string     `json:"manifestHash"`
	ExportState      string     `json:"exportState"`
	Scope            string     `json:"scope,omitempty"`
	ExportError      string     `json:"exportError,omitempty"`
	ExportMessage    string     `json:"exportMessage,omitempty"`
	ArchiveHash      string     `json:"archiveHash,omitempty"`
	ArchiveBytes     int64      `json:"archiveBytes,omitempty"`
	ExportedFiles    int        `json:"exportedFiles"`
	TotalFiles       int        `json:"totalFiles"`
	ExportedAt       *time.Time `json:"exportedAt,omitempty"`
	// StorageBytes is computed from the checkpoint directory on read; it is
	// never business truth and is not relied on after a restart.
	StorageBytes int64 `json:"storageBytes,omitempty"`
}

const (
	backupScopeComplete = "complete"
	backupScopeMetadata = "metadata"
)

// normalizeScope maps stored and legacy values to the API vocabulary. An empty
// value predates scopes, when every export carried the original media.
func normalizeScope(value string) string {
	if value == backupScopeMetadata {
		return backupScopeMetadata
	}
	return backupScopeComplete
}

func archiveScope(value string) string {
	if normalizeScope(value) == backupScopeMetadata {
		return archive.ScopeMetadata
	}
	return archive.ScopeComplete
}

type backupEntry struct {
	AssetID        string `json:"assetId"`
	ContentVersion int64  `json:"contentVersion"`
	Revision       int64  `json:"revision"`
	RelativeFile   string `json:"relativeFile"`
	Hash           string `json:"hash"`
	Size           int64  `json:"size"`
}

func (s *Service) CreateBackup(ctx context.Context, w *Write) (Backup, error) {
	if e := Authorize(w.Principal, "admin", ""); e != nil {
		return Backup{}, e
	}
	return s.createBackupCheckpoint(ctx, w, nil)
}
func (s *Service) createBackupCheckpoint(ctx context.Context, w *Write, inspect func(*sql.DB) error) (Backup, error) {
	b := Backup{ID: w.ID("backup"), State: "awaiting_external_backup", ExportState: "not_started", Scope: backupScopeMetadata, CreatedAt: time.Now().UTC()}
	directory := "backups/" + b.ID
	var pending int
	if e := s.DB.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM file_intents WHERE state='prepared'").Scan(&pending); e != nil {
		return b, e
	}
	if pending > 0 {
		return b, Fail(409, "publication_pending", "文件发布尚未完成恢复，暂不能生成与原件一致的备份清单。")
	}
	if e := s.Root.MkdirAll(directory, 0700); e != nil {
		return b, e
	}
	snapshot := directory + "/snapshot.sqlite"
	// A previous uncommitted attempt may have completed its snapshot. Its manifest
	// must be rebuilt against that same snapshot, never relabelled as a newer one.
	if _, e := s.Root.Stat(snapshot); errors.Is(e, os.ErrNotExist) {
		var pages, pageSize int64
		if e = s.DB.DB.QueryRowContext(ctx, "PRAGMA page_count").Scan(&pages); e != nil {
			return b, e
		}
		if e = s.DB.DB.QueryRowContext(ctx, "PRAGMA page_size").Scan(&pageSize); e != nil {
			return b, e
		}
		free, e := freeBytes(s.Config.DataDir)
		if e != nil {
			return b, e
		}
		if free-pages*pageSize-(4<<20) < s.Config.ReserveBytes {
			return b, Fail(507, "insufficient_storage", "空间不足以生成业务快照并保留存储余量。")
		}
		if e = s.publishCheckpointDatabase(ctx, snapshot); e != nil {
			return b, e
		}
	} else if e != nil {
		return b, e
	}
	// The checkpoint is the portable relink copy. Remove credentials, request
	// receipts and machine-local runtime rows before hashing it, so both the
	// archive and any protection copy are free of those secrets.
	absolute, e := s.Root.AbsolutePath(snapshot)
	if e != nil {
		return b, e
	}
	if e = scrubSnapshotFile(ctx, absolute); e != nil {
		return b, e
	}
	for _, suffix := range []string{"-journal", "-wal", "-shm"} {
		_ = s.Root.Remove(snapshot + suffix)
	}
	snapshotDB, e := store.OpenDatabase(filepath.Join(s.Config.DataDir, filepath.FromSlash(snapshot)), store.SnapshotDatabase)
	if e != nil {
		return b, e
	}
	defer snapshotDB.Close()
	var hasRevision int
	if e = snapshotDB.QueryRowContext(ctx, "SELECT 1 FROM sqlite_schema WHERE type='table' AND name='backup_business_state'").Scan(&hasRevision); e != nil && !errors.Is(e, sql.ErrNoRows) {
		return b, e
	}
	if hasRevision == 1 {
		if e = snapshotDB.QueryRowContext(ctx, "SELECT revision FROM backup_business_state WHERE id=1").Scan(&b.BusinessRevision); e != nil {
			return b, e
		}
	}
	b.SnapshotHash, _, e = s.hashFile(snapshot)
	if e != nil {
		return b, e
	}
	if inspect != nil {
		if e = inspect(snapshotDB); e != nil {
			return b, e
		}
	}
	b.AssetCount, b.ManifestHash, e = s.publishCheckpointManifest(ctx, snapshotDB, b)
	if e != nil {
		return b, e
	}
	raw, e := store.Encode(b)
	if e != nil {
		return b, e
	}
	q, e := w.Q()
	if e != nil {
		return b, e
	}
	_, e = q.ExecContext(ctx, "INSERT INTO backups(id,data) VALUES(?,?)", b.ID, raw)
	return b, e
}
