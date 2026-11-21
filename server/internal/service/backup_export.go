package service

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"time"

	archive "qimeng.local/next/server/internal/backup"
	"qimeng.local/next/server/internal/model"
	"qimeng.local/next/server/internal/storage"
	"qimeng.local/next/server/internal/store"
)

func (s *Service) Backup(ctx context.Context, p model.Principal, id string) (Backup, error) {
	if e := Authorize(p, "admin", ""); e != nil {
		return Backup{}, e
	}
	b, e := store.Get[Backup](ctx, s.DB.DB, "SELECT data FROM backups WHERE id=?", id)
	if e == nil {
		b = s.describeBackup(b)
	}
	return b, Normalize(e)
}

// describeBackup adds values derived from the checkpoint directory. The record
// itself stays the durable export state; on-disk numbers are never trusted
// after a restart because the directory can be swept independently.
func (s *Service) describeBackup(b Backup) Backup {
	if b.ExportState == "" {
		b.ExportState = "not_started"
	}
	b.Scope = normalizeScope(b.Scope)
	b.StorageBytes = s.backupStorageBytes(b.ID)
	return b
}
func (s *Service) Backups(ctx context.Context, p model.Principal, cursor string, limit int) (model.Page[Backup], error) {
	result := model.Page[Backup]{Items: []Backup{}}
	if e := Authorize(p, "admin", ""); e != nil {
		return result, e
	}
	n, e := limited(limit, 20)
	if e != nil {
		return result, e
	}
	after, e := readSeek(cursor, "backups:1", "id")
	if e != nil {
		return result, e
	}
	items, e := store.List[Backup](ctx, s.DB.DB, "SELECT data FROM backups WHERE id>? ORDER BY id LIMIT ?", after.ID, n+1)
	if e != nil {
		return result, e
	}
	if len(items) > n {
		result.NextCursor = makeSeek("backups:1", "id", "", items[n-1].ID)
		items = items[:n]
	}
	for i := range items {
		items[i] = s.describeBackup(items[i])
	}
	result.Items = items
	return result, nil
}
func saveBackup(ctx context.Context, q store.Querier, b Backup) error {
	raw, e := store.Encode(b)
	if e != nil {
		return e
	}
	_, e = q.ExecContext(ctx, "UPDATE backups SET data=? WHERE id=?", raw, b.ID)
	return e
}
func (s *Service) StartBackupExport(ctx context.Context, w *Write, id string) (Backup, error) {
	b, e := s.Backup(ctx, w.Principal, id)
	if e != nil {
		return b, e
	}
	if b.State == "snapshot_not_included" {
		return b, Fail(409, "backup_snapshot_not_included", "此项是迁入的历史备份记录，原检查点归档未包含在当前实例。")
	}
	if b.ExportState == "completed" || b.ExportState == "queued" || b.ExportState == "running" {
		return b, nil
	}
	if _, e := s.Root.Stat("backups/" + b.ID + "/snapshot.sqlite"); e != nil {
		if errors.Is(e, os.ErrNotExist) {
			return b, Fail(409, "backup_checkpoint_missing", "备份检查点文件已被清理，请重新建立检查点。")
		}
		return b, e
	}
	var active int
	if e = s.DB.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM backups WHERE json_extract(data,'$.exportState') IN ('queued','running')").Scan(&active); e != nil {
		return b, e
	}
	if active > 0 {
		return b, Fail(409, "backup_export_busy", "已有备份正在导出，请回读其进度。")
	}
	b.ExportState = "queued"
	b.Scope = backupScopeMetadata
	b.ExportError = ""
	b.ExportMessage = ""
	b.ArchiveHash = ""
	b.ArchiveBytes = 0
	b.ExportedAt = nil
	b.ExportedFiles = 0
	b.TotalFiles = 0
	q, e := w.Q()
	if e != nil {
		return b, e
	}
	if e = saveBackup(ctx, q, b); e != nil {
		return b, e
	}
	w.after = append(w.after, func() error {
		s.workers.Add(1)
		go func() { defer s.workers.Done(); s.exportBackup(s.workCtx, b) }()
		return nil
	})
	return b, nil
}
func (s *Service) setBackupProgress(ctx context.Context, b Backup) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return saveBackup(ctx, s.DB.DB, b)
}

