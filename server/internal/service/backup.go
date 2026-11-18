package service

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"time"

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
	ExportError      string     `json:"exportError,omitempty"`
	ArchiveHash      string     `json:"archiveHash,omitempty"`
	ArchiveBytes     int64      `json:"archiveBytes,omitempty"`
	ExportedFiles    int        `json:"exportedFiles"`
	TotalFiles       int        `json:"totalFiles"`
	ExportedAt       *time.Time `json:"exportedAt,omitempty"`
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
	return s.createBackupCheckpoint(ctx, w)
}
func (s *Service) createBackupCheckpoint(ctx context.Context, w *Write) (Backup, error) {
	b := Backup{ID: w.ID("backup"), State: "awaiting_external_backup", ExportState: "not_started", CreatedAt: time.Now().UTC()}
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
