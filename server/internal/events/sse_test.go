package events

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"testing"
	"time"
)

// sseGet opens one stream; the body closes with the test.
func sseGet(t *testing.T, srv *httptest.Server, ctx context.Context) (*http.Response, *bufio.Reader) {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+Path, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp, bufio.NewReader(resp.Body)
}

// readFrame reads one frame up to its terminating empty line.
func readFrame(t *testing.T, r *bufio.Reader) string {
	t.Helper()
	var sb bytes.Buffer
	for {
		line, err := r.ReadString('\n')
		sb.WriteString(line)
		if err != nil {
			t.Fatalf("frame ended unexpectedly: %v (read %q)", err, sb.String())
		}
		if line == "\n" {
			return sb.String()
		}
	}
}

// waitFor polls a convergence condition. It is not a race window: it only
// makes a delayed but guaranteed transition (goroutine exit, counter reset)
// observable without a fixed sleep.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestSSEStreamFrameFormat(t *testing.T) {
	bus := NewBus(nil, 0)
	defer bus.Close()
	h := NewHandler(bus, WithVersion("t-1.0"), WithHeartbeat(time.Hour))
	srv := httptest.NewServer(h)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	resp, r := sseGet(t, srv, ctx)

	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream; charset=utf-8" {
		t.Fatalf("Content-Type = %q", ct)
	}
	if cc := resp.Header.Get("Cache-Control"); cc != "no-cache" {
		t.Fatalf("Cache-Control = %q", cc)
	}
	if ab := resp.Header.Get("X-Accel-Buffering"); ab != "no" {
		t.Fatalf("X-Accel-Buffering = %q", ab)
	}

	if got, want := readFrame(t, r), "retry: 3000\n\n"; got != want {
		t.Fatalf("retry frame:\n got  %q\n want %q", got, want)
	}
	if got, want := readFrame(t, r), "event: hello\ndata: {\"version\":\"t-1.0\"}\nid: 0\n\n"; got != want {
		t.Fatalf("hello frame:\n got  %q\n want %q", got, want)
	}

	type scanPayload struct {
		LibraryID int64 `json:"libraryId"`
		Done      int   `json:"done"`
	}
	if err := bus.Publish(Event{Topic: TopicScanProgress, Payload: scanPayload{LibraryID: 7, Done: 42}}); err != nil {
		t.Fatal(err)
	}
	if got, want := readFrame(t, r), "event: scan.progress\ndata: {\"libraryId\":7,\"done\":42}\nid: 1\n\n"; got != want {
		t.Fatalf("business frame:\n got  %q\n want %q", got, want)
	}

	if err := bus.Publish(Event{Topic: TopicLibraryChanged}); err != nil {
		t.Fatal(err)
	}
	if got, want := readFrame(t, r), "event: library.changed\ndata: null\nid: 2\n\n"; got != want {
		t.Fatalf("broadcast frame:\n got  %q\n want %q", got, want)
	}
}

// TestSSEEngagementEventsPassthrough locks the payload shape shared by the
// favorite and vote signals. Clients parse only the asset identity.
func TestSSEEngagementEventsPassthrough(t *testing.T) {
	bus := NewBus(nil, 0)
	defer bus.Close()
	h := NewHandler(bus, WithHeartbeat(time.Hour))
	srv := httptest.NewServer(h)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, r := sseGet(t, srv, ctx)
	readFrame(t, r) // retry
	readFrame(t, r) // hello

	if err := bus.Publish(Event{Topic: TopicFavoriteChanged, Payload: AssetChangedEvent{AssetID: "018f0000-0000-7000-8000-000000000001"}}); err != nil {
		t.Fatal(err)
	}
	if got, want := readFrame(t, r),
		"event: favorite.changed\ndata: {\"assetId\":\"018f0000-0000-7000-8000-000000000001\"}\nid: 1\n\n"; got != want {
		t.Fatalf("favorite frame:\n got  %q\n want %q", got, want)
	}

	if err := bus.Publish(Event{Topic: TopicLikeChanged, Payload: AssetChangedEvent{AssetID: "018f0000-0000-7000-8000-000000000002"}}); err != nil {
		t.Fatal(err)
	}
	if got, want := readFrame(t, r),
		"event: like.changed\ndata: {\"assetId\":\"018f0000-0000-7000-8000-000000000002\"}\nid: 2\n\n"; got != want {
		t.Fatalf("like frame:\n got  %q\n want %q", got, want)
	}
}