func (s *Service) backupSource(ctx context.Context, f archive.File) (*os.File, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	name := f.StoragePath
	if f.Kind == "asset" {
		a, e := store.Get[model.Asset](ctx, s.DB.DB, "SELECT data FROM assets WHERE id=?", f.ObjectID)
		if e != nil {
			return nil, Fail(409, "backup_source_unavailable", "检查点中的原件已移除，不能读取此原件。")
		}
		if a.Hash != f.SHA256 || a.Size != f.Size || a.ContentVersion != f.ContentVersion {
			return nil, Fail(409, "backup_source_changed", "检查点中的原件版本已变化，请重新建立检查点。")
		}
		name = assetPath(a)
	} else if f.Kind == "upload" {
		u, e := store.Get[model.Upload](ctx, s.DB.DB, "SELECT data FROM uploads WHERE id=?", f.ObjectID)
		if e != nil {
			return nil, e
		}
		name = uploadPath(u.ID)
		if u.State == "ingested" {
			a, e := store.Get[model.Asset](ctx, s.DB.DB, "SELECT data FROM assets WHERE id=?", u.AssetID)
			if e != nil {
				return nil, e
			}
			if a.Hash != u.Hash || a.Size != u.Size {
				return nil, Fail(409, "backup_input_changed", "检查点中的未完成输入已无法读取原始字节。")
			}
			name = assetPath(a)
		}
	}
	if e := s.safe(name); e != nil {
		return nil, e
	}
	file, e := s.Root.Open(name)
	if e != nil {
		return nil, e
	}
	info, e := file.Stat()
	if e != nil {
		file.Close()
		return nil, e
	}
	if !info.Mode().IsRegular() || info.Size() < f.Size || f.Kind != "upload" && info.Size() != f.Size {
		file.Close()
		return nil, Fail(409, "backup_length_changed", "备份输入的长度与检查点不一致。")
	}
	return file, nil
}

// prepareArchive builds a records-only manifest. The archive carries the
// scrubbed database snapshot as its only payload; the checkpoint's
// manifest.json stays on the server as the relink catalogue. Original media is
// never copied into the archive.
func (s *Service) prepareArchive(b Backup, snapshot archiveSnapshot) (archive.Manifest, error) {
	manifest := archive.Manifest{
		Format:     archive.Format,
		Version:    archive.Version,
		BackupID:   b.ID,
		InstanceID: s.InstanceID,
		CreatedAt:  b.CreatedAt,
		Scope:      archive.ScopeMetadata,
		Snapshot:   archive.File{Kind: "snapshot", ObjectID: b.ID, StoragePath: "qimeng.sqlite", SHA256: snapshot.hash, Size: snapshot.size},
		Files:      []archive.File{},
	}
	return manifest, manifest.Validate()
}

// archiveSnapshot is the exact bytes the archive carries. A checkpoint created
// by this version is already scrubbed. An older checkpoint is copied first so
// its recorded hash and any protection reference stay valid.
type archiveSnapshot struct {
	name    string
	size    int64
	hash    string
	cleanup func()
}

// Runtime permissions, request receipts and machine-local paths never belong in
// a portable records backup. Deleting them from the private snapshot copy is
// safe for protection and migration checks, which only read business tables.
var scrubSnapshotTables = []string{
	"device_apply_requests",
	"device_apply_chunks",
	"delegated_tasks",
	"ai_delegations",
	"credentials",
	"idempotency",
	"file_intents",
	"client_diagnostics",
	"author_list_mirror",
	"detached_upload_roots",
}

