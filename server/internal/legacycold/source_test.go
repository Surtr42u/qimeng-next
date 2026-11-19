package legacycold

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"qimeng.local/next/server/internal/backup"
)

func TestArchiveRejectsSameLengthDifferentBytesWithPreservedTimestamp(t *testing.T) {
	ctx := context.Background()
	source := t.TempDir()
	scratchDir := t.TempDir()
	input := filepath.Join(source, "fixture.png")
	if err := os.WriteFile(input, []byte("first"), 0600); err != nil {
		t.Fatal(err)
	}
	roots := make([]*readRoot, 0, 2)
	for _, name := range []string{source, scratchDir} {
		root, err := openDirectory(name)
		if err != nil {
			t.Fatal(err)
		}
		defer root.root.Close()
		roots = append(roots, root)
	}
	ref, hash, err := sealFile(ctx, roots[0], "fixture.png")
	if err != nil {
		t.Fatal(err)
	}
	if err = writeExclusive(roots[1].root, "snapshot", []byte("synthetic snapshot")); err != nil {
		t.Fatal(err)
	}
	snapshot, snapshotHash, err := sealFile(ctx, roots[1], "snapshot")
	if err != nil {
		t.Fatal(err)
	}
	s := newMediaSet(&Report{})
	entry := backup.File{Kind: "asset", ObjectID: "first", ContentVersion: 1, StoragePath: "libraries/l/fixture.png", SHA256: hash, Size: 5}
	s.files = []original{{entry, ref}}
	s.ids["first"] = 0
	s.report.OriginalBytes = 5
	m := backup.Manifest{Format: backup.Format, Version: backup.Version, InstanceID: "source", BackupID: "fixture", CreatedAt: time.Now().UTC(),
		Snapshot: backup.File{Kind: "snapshot", ObjectID: "snapshot", StoragePath: "qimeng.sqlite", Size: snapshot.info.Size(), SHA256: snapshotHash},
		Files:    []backup.File{entry}}
	if err = os.WriteFile(input, []byte("other"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.Chtimes(input, ref.info.ModTime(), ref.info.ModTime()); err != nil {
		t.Fatal(err)
	}
	if err = ref.check(); err != nil {
		t.Fatal("physical identity should still match before the second byte check", err)
	}
	_, err = writeArchive(ctx, Options{Output: scratchDir, ReserveBytes: 1}, roots[1], snapshot, m, s)
	var problem *issue
	if !errors.As(err, &problem) || problem.Code != "archive_copy_or_source_verification_failed" {
		t.Fatalf("changed bytes accepted or unrelated error: %v", err)
	}
}

func TestReadRootRejectsIdenticalReplacementAndLinkedOriginal(t *testing.T) {
	dir := t.TempDir()
	root, err := openDirectory(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.root.Close()
	if err = writeExclusive(root.root, "original", []byte("equal")); err != nil {
		t.Fatal(err)
	}
	ref, _, err := sealFile(context.Background(), root, "original")
	if err != nil {
		t.Fatal(err)
	}
	if err = root.root.Rename("original", "retained"); err != nil {
		t.Fatal(err)
	}
	if err = writeExclusive(root.root, "original", []byte("equal")); err != nil {
		t.Fatal(err)
	}
	if err = os.Chtimes(filepath.Join(dir, "original"), ref.info.ModTime(), ref.info.ModTime()); err != nil {
		t.Fatal(err)
	}
	if err = ref.check(); err == nil {
		t.Fatal("replaced source identity accepted")
	}
	if err = os.Symlink(filepath.Join(dir, "retained"), filepath.Join(dir, "alias")); err != nil {
		t.Skip("creating symbolic links requires Windows privileges")
	}
	if _, _, err = sealFile(context.Background(), root, "alias"); err == nil {
		t.Fatal("linked original accepted")
	}
}
