package service

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"log/slog"
	"maps"
	"os"
	"path"
	"slices"
	"strings"
	"time"

	"qimeng.local/next/server/internal/domain"
	"qimeng.local/next/server/internal/model"
	"qimeng.local/next/server/internal/store"
)

type ScanIssue struct {
	RelativePath string `json:"relativePath"`
	Code         string `json:"code"`
}
type ScanJob struct {
	ID         string      `json:"id"`
	LibraryID  string      `json:"libraryId"`
	Trigger    string      `json:"trigger,omitempty"`
	Scope      string      `json:"scope,omitempty"`
	State      string      `json:"state"`
	Processed  int         `json:"processed"`
	Added      int         `json:"added"`
	Moved      int         `json:"moved"`
	Changed    int         `json:"changed"`
	Skipped    int         `json:"skipped"`
	Missing    int         `json:"missing"`
	IssueCount int         `json:"issueCount"`
	Issues     []ScanIssue `json:"issues"`
	CreatedAt  time.Time   `json:"createdAt"`
	UpdatedAt  time.Time   `json:"updatedAt"`
}

func (s *Service) Scan(ctx context.Context, id string) (ScanJob, error) {
	j, e := store.Get[ScanJob](ctx, s.DB.DB, "SELECT data FROM scans WHERE id=?", id)
	return j, Normalize(e)
}
func saveScan(ctx context.Context, q store.Querier, j ScanJob) error {
	raw, e := store.Encode(struct {
		ScanJob
		CreatedAt string `json:"createdAt"`
		UpdatedAt string `json:"updatedAt"`
	}{j, dateKey(j.CreatedAt), dateKey(j.UpdatedAt)})
	if e != nil {
		return e
	}
	_, e = q.ExecContext(ctx, "INSERT INTO scans(id,library_id,state,data) VALUES(?,?,?,?) ON CONFLICT(id) DO UPDATE SET state=excluded.state,data=excluded.data", j.ID, j.LibraryID, j.State, raw)
	return e
}
func (s *Service) Scans(ctx context.Context, libraryID, state, cursor string, limit int) (model.Page[ScanJob], error) {
	n, e := limited(limit, 50)
	if e != nil {
		return model.Page[ScanJob]{}, e
	}
	filter := libraryID + "\x00" + state
	seek, e := readSeek(cursor, filter, "created_desc")
	if e != nil {
		return model.Page[ScanJob]{}, e
	}
	where := "(?='' OR library_id=?) AND (?='' OR state=?)"
	args := []any{libraryID, libraryID, state, state}
	if seek.ID != "" {
		where += " AND (json_extract(data,'$.createdAt')<? OR (json_extract(data,'$.createdAt')=? AND id<?))"
		args = append(args, seek.Value, seek.Value, seek.ID)
	}
	args = append(args, n+1)
	items, e := store.List[ScanJob](ctx, s.DB.DB, "SELECT data FROM scans WHERE "+where+" ORDER BY json_extract(data,'$.createdAt') DESC,id DESC LIMIT ?", args...)
	if e != nil {
		return model.Page[ScanJob]{}, e
	}
	page := model.Page[ScanJob]{Items: items}
	if len(items) > n {
		page.Items = items[:n]
		last := page.Items[n-1]
		page.NextCursor = makeSeek(filter, "created_desc", dateKey(last.CreatedAt), last.ID)
	}
	return page, nil
}
func (s *Service) StartScan(ctx context.Context, w *Write, libraryID string, revision int64) (ScanJob, error) {
	return s.startScan(ctx, w, libraryID, revision, "manual", scanTargets{inventory: true})
}
func (s *Service) startScan(ctx context.Context, w *Write, libraryID string, revision int64, trigger string, targets scanTargets) (ScanJob, error) {
	if e := Authorize(w.Principal, "admin", ""); e != nil {
		return ScanJob{}, e
	}
	l, e := s.library(ctx, libraryID)
	if e != nil {
		return ScanJob{}, e
	}
	if trigger == "manual" && l.Revision != revision {
		return ScanJob{}, Conflict(l.Revision)
	}
	if !l.Enabled {
		return ScanJob{}, Fail(409, "library_disabled", "库已停用。")
	}
	var active, sameLibrary int
	if e = s.DB.DB.QueryRowContext(ctx, "SELECT count(*),coalesce(sum(CASE WHEN library_id=? THEN 1 ELSE 0 END),0) FROM scans WHERE state IN ('queued','running')", libraryID).Scan(&active, &sameLibrary); e != nil {
		return ScanJob{}, e
	}
	if sameLibrary > 0 {
		if trigger == "manual" || trigger == "poll" {
			existing, e := store.Get[ScanJob](ctx, s.DB.DB, "SELECT data FROM scans WHERE library_id=? AND state IN ('queued','running') LIMIT 1", libraryID)
			if e != nil {
				return ScanJob{}, e
			}
			if existing.Scope != "paths" && (trigger == "poll" || existing.Trigger == "poll" || existing.Trigger == "watch") {
				return existing, nil
			}
		}
		return ScanJob{}, Fail(409, "scan_already_queued", "此库已有扫描正在执行或等待。")
	}
	if active >= 64 {
		return ScanJob{}, Fail(429, "scan_capacity", "扫描队列已满，请等待现有任务完成。")
	}
	scope := "library"
	if !targets.inventory {
		scope = "paths"
	}
	now := time.Now().UTC()
	job := ScanJob{ID: w.ID("scan"), LibraryID: libraryID, Trigger: trigger, Scope: scope, State: "queued", Issues: []ScanIssue{}, CreatedAt: now, UpdatedAt: now}
	q, e := w.Q()
	if e != nil {
		return job, e
	}
	if e = saveScan(ctx, q, job); e != nil {
		return job, e
	}
	if trigger != "manual" {
		if _, e = q.ExecContext(ctx, `DELETE FROM scans WHERE id IN (SELECT id FROM scans WHERE library_id=? AND json_extract(data,'$.trigger') IN ('poll','watch') AND state NOT IN ('queued','running') ORDER BY json_extract(data,'$.createdAt') DESC,id DESC LIMIT -1 OFFSET 32)`, libraryID); e != nil {
			return job, e
		}
	}
	w.after = append(w.after, func() error { s.launchScan(job, targets); return nil })
	return job, nil
}
func (s *Service) CancelScan(ctx context.Context, w *Write, id string) (ScanJob, error) {
	if e := Authorize(w.Principal, "admin", ""); e != nil {
		return ScanJob{}, e
	}
	j, e := s.Scan(ctx, id)
	if e != nil {
		return j, e
	}
	if j.State == "completed" || j.State == "partial" || j.State == "failed" || j.State == "interrupted" || j.State == "cancelled" {
		return j, nil
	}
	j.State = "cancelled"
	j.UpdatedAt = time.Now().UTC()
	q, e := w.Q()
	if e != nil {
		return j, e
	}
	if e = saveScan(ctx, q, j); e != nil {
		return j, e
	}
	w.after = append(w.after, func() error {
		if worker, ok := s.scanWorkers[id]; ok {
			worker.stop()
		}
		s.wakeScans()
		return nil
	})
	return j, nil
}
func scanIssue(j *ScanJob, relative, code string) {
	j.IssueCount++
	if len(j.Issues) < 100 {
		j.Issues = append(j.Issues, ScanIssue{RelativePath: relative, Code: code})
	}
}
func (s *Service) scanProgress(ctx context.Context, id, relative, code string, skipped bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	j, e := s.Scan(ctx, id)
	if e != nil {
		return e
	}
	if j.State == "cancelled" {
		return context.Canceled
	}
	j.Processed++
	if skipped {
		j.Skipped++
	}
	if code == "missing_original" {
		j.Missing++
	}
	if code != "" {
		scanIssue(&j, relative, code)
	}
	j.UpdatedAt = time.Now().UTC()
	return saveScan(ctx, s.DB.DB, j)
}
func (s *Service) scanLibrary(ctx context.Context, id string, targets scanTargets) {
	j, e := s.admitScan(ctx, id)
	run := &scanRun{id: id, lastFlush: time.Now(), manualFull: j.Trigger == "manual", targets: targets}
	if e == nil {
		defer s.releaseScan()
		run.library, e = s.library(ctx, j.LibraryID)
		if e == nil && targets.inventory {
			e = s.walkLibrary(ctx, run, "", 0)
			if len(run.prepared) > 0 && ctx.Err() == nil {
				e = errors.Join(e, s.flushPreparedScans(ctx, run))
			}
			if e == nil {
				e = s.scanMissing(ctx, id, j.LibraryID)
			}
		} else if e == nil {
			paths := slices.Sorted(maps.Keys(targets.writes))
			var known map[string]scanSignature
			known, e = s.scanSignatures(ctx, j.LibraryID, paths)
			if e == nil {
				for _, relative := range paths {
					if e = ctx.Err(); e != nil {
						break
					}
					if e = s.scanFile(ctx, run, relative, known[relative]); e != nil {
						break
					}
				}
			}
		}
	}
	finishCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	s.mu.Lock()
	defer s.mu.Unlock()
	j, readErr := s.Scan(finishCtx, id)
	if readErr != nil {
		return
	}
	j.Skipped += run.skipped
	j.Processed += run.skipped
	if j.State == "cancelled" {
		if run.skipped > 0 {
			if err := saveScan(finishCtx, s.DB.DB, j); err != nil {
				slog.Error("cancelled scan count persistence failed", "scanId", id)
			}
		}
		return
	}
	if errors.Is(e, context.Canceled) {
		j.State = "interrupted"
	} else if e != nil {
		j.State = "failed"
		scanIssue(&j, "", "scan_io_failed")
	} else if j.IssueCount > 0 {
		j.State = "partial"
	} else {
		j.State = "completed"
	}
	j.UpdatedAt = time.Now().UTC()
	if e = saveScan(finishCtx, s.DB.DB, j); e != nil {
		slog.Error("scan terminal persistence failed", "scanId", id)
	}
	if j.Added+j.Moved+j.Changed > 0 {
		s.previews.changed(j.LibraryID, "")
	}
}
func (s *Service) walkLibrary(ctx context.Context, run *scanRun, relative string, depth int) error {
	jobID, libraryID := run.id, run.library.ID
	if e := ctx.Err(); e != nil {
		return e
	}
	if depth > 32 {
		return s.scanProgress(ctx, jobID, relative, "directory_depth_limit", true)
	}
	full := path.Join("libraries", libraryID, relative)
	if e := s.safe(full); e != nil {
		return e
	}
	directory, e := s.Root.Open(full)
	if e != nil {
		return e
	}
	defer directory.Close()
	for {
		entries, readErr := directory.ReadDir(128)
		var mediaPaths []string
		for _, entry := range entries {
			if !entry.IsDir() {
				if _, _, media := domain.MediaType(entry.Name()); media {
					mediaPaths = append(mediaPaths, path.Join(relative, entry.Name()))
				}
			}
		}
		known, e := s.scanSignatures(ctx, libraryID, mediaPaths)
		if e != nil {
			return e
		}
		unchanged, e := s.knownDirectoryEntries(ctx, run, directory, relative, entries, known)
		if e != nil {
			return e
		}
		for _, skip := range unchanged {
			if skip {
				if e = ctx.Err(); e != nil {
					return e
				}
				if e = s.skipScannedEntry(ctx, run); e != nil {
					return e
				}
			}
		}
		for index, entry := range entries {
			if e := ctx.Err(); e != nil {
				return e
			}
			if unchanged != nil && unchanged[index] {
				continue
			}
			if privateLibraryEntry(entry.Name()) {
				continue
			}
			child := path.Join(relative, entry.Name())
			if !domain.ValidMediaPath(child) {
				if e = s.scanProgress(ctx, jobID, child, "invalid_path", true); e != nil {
					return e
				}
				continue
			}
			if entry.Type()&os.ModeSymlink != 0 {
				if e = s.scanProgress(ctx, jobID, child, "symlink_denied", true); e != nil {
					return e
				}
				continue
			}
			if entry.IsDir() {
				info, err := s.Root.Lstat(path.Join(full, entry.Name()))
				if err != nil {
					return err
				}
				if syncPathLink(info) {
					if e = s.scanProgress(ctx, jobID, child, "symlink_denied", true); e != nil {
						return e
					}
					continue
				}
				if e = s.walkLibrary(ctx, run, child, depth+1); e != nil {
					return e
				}
			} else if e = s.scanFile(ctx, run, child, known[child]); e != nil {
				return e
			}
		}
		if errors.Is(readErr, io.EOF) {
			return nil
		}
		if readErr != nil {
			return readErr
		}
	}
}
func (s *Service) scanFile(ctx context.Context, run *scanRun, relative string, previous scanSignature) error {
	jobID, libraryID := run.id, run.library.ID
	if len(run.prepared) > 0 && time.Since(run.preparedAt) >= scanGatherWindow {
		if err := s.flushPreparedScans(ctx, run); err != nil {
			return err
		}
	}
	kind, mime, ok := domain.MediaType(relative)
	if !ok {
		return s.skipScannedEntry(ctx, run)
	}
	full := path.Join("libraries", libraryID, relative)
	if !domain.ValidRelative(full) {
		return s.scanProgress(ctx, jobID, relative, "symlink_denied", true)
	}
	// The registered storage root checks every physical component on this
	// lookup. Lstat also observes a link swapped into the final component.
	initial, e := s.Root.Lstat(full)
	if e != nil {
		if !errors.Is(e, os.ErrNotExist) {
			return s.scanProgress(ctx, jobID, relative, "symlink_denied", true)
		}
		return s.scanProgress(ctx, jobID, relative, "stat_failed", true)
	}
	if syncPathLink(initial) {
		return s.scanProgress(ctx, jobID, relative, "symlink_denied", true)
	}
	if !initial.Mode().IsRegular() {
		return s.scanProgress(ctx, jobID, relative, "not_regular_file", true)
	}
	unchanged := previous.sameSizeAndTime(initial)
	if unchanged {
		depth := strings.Count(relative, "/")
		unchanged = run.library.Kind != "cos" || depth == 0 || previous.DirectoryAuthor != "" && (depth < 2 || previous.COSWork != "")
	}
	if unchanged && !run.targets.verifyAll && !run.targets.writes[relative] {
		return s.skipScannedEntry(ctx, run)
	}
	batchable := run.targets.inventory && previous.RelativePath == "" && run.library.Kind == "regular" &&
		builtinImage(mime) && initial.Size() <= scanBatchFileBytes
	if !batchable && len(run.prepared) > 0 {
		if e = s.flushPreparedScans(ctx, run); e != nil {
			return e
		}
	}
	file := scannedFile{relative: relative, full: full, kind: kind, initial: initial, probe: model.Asset{ID: "scan", MimeType: mime}}
	var skipHash string
	if unchanged {
		skipHash = previous.Hash
	}
	issue := s.inspectScanOriginal(ctx, run, &file, skipHash)
	if e = ctx.Err(); e != nil {
		return e
	}
	if issue != "" {
		return s.scanProgress(ctx, jobID, relative, issue, true)
	}
	if file.hash == skipHash {
		return s.skipScannedEntry(ctx, run)
	}
	if !batchable {
		return s.commitScannedFile(ctx, run, file)
	}
	if len(run.prepared) == 0 {
		run.preparedAt = time.Now()
	}
	run.prepared = append(run.prepared, file)
	if len(run.prepared) == scanBatchFiles {
		return s.flushPreparedScans(ctx, run)
	}
	return nil
}