func (s *Service) exportSnapshot(ctx context.Context, b Backup) (archiveSnapshot, error) {
	original := "backups/" + b.ID + "/snapshot.sqlite"
	info, e := s.Root.Stat(original)
	if e != nil {
		if errors.Is(e, os.ErrNotExist) {
			return archiveSnapshot{}, Fail(409, "backup_checkpoint_missing", "备份检查点文件已被清理，请重新建立检查点。")
		}
		return archiveSnapshot{}, e
	}
	if e = s.verifyContext(ctx, original, b.SnapshotHash, info.Size()); e != nil {
		return archiveSnapshot{}, e
	}
	absolute, e := s.Root.AbsolutePath(original)
	if e != nil {
		return archiveSnapshot{}, e
	}
	secrets, e := snapshotHasSecrets(ctx, absolute)
	if e != nil {
		return archiveSnapshot{}, e
	}
	if !secrets {
		return archiveSnapshot{name: original, size: info.Size(), hash: b.SnapshotHash}, nil
	}
	free, e := freeBytes(s.Config.DataDir)
	if e != nil {
		return archiveSnapshot{}, e
	}
	reserve := s.Config.ReserveBytes + archiveWriteGuard
	if free < info.Size()+reserve {
		return archiveSnapshot{}, Fail(507, "insufficient_storage", fmt.Sprintf("记录备份需要 %s 的检查点副本，当前可用 %s（另需保留存储余量 %s）。请清理空间后重试。", sizeText(info.Size()), sizeText(free), sizeText(reserve)))
	}
	copyName := "backups/" + b.ID + "/snapshot.export.sqlite"
	cleanup := func() {
		for _, name := range []string{copyName, copyName + "-journal", copyName + "-wal", copyName + "-shm"} {
			_ = s.Root.Remove(name)
		}
	}
	cleanup()
	if e = s.copySnapshot(ctx, original, copyName); e != nil {
		cleanup()
		return archiveSnapshot{}, e
	}
	copyInfo, e := s.Root.Stat(copyName)
	if e != nil {
		cleanup()
		return archiveSnapshot{}, e
	}
	copyHash, _, e := s.hashFile(copyName)
	if e != nil {
		cleanup()
		return archiveSnapshot{}, e
	}
	return archiveSnapshot{name: copyName, size: copyInfo.Size(), hash: copyHash, cleanup: cleanup}, nil
}

func (s *Service) copySnapshot(ctx context.Context, source, target string) error {
	input, e := s.Root.Open(source)
	if e != nil {
		return e
	}
	defer input.Close()
	output, e := s.Root.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if e != nil {
		return e
	}
	_, copyErr := io.Copy(output, input)
	syncErr := output.Sync()
	closeErr := output.Close()
	if e = errors.Join(copyErr, syncErr, closeErr); e != nil {
		return e
	}
	absolute, e := s.Root.AbsolutePath(target)
	if e != nil {
		return e
	}
	if e = scrubSnapshotFile(ctx, absolute); e != nil {
		return e
	}
	for _, suffix := range []string{"-journal", "-wal", "-shm"} {
		_ = s.Root.Remove(target + suffix)
	}
	return s.syncParent(target)
}

func snapshotHasSecrets(ctx context.Context, absolute string) (bool, error) {
	db, e := store.OpenDatabase(absolute, store.SnapshotDatabase)
	if e != nil {
		return false, e
	}
	defer db.Close()
	for _, table := range scrubSnapshotTables {
		var exists int
		if e = db.QueryRowContext(ctx, "SELECT 1 FROM sqlite_schema WHERE type='table' AND name=?", table).Scan(&exists); errors.Is(e, sql.ErrNoRows) {
			continue
		} else if e != nil {
			return false, e
		}
		var count int
		if e = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table).Scan(&count); e != nil {
			return false, e
		}
		if count > 0 {
			return true, nil
		}
	}
	return false, nil
}

func scrubSnapshotFile(ctx context.Context, absolute string) error {
	db, e := store.OpenDatabase(absolute, store.SnapshotScrubDatabase)
	if e != nil {
		return e
	}
	defer db.Close()
	// secure_delete overwrites deleted cells instead of leaving recoverable
	// token hashes and request bodies in free pages of the portable copy.
	if _, e = db.ExecContext(ctx, "PRAGMA secure_delete=ON"); e != nil {
		return e
	}
	tx, e := db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	for _, table := range scrubSnapshotTables {
		var exists int
		if e = tx.QueryRowContext(ctx, "SELECT 1 FROM sqlite_schema WHERE type='table' AND name=?", table).Scan(&exists); errors.Is(e, sql.ErrNoRows) {
			continue
		} else if e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, "DELETE FROM "+table); e != nil {
			return e
		}
	}
	return tx.Commit()
}

type reserveWriter struct {
	ctx   context.Context
	s     *Service
	file  *os.File
	since int64
}

// archiveWriteGuard is the headroom kept beyond the configured reserve for the
// chunk being written. Preflight and the per-chunk backstop use the same value.
const archiveWriteGuard = 8 << 20

