package service

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"qimeng.local/next/server/internal/domain"
)

const (
	maxWatchRoots       = 64
	maxWatchDirectories = 8192
	maxRootDirectories  = 2048
	maxWatchPaths       = 128
	watchQuiet          = 300 * time.Millisecond
)

type watchBatch struct {
	mu          sync.Mutex
	full        bool
	paths       map[string]bool
	first, last time.Time
	notice      chan struct{}
}

func (b *watchBatch) add(relative string) {
	b.mu.Lock()
	if relative == "" || !b.paths[relative] && len(b.paths) >= maxWatchPaths {
		b.full = true
		b.paths = nil
	} else if !b.full {
		if b.paths == nil {
			b.paths = make(map[string]bool)
		}
		b.paths[relative] = true
	}
	now := time.Now()
	if b.first.IsZero() {
		b.first = now
	}
	b.last = now
	b.mu.Unlock()
	select {
	case b.notice <- struct{}{}:
	default:
	}
}

func (b *watchBatch) take() (ready bool, paths []string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.first.IsZero() || time.Since(b.last) < watchQuiet && time.Since(b.first) < 30*time.Second {
		return false, nil
	}
	for p := range b.paths {
		paths = append(paths, p)
	}
	b.first = time.Time{}
	b.paths = nil
	b.full = false
	return true, paths
}

func (s *Service) consumeWatchBatch(ctx context.Context, id string, b *watchBatch) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-b.notice:
		}
		for {
			timer := time.NewTimer(watchQuiet)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
			ready, paths := b.take()
			if !ready {
				b.mu.Lock()
				empty := b.first.IsZero()
				b.mu.Unlock()
				if empty {
					break
				}
				continue
			}
			err := s.enqueueMonitoredScan(ctx, id, "watch", paths)
			if err == nil {
				break
			}
			var app *Error
			if errors.As(err, &app) {
				if app.Code == "library_disabled" || app.Status == 404 {
					return
				}
				if app.Code == "scan_already_queued" || app.Code == "scan_capacity" {
					b.add("")
					continue
				}
			}
			if ctx.Err() != nil {
				return
			}
			slog.Warn("library change could not be queued; reconciliation will retry")
			select {
			case <-ctx.Done():
				return
			case <-time.After(5 * time.Second):
				b.add("")
			}
		}
	}
}

func privateLibraryEntry(name string) bool {
	return strings.EqualFold(name, ".qimeng-internal") || strings.EqualFold(name, "$RECYCLE.BIN") || strings.EqualFold(name, "System Volume Information")
}

func ignoredWatchName(relative string) bool {
	if !domain.ValidMediaPath(relative) {
		return true
	}
	for part := range strings.SplitSeq(relative, "/") {
		if privateLibraryEntry(part) {
			return true
		}
	}
	return false
}

// Directory registrations are bounded by both the individual root and the
// instance. The event receiver can recognize directory removals while the same
// stream is still being registered. File renames do not rebuild the watches.
type watchTree struct {
	watcher  *fsnotify.Watcher
	root     *os.Root
	absolute string
	slots    chan struct{}
	dirs     sync.Map
	count    int
}

func (t *watchTree) register(ctx context.Context, relative string, depth int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if depth > 32 || t.count >= maxRootDirectories {
		return errors.New("directory watch budget exceeded")
	}
	name := relative
	if name == "" {
		name = "."
	}
	dir, info, err := openSyncSource(t.root, name)
	if err != nil {
		return err
	}
	defer dir.Close()
	if !info.IsDir() {
		return errors.New("watch directory changed")
	}
	select {
	case t.slots <- struct{}{}:
	default:
		return errors.New("instance watch budget exceeded")
	}
	hostPath := filepath.Join(t.absolute, filepath.FromSlash(relative))
	if err = t.watcher.Add(hostPath); err != nil {
		<-t.slots
		return err
	}
	t.count++
	t.dirs.Store(relative, true)
	entry, err := os.Lstat(hostPath)
	if err != nil || syncPathLink(entry) {
		return errors.New("registered directory changed")
	}
	// On Windows a path-only FileInfo can defer the file-ID lookup until
	// SameFile, where an exclusive lookup may collide with the live watcher.
	host, err := os.Open(hostPath)
	if err != nil {
		return err
	}
	registered, err := host.Stat()
	host.Close()
	if err != nil || !os.SameFile(info, registered) {
		return errors.New("registered directory changed")
	}
	for {
		entries, readErr := dir.ReadDir(128)
		for _, entry := range entries {
			if !entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
				continue
			}
			child := filepath.ToSlash(filepath.Join(relative, entry.Name()))
			if ignoredWatchName(child) {
				continue
			}
			info, err := t.root.Lstat(filepath.FromSlash(child))
			if err != nil {
				return err
			}
			if syncPathLink(info) {
				continue
			}
			if err = t.register(ctx, child, depth+1); err != nil {
				return err
			}
		}
		if errors.Is(readErr, io.EOF) {
			return nil
		}
		if readErr != nil {
			return readErr
		}
		if err = ctx.Err(); err != nil {
			return err
		}
	}
}

func (t *watchTree) receive(ctx context.Context, batch *watchBatch, rebuild chan<- struct{}) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err, ok := <-t.watcher.Errors:
			if !ok {
				return errors.New("filesystem notification stream closed")
			}
			return err
		case event, ok := <-t.watcher.Events:
			if !ok {
				return errors.New("filesystem notification stream closed")
			}
			relative, err := filepath.Rel(t.absolute, event.Name)
			if err != nil {
				continue
			}
			relative = filepath.ToSlash(relative)
			if relative == "." {
				if event.Has(fsnotify.Remove) || event.Has(fsnotify.Rename) {
					return errors.New("watched root changed")
				}
				continue
			}
			if ignoredWatchName(relative) {
				continue
			}
			structural := event.Has(fsnotify.Create) || event.Has(fsnotify.Remove) || event.Has(fsnotify.Rename)
			if !structural {
				if _, _, media := domain.MediaType(relative); media && event.Has(fsnotify.Write) {
					batch.add(relative)
				}
				continue
			}
			batch.add("")
			_, wasDirectory := t.dirs.Load(relative)
			if event.Has(fsnotify.Create) {
				if info, err := t.root.Lstat(filepath.FromSlash(relative)); err == nil && info.IsDir() && !syncPathLink(info) {
					wasDirectory = true
				}
			}
			if wasDirectory {
				select {
				case rebuild <- struct{}{}:
				default:
				}
			}
		}
	}
}

func (s *Service) watchLibraryRoot(ctx context.Context, id, absolute string, batch *watchBatch) error {
	if err := s.safe("libraries/" + id); err != nil {
		return err
	}
	root, err := os.OpenRoot(absolute)
	if err != nil {
		return err
	}
	defer root.Close()
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	tree := watchTree{watcher: w, root: root, absolute: absolute, slots: s.monitor.slots}
	readCtx, cancel := context.WithCancel(ctx)
	finished := make(chan struct{})
	rebuild := make(chan struct{}, 1)
	var streamErr error
	go func() {
		defer close(finished)
		streamErr = tree.receive(readCtx, batch, rebuild)
	}()
	defer func() {
		cancel()
		w.Close()
		<-finished
		for i := 0; i < tree.count; i++ {
			<-tree.slots
		}
	}()
	if err = tree.register(ctx, "", 0); err != nil {
		return err
	}
	s.monitor.setStatus(id, "watching", tree.count, "")
	batch.add("")
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-finished:
		return streamErr
	case <-rebuild:
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(watchQuiet):
			return nil
		}
	}
}
