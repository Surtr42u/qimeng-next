package httpapi_test

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"

	"qimeng.local/next/server/internal/model"
	"qimeng.local/next/server/internal/service"
)

func TestSettingsUploadPausePreservesResumeAndRejectsStaleOrMissingFields(t *testing.T) {
	f := setup(t)
	u, body := f.upload("paused.png", 22, nil)
	var settings service.Settings
	decode(t, f.json("GET", "/settings", "", nil, 200), &settings)
	if settings.Revision != 1 || settings.Scan.Workers != 0 || settings.Scan.ThumbEdge != 800 || !settings.Upload.AutoAccept || settings.EffectiveScanWorkers < 1 || settings.EffectiveScanWorkers > 8 {
		t.Fatalf("unexpected defaults: %+v", settings)
	}
	update := map[string]any{"expectedRevision": settings.Revision, "scan": map[string]int{"workers": 2, "thumbEdge": 800}, "upload": map[string]bool{"autoAccept": false}}
	decode(t, f.json("PUT", "/settings", "settings-close-upload", update, 200), &settings)
	f.json("PUT", "/settings", "settings-stale-write", update, 409)
	f.json("POST", "/uploads/"+u.ID+"/complete", "paused-completion", struct{}{}, 403)
	r := f.request("PUT", "/uploads/"+u.ID+"/content", "", testSecret, body, map[string]string{"Upload-Offset": "0"})
	if r.Code != 403 {
		t.Fatalf("pause bypassed via chunk: %d", r.Code)
	}
	f.json("POST", "/uploads", "paused-new-upload", map[string]any{"libraryId": f.library, "fileName": "next.png", "size": len(body), "mimeType": "image/png", "mode": "ordinary"}, 403)
	f.json("PUT", "/settings", "settings-missing-field", map[string]any{"expectedRevision": settings.Revision, "scan": map[string]int{"workers": 1, "thumbEdge": 800}, "upload": map[string]any{}}, 400)
	update["expectedRevision"] = settings.Revision
	update["upload"] = map[string]bool{"autoAccept": true}
	f.json("PUT", "/settings", "settings-reopen-upload", update, 200)
	out := f.complete(u)
	if out.Asset == nil || out.Upload.ID != u.ID {
		t.Fatal("resuming lost the staged source or upload identity")
	}
}

func TestThumbnailSizesHaveIndependentRepresentationsAndOriginalBytes(t *testing.T) {
	f := setup(t)
	source := image.NewRGBA(image.Rect(0, 0, 1200, 600))
	for y := 0; y < 600; y++ {
		for x := 0; x < 1200; x++ {
			source.SetRGBA(x, y, color.RGBA{uint8(x % 251), uint8(y % 239), 180, 255})
		}
	}
	var buffer bytes.Buffer
	if e := png.Encode(&buffer, source); e != nil {
		t.Fatal(e)
	}
	body := buffer.Bytes()
	var upload model.Upload
	decode(t, f.json("POST", "/uploads", "thumbnail-size-input", map[string]any{"libraryId": f.library, "fileName": "sizing.png", "size": len(body), "mimeType": "image/png", "mode": "ordinary"}, 201), &upload)
	r := f.request("PUT", "/uploads/"+upload.ID+"/content", "", testSecret, body, map[string]string{"Upload-Offset": "0"})
	if r.Code != 200 {
		t.Fatal(r.Body.String())
	}
	asset := f.complete(upload).Asset
	tags := map[string]bool{}
	for size, edge := range map[string]int{"sm": 256, "md": 512, "lg": 1024} {
		r = f.request("GET", "/assets/"+asset.ID+"/thumbnail?version=1&size="+size, "", testSecret, nil, nil)
		if r.Code != 200 {
			t.Fatalf("size %s: %s", size, r.Body.String())
		}
		cfg, e := jpeg.DecodeConfig(bytes.NewReader(r.Body.Bytes()))
		if e != nil || cfg.Width != edge || cfg.Height != edge/2 {
			t.Fatalf("size %s dimensions: %+v %v", size, cfg, e)
		}
		etag := r.Header().Get("ETag")
		if tags[etag] || etag == "" {
			t.Fatal("different thumbnail bytes shared a strong ETag")
		}
		tags[etag] = true
		cached := f.request("GET", "/assets/"+asset.ID+"/thumbnail?version=1&size="+size, "", testSecret, nil, map[string]string{"If-None-Match": etag})
		if cached.Code != 304 {
			t.Fatalf("same representation did not revalidate: %d", cached.Code)
		}
	}
	f.json("GET", "/assets/"+asset.ID+"/thumbnail?size=huge", "", nil, 400)
	original := f.request("GET", "/assets/"+asset.ID+"/content", "", testSecret, nil, nil)
	if original.Code != 200 || !bytes.Equal(original.Body.Bytes(), body) {
		t.Fatal("preview generation modified the original")
	}
}
