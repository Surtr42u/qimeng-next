package httpapi_test

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"image/jpeg"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"qimeng.local/next/server/internal/model"
	"qimeng.local/next/server/internal/service"
)

func TestBackgroundThumbnailSizesBeforeBrowsing(t *testing.T) {
	clip := makeNativeVideo(t, "1280x640")
	scanned := []struct {
		name     string
		mime     string
		width    int
		height   int
		original []byte
	}{
		{"legacy-mp4.mkv", "video/x-matroska", 3840, 2160, makeNativeVideo(t, "3840x2160")},
		{"native.mkv", "video/x-matroska", 1280, 640, makeWarmupVideo(t, "matroska", "libx264")},
		{"native.webm", "video/webm", 1280, 640, makeWarmupVideo(t, "webm", "libvpx-vp9")},
		{"native.mov", "video/quicktime", 1280, 640, makeWarmupVideo(t, "mov", "mpeg4")},
		{"native.avi", "video/x-msvideo", 1280, 640, makeWarmupVideo(t, "avi", "mpeg4")},
	}
	f := setup(t)
	upload, image := f.upload("unopened.png", 106, nil)
	picture := f.complete(upload).Asset
	video := f.complete(nativeVideoUpload(t, f, "unopened.mp4", clip, nil)).Asset
	if picture == nil || video == nil {
		t.Fatal("ordinary upload did not return both assets")
	}
	type warmupOriginal struct {
		asset    *model.Asset
		original []byte
	}
	media := []warmupOriginal{{picture, image}, {video, clip}}
	root, err := f.s.Root.LibraryPath(f.library)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range scanned {
		if err := os.WriteFile(filepath.Join(root, m.name), m.original, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if job := f.scan("scan-synthetic-video-containers", 45*time.Second); job.State != "completed" || job.IssueCount != 0 {
		t.Fatalf("scan of existing videos: %+v", job)
	}
	var assets model.Page[model.Asset]
	decode(t, f.json("GET", "/assets", "", nil, 200), &assets)
	byName := make(map[string]*model.Asset, len(assets.Items))
	for i := range assets.Items {
		item := &assets.Items[i]
		byName[item.FileName] = item
	}
	for _, m := range scanned {
		a := byName[m.name]
		if a == nil || a.ProbeState != "ready" || a.Width != m.width || a.Height != m.height ||
			a.MimeType != m.mime || a.RelativePath != m.name || a.Hash != fmt.Sprintf("%x", sha256.Sum256(m.original)) {
			t.Fatalf("unexpected catalog record for %s: %+v", m.name, a)
		}
		media = append(media, warmupOriginal{a, m.original})
	}
	sizes := []struct {
		name string
		edge int
	}{{"md", 512}, {"sm", 256}, {"lg", 1024}}
	cache := func(a *model.Asset, edge int) string {
		// The path encodes the service's preview policy; derive it from the
		// exported version instead of duplicating the current literal.
		return fmt.Sprintf("cache/%s/%s-%d-%d.jpg", a.ID, service.PreviewPolicyVersion(), a.ContentVersion, edge)
	}
	// Observe only the cache directory until the scheduled worker has written
	// every size. Requesting a thumbnail here would generate it on demand.
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
			frame, err := jpeg.Decode(bytes.NewReader(data))
			if err != nil {
				t.Fatalf("%s %s is not a decodable JPEG: %v", m.asset.FileName, size.name, err)
			}
			width := min(size.edge, m.asset.Width)
			bounds := frame.Bounds()
			if bounds.Dx() != width || bounds.Dy() != width*m.asset.Height/m.asset.Width {
				t.Fatalf("%s %s JPEG dimensions %v", m.asset.FileName, size.name, bounds.Size())
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
		var after model.Asset
		decode(t, f.json("GET", "/assets/"+m.asset.ID, "", nil, 200), &after)
		if after.ID != m.asset.ID || after.FileName != m.asset.FileName || after.RelativePath != m.asset.RelativePath ||
			after.Hash != m.asset.Hash || after.MimeType != m.asset.MimeType || after.ContentVersion != m.asset.ContentVersion ||
			after.Revision != m.asset.Revision {
			t.Fatal("background previews changed the catalog identity", m.asset.FileName)
		}
	}
}

func makeWarmupVideo(t *testing.T, muxer, codec string) []byte {
	t.Helper()
	name := filepath.Join(t.TempDir(), "clip")
	output, err := exec.Command("ffmpeg", "-nostdin", "-v", "error", "-f", "lavfi", "-i", "testsrc2=size=1280x640:rate=4",
		"-t", "1", "-threads", "1", "-c:v", codec, "-pix_fmt", "yuv420p", "-f", muxer, name).CombinedOutput()
	if err != nil {
		t.Fatalf("creating synthetic %s video: %v\n%s", muxer, err, output)
	}
	data, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