func (w *reserveWriter) Write(p []byte) (int, error) {
	if e := w.ctx.Err(); e != nil {
		return 0, e
	}
	if w.since == 0 || w.since >= archiveWriteGuard {
		free, e := freeBytes(w.s.Config.DataDir)
		if e != nil {
			return 0, e
		}
		if free-w.s.Config.ReserveBytes < int64(len(p))+archiveWriteGuard {
			return 0, Fail(507, "insufficient_storage", "存储余量不足，未发布不完整归档。")
		}
		w.since = 0
	}
	n, e := w.file.Write(p)
	w.since += int64(n)
	return n, e
}

func sizeText(bytes int64) string {
	switch {
	case bytes >= 1<<30:
		return fmt.Sprintf("%.1f GiB", float64(bytes)/(1<<30))
	case bytes >= 1<<20:
		return fmt.Sprintf("%.1f MiB", float64(bytes)/(1<<20))
	case bytes >= 1<<10:
		return fmt.Sprintf("%.1f KiB", float64(bytes)/(1<<10))
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}

// ensureArchiveSpace makes the reserve explicit before the output file exists.
// ContainerSize is exact for the assembled manifest, so a library larger than
// the free space fails without writing a partial that can fill the volume.
func (s *Service) ensureArchiveSpace(manifest archive.Manifest) error {
	archiveBytes, e := archive.ContainerSize(manifest)
	if e != nil {
		return e
	}
	free, e := freeBytes(s.Config.DataDir)
	if e != nil {
		return e
	}
	reserve := s.Config.ReserveBytes + archiveWriteGuard
	if free < reserve || archiveBytes > free-reserve {
		return Fail(507, "insufficient_storage", fmt.Sprintf("记录备份约需 %s，当前可用 %s（另需保留存储余量 %s）。请清理未完成备份或本机空间后重试。", sizeText(archiveBytes), sizeText(free), sizeText(reserve)))
	}
	return nil
}

func (s *Service) exportBackup(ctx context.Context, b Backup) {
	b.ExportState = "running"
	if e := s.setBackupProgress(ctx, b); e != nil {
		return
	}
	directory := "backups/" + b.ID
	final := directory + "/archive.qnext"
	temporary := directory + "/archive.partial"
	var cleanupSnapshot func()
	fail := func(e error) {
		b.ExportState = "failed"
		b.ExportError = "backup_export_failed"
		b.ExportMessage = ""
		if errors.Is(e, context.Canceled) || errors.Is(e, context.DeadlineExceeded) {
			b.ExportState = "interrupted"
			b.ExportError = "backup_export_interrupted"
			b.ExportMessage = "导出已中断，未发布归档。可重新导出或清除未完成备份。"
		}
		var typed *Error
		if errors.As(e, &typed) {
			b.ExportError = typed.Code
			b.ExportMessage = typed.Message
		}
		if b.ExportMessage == "" {
			b.ExportMessage = "导出未完成，请核对存储空间后重试。"
		}
		// Reclaim the partial and any scrubbed export copy immediately. The
		// snapshot and manifest stay so a transient failure can retry.
		if err := s.Root.Remove(temporary); err != nil && !errors.Is(err, os.ErrNotExist) {
			slog.Warn("backup partial cleanup failed", "backupId", b.ID)
		}
		if cleanupSnapshot != nil {
			cleanupSnapshot()
			cleanupSnapshot = nil
		}
		_ = s.setBackupProgress(context.Background(), b)
	}
	// A process may stop after publishing the verified archive but before saving
	// its completion record. Recovery validates that exact archive instead of
	// silently replacing it with another snapshot.
	if existing, e := s.Root.Open(final); e == nil {
		hash := sha256.New()
		manifest, verifyErr := archive.Read(ctx, io.TeeReader(existing, hash), nil)
		info, statErr := existing.Stat()
		closeErr := existing.Close()
		if verifyErr != nil {
			fail(verifyErr)
			return
		}
		if statErr != nil {
			fail(statErr)
			return
		}
		if closeErr != nil {
			fail(closeErr)
			return
		}
		// Older full archives bind the exact checkpoint bytes. A records-only
		// archive may carry a scrubbed derivative, so its snapshot hash is
		// allowed to differ while the checkpoint identity and scope still match.
		if manifest.BackupID != b.ID || manifest.Scope != archiveScope(b.Scope) ||
			(manifest.Scope == archive.ScopeComplete && manifest.Snapshot.SHA256 != b.SnapshotHash) {
			fail(Fail(409, "backup_archive_conflict", "已有归档与此检查点的范围或快照不一致，已保留供核对。"))
			return
		}
		b.ArchiveHash = hex.EncodeToString(hash.Sum(nil))
		b.ArchiveBytes = info.Size()
		b.TotalFiles = len(manifest.Files)
		b.ExportedFiles = b.TotalFiles
	} else if !errors.Is(e, os.ErrNotExist) {
		fail(e)
		return
	} else {
		snapshot, e := s.exportSnapshot(ctx, b)
		if e != nil {
			fail(e)
			return
		}
		cleanupSnapshot = snapshot.cleanup
		manifest, e := s.prepareArchive(b, snapshot)
		if e != nil {
			fail(e)
			return
		}
		b.TotalFiles = len(manifest.Files)
		if e = s.setBackupProgress(ctx, b); e != nil {
			fail(e)
			return
		}
		// Preflight before the first byte reaches disk. The per-chunk reserve
		// guard stays as the backstop for a volume that shrinks mid-export.
		if e = s.ensureArchiveSpace(manifest); e != nil {
			fail(e)
			return
		}
		file, e := s.Root.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
		if e != nil {
			fail(e)
			return
		}
		hash := sha256.New()
		writer := io.MultiWriter(&reserveWriter{ctx: ctx, s: s, file: file}, hash)
		open := func(f archive.File) (io.ReadCloser, error) {
			if f.Kind == "snapshot" {
				return s.Root.Open(snapshot.name)
			}
			return nil, errors.New("records backup contains an unexpected payload")
		}
		e = archive.Write(ctx, writer, manifest, open, func(count int) error {
			b.ExportedFiles = count
			if count%32 == 0 || count == b.TotalFiles {
				return s.setBackupProgress(ctx, b)
			}
			return nil
		})
		if e == nil {
			e = file.Sync()
		}
		info, statErr := file.Stat()
		closeErr := file.Close()
		if e == nil {
			e = statErr
		}
		if e == nil {
			e = closeErr
		}
		if e != nil {
			fail(e)
			return
		}
		// DiscardBackup runs under s.mu. Serializing publication with it means
		// an archive is either already visible (and therefore refused) or its
		// staging file is removed before publication can create a new one.
		s.mu.Lock()
		e = s.Root.PublishTemp(temporary, final)
		s.mu.Unlock()
		if e != nil {
			fail(e)
			return
		}
		if e = s.syncParent(final); e != nil {
			fail(e)
			return
		}
		b.ArchiveHash = hex.EncodeToString(hash.Sum(nil))
		b.ArchiveBytes = info.Size()
		if cleanupSnapshot != nil {
			cleanupSnapshot()
			cleanupSnapshot = nil
		}
	}
	now := time.Now().UTC()
	b.ExportedAt = &now
	b.ExportState = "completed"
	b.ExportError = ""
	b.ExportMessage = ""
	if e := s.setBackupProgress(context.Background(), b); e != nil {
		return
	}
}
func (s *Service) BackupArchive(ctx context.Context, p model.Principal, id string) (*os.File, Backup, error) {
	b, e := s.Backup(ctx, p, id)
	if e != nil {
		return nil, b, e
	}
	if b.ExportState != "completed" {
		return nil, b, Fail(409, "backup_not_ready", "归档尚未生成，请回读备份状态。")
	}
	f, e := s.Root.Open("backups/" + b.ID + "/archive.qnext")
	return f, b, e
}

// BackupExportEstimate is the client-side pre-check mirror. The export worker
// repeats the check against the exact manifest before it opens the partial file,
// so a stale estimate can never authorize an unsafe write.
type BackupExportEstimate struct {
	Scope         string `json:"scope"`
	SnapshotBytes int64  `json:"snapshotBytes"`
	OriginalBytes int64  `json:"originalBytes"`
	OriginalFiles int    `json:"originalFiles"`
	ArchiveBytes  int64  `json:"archiveBytes"`
	RequiredBytes int64  `json:"requiredBytes"`
	FreeBytes     int64  `json:"freeBytes"`
	ReserveBytes  int64  `json:"reserveBytes"`
	Fits          bool   `json:"fits"`
}

func estimateArchiveBytes(snapshotBytes int64) int64 {
	// The records archive is one manifest entry, one snapshot and the closing
	// blocks. Three KiB covers their headers, padding and the small manifest.
	return snapshotBytes + 3<<10
}

func (s *Service) EstimateBackupExport(ctx context.Context, p model.Principal) (BackupExportEstimate, error) {
	estimate := BackupExportEstimate{Scope: backupScopeMetadata, ReserveBytes: s.Config.ReserveBytes}
	if e := Authorize(p, "admin", ""); e != nil {
		return estimate, e
	}
	tx, e := s.DB.DB.BeginTx(ctx, nil)
	if e != nil {
		return estimate, e
	}
	defer tx.Rollback()
	var pages, pageSize int64
	if e = tx.QueryRowContext(ctx, "PRAGMA page_count").Scan(&pages); e != nil {
		return estimate, e
	}
	if e = tx.QueryRowContext(ctx, "PRAGMA page_size").Scan(&pageSize); e != nil {
		return estimate, e
	}
	estimate.SnapshotBytes = pages * pageSize
	estimate.ArchiveBytes = estimateArchiveBytes(estimate.SnapshotBytes)
	free, e := freeBytes(s.Config.DataDir)
	if e != nil {
		return estimate, e
	}
	estimate.FreeBytes = free
	estimate.ReserveBytes = s.Config.ReserveBytes + archiveWriteGuard
	estimate.RequiredBytes = estimate.ArchiveBytes + estimate.ReserveBytes
	estimate.Fits = free >= estimate.ReserveBytes && estimate.ArchiveBytes <= free-estimate.ReserveBytes
	return estimate, nil
}

func (s *Service) backupStorageBytes(id string) int64 {
	if !safeBackupID(id) {
		return 0
	}
	directory := "backups/" + id
	file, e := s.Root.Open(directory)
	if e != nil {
		return 0
	}
	entries, e := file.ReadDir(-1)
	closeErr := file.Close()
	if e != nil || closeErr != nil {
		return 0
	}
	var total int64
	for _, entry := range entries {
		info, e := s.Root.Lstat(directory + "/" + entry.Name())
		if e == nil && info.Mode().IsRegular() {
			total += info.Size()
		}
	}
	return total
}

func (s *Service) backupArchivePublished(id string) bool {
	_, e := s.Root.Lstat("backups/" + id + "/archive.qnext")
	return e == nil
}

func (s *Service) backupCheckpointReferenced(ctx context.Context, id string) (bool, error) {
	var protections, plans int
	if e := s.DB.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM batch_protections WHERE json_extract(data,'$.checkpointId')=?", id).Scan(&protections); e != nil {
		return false, e
	}
	if e := s.DB.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM migration_plans WHERE json_extract(data,'$.checkpointId')=?", id).Scan(&plans); e != nil {
		return false, e
	}
	return protections > 0 || plans > 0, nil
}

