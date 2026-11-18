package service

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"sync"
	"time"

	"qimeng.local/next/server/internal/model"
	"qimeng.local/next/server/internal/store"
)

type LibraryMonitorConfig struct {
	WatchEnabled bool
	PollInterval time.Duration
}

type LibraryMonitoring struct {
	WatchEnabled       bool   `json:"watchEnabled"`
	State              string `json:"state"`
	WatchedDirectories int    `json:"watchedDirectories"`
	PollIntervalSeconds int   `json:"pollIntervalSeconds"`
	LastError          string `json:"lastError"`
}

type libraryMonitor struct {
	config LibraryMonitorConfig
	wake   chan struct{}
	slots  chan struct{}
	mu     sync.Mutex
	states map[string]LibraryMonitoring
}

func newLibraryMonitor(in *LibraryMonitorConfig) *libraryMonitor {
	if in == nil {
		return nil
	}
	cfg := *in
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 5 * time.Minute
	}
	return &libraryMonitor{
		config: cfg, wake: make(chan struct{}, 1), slots: make(chan struct{}, maxWatchDirectories),
		states: make(map[string]LibraryMonitoring),
	}
}

func (s *Service) wakeLibraryMonitor() {
	if s.monitor != nil {
		select {
		case s.monitor.wake <- struct{}{}:
		default:
		}
	}
}

func (m *libraryMonitor) status(state string) LibraryMonitoring {
	return LibraryMonitoring{
		State: state, WatchEnabled: m.config.WatchEnabled,
		PollIntervalSeconds: int(m.config.PollInterval.Truncate(time.Second).Seconds()),
	}
}

func (m *libraryMonitor) setStatus(id, state string, count int, problem string) {
	v := m.status(state)
	v.WatchedDirectories = count
	v.LastError = problem
	m.mu.Lock()
	m.states[id] = v
	m.mu.Unlock()
}

func (s *Service) LibraryMonitoring(ctx context.Context, p model.Principal, id string) (LibraryMonitoring, error) {
	if err := Authorize(p, "admin", ""); err != nil {
		return LibraryMonitoring{}, err
	}
	lib, err := s.library(ctx, id)
	if err != nil {
		return LibraryMonitoring{}, err
	}
	m := s.monitor
	if m == nil {
		return LibraryMonitoring{State: "disabled"}, nil
	}
	if !lib.Enabled {
		return m.status("disabled"), nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if v, ok := m.states[id]; ok {
		return v, nil
	}
	return m.status("polling"), nil
}

func (s *Service) enqueueMonitoredScan(ctx context.Context, id, trigger string, paths []string) error {
	_, err := s.Run(ctx, syncOwner(), "automaticScan", "", nil, func(w *Write) (any, int, error) {
		job, err := s.startScan(ctx, w, id, 0, trigger, paths)
		return job, 202, err
	})
	return err
}

func (s *Service) pollLibraries(ctx context.Context) error {
	last := ""
	for {
		items, err := store.List[model.Library](ctx, s.DB.DB,
			"SELECT data FROM libraries WHERE id>? AND json_extract(data,'$.enabled')=1 ORDER BY id LIMIT 32", last)
		if err != nil {
			return err
		}
		for _, lib := range items {
			last = lib.ID
			for {
				err = s.enqueueMonitoredScan(ctx, lib.ID, "poll", nil)
				if err == nil {
					break
				}
				var app *Error
				if errors.As(err, &app) {
					if app.Code == "library_disabled" || app.Status == 404 {
						break
					}
					// A full scan already queued on this library is joined in
					// startScan. A targeted write scan cannot satisfy this poll.
					if app.Code == "scan_capacity" || app.Code == "scan_already_queued" {
						select {
						case <-ctx.Done():
							return ctx.Err()
						case <-time.After(time.Second):
							continue
						}
					}
				}
				if ctx.Err() != nil {
					return ctx.Err()
				}
				slog.Warn("library reconciliation could not be queued")
				break
			}
		}
		if len(items) < 32 {
			return nil
		}
	}
}

func (s *Service) monitorLibrary(ctx context.Context, id string) {
	m := s.monitor
	batch := &watchBatch{notice: make(chan struct{}, 1)}
	consumeCtx, stop := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.consumeWatchBatch(consumeCtx, id, batch)
	}()
	defer func() { stop(); <-done }()
	for ctx.Err() == nil {
		m.setStatus(id, "registering", 0, "")
		absolute, err := s.Root.LibraryPath(id)
		if err == nil {
			err = s.watchLibraryRoot(ctx, id, absolute, batch)
		}
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			batch.add("")
			m.setStatus(id, "degraded", 0, "系统文件通知暂不可用，周期扫描继续。"+publicSyncError(err))
			select {
			case <-ctx.Done():
				return
			case <-time.After(5 * time.Minute):
			}
		}
	}
}

func (s *Service) runLibraryMonitor() {
	defer s.workers.Done()
	m, ctx := s.monitor, s.workCtx
	type rootWorker struct {
		cancel   context.CancelFunc
		done     chan struct{}
		revision int64
	}
	watches := make(map[string]rootWorker)
	var group sync.WaitGroup
	defer func() {
		for _, worker := range watches {
			worker.cancel()
		}
		group.Wait()
	}()

	polls := make(chan struct{}, 1)
	requestPoll := func() {
		select {
		case polls <- struct{}{}:
		default:
		}
	}
	group.Add(1)
	go func() {
		defer group.Done()
		for {
			select {
			case <-ctx.Done():
				return
			case <-polls:
				if err := s.pollLibraries(ctx); err != nil && ctx.Err() == nil {
					slog.Warn("automatic index reconciliation could not read registered libraries")
				}
			}
		}
	}()

	refresh := func() {
		libs, err := store.List[model.Library](ctx, s.DB.DB,
			"SELECT data FROM libraries WHERE json_extract(data,'$.enabled')=1 ORDER BY id LIMIT ?", maxWatchRoots)
		if err != nil {
			if !errors.Is(err, context.Canceled) && !errors.Is(err, sql.ErrConnDone) {
				slog.Warn("library watch registration could not be refreshed")
			}
			return
		}
		desired := make(map[string]int64, len(libs))
		for _, lib := range libs {
			desired[lib.ID] = lib.Revision
		}
		for id, worker := range watches {
			// A disable followed immediately by re-enabling may coalesce into
			// one registry notification. Its new revision needs a fresh scan.
			if desired[id] != worker.revision {
				worker.cancel()
				<-worker.done
				delete(watches, id)
				m.mu.Lock()
				delete(m.states, id)
				m.mu.Unlock()
			}
		}
		for _, lib := range libs {
			if _, ok := watches[lib.ID]; ok {
				continue
			}
			child, cancel := context.WithCancel(ctx)
			worker := rootWorker{cancel: cancel, done: make(chan struct{}), revision: lib.Revision}
			watches[lib.ID] = worker
			group.Add(1)
			go func(id string) {
				defer group.Done()
				defer close(worker.done)
				s.monitorLibrary(child, id)
			}(lib.ID)
		}
	}
	if m.config.WatchEnabled {
		refresh()
	} else {
		requestPoll()
	}
	registry := time.NewTicker(30 * time.Second)
	defer registry.Stop()
	poll := time.NewTicker(m.config.PollInterval)
	defer poll.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-m.wake:
			if m.config.WatchEnabled {
				refresh()
			} else {
				requestPoll()
			}
		case <-registry.C:
			if m.config.WatchEnabled {
				refresh()
			}
		case <-poll.C:
			requestPoll()
		}
	}
}
