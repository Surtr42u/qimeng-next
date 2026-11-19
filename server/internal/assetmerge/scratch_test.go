package assetmerge

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func scratchFixture(t *testing.T) (string, *operation) {
	t.Helper()
	base := t.TempDir()
	a, err := openArea(base)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.root.Close() })
	s, err := newScratch(a)
	if err != nil {
		t.Fatal(err)
	}
	op := &operation{area: a, scratch: s}
	f, err := s.create(projectedName)
	if err != nil {
		t.Fatal(err)
	}
	_, writeErr := f.WriteString("synthetic disposable catalog")
	if err := errors.Join(writeErr, f.Close()); err != nil {
		t.Fatal(err)
	}
	if err := s.reserveSidecars(); err != nil {
		t.Fatal(err)
	}
	return s.dir.name, op
}

func assertPrivateScratchFailure(t *testing.T, op *operation) {
	t.Helper()
	prior := errors.New("simulated earlier refusal")
	receipt, err := op.finishScratch(Receipt{State: "checked"}, prior)
	if receipt.State != "" || FailurePhase(err) != "private_scratch_cleanup_required" || !errors.Is(err, prior) {
		t.Fatalf("unexpected scratch failure receipt: %q, %v", receipt.State, err)
	}
}

func TestScratchPreservesUnexpectedOrReplacedFiles(t *testing.T) {
	for _, leaf := range []string{"unrelated.txt", projectedName, projectedName + "-wal", projectedName + "-shm", projectedName + "-journal"} {
		t.Run(leaf, func(t *testing.T) {
			dir, op := scratchFixture(t)
			name := filepath.Join(dir, leaf)
			if leaf != "unrelated.txt" {
				if err := os.Rename(name, filepath.Join(op.name, "moved-owned-file")); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(name, []byte("synthetic other owner"), 0600); err != nil {
				t.Fatal(err)
			}
			assertPrivateScratchFailure(t, op)
			original, err := os.ReadFile(name)
			if err != nil || string(original) != "synthetic other owner" {
				t.Fatalf("other owner's file was changed: %v", err)
			}
		})
	}
}

func TestScratchPreservesReassignedDirectoryName(t *testing.T) {
	dir, op := scratchFixture(t)
	moved := filepath.Join(op.name, "moved-owned-directory")
	if err := os.Rename(dir, moved); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	reused := filepath.Join(dir, projectedName)
	if err := os.WriteFile(reused, []byte("replacement directory"), 0600); err != nil {
		t.Fatal(err)
	}
	assertPrivateScratchFailure(t, op)
	for name, want := range map[string]string{reused: "replacement directory", filepath.Join(moved, projectedName): "synthetic disposable catalog"} {
		b, err := os.ReadFile(name)
		if err != nil || string(b) != want {
			t.Fatalf("directory contents changed: %v", err)
		}
	}
}

func TestScratchReportsRemovalRefusedByFilesystem(t *testing.T) {
	dir, op := scratchFixture(t)
	if runtime.GOOS == "windows" {
		// Standard os.Open on Windows does not share delete access.
		f, err := os.Open(filepath.Join(dir, projectedName))
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
	} else {
		if os.Geteuid() == 0 {
			if err := op.clearScratch(); err != nil {
				t.Fatal(err)
			}
			t.Skip("root bypasses directory mode")
		}
		if err := os.Chmod(op.name, 0500); err != nil {
			t.Fatal(err)
		}
		defer os.Chmod(op.name, 0700)
	}
	receipt, err := op.finishScratch(Receipt{State: "checked"}, nil)
	if receipt.State != "" || FailurePhase(err) != "private_scratch_cleanup_required" {
		t.Fatalf("filesystem refusal reported as %q, %v", receipt.State, err)
	}
	// A sharing violation may occur while claiming the scratch directory or
	// its file. The nonempty private copy remains in either case.
	matches, err := filepath.Glob(filepath.Join(op.name, ".qimeng-merge-*", "*"))
	if err != nil || len(matches) == 0 {
		t.Fatalf("filesystem refusal did not retain a private copy: %v", err)
	}
}

func TestScratchConsumesOnlyOwnedFiles(t *testing.T) {
	dir, op := scratchFixture(t)
	if err := os.Remove(filepath.Join(dir, projectedName+"-wal")); err != nil {
		t.Fatal(err)
	}
	if err := op.clearScratch(); err != nil {
		t.Fatal(err)
	}
	items, err := os.ReadDir(op.name)
	if err != nil || len(items) != 0 {
		t.Fatalf("private catalog cleanup left %d items: %v", len(items), err)
	}
}
