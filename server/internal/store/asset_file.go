package store

import (
	"context"
	"time"

	"qimeng.local/next/server/internal/model"
)

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
