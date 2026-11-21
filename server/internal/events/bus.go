// Package events delivers committed invalidation signals to connected clients.
//
// Mutations record what a write invalidated; this package only transports that
// fact. It carries no business state, no history and no per-client view. A
// subscriber that falls behind loses frames instead of slowing the writer,
// because every publisher sits on a request or scan hot path.
package events

import (
	"errors"
	"log/slog"
	"sync"
)

const (
	// TopicScanProgress reports scan progress. Progress is best effort: a slow
	// client may miss frames without losing the scan itself.
	TopicScanProgress = "scan.progress"
	// TopicLibraryChanged tells every client that library content changed.
	TopicLibraryChanged = "library.changed"
	// TopicThumbnailProgress reports thumbnail generation progress.
	TopicThumbnailProgress = "thumbnail.progress"
	// TopicUploadDone reports that one upload became an asset.
	TopicUploadDone = "upload.done"
	// TopicFavoriteChanged reports that one asset's favorite state changed.
	TopicFavoriteChanged = "favorite.changed"
	// TopicLikeChanged reports that one asset's dated vote changed.
	TopicLikeChanged = "like.changed"
)

// DefaultBuffer is the per-subscription delivery buffer. It absorbs one burst
// of progress frames without letting a stalled reader grow memory.
const DefaultBuffer = 64

// ErrBusClosed is returned when publishing or subscribing after Close.
var ErrBusClosed = errors.New("events: bus is closed")

// AssetChangedEvent is the data frame shared by topics that identify one asset.
// It is a change signal, not a state projection: the receiver re-reads the
// authoritative record instead of trusting this payload.
type AssetChangedEvent struct {
	AssetID string `json:"assetId"`
}

// Event is one in-process notification. Payload is serialized as the SSE data
// frame; nil payloads are published as a JSON null broadcast.
type Event struct {
	Topic   string
	Payload any
}

// Subscription is one reader's filtered view of the bus. C is read-only and
// Close must be called when the reader stops; otherwise the bus retains the
// channel and the reader can never be collected.
type Subscription struct {
	C <-chan Event

	ch        chan Event
	id        uint64
	topics    map[string]struct{}
	bus       *Bus
	closeOnce sync.Once
}

// Close ends this subscription. It is idempotent and safe after Bus.Close.
func (s *Subscription) Close() { s.bus.remove(s) }

// shutdown closes the channel. Both Bus.Close and Subscription.Close funnel
// here, and closeOnce guarantees a single close.
func (s *Subscription) shutdown() { s.closeOnce.Do(func() { close(s.ch) }) }

// Bus is the process-local publish/subscribe hub.
//
// Concurrency contract: Publish, Close, Subscribe and Subscription.Close may
// run concurrently without panic. mu protects only the subscription map and
// the closed flag. Sending happens only under the read lock, closing only
// under the write lock, so a send can never race a close. Delivery itself is
// non-blocking: a full buffer drops that subscriber's frame and logs, never
// back-pressures the writer.
type Bus struct {
	mu     sync.RWMutex
	subs   map[uint64]*Subscription
	nextID uint64
	closed bool
	logger *slog.Logger
	buf    int
}

// NewBus creates the bus. A nil logger uses slog.Default; a non-positive
// buffer size uses DefaultBuffer.
func NewBus(logger *slog.Logger, bufSize int) *Bus {
	if logger == nil {
		logger = slog.Default()
	}
	if bufSize <= 0 {
		bufSize = DefaultBuffer
	}
	return &Bus{subs: make(map[uint64]*Subscription), logger: logger, buf: bufSize}
}

// Subscribe registers interest in topics. An empty list is an error: a
// subscriber must state its scope instead of silently receiving everything.
func (b *Bus) Subscribe(topics ...string) (*Subscription, error) {
	if len(topics) == 0 {
		return nil, errors.New("events: at least one topic is required")
	}
	ts := make(map[string]struct{}, len(topics))
	for _, t := range topics {
		ts[t] = struct{}{}
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return nil, ErrBusClosed
	}
	b.nextID++
	s := &Subscription{ch: make(chan Event, b.buf), id: b.nextID, topics: ts, bus: b}
	s.C = s.ch
	b.subs[s.id] = s
	return s, nil
}

// Publish delivers one event to every subscriber of its topic. A full buffer
// is not an error: the frame is dropped for that subscriber only. Publishing
// after Close returns ErrBusClosed.
func (b *Bus) Publish(e Event) error {
	b.mu.RLock()
	if b.closed {
		b.mu.RUnlock()
		return ErrBusClosed
	}
	var dropped []uint64
	for _, s := range b.subs {
		if _, ok := s.topics[e.Topic]; !ok {
			continue
		}
		select {
		case s.ch <- e:
		default:
			dropped = append(dropped, s.id)
		}
	}
	b.mu.RUnlock()
	// Log outside the lock: the handler may do IO and must not stall publishers.
	for _, id := range dropped {
		b.logger.Warn("events: subscriber buffer full, frame dropped", "topic", e.Topic, "subscriber", id)
	}
	return nil
}

// Close ends every subscription and rejects later publishes and subscribes.
// It is idempotent and may run concurrently with the other methods.
func (b *Bus) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return
	}
	b.closed = true
	for id, s := range b.subs {
		s.shutdown()
		delete(b.subs, id)
	}
}

// remove detaches and closes one subscription.
func (b *Bus) remove(s *Subscription) {
	b.mu.Lock()
	defer b.mu.Unlock()
	// A concurrent Bus.Close may already have removed it; delete and the
	// once-guarded shutdown are both safe in that case.
	delete(b.subs, s.id)
	s.shutdown()
}
