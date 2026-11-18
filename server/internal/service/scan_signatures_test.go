package service

import (
	"context"
	"testing"
	"time"

	"qimeng.local/next/server/internal/store"
)

func TestScanSignaturesRetainPreciseTimeHashAndCOSIdentity(t *testing.T) {
	ctx := context.Background()
	s, run, root := preparedScanFixture(t)
	known := existingScanOriginal(t, s, run, root, "合集/作者/新图.png")
	stamp := time.Date(2026, 2, 13, 14, 15, 16, 123456789, time.FixedZone("fixture", 8*60*60))
	known.ContentMtime = &stamp
	known.COSDirectoryAuthorID, known.CosWorkID = "fixture-author", "fixture-work"
	if err := store.SaveAsset(ctx, s.DB.DB, known); err != nil {
		t.Fatal(err)
	}
	legacy := existingScanOriginal(t, s, run, root, "missing-time.png")
	legacy.ContentMtime = nil
	if err := store.SaveAsset(ctx, s.DB.DB, legacy); err != nil {
		t.Fatal(err)
	}
	trashed := existingScanOriginal(t, s, run, root, "trashed.png")
	trashed.Trashed = true
	if err := store.SaveAsset(ctx, s.DB.DB, trashed); err != nil {
		t.Fatal(err)
	}
	rows, err := s.scanSignatures(ctx, run.library.ID, []string{legacy.RelativePath, known.RelativePath, known.RelativePath, trashed.RelativePath, "absent.png"})
	if err != nil || len(rows) != 2 {
		t.Fatal(rows, err)
	}
	a := rows[known.RelativePath]
	if a.RelativePath != known.RelativePath || a.Mtime == nil || !a.Mtime.Equal(stamp) || a.Hash != known.Hash ||
		a.Size != known.Size || a.DirectoryAuthor != known.COSDirectoryAuthorID || a.COSWork != known.CosWorkID {
		t.Fatal("known original cannot be matched by a scan or content write notice", a)
	}
	if a := rows[legacy.RelativePath]; a.RelativePath != legacy.RelativePath || a.Mtime != nil || a.DirectoryAuthor != "" || a.COSWork != "" {
		t.Fatal("legacy original incorrectly acquired a skip signature", a)
	}

	if _, err := s.DB.DB.ExecContext(ctx, "UPDATE assets SET data=json_set(data,'$.contentMtime',NULL,'$.cosDirectoryAuthorId',NULL) WHERE id=?", legacy.ID); err != nil {
		t.Fatal(err)
	}
	rows, err = s.scanSignatures(ctx, run.library.ID, []string{legacy.RelativePath})
	if a := rows[legacy.RelativePath]; err != nil || a.RelativePath != legacy.RelativePath || a.Mtime != nil || a.DirectoryAuthor != "" {
		t.Fatal("explicit null differs from absent metadata", a, err)
	}
}

func TestScanSignaturesRejectInvalidStoredMetadata(t *testing.T) {
	ctx := context.Background()
	s, run, root := preparedScanFixture(t)
	a := existingScanOriginal(t, s, run, root, "corrupt.png")
	for _, invalid := range []struct{ field, value string }{
		{"$.contentMtime", `"not a timestamp"`},
		{"$.cosDirectoryAuthorId", `["unexpected"]`},
	} {
		if _, err := s.DB.DB.ExecContext(ctx, "UPDATE assets SET data=json_set(data,'$.contentMtime',NULL,?,json(?)) WHERE id=?", invalid.field, invalid.value, a.ID); err != nil {
			t.Fatal(err)
		}
		if rows, err := s.scanSignatures(ctx, run.library.ID, []string{a.RelativePath}); err == nil || rows != nil {
			t.Fatal("invalid metadata could suppress a scan", rows, err)
		}
	}
}
