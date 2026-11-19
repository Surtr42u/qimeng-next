package service

import (
	"context"
	"errors"
	"io"
	"os"
	"testing"
)

func TestInheritedDecoderReadsTheSelectedObjectAfterPathReused(t *testing.T) {
	if !inheritedMediaFD {
		t.Skip("inherited descriptor platform")
	}
	s, err := New(Config{DataDir: t.TempDir(), AdminToken: "synthetic-cloud-descriptor-credential", ReserveBytes: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	source, shadow := "media-fixture/original.png", "media-fixture/should-not-be-created.png"
	body := syntheticPNG(t, 132)
	write := func(data []byte) {
		t.Helper()
		file, e := s.Root.OpenFile(source, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = file.Write(data); e != nil {
			t.Fatal(e)
		}
		if e = file.Close(); e != nil {
			t.Fatal(e)
		}
	}
	write(body)
	input, err := s.pinMedia(source, shadow, int64(len(body)))
	if err != nil {
		t.Fatal(err)
	}
	defer input.release(s)
	if _, err = s.Root.Stat(shadow); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("descriptor pin created a shadow file")
	}
	if err = s.Root.Remove(source); err != nil {
		t.Fatal(err)
	}
	write([]byte("unrelated replacement at the original name"))
	if err = input.verify(context.Background(), s, digest(body), int64(len(body))); err != nil {
		t.Fatal(err)
	}
	if binary := toolPath("QIMENG_FFPROBE", "ffprobe"); binary != "" {
		probe, err := s.probeStreams(context.Background(), binary, "png_pipe", input)
		if err != nil {
			t.Fatal(err)
		}
		video, ok := probe.primary()
		if !ok || video.Width != 2 || video.Height != 2 {
			t.Fatal("decoder followed the replacement at the original path")
		}
	}
}

func TestVideoDemuxerReadsThePinnedObjectWithoutMovingItsPosition(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "source-*.mkv")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	n, err := file.WriteString("\x00\x00\x00\x14ftypisom\x00\x00\x00\x00mp42")
	if err != nil {
		t.Fatal(err)
	}
	s := &Service{}
	input := mediaInput{file: file, size: int64(n)}
	demuxer, err := s.inputDemuxer(t.Context(), input, "video/x-matroska")
	if err != nil || demuxer != "mov" {
		t.Fatalf("detected %q from the selected descriptor: %v", demuxer, err)
	}
	offset, err := file.Seek(0, io.SeekCurrent)
	if err != nil || offset != int64(n) {
		t.Fatalf("media header read changed the descriptor position to %d: %v", offset, err)
	}
}

func TestVideoDemuxerRetainsTheExistingFormatBoundary(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "source-*.mov")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	n, err := file.WriteString("\x00\x00\x00\x08wide")
	if err != nil {
		t.Fatal(err)
	}
	s := &Service{}
	input := mediaInput{file: file, size: int64(n)}
	for mime, expected := range map[string]string{"video/quicktime": "mov", "image/webp": "webp_pipe", "application/octet-stream": ""} {
		demuxer, err := s.inputDemuxer(t.Context(), input, mime)
		if err != nil || demuxer != expected {
			t.Fatalf("%s uses demuxer %q, want %q: %v", mime, demuxer, expected, err)
		}
	}
}
