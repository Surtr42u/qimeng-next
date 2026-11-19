package service

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"math"
	"os"
	"testing"

	"golang.org/x/image/draw"
	"qimeng.local/next/server/internal/model"
)

func TestSelectedImageReadPreviewUsesCheckedDecoderInput(t *testing.T) {
	s, err := New(Config{DataDir: t.TempDir(), AdminToken: "synthetic-selected-read-credential", ReserveBytes: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := t.Context()
	libResult := testMutation(t, s, "selected-read-library", func(w *Write) (any, int, error) {
		lib, err := s.CreateLibrary(ctx, w, "Synthetic", "regular")
		return lib, 201, err
	})
	var lib model.Library
	if err = json.Unmarshal(libResult.Body, &lib); err != nil {
		t.Fatal(err)
	}
	u := stagedUpload(t, s, lib.ID, 130, "fixture.png")
	upload := testMutation(t, s, "selected-read-commit", func(w *Write) (any, int, error) {
		result, err := s.CompleteUpload(ctx, w, u.ID)
		return result, 200, err
	})
	var result model.UploadResult
	if err = json.Unmarshal(upload.Body, &result); err != nil || result.Asset == nil {
		t.Fatal("missing fixture image", err)
	}
	a := *result.Asset
	old, _, err := s.Thumbnail(ctx, a.ID, a.ContentVersion)
	if err != nil {
		t.Fatal(err)
	}
	want, err := io.ReadAll(old)
	if err = errors.Join(err, old.Close()); err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB.DB.Exec(`INSERT INTO selected_image_merge_groups(representative_id,source_id,baseline_sha256,decision,applied_at)
		VALUES(?,'synthetic',?,'{}','2026-01-01T00:00:00Z')`, a.ID, a.Hash); err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB.DB.Exec(`INSERT INTO selected_image_merge_members(physical_asset_id,representative_id,original_asset,original_relations)
		VALUES(?,?,'{}','{}')`, a.ID, a.ID); err != nil {
		t.Fatal(err)
	}
	cached, _, _, source, err := s.thumbnailSource(ctx, a.ID, a.ContentVersion, 512, true)
	if cached != nil {
		cached.Close()
		t.Fatal("a cache created before the selection was accepted for a reviewed image")
	}
	if err != nil {
		t.Fatal(err)
	}
	defer source.release(s)
	var external []byte
	if toolPath("QIMENG_FFMPEG", "ffmpeg") != "" {
		external, err = s.externalPreview(ctx, mediaInput{name: assetPath(a)}, a, 512)
		if err != nil {
			t.Fatal(err)
		}
	}
	name, err := s.Root.AbsolutePath(assetPath(a))
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(name, syntheticPNG(t, 21), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := s.scaledStill(source, a.MimeType, 512, 92, draw.CatmullRom)
	if err != nil || !bytes.Equal(want, got) {
		t.Fatal("the builtin decoder lost its selected input", err)
	}
	if external != nil {
		got, err = s.externalPreview(ctx, source, a, 512)
		if err != nil || !bytes.Equal(external, got) {
			t.Fatal("the subprocess decoder lost its selected input", err)
		}
	}
	if err = source.release(s); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(name, syntheticPNG(t, 130), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.Chtimes(name, *a.ContentMtime, *a.ContentMtime); err != nil {
		t.Fatal(err)
	}
	if err = s.warmPreviews(ctx, previewScope{asset: a.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Root.Stat(previewPath(a.ID, a.ContentVersion, 512, false)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("obsolete preview survived selected cache maintenance", err)
	}
	ready, _, err := s.Thumbnail(ctx, a.ID, a.ContentVersion)
	if err != nil {
		t.Fatal(err)
	}
	got, err = io.ReadAll(ready)
	if err = errors.Join(err, ready.Close()); err != nil || !bytes.Equal(want, got) {
		t.Fatal("warmed selected preview changed", err)
	}

	t.Run("disk reserve", func(t *testing.T) {
		limited := &Service{Root: s.Root, Config: Config{DataDir: s.Config.DataDir, ReserveBytes: math.MaxInt64}}
		original, err := s.Root.Open(assetPath(a))
		if err != nil {
			t.Fatal(err)
		}
		defer original.Close()
		const scratch = "cache/selected-image-reads/reserve-test"
		file, err := limited.stageSelectedImage(t.Context(), original, a, scratch)
		var problem *Error
		if file != nil || !errors.As(err, &problem) || problem.Status != 507 {
			t.Fatal("disk reserve did not prevent the snapshot", err)
		}
		if _, err = s.Root.Stat(scratch); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("disk refusal left a scratch file", err)
		}
	})
	active, _, err := s.Content(ctx, a.ID, a.ContentVersion)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err = active.Read(make([]byte, 1)); !errors.Is(err, os.ErrClosed) {
		t.Fatal("service shutdown kept the response file open", err)
	}
	if err = active.Close(); err != nil {
		t.Fatal(err)
	}
}