func (s *Service) removeBackupDirectory(id string) error {
	if !safeBackupID(id) {
		return Fail(400, "invalid_backup_id", "备份编号无效。")
	}
	root, e := s.Root.OpenDirectoryRoot("backups", syncPathLink)
	if e != nil {
		return e
	}
	defer root.Close()
	return root.RemoveAll(id)
}

// DiscardBackup removes debris only. A published archive and any checkpoint
// still used by a protection or migration plan are kept, and an active export
// is refused instead of racing it.
func (s *Service) DiscardBackup(ctx context.Context, w *Write, id string) error {
	if e := Authorize(w.Principal, "admin", ""); e != nil {
		return e
	}
	if !safeBackupID(id) {
		return Fail(404, "not_found", "对象不存在。")
	}
	b, e := store.Get[Backup](ctx, s.DB.DB, "SELECT data FROM backups WHERE id=?", id)
	if errors.Is(e, sql.ErrNoRows) {
		return Fail(404, "not_found", "对象不存在。")
	}
	if e != nil {
		return e
	}
	switch b.ExportState {
	case "failed", "interrupted", "unavailable":
	default:
		if b.ExportState == "queued" || b.ExportState == "running" {
			return Fail(409, "backup_export_active", "备份正在导出，不能清除。")
		}
		return Fail(409, "backup_not_discardable", "只有未完成的导出可以清除；已完成的备份与待导出检查点保留。")
	}
	if s.backupArchivePublished(id) {
		return Fail(409, "backup_archive_published", "此检查点已发布归档，不能作为未完成备份清除。")
	}
	referenced, e := s.backupCheckpointReferenced(ctx, id)
	if e != nil {
		return e
	}
	if referenced {
		return Fail(409, "backup_checkpoint_in_use", "此检查点仍被批次保护或迁移计划引用，已保留。")
	}
	if e = s.removeBackupDirectory(id); e != nil {
		return e
	}
	q, e := w.Q()
	if e != nil {
		return e
	}
	_, e = q.ExecContext(ctx, "DELETE FROM backups WHERE id=?", id)
	return e
}

