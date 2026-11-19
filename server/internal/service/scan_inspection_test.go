package service

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/gif"
	"image/jpeg"
	"os"
	"path/filepath"
	"testing"

	"qimeng.local/next/server/internal/model"
	"qimeng.local/next/server/internal/store"
)

func TestScanBatchInspectsWholeNativeImagesAndJPEGOrientation(t *testing.T) {
	s, run, root := preparedScanFixture(t)
	run.targets.inventory = true
	frame := image.NewGray(image.Rect(0, 0, 8, 3))
	var jpegBody, gifBody bytes.Buffer
	if err := jpeg.Encode(&jpegBody, frame, nil); err != nil {
		t.Fatal(err)
	}
	if err := gif.Encode(&gifBody, frame, nil); err != nil {
		t.Fatal(err)
	}
	// A complete JPEG with an APP1 TIFF orientation of 6 after its SOI.
	oriented := []byte{255, 216, 255, 225, 0, 34, 'E', 'x', 'i', 'f', 0, 0, 'I', 'I', 42, 0,
		8, 0, 0, 0, 1, 0, 0x12, 1, 3, 0, 1, 0, 0, 0, 6, 0, 0, 0, 0, 0, 0, 0}
	oriented = append(oriented, jpegBody.Bytes()[2:]...)
	type sample struct {
		data          []byte
		width, height int
	}
	files := map[string]sample{
		"photo.jpg": {oriented, 3, 8},
		"frame.gif": {gifBody.Bytes(), 8, 3},
		"still.png": {syntheticPNG(t, 33), 2, 2},
	}
	for name, sample := range files {
		if err := os.WriteFile(filepath.Join(root, name), sample.data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	ctx := context.Background()
	if err := s.walkLibrary(ctx, run, "", 0); err != nil {
		t.Fatal(err)
	}
	if err := s.flushPreparedScans(ctx, run); err != nil {
		t.Fatal(err)
	}
	for name, sample := range files {
		asset, err := store.Get[model.Asset](ctx, s.DB.DB, "SELECT data FROM assets WHERE file_name=?", name)
		if err != nil || asset.Hash != digest(sample.data) || asset.Size != int64(len(sample.data)) ||
			asset.Width != sample.width || asset.Height != sample.height || asset.ProbeState != "ready" {
			t.Fatal("native image inspection differs", name, asset, err)
		}
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if err := s.scanFile(cancelled, run, "photo.jpg", scanSignature{}); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled read reported a media result", err)
	}
}
