package httpapi_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"qimeng.local/next/server/internal/service"
)

func TestSelectedImageReadRangeAndLifetime(t *testing.T) {
	f, rep, donor, original := selectedImageFixture(t)
	etag := `"` + rep.Hash + `"`
	for _, id := range []string{rep.ID, donor.ID} {
		url := "/assets/" + id + "/content?version=1"
		part := f.request("GET", url, "", testSecret, nil, map[string]string{"Range": "bytes=2-9", "If-Range": etag})
		if part.Code != http.StatusPartialContent || part.Header().Get("ETag") != etag ||
			part.Header().Get("Content-Range") != fmt.Sprintf("bytes 2-9/%d", len(original)) || !bytes.Equal(part.Body.Bytes(), original[2:10]) {
			t.Fatal("the selected range and strong ETag must identify the same bytes", part.Code)
		}
		cached := f.request("GET", url, "", testSecret, nil, map[string]string{"If-None-Match": etag})
		if cached.Code != http.StatusNotModified || cached.Body.Len() != 0 {
			t.Fatal("conditional read changed", cached.Code)
		}
	}
	u, control := f.upload("independent-read.png", 91, nil)
	unselected := f.complete(u).Asset
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	first, _, err := f.s.Content(ctx, rep.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, _, err := f.s.Content(t.Context(), donor.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	wait, stop := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer stop()
	if _, _, err = f.s.Content(wait, rep.ID, 1); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("a queued selected read must honor its deadline", err)
	}
	assertNativeOriginal(t, f, unselected.ID, 1, control)

	root, err := f.s.Root.LibraryPath(f.library)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(root, filepath.FromSlash(rep.RelativePath)), pngBytes(t, 78), 0600); err != nil {
		t.Fatal(err)
	}
	for _, reader := range []io.ReadSeeker{first, second} {
		if _, err = reader.Seek(7, io.SeekStart); err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(reader)
		if err != nil || !bytes.Equal(data, original[7:]) {
			t.Fatal("a reader lost its checked image after the source was edited", err)
		}
	}
	// A cancelled response frees its slot even before the handler gets to
	// its deferred Close. The remaining live response still owns its bytes.
	cancel()
	ready, finish := context.WithTimeout(t.Context(), 5*time.Second)
	defer finish()
	_, _, err = f.s.Content(ready, rep.ID, 1)
	var problem *service.Error
	if !errors.As(err, &problem) || problem.Code != "content_mismatch" {
		t.Fatal("a new request must reach the source check after cancellation", err)
	}
	if err = errors.Join(first.Close(), second.Close()); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Join(f.s.Config.DataDir, "cache", "selected-image-reads"))
	if err != nil || len(entries) != 0 {
		t.Fatal("closed and rejected responses left scratch files", len(entries), err)
	}
}