func TestFrameSplitsMultilineData(t *testing.T) {
	// JSON never emits raw newlines today; the splitter keeps the output valid
	// if another encoder ever does.
	var buf bytes.Buffer
	frame(&buf, 3, "x.event", []byte("a\nb\nc"))
	want := "event: x.event\ndata: a\ndata: b\ndata: c\nid: 3\n\n"
	if got := buf.String(); got != want {
		t.Fatalf("multiline frame:\n got  %q\n want %q", got, want)
	}
}

func TestSSEHeartbeatCommentLine(t *testing.T) {
	bus := NewBus(nil, 0)
	defer bus.Close()
	h := NewHandler(bus, WithHeartbeat(20*time.Millisecond))
	srv := httptest.NewServer(h)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, r := sseGet(t, srv, ctx)
	readFrame(t, r) // retry
	readFrame(t, r) // hello

	deadline := time.Now().Add(3 * time.Second)
	for {
		if time.Now().After(deadline) {
			t.Fatal("no heartbeat comment frame within 3s")
		}
		if got := readFrame(t, r); got == ": ping\n\n" {
			return
		}
	}
}

func TestSSEMaxConnsReturns503(t *testing.T) {
	bus := NewBus(nil, 0)
	defer bus.Close()
	h := NewHandler(bus, WithMaxConns(1), WithHeartbeat(time.Hour))
	srv := httptest.NewServer(h)
	defer srv.Close()

	// The first connection is inside the stream loop once retry arrives.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, r := sseGet(t, srv, ctx)
	if got := readFrame(t, r); got != "retry: 3000\n\n" {
		t.Fatalf("first frame = %q", got)
	}

	resp2, err := http.Get(srv.URL + Path)
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("StatusCode = %d, want 503", resp2.StatusCode)
	}
	if ct := resp2.Header.Get("Content-Type"); ct != "application/json; charset=utf-8" {
		t.Fatalf("Content-Type = %q", ct)
	}
	var body struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if err := json.NewDecoder(resp2.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Code != "too_many_connections" {
		t.Fatalf("code = %q", body.Code)
	}
	if body.Message == "" {
		t.Fatal("message must not be empty")
	}
}

func TestSSEDisconnectCleanupNoLeak(t *testing.T) {
	bus := NewBus(nil, 0)
	defer bus.Close()
	h := NewHandler(bus, WithHeartbeat(time.Hour))
	srv := httptest.NewServer(h)
	defer srv.Close()

	runtime.GC()
	before := runtime.NumGoroutine()

	ctx, cancel := context.WithCancel(context.Background())
	_, r := sseGet(t, srv, ctx)
	readFrame(t, r) // retry
	readFrame(t, r) // hello

	// Client disconnect cancels the request context and unwinds the handler.
	cancel()
	waitFor(t, "connection count to return to zero", func() bool { return h.ActiveConnections() == 0 })
	// The httptest server and transport add noise; allow a small tolerance.
	waitFor(t, "goroutine count to settle", func() bool { return runtime.NumGoroutine() <= before+2 })
}

func TestSSEBusCloseEndsStream(t *testing.T) {
	bus := NewBus(nil, 0)
	h := NewHandler(bus, WithHeartbeat(time.Hour))
	srv := httptest.NewServer(h)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, r := sseGet(t, srv, ctx)
	readFrame(t, r) // retry
	readFrame(t, r) // hello

	bus.Close()
	if _, err := r.ReadString('\n'); err != io.EOF {
		t.Fatalf("stream should end with EOF after bus close, got %v", err)
	}
}

func TestSSEShutdownRejectsNewStream(t *testing.T) {
	bus := NewBus(nil, 0)
	bus.Close()
	h := NewHandler(bus, WithHeartbeat(time.Hour))
	srv := httptest.NewServer(h)
	defer srv.Close()

	resp, err := http.Get(srv.URL + Path)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("StatusCode = %d, want 503", resp.StatusCode)
	}
	var body struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Code != "events_shutdown" {
		t.Fatalf("code = %q", body.Code)
	}
}
