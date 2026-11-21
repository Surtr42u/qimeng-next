package httpapi_test

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"qimeng.local/next/server/internal/model"
	"qimeng.local/next/server/internal/service"
)

// TestThumbnailCandidatesSeekKeepsAssetIDOrderAndCurrentVersion covers the
// narrow candidate projection: the ID seek must visit every eligible asset once
// in stable order, and the reported version must equal the asset read.
func TestThumbnailCandidatesSeekKeepsAssetIDOrderAndCurrentVersion(t *testing.T) {
	f := setup(t)
	want := []string{}
	for index, name := range []string{"seek-first.png", "seek-second.png", "seek-third.png"} {
		upload, _ := f.upload(name, uint8(131+index), nil)
		want = append(want, f.complete(upload).Asset.ID)
	}
	slices.Sort(want)
	got := []string{}
	query := "/thumbnails/candidates?limit=1"
	for {
		var page service.ThumbnailCandidatePage
		decode(t, f.json("GET", query, "", nil, 200), &page)
		if len(page.Items) != 1 {
			t.Fatalf("candidate page: %+v", page)
		}
		item := page.Items[0]
		asset, err := f.s.Asset(t.Context(), item.AssetID)
		if err != nil || asset.ContentVersion != item.ContentVersion {
			t.Fatalf("candidate %s version %d, asset read %d, %v", item.AssetID, item.ContentVersion, asset.ContentVersion, err)
		}
		got = append(got, item.AssetID)
		if page.NextCursor == "" {
			break
		}
		query = "/thumbnails/candidates?limit=1&cursor=" + url.QueryEscape(page.NextCursor)
	}
	if !slices.Equal(got, want) {
		t.Fatalf("candidate seek order = %v, want %v", got, want)
	}
}