// sweepBackupPartials runs immediately after the exclusive ownership lock and
// before the catalog opens. The lock proves no other process can be exporting,
// so a persisted partial is dead even when the volume is too full for SQLite to
// open. It removes only staging names, never a published archive or a record.
func sweepBackupPartials(root *storage.Root) {
	directory, e := root.OpenDirectoryRoot("backups", syncPathLink)
	if e != nil {
		slog.Warn("backup partial sweep could not open the backup root")
		return
	}
	defer directory.Close()
	entries, e := fs.ReadDir(directory.FS(), ".")
	if e != nil {
		slog.Warn("backup partial sweep could not list the backup root")
		return
	}
	removed := 0
	for _, entry := range entries {
		if !entry.IsDir() || !safeBackupID(entry.Name()) {
			continue
		}
		for _, name := range []string{"archive.partial", "manifest.json.building", "snapshot.sqlite.building", "snapshot.export.sqlite", "snapshot.export.sqlite-journal"} {
			err := root.Remove("backups/" + entry.Name() + "/" + name)
			if err == nil {
				removed++
			} else if !errors.Is(err, os.ErrNotExist) {
				slog.Warn("backup partial sweep could not remove a staging file")
			}
		}
	}
	if removed > 0 {
		slog.Info("backup partial sweep reclaimed staging bytes", "files", removed)
	}
}

