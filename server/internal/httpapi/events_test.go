package httpapi_test

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"qimeng.local/next/server/internal/events"
	"qimeng.local/next/server/internal/model"
)

// takeEvent waits for the next invalidation. The timeout turns a missing
// publish into a clear failure instead of a hanging test.
func takeEvent(t *testing.T, ch <-chan events.Event) events.Event {
	t.Helper()
	select {
	case e, ok := <-ch:
		if !ok {
			t.Fatal("event subscription closed")
		}
		return e
	case <-time.After(2 * time.Second):
		t.Fatal("no invalidation event published")
		return events.Event{}
	}
}

func noEvent(t *testing.T, ch <-chan events.Event, what string) {
	t.Helper()
	select {
	case e := <-ch:
		t.Fatalf("%s: unexpected %s event %+v", what, e.Topic, e.Payload)
	case <-time.After(150 * time.Millisecond):
	}
}

// TestEventStreamEnforcesAuthenticationAndStartsWithHello mirrors the
// established stream contract: the endpoint sits behind the same middleware as
// every other route, and a successful response is text/event-stream whose
// first frames announce the reconnect delay and the API version.
func TestEventStreamEnforcesAuthenticationAndStartsWithHello(t *testing.T) {
	f := setup(t)
	srv := httptest.NewServer(f.h)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/events")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated stream status = %d, want 401", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("unauthenticated content type = %q", ct)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/api/v1/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+testSecret)
	stream, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Body.Close()
	if stream.StatusCode != http.StatusOK {
		t.Fatalf("stream status = %d, want 200", stream.StatusCode)
	}
	if ct := stream.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("stream content type = %q", ct)
	}

	reader := bufio.NewReader(stream.Body)
	if frame := streamFrame(t, reader); frame != "retry: 3000\n\n" {
		t.Fatalf("first frame = %q", frame)
	}
	if frame := streamFrame(t, reader); !strings.Contains(frame, "event: hello") || !strings.Contains(frame, `"version":"0.1.0"`) {
		t.Fatalf("hello frame = %q", frame)
	}

	if err := f.s.Events.Publish(events.Event{Topic: events.TopicLibraryChanged}); err != nil {
		t.Fatal(err)
	}
	if frame := streamFrame(t, reader); frame != "event: library.changed\ndata: null\nid: 1\n\n" {
		t.Fatalf("published frame = %q", frame)
	}
}

func streamFrame(t *testing.T, r *bufio.Reader) string {
	t.Helper()
	var b strings.Builder
	for {
		line, err := r.ReadString('\n')
		b.WriteString(line)
		if err != nil {
			t.Fatalf("stream ended: %v (read %q)", err, b.String())
		}
		if line == "\n" {
			return b.String()
		}
	}
}

// TestMutationsPublishMinimalInvalidations proves the write paths that the
// clients rely on: content changes publish library.changed, upload completion
// adds upload.done, and engagement publishes its own topic without pretending
// the asset set changed.
func TestMutationsPublishMinimalInvalidations(t *testing.T) {
	f := setup(t)
	sub, err := f.s.Events.Subscribe(events.TopicLibraryChanged, events.TopicUploadDone, events.TopicFavoriteChanged, events.TopicLikeChanged)
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Close()

	u, _ := f.upload("事件 1.png", 90, nil)
	out := f.complete(u)
	asset := *out.Asset

	first, second := takeEvent(t, sub.C), takeEvent(t, sub.C)
	topics := map[string]events.Event{first.Topic: first, second.Topic: second}
	if _, ok := topics[events.TopicLibraryChanged]; !ok {
		t.Fatalf("upload completion published %v, want library.changed and upload.done", []string{first.Topic, second.Topic})
	}
	done, ok := topics[events.TopicUploadDone]
	if !ok {
		t.Fatalf("upload completion published %v, want upload.done", []string{first.Topic, second.Topic})
	}
	if payload, ok := done.Payload.(events.AssetChangedEvent); !ok || payload.AssetID != asset.ID {
		t.Fatalf("upload.done payload = %#v, want asset %s", done.Payload, asset.ID)
	}

	// Asset metadata, authors, vocabulary and collections all invalidate the
	// same client view.
	var edited model.Asset
	for name, request := range map[string]func(){
		"asset note": func() {
			decode(t, f.json("PATCH", "/assets/"+asset.ID, "event-note", map[string]any{"expectedRevision": asset.Revision, "note": "changed"}, 200), &edited)
		},
		"tag": func() {
			f.json("POST", "/tags", "event-tag", map[string]string{"name": "事件标签"}, 201)
		},
		"collection": func() {
			f.json("POST", "/collections", "event-collection", map[string]any{"libraryId": f.library, "name": "事件集合", "kind": "image_set"}, 201)
		},
		"author": func() {
			f.json("POST", "/authors", "event-author", map[string]any{"name": "事件作者"}, 201)
		},
	} {
		request()
		if e := takeEvent(t, sub.C); e.Topic != events.TopicLibraryChanged {
			t.Fatalf("%s published %s, want library.changed", name, e.Topic)
		}
	}

	f.json("PUT", "/assets/"+asset.ID+"/favorite", "event-favorite", map[string]any{"favorite": true}, 200)
	favorite := takeEvent(t, sub.C)
	if favorite.Topic != events.TopicFavoriteChanged {
		t.Fatalf("favorite published %s, want favorite.changed", favorite.Topic)
	}
	if payload, ok := favorite.Payload.(events.AssetChangedEvent); !ok || payload.AssetID != asset.ID {
		t.Fatalf("favorite payload = %#v, want asset %s", favorite.Payload, asset.ID)
	}
	// Engagement changes personal state, not library membership.
	noEvent(t, sub.C, "favorite must not publish library.changed")

	f.json("PUT", "/assets/"+asset.ID+"/like/today", "event-like", map[string]any{"liked": true}, 200)
	like := takeEvent(t, sub.C)
	if like.Topic != events.TopicLikeChanged {
		t.Fatalf("like published %s, want like.changed", like.Topic)
	}
	if payload, ok := like.Payload.(events.AssetChangedEvent); !ok || payload.AssetID != asset.ID {
		t.Fatalf("like payload = %#v, want asset %s", like.Payload, asset.ID)
	}
	noEvent(t, sub.C, "like must not publish library.changed")

	// Trash invalidates content just like an edit. 收藏/点赞各自推进一次内容修订，
	// 所以删除前重新读取当前修订，不能沿用编辑时的旧值（否则先撞 409，事件断言不会执行）。
	var current model.Asset
	decode(t, f.json("GET", "/assets/"+asset.ID, "", nil, 200), &current)
	f.json("DELETE", "/assets/"+asset.ID+"?expectedRevision="+strconv.FormatInt(current.Revision, 10), "event-trash", struct{}{}, 200)
	if e := takeEvent(t, sub.C); e.Topic != events.TopicLibraryChanged {
		t.Fatalf("trash published %s, want library.changed", e.Topic)
	}
}
