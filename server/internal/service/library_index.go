package service

import (
	"context"
	"database/sql"

	"qimeng.local/next/server/internal/model"
	"qimeng.local/next/server/internal/store"
)

const libraryProjection = `SELECT json_set(l.data,
 '$.fileCount',(SELECT count(*) FROM assets a WHERE a.library_id=l.id AND a.trashed=0),
 '$.latestScanId',sc.id,
 '$.scanState',CASE sc.state WHEN 'queued' THEN 'scanning' WHEN 'running' THEN 'scanning'
  WHEN 'failed' THEN 'error' WHEN 'partial' THEN 'error' WHEN 'interrupted' THEN 'error' ELSE 'idle' END)
 FROM libraries l LEFT JOIN scans sc ON sc.id=(
 SELECT s.id FROM scans s WHERE s.library_id=l.id ORDER BY json_extract(s.data,'$.createdAt') DESC,s.id DESC LIMIT 1) `

func (s *Service) Libraries(ctx context.Context, cursor string, limit int) (model.Page[model.Library], error) {
	page := model.Page[model.Library]{Items: []model.Library{}}
	n, e := limited(limit, 100)
	if e != nil {
		return page, e
	}
	seek, e := readSeek(cursor, "libraries", "name")
	if e != nil {
		return page, e
	}
	tx, e := s.DB.DB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if e != nil {
		return page, e
	}
	defer tx.Rollback()
	var total int64
	if e = tx.QueryRowContext(ctx, "SELECT count(*) FROM libraries").Scan(&total); e != nil {
		return page, e
	}
	page.TotalMatched = &total
	items, e := store.List[model.Library](ctx, tx, libraryProjection+"WHERE (?='' OR (l.name,l.id)>(?,?)) ORDER BY l.name,l.id LIMIT ?", seek.ID, seek.Value, seek.ID, n+1)
	if e != nil {
		return page, e
	}
	if len(items) > n {
		items = items[:n]
		last := items[n-1]
		page.NextCursor = makeSeek("libraries", "name", last.Name, last.ID)
	}
	page.Items = items
	return page, tx.Commit()
}

func (s *Service) Library(ctx context.Context, id string) (model.Library, error) {
	lib, e := store.Get[model.Library](ctx, s.DB.DB, libraryProjection+"WHERE l.id=?", id)
	return lib, Normalize(e)
}

func (s *Service) DeleteLibrary(ctx context.Context, w *Write, id string, revision int64) error {
	if e := Authorize(w.Principal, "admin", ""); e != nil {
		return e
	}
	q, e := w.Q()
	if e != nil {
		return e
	}
	lib, e := store.Get[model.Library](ctx, q, "SELECT data FROM libraries WHERE id=?", id)
	if e != nil {
		return Normalize(e)
	}
	if lib.Revision != revision {
		return Conflict(lib.Revision)
	}
	checks := []struct{ query, code, message string }{
		{"SELECT EXISTS(SELECT 1 FROM assets WHERE library_id=? AND trashed=1)", "library_has_trash", "此库有可恢复的回收站项目，请先恢复或明确处置。"},
		{"SELECT EXISTS(SELECT 1 FROM scans WHERE library_id=? AND state IN ('queued','running'))", "library_scan_active", "库扫描正在执行，请等待完成或取消。"},
		{"SELECT EXISTS(SELECT 1 FROM uploads WHERE json_extract(data,'$.libraryId')=? AND state IN ('receiving','staged'))", "library_upload_active", "此库有未完成上传，请先完成或取消。"},
		{`SELECT EXISTS(SELECT 1 FROM batches b WHERE json_extract(b.data,'$.libraryId')=? AND json_extract(b.data,'$.state') NOT IN ('completed','cancelled')
   AND (json_extract(b.data,'$.state')!='partial' OR EXISTS(SELECT 1 FROM batch_inputs i WHERE i.batch_id=b.id AND i.cancelled=0 AND i.rejection_code='' AND i.upload_id IS NULL)))`, "library_batch_active", "此库有尚未结束的接收批次，请先完成或取消。"},
		{"SELECT EXISTS(SELECT 1 FROM file_intents WHERE json_extract(data,'$.After.libraryId')=? AND state NOT IN ('done','cancelled'))", "library_publication_pending", "文件发布或收尾尚未完成，请先恢复原操作。"},
		{"SELECT EXISTS(SELECT 1 FROM batch_protections p JOIN batches b ON b.id=p.batch_id WHERE json_extract(b.data,'$.libraryId')=? AND p.state IN ('queued','running'))", "library_protection_active", "此库的独立保护正在执行。"},
		{"SELECT EXISTS(SELECT 1 FROM source_cleanup_permits p JOIN batches b ON b.id=p.batch_id WHERE json_extract(b.data,'$.libraryId')=? AND p.state='issued' AND p.expires_at>qimeng_time_key(strftime('%Y-%m-%dT%H:%M:%fZ','now')))", "library_cleanup_active", "来源回收许可仍有效，请等待执行回执或许可过期。"},
		{"SELECT EXISTS(SELECT 1 FROM backups WHERE json_extract(data,'$.exportState') IN ('queued','running')) AND EXISTS(SELECT 1 FROM assets WHERE library_id=?)", "library_backup_active", "完整归档正在读取原件，请等待导出完成。"},
	}
	for _, check := range checks {
		var blocked bool
		if e = q.QueryRowContext(ctx, check.query, id).Scan(&blocked); e != nil {
			return e
		}
		if blocked {
			return Fail(409, check.code, check.message)
		}
	}
	var retain bool
	if e = q.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM uploads WHERE json_extract(data,'$.libraryId')=? AND state!='ingested')", id).Scan(&retain); e != nil {
		return e
	}
	if retain {
		if _, e = q.ExecContext(ctx, "INSERT INTO detached_upload_roots(library_id,storage_id,relative_path) VALUES(?,?,?)", id, lib.StorageID, lib.RootRelativePath); e != nil {
			return e
		}
	}
	// Raw events, ingestion receipts, historical permits and declared author work
	// names remain. Asset-scoped display and statistical projections disappear.
	for _, table := range []string{"likes", "asset_daily_stats", "asset_hourly_stats", "event_sessions", "recommendation_shown"} {
		if _, e = q.ExecContext(ctx, "DELETE FROM "+table+" WHERE asset_id IN (SELECT id FROM assets WHERE library_id=?)", id); e != nil {
			return e
		}
	}
	if _, e = q.ExecContext(ctx, "DELETE FROM asset_records WHERE id IN (SELECT asset_id FROM asset_locations WHERE library_id=?)", id); e != nil {
		return e
	}
	for _, table := range []string{"cos_works", "collections", "scans"} {
		if _, e = q.ExecContext(ctx, "DELETE FROM "+table+" WHERE library_id=?", id); e != nil {
			return e
		}
	}
	if _, e = q.ExecContext(ctx, "DELETE FROM libraries WHERE id=?", id); e != nil {
		return e
	}
	w.after = append(w.after, func() error {
		s.Root.DetachLibrary(id, retain)
		s.previews.changed("", "")
		s.wakeLibraryMonitor()
		return nil
	})
	return nil
}
