package store

import (
	"context"
	"database/sql"
	"time"

	"qimeng.local/next/server/internal/model"
)

// UpdateScannedFile saves the observed original for an existing regular-library
// asset. The scan supplies its current mtime, which also drives file-date sorting.
func UpdateScannedFile(ctx context.Context, tx *sql.Tx, a model.Asset) error {
	modified := a.ContentMtime.UTC()
	updated := a.UpdatedAt.UTC()
	_, err := tx.ExecContext(ctx, `UPDATE asset_records SET revision=?1,updated_at=?2,file_date=?3,
		data=json_patch(data,json_object('revision',?1,'updatedAt',?4,'fileDate',?5,'contentUrl',?6,'thumbnailUrl',nullif(?7,'')))
		WHERE id=?8`, a.Revision, updated.Format("2006-01-02T15:04:05.000000000Z"), modified.UnixMilli(),
		updated.Format(time.RFC3339Nano), modified.Format(time.RFC3339Nano), a.ContentURL, a.ThumbnailURL, a.ID)
	if err != nil {
		return err
	}
	return saveAssetFile(ctx, tx, a, false)
}

// saveAssetFile runs in the transaction which owns the stable asset record.
// Reusing a version with different facts reaches the immutable-table trigger.
// Most business edits rewrite neither the content record nor the location.
func saveAssetFile(ctx context.Context, q Querier, a model.Asset, insertOnly bool) error {
	version := `INSERT INTO asset_content_versions(asset_id,version,hash,size,media_type,mime_type,width,height,duration_ms,probe_state)
		VALUES(?,?,nullif(?,''),?,?,?,nullif(?,0),nullif(?,0),nullif(?,0),?)`
	if !insertOnly {
		version += ` ON CONFLICT(asset_id,version) DO UPDATE SET hash=excluded.hash
			WHERE hash IS NOT excluded.hash OR size!=excluded.size OR media_type IS NOT excluded.media_type
			OR coalesce(mime_type,'')!=excluded.mime_type OR coalesce(width,0)!=coalesce(excluded.width,0)
			OR coalesce(height,0)!=coalesce(excluded.height,0) OR coalesce(duration_ms,0)!=coalesce(excluded.duration_ms,0)
			OR coalesce(probe_state,'')!=excluded.probe_state`
	}
	if _, err := q.ExecContext(ctx, version, a.ID, a.ContentVersion, a.Hash, a.Size, a.MediaType, a.MimeType,
		a.Width, a.Height, a.DurationMs, a.ProbeState); err != nil {
		return err
	}
	var mtime any
	if a.ContentMtime != nil {
		mtime = a.ContentMtime.Format(time.RFC3339Nano)
	}
	location := `INSERT INTO asset_locations(asset_id,library_id,relative_path,file_name,current_version,trashed,storage_layout,content_mtime)
		VALUES(?,?,?,?,?,?,?,?)`
	if !insertOnly {
		location += ` ON CONFLICT(asset_id) DO UPDATE SET library_id=excluded.library_id,relative_path=excluded.relative_path,
			file_name=excluded.file_name,current_version=excluded.current_version,trashed=excluded.trashed,
			storage_layout=excluded.storage_layout,content_mtime=excluded.content_mtime
			WHERE library_id!=excluded.library_id OR relative_path!=excluded.relative_path OR file_name!=excluded.file_name
			OR current_version!=excluded.current_version OR trashed!=excluded.trashed OR storage_layout!=excluded.storage_layout
			OR content_mtime IS NOT excluded.content_mtime`
	}
	_, err := q.ExecContext(ctx, location, a.ID, a.LibraryID, a.RelativePath, a.FileName,
		a.ContentVersion, a.Trashed, a.StorageLayoutVersion, mtime)
	return err
}
