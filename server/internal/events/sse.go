package events

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

// Path is the stream endpoint relative to the API base URL. The HTTP layer
// joins it with the generated contract base; clients repeat the full path.
const Path = "/events"

const (
	// DefaultMaxConns bounds concurrent streams. Every stream costs one
	// goroutine plus one subscription buffer; a household deployment rarely
	// exceeds a handful of screens, and a cap keeps a broken client from
	// exhausting the process.
	DefaultMaxConns = 16
	// DefaultHeartbeat must stay below common intermediary idle timeouts
	// (nginx defaults to 60s). SSE comment lines keep the link open.
	DefaultHeartbeat = 15 * time.Second
	// DefaultRetryMS is the reconnect delay announced to clients in the first
	// frame. It only informs clients; the server never replays missed frames.
	DefaultRetryMS = 3000
	// frameWriteTimeout bounds one frame write. A peer that stops reading
	// would otherwise hold its connection slot forever. Normal frames are
	// a few hundred bytes, so this is far above a healthy write.
	frameWriteTimeout = 15 * time.Second
	// noCache prevents intermediaries from turning a live stream into a
	// replayed batch.
	noCache = "no-cache"

	// Error codes follow the service error convention so a client can branch
	// on code exactly as it does for JSON endpoints.
	codeTooManyConnections = "too_many_connections"
	codeStreamUnsupported  = "stream_unsupported"
	codeEventsShutdown     = "events_shutdown"
)

// allTopics is the fixed subscription of one stream. Unknown future topics
// simply never arrive; adding one here is the only server-side change needed.
var allTopics = []string{
	TopicScanProgress,
	TopicLibraryChanged,
	TopicThumbnailProgress,
	TopicUploadDone,
	TopicFavoriteChanged,
	TopicLikeChanged,
}

// helloPayload is the first event frame: the API version lets a client detect
// a talking-to-a-different-build server without waiting for a business event.
type helloPayload struct {
	Version string `json:"version"`
}

// Handler streams bus events as text/event-stream. Its method set matches
// http.Handler, so the router registers it directly and composes authentication
// in front exactly like any other route.
type Handler struct {
	bus       *Bus
	logger    *slog.Logger
	version   string
	maxConns  int64
	heartbeat time.Duration
	conns     atomic.Int64
}

// Option configures a Handler.
type Option func(*Handler)

// WithVersion sets the version reported by the hello frame.
func WithVersion(v string) Option { return func(h *Handler) { h.version = v } }

// WithMaxConns overrides the concurrent stream cap. Non-positive values keep
// the default.
func WithMaxConns(n int) Option {
	return func(h *Handler) {
		if n > 0 {
			h.maxConns = int64(n)
		}
	}
}

// WithHeartbeat overrides the keep-alive interval. Non-positive values keep
// the default; tests shorten it to observe the comment frame.
func WithHeartbeat(d time.Duration) Option {
	return func(h *Handler) {
		if d > 0 {
			h.heartbeat = d
		}
	}
}

// WithLogger overrides the logger; nil keeps slog.Default.
func WithLogger(l *slog.Logger) Option {
	return func(h *Handler) {
		if l != nil {
			h.logger = l
		}
	}
}

