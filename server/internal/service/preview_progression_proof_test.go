package service

import (
	"bytes"
	"image"
	"image/jpeg"
	"os"
	"testing"
)

func TestProgressiveOrdinaryChromaAndRestartIntervals(t *testing.T) {
	for _, name := range []string{"progressive-420.jpg", "progressive-gray.jpg", "separate-dc.jpg"} {
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile("testdata/" + name)
			if err != nil {
				t.Fatal(err)
			}
			if bytes.Count(data, []byte{0xff, 0xd0}) < 2 {
				t.Fatal("fixture does not contain repeated restart markers")
			}
			original, err := jpeg.Decode(bytes.NewReader(data))
			if err != nil {
				t.Fatal(err)
			}
			cfg := image.Config{Width: original.Bounds().Dx(), Height: original.Bounds().Dy()}
			small, err := progressivePreview(t.Context(), bytes.NewReader(data), cfg)
			if err != nil {
				t.Fatal(err)
			}
			for _, p := range []image.Point{{X: 3, Y: 2}, {X: 26, Y: 2}, {X: 3, Y: 19}, {X: 26, Y: 19}} {
				r, g, b, _ := original.At(8*p.X+4, 8*p.Y+4).RGBA()
				checkPreviewColor(t, small.At(p.X, p.Y), [3]int{int(r >> 8), int(g >> 8), int(b >> 8)})
			}
		})
	}
}
