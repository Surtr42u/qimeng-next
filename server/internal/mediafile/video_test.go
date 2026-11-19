package mediafile

import (
	"errors"
	"strings"
	"testing"
)

func TestVideoContainerSignatures(t *testing.T) {
	for _, fixture := range []struct {
		name string
		head string
		want VideoContainer
	}{
		{"MP4", "\x00\x00\x00\x14ftypisom\x00\x00\x00\x00mp42", VideoISOBaseMedia},
		{"QuickTime", "\x00\x00\x00\x14ftypqt  \x00\x00\x00\x00qt  ", VideoISOBaseMedia},
		{"Matroska", "\x1a\x45\xdf\xa3\x8f\x42\x86\x81\x01\x42\x82\x88matroska", VideoMatroska},
		{"WebM", "\x1a\x45\xdf\xa3\x87\x42\x82\x84webm", VideoMatroska},
		{"AVI", "RIFF\x24\x00\x00\x00AVI ", VideoAVI},
		{"old QuickTime without ftyp", "\x00\x00\x00\x08wide", ""},
		{"AVIF", "\x00\x00\x00\x14ftypavif\x00\x00\x00\x00mif1", ""},
		{"non-video RIFF", "RIFF\x24\x00\x00\x00WAVE", ""},
		{"other EBML document type", "\x1a\x45\xdf\xa3\x87\x42\x82\x84test", ""},
		{"bare EBML header", "\x1a\x45\xdf\xa3", ""},
		{"incomplete box", "\x00\x00\x00\x14ftypisom", ""},
		{"unrecognized bytes", "plain text", ""},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			if got := DetectVideoContainer([]byte(fixture.head)); got != fixture.want {
				t.Fatalf("detected %q, want %q", got, fixture.want)
			}
		})
	}
}

func TestMixedVideoContainerIsStillRejectedOnUpload(t *testing.T) {
	const mp4 = "\x00\x00\x00\x14ftypisom\x00\x00\x00\x00mp42"
	_, _, err := Inspect(t.Context(), strings.NewReader(mp4), "upload.mkv", "video/x-matroska")
	if !errors.Is(err, ErrContainer) {
		t.Fatalf("expected the upload container check to reject MP4 declared as Matroska, got %v", err)
	}
}
