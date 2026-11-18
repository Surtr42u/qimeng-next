package service

import (
	"context"
	"errors"
	"os"
	"time"

	"qimeng.local/next/server/internal/model"
)

// Only small regular-library images with no previously indexed path are
// gathered. Slow probes and large original reads start after earlier work is
// committed. The writer yields between files when its time budget expires.
const (
	scanBatchFiles     = 32
	scanBatchFileBytes = 1 << 20
	scanGatherWindow   = 100 * time.Millisecond
	scanWriteWindow    = 20 * time.Millisecond
)

type scannedFile struct {
	relative, full, kind, hash string
	size                       int64
	initial                    os.FileInfo
	probe                      model.Asset
}

func (s *Service) activeScanLibrary(ctx context.Context, run *scanRun) (ScanJob, model.Library, error) {
	j, err := s.Scan(ctx, run.id)
	if err != nil {
		return j, model.Library{}, err
	}
	if j.State == "cancelled" {
		return j, model.Library{}, context.Canceled
	}
	library, err := s.library(ctx, run.library.ID)
	if err == nil && !library.Enabled {
		err = Fail(409, "library_disabled", "库已停用。")
	}
	return j, library, err
}

// Hashing and probing already finished without the mutation lock. Reopen the
// physical location through its registered root immediately before publishing.
func (s *Service) scanOriginalStable(file scannedFile) (bool, error) {
	current, err := s.Root.Stat(file.full)
	if err != nil {
		return false, err
	}
	return os.SameFile(file.initial, current) && file.size == file.initial.Size() &&
		current.Size() == file.size && file.initial.ModTime().Equal(current.ModTime()), nil
}

func skipScanIssue(j *ScanJob, relative, code string) {
	j.Processed++
	j.Skipped++
	j.UpdatedAt = time.Now().UTC()
	scanIssue(j, relative, code)
}

func (s *Service) flushPreparedScans(ctx context.Context, run *scanRun) error {
	files := run.prepared
	run.prepared = nil
	for len(files) > 0 {
		if err := ctx.Err(); err != nil {
			return err
		}
		committed, individual, err := s.commitFreshScanPrefix(ctx, run, files)
		files = files[committed:]
		for _, file := range files[:individual] {
			if singleErr := s.commitScannedFile(ctx, run, file); singleErr != nil {
				return errors.Join(err, singleErr)
			}
		}
		if err != nil {
			return err
		}
		files = files[individual:]
	}
	return nil
}

// Commit as many consecutive, unambiguous new identities as fit a short
// writer turn. Return the following entries needing individual reconciliation.
// All checks use live rows, including inserts from earlier in this transaction.
func (s *Service) commitFreshScanPrefix(ctx context.Context, run *scanRun, files []scannedFile) (int, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	turnStart := time.Now()
	j, library, err := s.activeScanLibrary(ctx, run)
	if err != nil {
		return 0, 0, err
	}
	tx, err := s.DB.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback()
	var pending bool
	if err = tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM file_intents WHERE state IN ('prepared','committed'))").Scan(&pending); err != nil {
		return 0, 0, err
	}
	if pending {
		// The individual path checks both sides of each durable intent.
		return 0, len(files), nil
	}
	var completed, individual int
	var itemErr error
	var inserted []scannedFile
	for _, file := range files {
		if completed > 0 && time.Since(turnStart) >= scanWriteWindow {
			break
		}
		var reconcile bool
		itemErr = tx.QueryRowContext(ctx, `SELECT
			EXISTS(SELECT 1 FROM assets WHERE library_id=? AND relative_path=? AND trashed=0)
			OR EXISTS(SELECT 1 FROM assets INDEXED BY assets_hash WHERE hash=? AND size=? AND library_id=? AND trashed=0)`,
			library.ID, file.relative, file.hash, file.size, library.ID).Scan(&reconcile)
		if itemErr != nil {
			break
		}
		if reconcile {
			individual = 1
			break
		}
		stable, checkErr := s.scanOriginalStable(file)
		if checkErr != nil {
			itemErr = checkErr
			break
		}
		if !stable {
			skipScanIssue(&j, file.relative, "source_changed")
			completed++
			continue
		}
		if _, err = tx.ExecContext(ctx, "SAVEPOINT scan_item"); err != nil {
			return 0, 0, err
		}
		itemErr = s.saveScannedFile(ctx, tx, library, file, model.Asset{}, true, false, &j)
		if itemErr != nil {
			// A failure after creating the asset or a relation must leave both
			// the failing item and its counters out of the successful prefix.
			if _, err = tx.ExecContext(ctx, "ROLLBACK TO scan_item"); err != nil {
				return 0, 0, errors.Join(itemErr, err)
			}
			break
		}
		if _, err = tx.ExecContext(ctx, "RELEASE scan_item"); err != nil {
			return 0, 0, err
		}
		inserted = append(inserted, file)
		completed++
	}
	if completed == 0 {
		return 0, individual, itemErr
	}
	// Staging more than one file widens the interval between the first check
	// and commit. Recheck those originals together at publication. If any was
	// replaced, discard the speculative rows and let each original report its
	// own result. A failed rollback must never be followed by a replay.
	if completed > 1 {
		for _, file := range inserted {
			stable, checkErr := s.scanOriginalStable(file)
			if checkErr != nil || !stable {
				if err = tx.Rollback(); err != nil {
					return 0, 0, errors.Join(itemErr, checkErr, err)
				}
				return 0, completed, errors.Join(itemErr, checkErr)
			}
		}
	}
	if err = saveScan(ctx, tx, j); err != nil {
		return 0, 0, errors.Join(itemErr, err)
	}
	if err = tx.Commit(); err != nil {
		return 0, 0, errors.Join(itemErr, err)
	}
	s.wakeAuthorMirror()
	return completed, individual, itemErr
}