func (s *Service) commitScannedFile(ctx context.Context, run *scanRun, file scannedFile) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	j, library, e := s.activeScanLibrary(ctx, run)
	if e != nil {
		return e
	}
	libraryID, relative := library.ID, file.relative
	stable, e := s.scanOriginalStable(file)
	if e != nil {
		return e
	}
	if !stable {
		skipScanIssue(&j, relative, "source_changed")
		return saveScan(ctx, s.DB.DB, j)
	}
	pending, e := scanPublicationPending(ctx, s.DB.DB, libraryID, file)
	if e != nil {
		return e
	}
	if pending {
		skipScanIssue(&j, relative, "publication_pending")
		return saveScan(ctx, s.DB.DB, j)
	}
	a, e := store.Get[model.Asset](ctx, s.DB.DB, "SELECT data FROM assets WHERE library_id=? AND relative_path=? AND trashed=0", libraryID, relative)
	isNew := errors.Is(e, sql.ErrNoRows)
	if e != nil && !isNew {
		return e
	}
	moved := false
	if isNew {
		candidates, e := store.List[model.Asset](ctx, s.DB.DB, "SELECT data FROM assets WHERE library_id=? AND hash=? AND size=? AND trashed=0 ORDER BY id LIMIT 101", libraryID, file.hash, file.size)
		if e != nil {
			return e
		}
		missing := []model.Asset{}
		if len(candidates) <= 100 {
			for _, candidate := range candidates {
				if _, e = s.Root.Stat(assetPath(candidate)); errors.Is(e, os.ErrNotExist) {
					missing = append(missing, candidate)
				} else if e != nil {
					return e
				}
			}
		}
		if len(missing) == 1 {
			a = missing[0]
			isNew = false
			moved = true
		} else if len(missing) > 1 || len(candidates) > 100 {
			skipScanIssue(&j, relative, "ambiguous_move")
			return saveScan(ctx, s.DB.DB, j)
		}
	}
	tx, e := s.DB.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if e = s.saveScannedFile(ctx, tx, library, file, a, isNew, moved, &j); e != nil {
		return e
	}
	if e = saveScan(ctx, tx, j); e != nil {
		return e
	}
	if e = tx.Commit(); e != nil {
		return e
	}
	s.wakeAuthorMirror()
	return nil
}