// NewHandler builds the stream handler over bus.
func NewHandler(bus *Bus, opts ...Option) *Handler {
	h := &Handler{bus: bus, logger: slog.Default(), version: "dev", maxConns: DefaultMaxConns, heartbeat: DefaultHeartbeat}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// ActiveConnections reports the current stream count for tests and metrics.
func (h *Handler) ActiveConnections() int64 { return h.conns.Load() }

// deadlineWriter is implemented by net/http's server response writer. The
// traffic wrapper exposes the same writer through Unwrap.
type deadlineWriter interface {
	SetWriteDeadline(time.Time) error
}

// setFrameDeadline bounds the next frame write. The transport wraps response
// writers (traffic accounting), so the deadline owner is discovered through
// Unwrap rather than assumed to be the outermost writer.
func (h *Handler) setFrameDeadline(w http.ResponseWriter) {
	for {
		if dw, ok := w.(deadlineWriter); ok {
			if err := dw.SetWriteDeadline(time.Now().Add(frameWriteTimeout)); err != nil && !errors.Is(err, http.ErrNotSupported) {
				h.logger.Warn("events: cannot set SSE write deadline", "error", err)
			}
			return
		}
		unwrapper, ok := w.(interface{ Unwrap() http.ResponseWriter })
		if !ok {
			return
		}
		w = unwrapper.Unwrap()
	}
}

// ServeHTTP serves one GET stream.
// Lifecycle: reserve a slot, validate, subscribe, send retry and hello, then
// loop until the client disconnects, the bus closes, or a write fails. Every
// exit path releases the slot and the subscription.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Reserve before validating, then roll back, so the count never oversells
	// under concurrent connects.
	if n := h.conns.Add(1); n > h.maxConns {
		h.conns.Add(-1)
		h.writeError(w, http.StatusServiceUnavailable, codeTooManyConnections, "实时连接数已达上限，请关闭其他页面后重试")
		return
	}
	defer h.conns.Add(-1)

	flusher, ok := w.(http.Flusher)
	if !ok {
		h.writeError(w, http.StatusInternalServerError, codeStreamUnsupported, "当前响应不支持流式写入")
		return
	}

	sub, err := h.bus.Subscribe(allTopics...)
	if err != nil {
		// The bus closes during graceful shutdown.
		h.writeError(w, http.StatusServiceUnavailable, codeEventsShutdown, "事件服务已关闭")
		return
	}
	defer sub.Close()

	// Headers must be complete before the first write starts the response.
	hdr := w.Header()
	hdr.Set("Content-Type", "text/event-stream; charset=utf-8")
	hdr.Set("Cache-Control", noCache)
	// Reverse proxies buffer responses by default, which turns live frames
	// into one delayed batch.
	hdr.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	// The first frames are immediate: retry tells the client how long to wait
	// after a drop; hello proves the stream is established.
	h.setFrameDeadline(w)
	if _, err := fmt.Fprintf(w, "retry: %d\n\n", DefaultRetryMS); err != nil {
		return
	}
	hello, err := json.Marshal(helloPayload{Version: h.version})
	if err != nil {
		// A struct of one string cannot fail; keep streaming if it ever does.
		h.logger.Error("events: hello payload encoding failed", "err", err)
	} else if err := h.writeFrame(w, 0, "hello", hello); err != nil {
		return
	}
	flusher.Flush()

	ticker := time.NewTicker(h.heartbeat)
	defer ticker.Stop()
	// Event ids are per connection for client-side observation only. No frame
	// history exists, so Last-Event-ID replay is deliberately unsupported;
	// clients re-read the authority after reconnecting.
	var id uint64
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			// A comment line is ignored by clients and proxies; it only keeps
			// the connection alive.
			h.setFrameDeadline(w)
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case e, ok := <-sub.C:
			if !ok {
				return
			}
			id++
			h.setFrameDeadline(w)
			if err := h.writeEvent(w, id, e); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

// writeEvent encodes one business frame. A payload that cannot be encoded
// drops that frame and keeps the stream: one bad record must not close the
// client's only live channel.
func (h *Handler) writeEvent(w io.Writer, id uint64, e Event) error {
	data, err := json.Marshal(e.Payload)
	if err != nil {
		h.logger.Warn("events: dropping unencodable frame", "topic", e.Topic, "err", err)
		return nil
	}
	return h.writeFrame(w, id, e.Topic, data)
}

func (h *Handler) writeFrame(w io.Writer, id uint64, event string, data []byte) error {
	var buf bytes.Buffer
	frame(&buf, id, event, data)
	_, err := w.Write(buf.Bytes())
	return err
}

// frame is the single SSE frame assembler, shared by hello and business
// frames so the wire format cannot drift. WHATWG syntax: field lines end with
// a newline and the frame ends with an empty line.
func frame(buf *bytes.Buffer, id uint64, event string, data []byte) {
	buf.WriteString("event: ")
	buf.WriteString(event)
	buf.WriteByte('\n')
	// Multi-line data must be split into one data line per line. JSON encoding
	// never emits a raw newline today; the split is defensive so another
	// serializer keeps the output valid.
	for _, line := range strings.Split(string(data), "\n") {
		buf.WriteString("data: ")
		buf.WriteString(line)
		buf.WriteByte('\n')
	}
	buf.WriteString("id: ")
	buf.WriteString(strconv.FormatUint(id, 10))
	buf.WriteByte('\n')
	buf.WriteByte('\n')
}

const contentTypeJSON = "application/json; charset=utf-8"

// writeError emits the service error shape (code/message) used by JSON routes.
func (h *Handler) writeError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", contentTypeJSON)
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(map[string]string{"code": code, "message": message}); err != nil {
		// The response has started; a failure means the peer is gone.
		h.logger.Warn("events: error response write failed", "code", code, "err", err)
	}
}
