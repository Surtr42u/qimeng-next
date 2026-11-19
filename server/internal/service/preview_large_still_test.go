package service

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestLargePNGRejectsFullFrameFallbackAndHonorsAlpha(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 80, 40))
	for y := 0; y < src.Rect.Dy(); y++ {
		for x := 0; x < src.Rect.Dx(); x++ {
			src.SetNRGBA(x, y, color.NRGBA{R: 224, G: 40, B: 12, A: byte(x/40) * 128})
		}
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, src); err != nil {
		t.Fatal(err)
	}
	cfg := image.Config{Width: 80, Height: 40}
	img, err := decodeLargeStill(t.Context(), bytes.NewReader(encoded.Bytes()), int64(encoded.Len()), "png", cfg, 20, 10)
	if err != nil || img.Bounds().Size() != image.Pt(20, 10) {
		t.Fatal("row-scaled PNG", err)
	}
	checkPreviewColor(t, img.At(3, 3), [3]int{0, 0, 0})
	checkPreviewColor(t, img.At(17, 3), [3]int{112, 20, 6})

	// A 1x1 image occupies exactly the first Adam7 pass. Its scanline data
	// therefore also makes a valid interlaced fixture with a new IHDR CRC.
	encoded.Reset()
	if err := png.Encode(&encoded, image.NewGray(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	data := encoded.Bytes()
	data[28] = 1
	binary.BigEndian.PutUint32(data[29:33], crc32.ChecksumIEEE(data[12:29]))
	if _, err = png.Decode(bytes.NewReader(data)); err != nil {
		t.Fatal("synthetic Adam7 source must open normally", err)
	}
	for _, tc := range []struct {
		config image.Config
		size   int64
	}{
		{image.Config{Width: 1, Height: 1}, int64(len(data))},
		{image.Config{Width: largeStillSide + 1, Height: 1}, 32},
		{image.Config{Width: largeStillSide, Height: largeStillSide}, 32},
		{image.Config{Width: 1, Height: 1}, largeStillBytes + 1},
	} {
		_, err := decodeLargeStill(t.Context(), bytes.NewReader(data), tc.size, "png", tc.config, 1, 1)
		var problem *Error
		if !errors.As(err, &problem) || problem.Code != "thumbnail_resource_limit" {
			t.Fatal("large still fallback budget", err)
		}
	}
}

func checkPreviewColor(t *testing.T, c color.Color, expected [3]int) {
	t.Helper()
	r, g, b, _ := c.RGBA()
	for i, got := range []int{int(r >> 8), int(g >> 8), int(b >> 8)} {
		if got < expected[i]-8 || got > expected[i]+8 {
			t.Fatalf("decoded color %v differs from %v", c, expected)
		}
	}
}

// The cloud proof generates fresh, valid 48MP PNG, 112MP progressive JPEG
// and 200MP sequential JPEG with the independent Pillow/libjpeg encoder.
func TestLargeStillThumbnailService(t *testing.T) {
	if os.Getenv("QIMENG_TEST_LARGE_STILLS") != "1" {
		t.Skip("set QIMENG_TEST_LARGE_STILLS=1 with Python and Pillow to run the large media integration")
	}
	dir := t.TempDir()
	python, err := exec.LookPath("python3")
	if err != nil {
		python, err = exec.LookPath("python")
		if err != nil {
			t.Fatal(err)
		}
	}
	if output, err := exec.CommandContext(t.Context(), python, "testdata/large_stills.py", dir).CombinedOutput(); err != nil {
		t.Fatalf("generating synthetic stills: %s: %v", output, err)
	}
	for _, tc := range []struct {
		name          string
		width, height int
		rotated       bool
	}{
		{"patches.png", 8003, 6001, false},
		{"sequential.jpg", 16001, 12503, true},
		{"progressive.jpg", 12005, 9377, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := t.Context()
			data, err := os.ReadFile(filepath.Join(dir, tc.name))
			if err != nil {
				t.Fatal(err)
			}
			s, run, root := preparedScanFixture(t)
			name := filepath.Join(root, tc.name)
			if err = os.WriteFile(name, data, 0600); err != nil {
				t.Fatal(err)
			}
			initial, err := os.Stat(name)
			if err != nil {
				t.Fatal(err)
			}
			if err = s.scanFile(ctx, run, tc.name, scanSignature{}); err != nil {
				t.Fatal(err)
			}
			if err = s.flushPreparedScans(ctx, run); err != nil {
				t.Fatal(err)
			}
			var id string
			if err = s.DB.DB.QueryRowContext(ctx, "SELECT id FROM assets WHERE library_id=? AND file_name=?", run.library.ID, tc.name).Scan(&id); err != nil {
				t.Fatal(err)
			}
			a, err := s.Asset(ctx, id)
			if err != nil || a.Hash != digest(data) || a.ContentVersion < 1 {
				t.Fatal("indexed source identity", err)
			}
			width, height := tc.width, tc.height
			if tc.rotated {
				width, height = height, width
			}
			if a.Width != width || a.Height != height {
				t.Fatal("probed dimensions", a.Width, a.Height)
			}
			cancelled, cancel := context.WithCancel(ctx)
			cancel()
			if err = s.generatePreview(cancelled, id, a.ContentVersion, 256); !errors.Is(err, context.Canceled) {
				t.Fatal("canceled decode", err)
			}
			if _, err = s.Root.Stat(previewPath(id, a.ContentVersion, 256, false)); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("canceled generation published a cache entry", err)
			}
			for i, size := range []string{"sm", "md", "lg"} {
				file, current, err := s.ThumbnailSize(ctx, id, a.ContentVersion, size)
				if err != nil {
					t.Fatal("thumbnail", size, err)
				}
				picture, err := jpeg.Decode(file)
				if err = errors.Join(err, file.Close()); err != nil {
					t.Fatal(err)
				}
				edge := 256 << i
				w, h := previewDimensions(width, height, edge)
				if current.ID != a.ID || current.ContentVersion != a.ContentVersion || picture.Bounds().Size() != image.Pt(w, h) {
					t.Fatal("thumbnail identity or bounds", picture.Bounds(), w, h)
				}
				want := [4][3]int{{216, 43, 42}, {25, 177, 80}, {38, 69, 215}, {235, 163, 41}}
				if tc.rotated {
					want = [4][3]int{want[2], want[0], want[3], want[1]}
				}
				if a.MimeType == "image/png" {
					want[2], want[3] = [3]int{}, [3]int{118, 82, 21}
				}
				for i, point := range []image.Point{{X: w / 8, Y: h / 8}, {X: w * 7 / 8, Y: h / 8}, {X: w / 8, Y: h * 7 / 8}, {X: w * 7 / 8, Y: h * 7 / 8}} {
					checkPreviewColor(t, picture.At(point.X, point.Y), want[i])
				}
				t.Logf("%s: source %dx%d -> thumbnail %dx%d", size, width, height, w, h)
			}
			original, facts, err := s.Content(ctx, id, a.ContentVersion)
			if err != nil {
				t.Fatal(err)
			}
			readBack, err := io.ReadAll(original)
			if err = errors.Join(err, original.Close()); err != nil {
				t.Fatal(err)
			}
			after, err := os.Stat(name)
			if err != nil || !os.SameFile(initial, after) || facts.ID != id || facts.Hash != a.Hash || !bytes.Equal(readBack, data) {
				t.Fatal("physical original, bytes or catalog identity changed", err)
			}
			_, err = s.researchFrame(ctx, mediaInput{name: assetPath(a), size: a.Size}, a.MimeType, tc.width, tc.height, 0)
			var problem *Error
			if !errors.As(err, &problem) || problem.Code != "research_image_limit" {
				t.Fatal("research input budget", err)
			}
		})
	}
}