// sweepBackupDebris runs once in New, before the service accepts requests or
// starts export workers. It reclaims interrupted exports without touching a
// published archive, a running job, or a checkpoint that is still a valid
// starting point (not_started) for a later export.
func (s *Service) sweepBackupDebris(ctx context.Context) {
	root, e := s.Root.OpenDirectoryRoot("backups", syncPathLink)
	if e != nil {
		slog.Warn("backup debris sweep could not open the backup root")
		return
	}
	defer root.Close()
	entries, e := fs.ReadDir(root.FS(), ".")
	if e != nil {
		slog.Warn("backup debris sweep could not list the backup root")
		return
	}
	directories, partials := 0, 0
	for _, entry := range entries {
		id := entry.Name()
		if !safeBackupID(id) || !entry.IsDir() {
			continue
		}
		directory := "backups/" + id
		b, found, e := backupRecord(ctx, s, id)
		if e != nil {
			slog.Warn("backup debris sweep could not read a record", "backupId", id)
			continue
		}
		state := ""
		if found {
			state = b.ExportState
			if state == "" {
				state = "not_started"
			}
		}
		if state != "running" && state != "queued" {
			for _, name := range []string{"archive.partial", "manifest.json.building", "snapshot.sqlite.building", "snapshot.export.sqlite", "snapshot.export.sqlite-journal"} {
				err := s.Root.Remove(directory + "/" + name)
				if err == nil {
					partials++
				} else if !errors.Is(err, os.ErrNotExist) {
					slog.Warn("backup debris partial removal failed", "backupId", id)
				}
			}
		}
		if s.backupArchivePublished(id) || state == "running" || state == "queued" || state == "completed" || state == "not_started" {
			continue
		}
		if state != "failed" && state != "interrupted" && state != "unavailable" && found {
			continue
		}
		referenced, e := s.backupCheckpointReferenced(ctx, id)
		if e != nil {
			slog.Warn("backup debris sweep could not check references", "backupId", id)
			continue
		}
		if referenced {
			continue
		}
		if e = root.RemoveAll(id); e != nil {
			slog.Warn("backup debris directory removal failed", "backupId", id)
			continue
		}
		directories++
	}
	if directories+partials > 0 {
		slog.Info("backup debris swept", "directories", directories, "partials", partials)
	}
}

func backupRecord(ctx context.Context, s *Service, id string) (Backup, bool, error) {
	b, e := store.Get[Backup](ctx, s.DB.DB, "SELECT data FROM backups WHERE id=?", id)
	if errors.Is(e, sql.ErrNoRows) {
		return b, false, nil
	}
	return b, e == nil, e
}
