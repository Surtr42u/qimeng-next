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
	pending     scanTargets
	first, last time.Time
	notice      chan struct{}
}

// Inventory discovers moves, additions and missing originals. Write notices
// independently require a digest even when size and mtime still match.
type scanTargets struct {
	inventory bool
	verifyAll bool
	writes    map[string]bool
}

func (b *watchBatch) add(targets scanTargets) {
	b.mu.Lock()
	b.pending.inventory = b.pending.inventory || targets.inventory
	if !b.pending.verifyAll {
		for name := range targets.writes {
			if !b.pending.writes[name] && len(b.pending.writes) == maxWatchPaths {
				targets.verifyAll = true
				break
			}
			if b.pending.writes == nil {
				b.pending.writes = make(map[string]bool)
			}
			b.pending.writes[name] = true
		}
		if targets.verifyAll {
			b.pending = scanTargets{inventory: true, verifyAll: true}
		}
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

func (b *watchBatch) enqueue(now time.Time, submit func(scanTargets) error) (bool, error) {
	b.mu.Lock()
	if b.first.IsZero() || now.Sub(b.last) < watchQuiet && now.Sub(b.first) < 30*time.Second {
		b.mu.Unlock()
		return false, nil
	}
	targets := b.pending
	b.pending = scanTargets{}
	b.first = time.Time{}
	b.mu.Unlock()
	if err := submit(targets); err != nil {
		// The catalog writer may have been busy while new events arrived.
		// Merge both sets under the same path budget before retrying.
		b.add(targets)
		return true, err
	}
	return true, nil
}

func (s *Service) consumeWatchBatch(ctx context.Context, id string, b *watchBatch) {
	submit := func(targets scanTargets) error { return s.enqueueMonitoredScan(ctx, id, "watch", targets) }
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
			ready, err := b.enqueue(time.Now(), submit)
			if !ready {
				b.mu.Lock()
				empty := b.first.IsZero()
				b.mu.Unlock()
				if empty {
					break
				}
				continue
			}
			if err == nil {
				break
			}
			var app *Error
			if errors.As(err, &app) {
				if app.Code == "library_disabled" || app.Status == 404 {
					return
				}
				if app.Code == "scan_already_queued" || app.Code == "scan_capacity" {
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
			}
		}
	}
}

func (b *watchBatch) observe(event fsnotify.Event, relative string) (structural bool) {
	structural = event.Has(fsnotify.Create) || event.Has(fsnotify.Remove) || event.Has(fsnotify.Rename)
	targets := scanTargets{inventory: structural}
	// A create can also name an atomic replacement of an indexed file.
	if _, _, media := domain.MediaType(relative); media && (event.Has(fsnotify.Write) || event.Has(fsnotify.Create)) {
		targets.writes = map[string]bool{relative: true}
	}
	if structural || len(targets.writes) > 0 {
		b.add(targets)
	}
	return structural
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
			if errors.Is(err, fsnotify.ErrEventOverflow) {
				batch.add(scanTargets{inventory: true, verifyAll: true})
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
			if !batch.observe(event, relative) {
				continue
			}
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
	batch.add(scanTargets{inventory: true})
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
