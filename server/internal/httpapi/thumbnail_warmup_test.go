package httpapi_test

import (
	"bytes"
	"errors"
	"fmt"
	"image/jpeg"
	"io"
	"os"
	"testing"
	"time"

	"qimeng.local/next/server/internal/model"
)

func TestBackgroundThumbnailSizesBeforeBrowsing(t *testing.T) {
	clip := makeNativeVideo(t, "1280x640")
	f := setup(t)
	upload, image := f.upload("unopened.png", 106, nil)
	picture := f.complete(upload).Asset
	video := f.complete(nativeVideoUpload(t, f, "unopened.mp4", clip, nil)).Asset
	if picture == nil || video == nil {
		t.Fatal("ordinary upload did not return both assets")
	}
	media := []struct {
		asset    *model.Asset
		original []byte
	}{{picture, image}, {video, clip}}
	sizes := []struct {
		name string
		edge int
	}{{"md", 512}, {"sm", 256}, {"lg", 1024}}
	cache := func(a *model.Asset, edge int) string {
		return fmt.Sprintf("cache/%s/v3-%d-%d.jpg", a.ID, a.ContentVersion, edge)
	}
	// Observe only the cache directory until the scheduled worker has written
	// all six files. Requesting a thumbnail here would generate it on demand.
	deadline := time.Now().Add(75 * time.Second)
	for _, m := range media {
		for _, size := range sizes {
			path := cache(m.asset, size.edge)
			for {
				_, err := f.s.Root.Stat(path)
				if err == nil {
					break
				}
				if !errors.Is(err, os.ErrNotExist) || time.Now().After(deadline) {
					t.Fatalf("background cache for %s %s: %v", m.asset.FileName, size.name, err)
				}
				time.Sleep(250 * time.Millisecond)
			}
		}
	}
	for _, m := range media {
		for _, size := range sizes {
			file, err := f.s.Root.Open(cache(m.asset, size.edge))
			if err != nil {
				t.Fatal(err)
			}
			data, err := io.ReadAll(file)
			if err = errors.Join(err, file.Close()); err != nil {
				t.Fatal(err)
			}
			cfg, err := jpeg.DecodeConfig(bytes.NewReader(data))
			width := min(size.edge, m.asset.Width)
			if err != nil || cfg.Width != width || cfg.Height != width*m.asset.Height/m.asset.Width {
				t.Fatalf("%s %s JPEG dimensions %dx%d: %v", m.asset.FileName, size.name, cfg.Width, cfg.Height, err)
			}
			response := f.request("GET", fmt.Sprintf("/assets/%s/thumbnail?size=%s&version=%d", m.asset.ID, size.name, m.asset.ContentVersion), "", testSecret, nil, nil)
			if response.Code != 200 || !bytes.Equal(response.Body.Bytes(), data) {
				t.Fatalf("%s %s did not serve the prepared bytes", m.asset.FileName, size.name)
			}
		}
		original := f.request("GET", "/assets/"+m.asset.ID+"/content", "", testSecret, nil, nil)
		if original.Code != 200 || !bytes.Equal(original.Body.Bytes(), m.original) {
			t.Fatal("background previews changed the original", m.asset.FileName)
		}
	}
}