func (s *Service) saveScannedFile(ctx context.Context, tx *sql.Tx, library model.Library, file scannedFile, a model.Asset, isNew, moved bool, j *ScanJob) error {
	relative, now := file.relative, time.Now().UTC()
	if isNew {
		a = model.Asset{ID: ID(), LibraryID: library.ID, OriginalFileName: path.Base(relative), AuthorID: "unknown", AuthorIDs: []string{"unknown"}, AuthorsRevision: 1, AuthorStatus: "unknown", TagsRevision: 1, Revision: 1, ContentVersion: 1, Tags: []string{}, SourceIDs: []string{}, CharacterIDs: []string{}, Characters: []string{}, AcquisitionSources: []string{}, MetadataSource: "scanner", CreatedAt: now}
	}
	previousName := a.FileName
	changedBytes := isNew || a.Hash != file.hash
	if !isNew {
		a.Revision++
		if changedBytes {
			a.ContentVersion++
		}
	}
	a.RelativePath = relative
	a.FileName = path.Base(relative)
	if changedBytes {
		a.Size = file.size
		a.Hash = file.hash
		a.MediaType = file.kind
		a.MimeType = file.probe.MimeType
		a.Width = file.probe.Width
		a.Height = file.probe.Height
		a.DurationMs = file.probe.DurationMs
		a.ProbeState = file.probe.ProbeState
	}
	a.UpdatedAt = now
	modified := file.initial.ModTime().UTC()
	a.ContentMtime = &modified
	a.ContentURL = "/api/v1/assets/" + a.ID + "/content"
	a.ThumbnailURL = ""
	if file.probe.ThumbnailURL != "" {
		a.ThumbnailURL = "/api/v1/assets/" + a.ID + "/thumbnail"
	}
	var e error
	if library.Kind == "cos" {
		if e = s.scanCOSAuthor(ctx, tx, &a); e != nil {
			return e
		}
	}
	var taxonomyChanged bool
	if library.Kind == "regular" {
		if taxonomyChanged, e = s.applySourceRules(ctx, tx, &a); e != nil {
			return e
		}
	}
	switch {
	case isNew:
		e = store.InsertAsset(ctx, tx, a)
	case library.Kind == "regular":
		e = store.UpdateScannedFile(ctx, tx, a)
		if e == nil && taxonomyChanged {
			e = store.SaveAssetTaxonomy(ctx, tx, a, store.CompleteTaxonomy(a))
		}
	default:
		e = store.SaveAsset(ctx, tx, a)
	}
	if e != nil {
		return e
	}
	if moved {
		if e = s.renameAuthorListWorks(ctx, tx, a, previousName); e != nil {
			return e
		}
	}
	if e = s.matchAuthorListsForAsset(ctx, tx, &a, isNew && library.Kind == "regular"); e != nil {
		return e
	}
	j.Processed++
	if isNew {
		j.Added++
	} else if moved {
		j.Moved++
	} else {
		j.Changed++
	}
	j.UpdatedAt = now
	return nil
}
func (s *Service) scanCOSAuthor(ctx context.Context, q store.Querier, a *model.Asset) error {
	author, work, e := s.planCOSPlacement(ctx, q, a)
	if e != nil {
		return e
	}
	return saveCOSPlacement(ctx, q, author, work)
}
