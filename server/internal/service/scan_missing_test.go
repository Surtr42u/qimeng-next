package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"testing"

	"qimeng.local/next/server/internal/model"
	"qimeng.local/next/server/internal/store"
)

func existingScanOriginal(t *testing.T, s *Service, run *scanRun, root, relative string) model.Asset {
	t.Helper()
	name := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(name), 0700); err != nil {
		t.Fatal(err)
	}
	data := syntheticPNG(t, 23)
	if err := os.WriteFile(name, data, 0600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(name)
	if err != nil {
		t.Fatal(err)
	}
	mtime := info.ModTime().UTC()
	a := model.Asset{ID: ID(), LibraryID: run.library.ID, RelativePath: relative, FileName: path.Base(relative),
		MediaType: "image", MimeType: "image/png", Hash: digest(data), Size: info.Size(), ContentMtime: &mtime,
		AuthorID: "unknown", AuthorIDs: []string{"unknown"}, Note: "manual note", Tags: []string{"manual tag"},
		Revision: 7, ContentVersion: 3, CreatedAt: mtime, UpdatedAt: mtime}
	if err = store.SaveAsset(context.Background(), s.DB.DB, a); err != nil {
		t.Fatal(err)
	}
	return a
}

func TestScanMissingChecksUnwalkedSubtreesAndDeletionAfterTraversal(t *testing.T) {
	ctx := context.Background()
	s, run, root := preparedScanFixture(t)
	var rootAsset, nested model.Asset
	for i := range 6 {
		rootAsset = existingScanOriginal(t, s, run, root, fmt.Sprintf("root-%d.png", i))
		nested = existingScanOriginal(t, s, run, root, fmt.Sprintf("移出/子目录/%d.png", i))
	}
	// This directory is entirely absent from the full walk.
	if err := os.Rename(filepath.Join(root, "移出"), filepath.Join(t.TempDir(), "detached")); err != nil {
		t.Fatal(err)
	}
	if err := s.walkLibrary(ctx, run, "", 0); err != nil {
		t.Fatal(err)
	}
	// A file seen as unchanged during traversal disappears before reconciliation.
	if err := os.Remove(filepath.Join(root, rootAsset.RelativePath)); err != nil {
		t.Fatal(err)
	}
	if err := s.scanMissing(ctx, run.id, run.library.ID); err != nil {
		t.Fatal(err)
	}
	job, err := s.Scan(ctx, run.id)
	if err != nil || job.Missing != 7 || job.IssueCount != 7 || len(job.Issues) != 7 {
		t.Fatal("missing originals were lost or counted twice", job, err)
	}
	reported := map[string]bool{}
	for _, issue := range job.Issues {
		if issue.Code != "missing_original" || reported[issue.RelativePath] {
			t.Fatal(issue)
		}
		reported[issue.RelativePath] = true
	}
	for _, expected := range []model.Asset{rootAsset, nested} {
		a, err := s.Asset(ctx, expected.ID)
		if !reported[expected.RelativePath] || err != nil || a.Revision != 7 || a.ContentVersion != 3 || a.Trashed ||
			a.Note != "manual note" || len(a.Tags) != 1 || a.Tags[0] != "manual tag" {
			t.Fatal("missing original lost its identity or manual fields", expected.RelativePath, a, err)
		}
	}
}

func TestScanMissingRechecksMoveTrashAndRecreationUnderMutationLock(t *testing.T) {
	ctx := context.Background()
	s, run, root := preparedScanFixture(t)
	var assets []model.Asset
	for _, name := range []string{"move.png", "trash.png", "recreate.png", "missing.png"} {
		assets = append(assets, existingScanOriginal(t, s, run, root, name))
		if err := os.Remove(filepath.Join(root, name)); err != nil {
			t.Fatal(err)
		}
	}
	observed, err := s.scanDirectoryPage(ctx, run.library.ID, "", 0, math.MaxInt64)
	if err != nil || len(observed) != 4 {
		t.Fatal(observed, err)
	}
	assets[0].RelativePath, assets[0].FileName, assets[0].Note = "moved.png", "moved.png", "new note"
	assets[1].Trashed = true
	for _, a := range assets[:2] {
		if err = store.SaveAsset(ctx, s.DB.DB, a); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"moved.png", "recreate.png"} {
		if err = os.WriteFile(filepath.Join(root, name), syntheticPNG(t, 23), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err = s.confirmMissingPaths(ctx, run.id, run.library.ID, observed); err != nil {
		t.Fatal(err)
	}
	job, err := s.Scan(ctx, run.id)
	if err != nil || job.Missing != 1 || job.Processed != 1 || job.IssueCount != 1 || len(job.Issues) != 1 || job.Issues[0].RelativePath != "missing.png" {
		t.Fatal(job, err)
	}
	moved, err := s.Asset(ctx, assets[0].ID)
	if err != nil || moved.Note != "new note" || moved.RelativePath != "moved.png" {
		t.Fatal(moved, err)
	}
	testMutation(t, s, "cancel-missing-check", func(w *Write) (any, int, error) {
		result, err := s.CancelScan(ctx, w, run.id)
		return result, 200, err
	})
	if err = s.confirmMissingPaths(ctx, run.id, run.library.ID, observed); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled job continued recording issues", err)
	}
}

func TestScanMissingDirectoryReadErrorsAreNotMissingOriginals(t *testing.T) {
	ctx := context.Background()
	s, run, root := preparedScanFixture(t)
	for i := range 4 {
		existingScanOriginal(t, s, run, root, fmt.Sprintf("indexed/%d.png", i))
	}
	original := filepath.Join(root, "indexed")
	elsewhere := filepath.Join(t.TempDir(), "original")
	if err := os.Rename(original, elsewhere); err != nil {
		t.Fatal(err)
	}
	checkFailure := func() {
		t.Helper()
		if err := s.scanMissing(ctx, run.id, run.library.ID); err == nil || errors.Is(err, os.ErrNotExist) {
			t.Fatal("unsafe or unreadable directory accepted", err)
		}
		job, err := s.Scan(ctx, run.id)
		if err != nil || job.Missing != 0 || job.IssueCount != 0 {
			t.Fatal("access failure misreported as missing", job, err)
		}
	}
	if err := os.WriteFile(original, []byte("not a directory"), 0600); err != nil {
		t.Fatal(err)
	}
	checkFailure()
	if err := os.Remove(original); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(elsewhere, original); err != nil {
		if runtime.GOOS == "windows" {
			t.Log("directory symlink unavailable on this runner")
			return
		}
		t.Fatal(err)
	}
	checkFailure()
	if runtime.GOOS != "windows" && os.Geteuid() != 0 {
		if err := os.Remove(original); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(elsewhere, original); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(original, 0100); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := os.Chmod(original, 0700); err != nil {
				t.Error(err)
			}
		})
		checkFailure()
		// An owner may list names while being unable to resolve a child.
		if err := os.Chmod(original, 0400); err != nil {
			t.Fatal(err)
		}
		checkFailure()
	}
}
