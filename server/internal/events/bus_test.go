package events

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"reflect"
	"sync"
	"testing"
	"time"
)

// The race detector cannot prove the absence of deadlocks; these tests
// exercise the lock/close ordering directly.

type captureHandler struct {
	mu      sync.Mutex
	records []slog.Record
}

func (c *captureHandler) Enabled(context.Context, slog.Level) bool { return true }

func (c *captureHandler) Handle(_ context.Context, r slog.Record) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.records = append(c.records, r)
	return nil
}

func (c *captureHandler) WithAttrs([]slog.Attr) slog.Handler { return c }
func (c *captureHandler) WithGroup(string) slog.Handler      { return c }

func discardLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// mustRecv receives one event or fails. The timeout only turns a hang into a
// test failure; delivery itself is synchronous on an in-process channel.
func mustRecv(t *testing.T, ch <-chan Event) Event {
	t.Helper()
	select {
	case e, ok := <-ch:
		if !ok {
			t.Fatal("expected an event, channel is closed")
		}
		return e
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for an event")
		return Event{}
	}
}

// mustNotRecv proves nothing arrives within a window. In-process channel
// delivery takes microseconds, so 100ms leaves ample margin.
func mustNotRecv(t *testing.T, ch <-chan Event, what string) {
	t.Helper()
	select {
	case e, ok := <-ch:
		if ok {
			t.Fatalf("%s: unexpected event %+v", what, e)
		}
		t.Fatalf("%s: channel closed unexpectedly", what)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestSubscribeFiltersByTopic(t *testing.T) {
	bus := NewBus(nil, 0)
	defer bus.Close()

	sub, err := bus.Subscribe(TopicScanProgress)
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Close()

	if err := bus.Publish(Event{Topic: TopicThumbnailProgress, Payload: "x"}); err != nil {
		t.Fatal(err)
	}
	mustNotRecv(t, sub.C, "unsubscribed topic")

	want := Event{Topic: TopicScanProgress, Payload: map[string]int{"done": 3}}
	if err := bus.Publish(want); err != nil {
		t.Fatal(err)
	}
	got := mustRecv(t, sub.C)
	if got.Topic != want.Topic || !reflect.DeepEqual(got.Payload, want.Payload) {
		t.Fatalf("event = %+v, want %+v", got, want)
	}
}

func TestSubscribeRequiresTopic(t *testing.T) {
	bus := NewBus(nil, 0)
	defer bus.Close()
	if _, err := bus.Subscribe(); err == nil {
		t.Fatal("an empty topic list must be rejected")
	}
}

func TestMultipleSubscribersAreIsolatedPerTopic(t *testing.T) {
	bus := NewBus(nil, 0)
	defer bus.Close()

	subA, err := bus.Subscribe(TopicScanProgress, TopicLibraryChanged)
	if err != nil {
		t.Fatal(err)
	}
	defer subA.Close()
	subB, err := bus.Subscribe(TopicScanProgress)
	if err != nil {
		t.Fatal(err)
	}
	defer subB.Close()
	subC, err := bus.Subscribe(TopicUploadDone)
	if err != nil {
		t.Fatal(err)
	}
	defer subC.Close()

	if err := bus.Publish(Event{Topic: TopicScanProgress, Payload: "e1"}); err != nil {
		t.Fatal(err)
	}
	for name, s := range map[string]*Subscription{"A": subA, "B": subB} {
		if e := mustRecv(t, s.C); e.Payload != "e1" {
			t.Fatalf("subscriber %s received %+v, want e1", name, e)
		}
	}
	mustNotRecv(t, subC.C, "upload-only subscriber")

	if err := bus.Publish(Event{Topic: TopicUploadDone, Payload: 7}); err != nil {
		t.Fatal(err)
	}
	if e := mustRecv(t, subC.C); e.Payload != 7 {
		t.Fatalf("subscriber C received %+v, want 7", e)
	}
	mustNotRecv(t, subA.C, "subscriber A did not subscribe upload.done")
	mustNotRecv(t, subB.C, "subscriber B did not subscribe upload.done")
}

func TestSlowSubscriberDoesNotBlockPublish(t *testing.T) {
	// A buffer of two simulates a client that stopped reading.
	bus := NewBus(discardLogger(), 2)
	slow, err := bus.Subscribe(TopicScanProgress)
	if err != nil {
		t.Fatal(err)
	}
	defer slow.Close()

	const n = 200
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < n; i++ {
			if err := bus.Publish(Event{Topic: TopicScanProgress, Payload: i}); err != nil {
				t.Errorf("publish failed: %v", err)
				return
			}
		}
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("publish blocked on a slow subscriber; a full buffer must drop frames")
	}

	if e := mustRecv(t, slow.C); e.Payload.(int) != 0 {
		t.Fatalf("first buffered frame = %v, want 0", e.Payload)
	}
	if e := mustRecv(t, slow.C); e.Payload.(int) != 1 {
		t.Fatalf("second buffered frame = %v, want 1", e.Payload)
	}
	mustNotRecv(t, slow.C, "frames beyond the buffer")

	// Drops stay per-subscriber: a new reader is unaffected.
	fresh, err := bus.Subscribe(TopicScanProgress)
	if err != nil {
		t.Fatal(err)
	}
	defer fresh.Close()
	if err := bus.Publish(Event{Topic: TopicScanProgress, Payload: "after"}); err != nil {
		t.Fatal(err)
	}
	if e := mustRecv(t, fresh.C); e.Payload != "after" {
		t.Fatalf("fresh subscriber received %+v, want after", e)
	}
}

func TestDropLogsWarn(t *testing.T) {
	cap := &captureHandler{}
	bus := NewBus(slog.New(cap), 1)
	sub, err := bus.Subscribe(TopicScanProgress)
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Close()

	if err := bus.Publish(Event{Topic: TopicScanProgress}); err != nil {
		t.Fatal(err)
	}
	// A dropped frame is not an error.
	if err := bus.Publish(Event{Topic: TopicScanProgress}); err != nil {
		t.Fatal(err)
	}

	cap.mu.Lock()
	defer cap.mu.Unlock()
	found := false
	for _, rec := range cap.records {
		if rec.Level != slog.LevelWarn {
			continue
		}
		rec.Attrs(func(a slog.Attr) bool {
			if a.Key == "topic" && a.Value.String() == TopicScanProgress {
				found = true
			}
			return true
		})
	}
	if !found {
		t.Fatal("a dropped frame must log a warning with the topic")
	}
}

func TestBusCloseClosesSubscriptionsAndErrors(t *testing.T) {
	bus := NewBus(nil, 0)
	sub, err := bus.Subscribe(TopicScanProgress, TopicLibraryChanged)
	if err != nil {
		t.Fatal(err)
	}

	bus.Close()

	if _, ok := <-sub.C; ok {
		t.Fatal("subscription channel should be closed")
	}
	if err := bus.Publish(Event{Topic: TopicScanProgress}); !errors.Is(err, ErrBusClosed) {
		t.Fatalf("publish error = %v, want ErrBusClosed", err)
	}
	if _, err := bus.Subscribe(TopicScanProgress); !errors.Is(err, ErrBusClosed) {
		t.Fatalf("subscribe error = %v, want ErrBusClosed", err)
	}
	// Repeated close calls must not panic.
	bus.Close()
	sub.Close()
}

func TestConcurrentPublishSubscribeClose(t *testing.T) {
	// Smoke test: concurrent publish, subscribe, unsubscribe and final close
	// must not deadlock or panic (send on closed channel).
	bus := NewBus(discardLogger(), 8)

	var pubWG, rotWG, readerWG sync.WaitGroup
	stop := make(chan struct{})

	for i := 0; i < 3; i++ {
		sub, err := bus.Subscribe(TopicScanProgress, TopicLibraryChanged)
		if err != nil {
			t.Fatal(err)
		}
		readerWG.Add(1)
		go func(s *Subscription) {
			defer readerWG.Done()
			defer s.Close()
			for {
				select {
				case _, ok := <-s.C:
					if !ok {
						return
					}
				case <-stop:
					return
				}
			}
		}(sub)
	}

	for p := 0; p < 4; p++ {
		pubWG.Add(1)
		go func() {
			defer pubWG.Done()
			for i := 0; i < 300; i++ {
				if err := bus.Publish(Event{Topic: TopicScanProgress}); err != nil {
					t.Errorf("concurrent publish failed: %v", err)
					return
				}
			}
		}()
	}

	for i := 0; i < 2; i++ {
		rotWG.Add(1)
		go func() {
			defer rotWG.Done()
			for j := 0; j < 100; j++ {
				s, err := bus.Subscribe(TopicLibraryChanged)
				if err != nil {
					return // closed bus: expected exit
				}
				s.Close()
			}
		}()
	}

	pubWG.Wait()
	rotWG.Wait()
	close(stop)
	bus.Close()
	readerWG.Wait()
}
