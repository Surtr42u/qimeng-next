package service

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestProgressivePreviewMatchesBlockColors(t *testing.T) {
	// This small, independently encoded 4:4:4 fixture is the "small" output
	// of testdata/large_stills.py. Compare all complete blocks, including the
	// colored ellipses, against full-resolution standard-library decoding.
	data, err := os.ReadFile("testdata/progressive-patches.jpg")
	if err != nil {
		t.Fatal(err)
	}
	original, err := jpeg.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	cfg := image.Config{Width: original.Bounds().Dx(), Height: original.Bounds().Dy()}
	preview, err := progressivePreview(t.Context(), bytes.NewReader(data), cfg)
	if err != nil || preview.Bounds().Size() != image.Pt((cfg.Width+7)/8, (cfg.Height+7)/8) {
		t.Fatal("progressive block grid", err)
	}
	var difference, samples int
	for by := 0; by < cfg.Height/8; by++ {
		for bx := 0; bx < cfg.Width/8; bx++ {
			var sums [3]int
			for dy := range 8 {
				for dx := range 8 {
					r, g, b, _ := original.At(8*bx+dx, 8*by+dy).RGBA()
					sums[0] += int(r >> 8)
					sums[1] += int(g >> 8)
					sums[2] += int(b >> 8)
				}
			}
			r, g, b, _ := preview.At(bx, by).RGBA()
			for i, v := range []int{int(r >> 8), int(g >> 8), int(b >> 8)} {
				delta := v - (sums[i]+32)/64
				difference += max(delta, -delta)
				samples++
			}
		}
	}
	// Rounding and clipping the individual full-resolution samples before
	// averaging can differ from converting the unrounded block means to RGB.
	if difference > 3*samples {
		t.Fatalf("mean channel difference from the full decode = %.2f", float64(difference)/float64(samples))
	}
}

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

type measuredStillRead struct {
	*bytes.Reader
	read        int
	cancelAfter int
	cancel      context.CancelFunc
}

func (r *measuredStillRead) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	r.read += n
	if r.cancel != nil && r.read >= r.cancelAfter {
		r.cancel()
	}
	return n, err
}

// Extended XMP stores one logical packet in numbered APP1 segments. The
// generator only adds a synthetic description to an existing valid JPEG.
func jpegWithExtendedDescription(jpegData []byte) []byte {
	xmp := func(attributes, body string) string {
		return `<x:xmpmeta xmlns:x="adobe:ns:meta/"><rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#"><rdf:Description rdf:about="" ` + attributes + `>` + body + `</rdf:Description></rdf:RDF></x:xmpmeta>`
	}
	description := strings.Repeat("Synthetic catalog description. ", 75_000)
	extended := xmp(`xmlns:dc="http://purl.org/dc/elements/1.1/"`, `<dc:description><rdf:Alt><rdf:li xml:lang="x-default">`+description+`</rdf:li></rdf:Alt></dc:description>`)
	id := fmt.Sprintf("%X", md5.Sum([]byte(extended)))
	primary := xmp(`xmlns:xmpNote="http://ns.adobe.com/xmp/note/" xmpNote:HasExtendedXMP="`+id+`"`, "")
	var result bytes.Buffer
	result.Write(jpegData[:2])
	appendAPP1 := func(payload []byte) {
		result.Write([]byte{0xff, 0xe1})
		result.Write(binary.BigEndian.AppendUint16(nil, uint16(len(payload)+2)))
		result.Write(payload)
	}
	appendAPP1([]byte("http://ns.adobe.com/xap/1.0/\x00" + primary))
	for offset := 0; offset < len(extended); offset += 60_000 {
		payload := []byte("http://ns.adobe.com/xmp/extension/\x00" + id)
		payload = binary.BigEndian.AppendUint32(payload, uint32(len(extended)))
		payload = binary.BigEndian.AppendUint32(payload, uint32(offset))
		appendAPP1(append(payload, extended[offset:min(len(extended), offset+60_000)]...))
	}
	result.Write(jpegData[2:])
	return result.Bytes()
}

func TestLargeStillLimitsTheFirstPhysicalHeaderRead(t *testing.T) {
	data, err := os.ReadFile("testdata/progressive-patches.jpg")
	if err != nil {
		t.Fatal(err)
	}
	data = jpegWithExtendedDescription(data)
	info, err := jpeg.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		t.Fatal("standard decoder could not read the synthetic extended XMP", err)
	}
	large := image.Pt(8000, 6000)
	for _, declared := range []int64{int64(len(data)), largeStillBytes + 1} {
		read := &measuredStillRead{Reader: bytes.NewReader(data)}
		_, err = renderStill(t.Context(), read, declared, "image/jpeg", large, 256, displayStill)
		var problem *Error
		want := largeStillHeader
		if declared > largeStillBytes {
			want = 0
		}
		if !errors.As(err, &problem) || problem.Code != "thumbnail_resource_limit" || read.read != want {
			t.Fatalf("large header read %d bytes, expected %d: %v", read.read, want, err)
		}
	}
	result, err := renderStill(t.Context(), bytes.NewReader(data), int64(len(data)), "image/jpeg", image.Pt(info.Width, info.Height), 256, displayStill)
	if err != nil {
		t.Fatal("small original with extended XMP", err)
	}
	if _, err = jpeg.Decode(bytes.NewReader(result)); err != nil {
		t.Fatal(err)
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
			if tc.rotated {
				largeXMP := jpegWithExtendedDescription(data)
				read := &measuredStillRead{Reader: bytes.NewReader(largeXMP)}
				_, err = renderStill(ctx, read, int64(len(largeXMP)), a.MimeType, image.Pt(width, height), 256, displayStill)
				var limit *Error
				if !errors.As(err, &limit) || limit.Code != "thumbnail_resource_limit" || read.read != largeStillHeader {
					t.Fatal("first probe of large JPEG with extended XMP", read.read, err)
				}
			}
			midRead, stop := context.WithCancel(ctx)
			measured := &measuredStillRead{Reader: bytes.NewReader(data), cancelAfter: 16 << 10, cancel: stop}
			_, err = renderStill(midRead, measured, int64(len(data)), a.MimeType, image.Pt(width, height), 512, displayStill)
			stop()
			if !errors.Is(err, context.Canceled) || measured.read >= len(data) {
				t.Fatal("decode did not stop while consuming the original", measured.read, len(data), err)
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
				var memoryBefore, memoryAfter runtime.MemStats
				runtime.ReadMemStats(&memoryBefore)
				file, current, err := s.ThumbnailSize(ctx, id, a.ContentVersion, size)
				runtime.ReadMemStats(&memoryAfter)
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
				t.Logf("%s: source %dx%d -> thumbnail %dx%d, Go allocated %d bytes during the service call", size, width, height, w, h, memoryAfter.TotalAlloc-memoryBefore.TotalAlloc)
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
