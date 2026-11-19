package service

import (
	"context"
	"log/slog"
	"runtime"
	"sync"
	"time"
)

type previewFlight struct {
	done chan struct{}
	err  error
}
type previewScope struct{ library, asset string }
type previewWork struct {
	mu                sync.Mutex
	active            map[string]*previewFlight
	space             chan struct{}
	closed            bool
	parallel, waiting int
	scopes            map[previewScope]bool
	full              bool
	progress          chan struct{}
	available         func(string) (int64, error)
}

func (p *previewWork) init() {
	p.active = make(map[string]*previewFlight)
	p.space = make(chan struct{})
	p.parallel = min(2, runtime.GOMAXPROCS(0))
	p.scopes = make(map[previewScope]bool)
	p.full = true
	p.progress = make(chan struct{}, 1)
	p.available = freeBytes
}
func (p *previewWork) signal() {
	if p.space != nil {
		close(p.space)
	}
	p.space = make(chan struct{})
}
func (p *previewWork) shutdown() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closed = true
	p.signal()
}
func (p *previewWork) join(ctx context.Context, s *Service, key string, background bool, generate func(context.Context) error) error {
	// Only admitted decodes hold a source link or start a goroutine. A cached
	// file never enters this gate. Each waiter owns only its own cancellation.
	p.mu.Lock()
	queued := false
	leaveQueue := func() {
		if queued {
			p.waiting--
			queued = false
		}
	}
	for {
		if p.closed || ctx.Err() != nil {
			leaveQueue()
			p.signal()
			p.mu.Unlock()
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return context.Canceled
		}
		if current := p.active[key]; current != nil {
			leaveQueue()
			p.mu.Unlock()
			select {
			case <-current.done:
				return current.err
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		limit := p.parallel
		if background {
			limit = max(1, limit-1)
		}
		if len(p.active) < limit && (!background || p.waiting == 0) {
			leaveQueue()
			flight := &previewFlight{done: make(chan struct{})}
			p.active[key] = flight
			s.workers.Add(1)
			p.mu.Unlock()
			go func() {
				defer s.workers.Done()
				work, cancel := context.WithTimeout(s.workCtx, time.Minute)
				defer cancel()
				var err error
				defer func() {
					if recover() != nil {
						slog.Error("thumbnail decoder panicked")
						err = Fail(500, "thumbnail_decode_error", "生成缩略图时发生错误，原件已保留。")
					}
					p.mu.Lock()
					flight.err = err
					delete(p.active, key)
					close(flight.done)
					p.signal()
					p.mu.Unlock()
				}()
				err = generate(work)
			}()
			select {
			case <-flight.done:
				return flight.err
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		if !background && !queued {
			p.waiting++
			queued = true
		}
		changed := p.space
		p.mu.Unlock()
		select {
		case <-changed:
		case <-ctx.Done():
		}
		p.mu.Lock()
	}
}

// Asset events coalesce until the next pass. Escalating to a complete scan
// keeps the pending set bounded while preserving all requested work.
func (p *previewWork) changed(library, asset string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed || p.full {
		return
	}
	if library == "" && asset == "" || len(p.scopes) >= 256 {
		p.full = true
		clear(p.scopes)
		return
	}
	key := previewScope{library, asset}
	if asset != "" && p.scopes[previewScope{library: library}] {
		return
	}
	if asset == "" {
		for k := range p.scopes {
			if k.library == library {
				delete(p.scopes, k)
			}
		}
	}
	p.scopes[key] = true
}
func (p *previewWork) pending(force bool) (bool, []previewScope) {
	p.mu.Lock()
	defer p.mu.Unlock()
	full := p.full || force
	p.full = false
	var scopes []previewScope
	if !full {
		for key := range p.scopes {
			scopes = append(scopes, key)
		}
	}
	clear(p.scopes)
	return full, scopes
}
