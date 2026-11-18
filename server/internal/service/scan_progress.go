package service

import (
	"context"
	"encoding/json"
	"io"
	"time"

	"qimeng.local/next/server/internal/model"
	"qimeng.local/next/server/internal/store"
)

type scanWorker struct {
	ctx       context.Context
	libraryID string
	automatic bool
	stop      context.CancelFunc
}

// Called after commit with the mutation lock held. The child context reaches
// file reads and ffprobe as well as workers still waiting for a scan slot.
func (s *Service) launchScan(job ScanJob, paths []string) {
	ctx, stop := context.WithCancel(s.workCtx)
	if s.scanWorkers == nil {
		s.scanWorkers = make(map[string]scanWorker)
	}
	worker := scanWorker{ctx: ctx, libraryID: job.LibraryID, automatic: job.Trigger != "manual", stop: stop}
	s.scanWorkers[job.ID] = worker
	s.workers.Add(1)
	go func() {
		defer s.workers.Done()
		defer func() { stop(); s.mu.Lock(); delete(s.scanWorkers, job.ID); s.mu.Unlock() }()
		s.scanLibrary(worker.ctx, job.ID, paths)
	}()
}
func (s *Service) stopLibraryScans(id string) {
	for _, worker := range s.scanWorkers {
		if worker.automatic && worker.libraryID == id {
			worker.stop()
		}
	}
}

type scanReader struct {
	ctx    context.Context
	source io.Reader
}

func (r scanReader) Read(buffer []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.source.Read(buffer)
}

// One scan owns these counters. Asset mutations still read the current asset
// and commit their own result together with the catalog row.
type scanRun struct {
	id        string
	library   model.Library
	skipped   int
	lastFlush time.Time
}

func (s *Service) skipScannedEntry(ctx context.Context, run *scanRun) error {
	run.skipped++
	if run.skipped < 128 && time.Since(run.lastFlush) < time.Second {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	job, err := s.Scan(ctx, run.id)
	if err != nil {
		return err
	}
	if job.State == "cancelled" {
		return context.Canceled
	}
	job.Skipped += run.skipped
	job.Processed += run.skipped
	job.UpdatedAt = time.Now().UTC()
	if err = saveScan(ctx, s.DB.DB, job); err != nil {
		return err
	}
	run.skipped = 0
	run.lastFlush = time.Now()
	return nil
}

type scanSignature struct {
	RelativePath    string     `json:"path"`
	Size            int64      `json:"size"`
	Hash            string     `json:"hash"`
	Mtime           *time.Time `json:"mtime"`
	DirectoryAuthor string     `json:"author"`
	COSWork         string     `json:"work"`
}

func (s *Service) scanSignatures(ctx context.Context, library string, paths []string) (map[string]scanSignature, error) {
	if len(paths) == 0 {
		return nil, nil
	}
	raw, err := json.Marshal(paths)
	if err != nil {
		return nil, err
	}
	items, err := store.List[scanSignature](ctx, s.DB.DB, `SELECT json_object('path',relative_path,'size',size,'hash',hash,
	 'mtime',json_extract(data,'$.contentMtime'),'author',json_extract(data,'$.cosDirectoryAuthorId'),'work',cos_work_id)
	 FROM assets WHERE library_id=? AND trashed=0 AND relative_path IN (SELECT value FROM json_each(?))`, library, string(raw))
	if err != nil {
		return nil, err
	}
	found := make(map[string]scanSignature, len(items))
	for _, item := range items {
		found[item.RelativePath] = item
	}
	return found, nil
}
