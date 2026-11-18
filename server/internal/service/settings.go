package service

import (
	"context"
	"database/sql"
	"errors"
	"runtime"
	"time"

	"qimeng.local/next/server/internal/store"
)

type ScanSettings struct {
	Workers   int `json:"workers"`
	ThumbEdge int `json:"thumbEdge"`
}
type UploadSettings struct {
	AutoAccept bool `json:"autoAccept"`
}
type Settings struct {
	Revision             int64          `json:"revision"`
	Scan                 ScanSettings   `json:"scan"`
	Upload               UploadSettings `json:"upload"`
	EffectiveScanWorkers int            `json:"effectiveScanWorkers"`
	MaxChunkBytes        int64          `json:"maxChunkBytes"`
	MaxActiveUploads     int            `json:"maxActiveUploads"`
	MaxBatchItems        int            `json:"maxBatchItems"`
	SourceCleanupEnabled bool           `json:"sourceCleanupEnabled"`
}
type SettingsUpdate struct {
	ExpectedRevision int64 `json:"expectedRevision"`
	Scan             *struct {
		Workers   *int `json:"workers"`
		ThumbEdge *int `json:"thumbEdge"`
	} `json:"scan"`
	Upload *struct {
		AutoAccept *bool `json:"autoAccept"`
	} `json:"upload"`
}

// Missing persisted settings have a documented default. Corrupt/unreadable
// settings are errors, never a reason to reopen the upload gate.
func (s *Service) Settings(ctx context.Context) (Settings, error) {
	result, e := store.Get[Settings](ctx, s.DB.DB, "SELECT value FROM settings WHERE key='operational_settings'")
	if errors.Is(e, sql.ErrNoRows) {
		result = Settings{Revision: 1, Scan: ScanSettings{Workers: 0, ThumbEdge: 800}, Upload: UploadSettings{AutoAccept: true}}
	} else if e != nil {
		return result, e
	}
	result.MaxChunkBytes = MaxChunkBytes
	result.MaxActiveUploads = MaxActiveUploads
	result.MaxBatchItems = MaxBatchItems
	result.EffectiveScanWorkers = result.Scan.Workers
	if result.EffectiveScanWorkers == 0 {
		result.EffectiveScanWorkers = automaticScanWorkers(runtime.GOMAXPROCS(0), availableMemory())
	}
	return result, nil
}

func automaticScanWorkers(cpus int, available uint64) int {
	// Leave CPU and memory headroom for browsing and decoding. If host memory
	// telemetry is unavailable, one sequential library remains the safe default.
	if available == 0 {
		return 1
	}
	cpuLimit := max(1, cpus/2)
	memoryLimit := max(1, int(min(available/(256<<20), uint64(8))))
	return min(8, cpuLimit, memoryLimit)
}

func (s *Service) UpdateSettings(ctx context.Context, w *Write, in SettingsUpdate) (Settings, error) {
	if e := Authorize(w.Principal, "admin", ""); e != nil {
		return Settings{}, e
	}
	current, e := s.Settings(ctx)
	if e != nil {
		return current, e
	}
	if in.ExpectedRevision != current.Revision {
		return current, Conflict(current.Revision)
	}
	if in.Scan == nil || in.Scan.Workers == nil || in.Scan.ThumbEdge == nil || in.Upload == nil || in.Upload.AutoAccept == nil {
		return current, Fail(400, "settings_required", "请提供完整扫描和上传设置，不能遗漏字段。")
	}
	if *in.Scan.Workers < 0 || *in.Scan.Workers > 32 || *in.Scan.ThumbEdge < 200 || *in.Scan.ThumbEdge > 1600 {
		return current, Fail(400, "invalid_settings", "扫描并发须为0–32，预留长边须为200–1600。")
	}
	current.Scan = ScanSettings{Workers: *in.Scan.Workers, ThumbEdge: *in.Scan.ThumbEdge}
	current.Upload.AutoAccept = *in.Upload.AutoAccept
	current.Revision++
	current.EffectiveScanWorkers = current.Scan.Workers
	if current.EffectiveScanWorkers == 0 {
		current.EffectiveScanWorkers = automaticScanWorkers(runtime.GOMAXPROCS(0), availableMemory())
	}
	raw, e := store.Encode(current)
	if e != nil {
		return current, e
	}
	q, e := w.Q()
	if e != nil {
		return current, e
	}
	if _, e = q.ExecContext(ctx, "INSERT INTO settings(key,value) VALUES('operational_settings',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value", raw); e != nil {
		return current, e
	}
	w.after = append(w.after, func() error { s.wakeScans(); return nil })
	return current, nil
}

func (s *Service) uploadsAllowed(ctx context.Context) error {
	settings, e := s.Settings(ctx)
	if e != nil {
		return e
	}
	if !settings.Upload.AutoAccept {
		return Fail(403, "uploads_paused", "上传接收已关闭；已接收内容保留，可在设置中重新开启后继续。")
	}
	return nil
}

// All accesses use Service.mu, including the after-commit callback in Run.
func (s *Service) wakeScans() { close(s.scanWake); s.scanWake = make(chan struct{}) }
func (s *Service) admitScan(ctx context.Context, id string) (ScanJob, error) {
	for {
		if e := ctx.Err(); e != nil {
			return ScanJob{}, e
		}
		s.mu.Lock()
		job, e := s.Scan(ctx, id)
		if e != nil {
			s.mu.Unlock()
			return job, e
		}
		if job.State == "cancelled" {
			s.mu.Unlock()
			return job, context.Canceled
		}
		config, e := s.Settings(ctx)
		if e != nil {
			s.mu.Unlock()
			return job, e
		}
		if s.activeScans < config.EffectiveScanWorkers {
			job.State = "running"
			job.UpdatedAt = time.Now().UTC()
			e = saveScan(ctx, s.DB.DB, job)
			if e == nil {
				s.activeScans++
			}
			s.mu.Unlock()
			return job, e
		}
		changed := s.scanWake
		s.mu.Unlock()
		select {
		case <-ctx.Done():
			return job, ctx.Err()
		case <-changed:
		}
	}
}
func (s *Service) releaseScan() { s.mu.Lock(); defer s.mu.Unlock(); s.activeScans--; s.wakeScans() }