func TestThumbnailCandidatesPageAndCoverageTrackCurrentDiskFiles(t *testing.T) {
	f := setup(t)
	const reader = "synthetic-thumbnail-read-only-credential"
	f.s.Config.AIToken = reader
	for _, path := range []string{"/thumbnails/candidates", "/thumbnails/progress"} {
		if got := f.request("GET", path, "", "", nil, nil).Code; got != 401 {
			t.Fatalf("unauthenticated %s returned %d", path, got)
		}
	}
	if got := f.request("GET", "/thumbnails/progress", "", reader, nil, nil).Code; got != 403 {
		t.Fatalf("read-only credential accessed maintenance progress: %d", got)
	}
	for _, query := range []string{"?limit=101", "?cursor=broken"} {
		f.json("GET", "/thumbnails/candidates"+query, "", nil, 400)
	}

	firstUpload, original := f.upload("thumbnail-first.png", 34, nil)
	first := f.complete(firstUpload).Asset
	secondUpload, _ := f.upload("thumbnail-second.png", 95, nil)
	second := f.complete(secondUpload).Asset
	frame := image.NewPaletted(image.Rect(0, 0, 8, 8), color.Palette{color.Black, color.White})
	var animation bytes.Buffer
	if err := gif.Encode(&animation, frame, nil); err != nil {
		t.Fatal(err)
	}
	var animatedUpload model.Upload
	decode(t, f.json("POST", "/uploads", "thumbnail-animated", map[string]any{
		"libraryId": f.library, "fileName": "thumbnail-animated.gif", "mimeType": "image/gif", "size": animation.Len(), "mode": "ordinary",
	}, 201), &animatedUpload)
	appendResponse := f.request("PUT", "/uploads/"+animatedUpload.ID+"/content", "", testSecret, animation.Bytes(),
		map[string]string{"Content-Type": "application/octet-stream", "Upload-Offset": "0"})
	if appendResponse.Code != 200 {
		t.Fatalf("upload synthetic animation: %d %s", appendResponse.Code, appendResponse.Body)
	}
	animated := f.complete(animatedUpload).Asset

	versions := func() map[string]int64 {
		t.Helper()
		result := map[string]int64{}
		var last, cursor string
		for pageNumber := 0; pageNumber < 3; pageNumber++ {
			response := f.request("GET", "/thumbnails/candidates?limit=1&cursor="+url.QueryEscape(cursor), "", reader, nil, nil)
			if response.Code != 200 {
				t.Fatalf("candidate request: %d %s", response.Code, response.Body)
			}
			var page service.ThumbnailCandidatePage
			decode(t, response, &page)
			if page.InstanceID != f.s.InstanceID || len(page.Items) != 1 || page.Items[0].AssetID <= last {
				t.Fatal("page lost instance, bound or stable ID order", page)
			}
			if pageNumber == 0 && (page.TotalMatched == nil || *page.TotalMatched != 3) ||
				pageNumber > 0 && page.TotalMatched != nil {
				t.Fatal("unexpected first-page count", page.TotalMatched)
			}
			last = page.Items[0].AssetID
			result[last] = page.Items[0].ContentVersion
			cursor = page.NextCursor
			if (cursor != "") != (pageNumber < 2) {
				t.Fatal("wrong continuation availability", page)
			}
		}
		return result
	}
	initial := versions()
	for _, asset := range []*model.Asset{first, second, animated} {
		if initial[asset.ID] != 1 {
			t.Fatal("missing initial candidate:", asset.MediaType)
		}
	}
	progress := func(covered, total int64) {
		t.Helper()
		// Generation and direct disk edits happen outside committed catalog
		// changes, so a cached snapshot would hide them. Force the recount here;
		// the 30-second TTL itself is covered by the service test.
		f.s.InvalidateThumbnailProgressForTest()
		var got service.ThumbnailProgress
		decode(t, f.json("GET", "/thumbnails/progress", "", nil, 200), &got)
		if got.InstanceID != f.s.InstanceID || got.ThumbsOnDisk != covered || got.TotalAssets != total {
			t.Fatalf("coverage %+v, wanted %d/%d", got, covered, total)
		}
	}
	thumbnail := func(asset *model.Asset, size string, code int) *bytes.Buffer {
		t.Helper()
		path := fmt.Sprintf("/assets/%s/thumbnail?version=%d&size=%s&background=true", asset.ID, asset.ContentVersion, size)
		response := f.request("GET", path, "", reader, nil, nil)
		if response.Code != code {
			t.Fatalf("thumbnail request: %d want %d %s", response.Code, code, response.Body)
		}
		if code == 200 && response.Header().Get("X-Qimeng-Instance-Id") != f.s.InstanceID {
			t.Fatal("thumbnail lost instance binding")
		}
		return response.Body
	}
	progress(0, 3)
	thumbnail(second, "sm", 200)
	preview := thumbnail(first, "md", 200)
	if _, err := jpeg.Decode(bytes.NewReader(preview.Bytes())); err != nil {
		t.Fatal("the preview was not real JPEG", err)
	}
	progress(1, 3)

	paths, err := filepath.Glob(filepath.Join(f.s.Config.DataDir, "cache", first.ID, "*-512.jpg"))
	if err != nil || len(paths) != 1 {
		t.Fatal("could not locate the synthetic md derivative", err)
	}
	if err = os.Truncate(paths[0], 0); err != nil {
		t.Fatal(err)
	}
	progress(0, 3)
	rebuilt := thumbnail(first, "md", 200)
	if !bytes.Equal(preview.Bytes(), rebuilt.Bytes()) {
		t.Fatal("empty derivative did not regenerate")
	}
	progress(1, 3)
	if content := f.request("GET", "/assets/"+first.ID+"/content?version=1", "", reader, nil, nil); content.Code != 200 || !bytes.Equal(content.Body.Bytes(), original) {
		t.Fatal("preview operations modified the original")
	}

	replacement, _ := f.upload("thumbnail-replacement.png", 123, map[string]any{"replaceAssetId": first.ID, "expectedContentVersion": 1})
	current := f.complete(replacement).Asset
	if current.ID != first.ID || versions()[first.ID] != 2 {
		t.Fatal("replacement did not advance the same candidate ID")
	}
	thumbnail(first, "md", 409)
	progress(0, 3)
	thumbnail(current, "md", 200)
	progress(1, 3)
	f.json("DELETE", fmt.Sprintf("/assets/%s?expectedRevision=%d", current.ID, current.Revision), "trash-thumbnail", nil, 200)
	progress(0, 2)
	f.json("PATCH", "/libraries/"+f.library, "disable-thumbnail-library", map[string]any{"expectedRevision": 1, "enabled": false}, 200)
	progress(0, 0)
	var disabled service.ThumbnailCandidatePage
	decode(t, f.json("GET", "/thumbnails/candidates", "", nil, 200), &disabled)
	if len(disabled.Items) != 0 || disabled.TotalMatched == nil || *disabled.TotalMatched != 0 {
		t.Fatal("disabled media remained eligible for prefetch")
	}
}
