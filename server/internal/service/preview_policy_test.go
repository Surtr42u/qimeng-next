package service

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestPreviewPolicyVersions(t *testing.T) {
	if got := PreviewPolicyVersion(); got != "v5" {
		t.Fatalf("PreviewPolicyVersion() = %q", got)
	}
	if got := previewPrefix(7, false); got != "v5-7-" {
		t.Fatalf("ordinary preview prefix = %q", got)
	}
	if got := previewPrefix(7, true); got != "v6-7-" {
		t.Fatalf("selected preview prefix = %q", got)
	}
	if got := previewPath("asset", 7, 512, false); got != "cache/asset/v5-7-512.jpg" {
		t.Fatalf("ordinary preview path = %q", got)
	}
	if got := previewPath("asset", 7, 512, true); got != "cache/asset/v6-7-512.jpg" {
		t.Fatalf("selected preview path = %q", got)
	}
}

func TestStillDecodeUsesFFmpegOnlyOnAndroidWithBinary(t *testing.T) {
	for _, tc := range []struct {
		name     string
		android  bool
		binary   string
		expected bool
	}{
		{"android with configured ffmpeg", true, "/data/app/libffmpeg.so", true},
		{"android without ffmpeg", true, "", false},
		{"desktop with ffmpeg keeps Go path", false, "/usr/bin/ffmpeg", false},
		{"desktop without ffmpeg", false, "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := stillDecodeUsesFFmpeg(tc.android, tc.binary); got != tc.expected {
				t.Fatalf("stillDecodeUsesFFmpeg(%v, %q) = %v", tc.android, tc.binary, got)
			}
		})
	}
}

func TestOrientationFilterChainMapsExifOrientation(t *testing.T) {
	// Mirrors the legacy generator: ffmpeg transpose directions are
	// 0=CCW+vertical flip, 1=CW, 2=CCW, 3=CW+vertical flip.
	for orientation, expected := range map[int]string{
		1: "",
		2: "hflip",
		3: "hflip,vflip",
		4: "vflip",
		5: "transpose=0",
		6: "transpose=1",
		7: "transpose=3",
		8: "transpose=2",
		0: "",
		9: "",
	} {
		if got := orientationFilterChain(orientation); got != expected {
			t.Fatalf("orientation %d filter = %q, want %q", orientation, got, expected)
		}
	}
}

func TestMobileLargeStillThreshold(t *testing.T) {
	for _, tc := range []struct {
		name          string
		width, height int
		size          int64
		expected      bool
	}{
		{"small still", 4000, 3000, 12 << 20, false},
		{"exact pixel threshold", 8000, 6250, 1, false},
		{"pixel threshold exceeded", 8000, 6251, 1, true},
		{"exact byte threshold", 1, 1, 128 << 20, false},
		{"byte threshold exceeded", 1, 1, 128<<20 + 1, true},
		{"unknown dimensions rely on size", 0, 0, 256 << 20, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := mobileLargeStill(tc.width, tc.height, tc.size); got != tc.expected {
				t.Fatalf("mobileLargeStill(%d, %d, %d) = %v", tc.width, tc.height, tc.size, got)
			}
		})
	}
}

func TestMobileLargeStillSemaphoreSerializesFullDecodes(t *testing.T) {
	var work previewWork
	work.init()
	release, err := work.acquireLargeStill(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	finished := make(chan struct{})
	go func() {
		second, err := work.acquireLargeStill(context.Background())
		if err != nil {
			t.Error(err)
			close(started)
			return
		}
		close(started)
		second()
		close(finished)
	}()
	select {
	case <-started:
		t.Fatal("a second large still entered while the first one was active")
	case <-time.After(50 * time.Millisecond):
	}
	release()
	select {
	case <-finished:
	case <-time.After(2 * time.Second):
		t.Fatal("waiting large still did not proceed after release")
	}

	releaseHeld, err := work.acquireLargeStill(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = work.acquireLargeStill(cancelled); !errors.Is(err, context.Canceled) {
		t.Fatalf("waiting acquire ignored cancellation: %v", err)
	}
	releaseHeld()
}
