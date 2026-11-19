package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLegacySnapshotCopyAndWaitingReaderCanCancel(t *testing.T) {
	s, err := New(Config{DataDir: t.TempDir(), AdminToken: "synthetic-protection-read-owner-token", ReserveBytes: 1 << 62})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	target, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer target.Close()
	content := []byte("synthetic snapshot stream")
	p := BatchProtection{InstanceID: s.InstanceID, SnapshotHash: digest(content), SnapshotBytes: int64(len(content))}
	files := protectionFiles{service: s, proof: p, target: target}
	name := protectionSnapshot(p)
	if err = target.MkdirAll("instances/"+p.InstanceID+"/snapshots", 0700); err != nil {
		t.Fatal(err)
	}
	if err = target.WriteFile(name, content, 0600); err != nil {
		t.Fatal(err)
	}
	scratch := filepath.Join(s.Config.DataDir, "cache", "protection-read-"+p.InstanceID, "snapshot.sqlite")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	gate := &installedReadContext{Context: ctx, target: scratch, reading: make(chan struct{}), proceed: make(chan struct{})}
	done := make(chan error, 1)
	go func() {
		_, err := files.independentSnapshotVersions(gate)
		done <- err
	}()
	select {
	case <-gate.reading:
	case err := <-done:
		t.Fatal("copy ended before reading its independent source", err)
	case <-ctx.Done():
		t.Fatal("copy did not reach its private scratch file")
	}
	waiter, stop := context.WithCancel(context.Background())
	stop()
	if _, err = files.independentSnapshotVersions(waiter); !errors.Is(err, context.Canceled) {
		t.Fatalf("waiting read returned %v", err)
	}
	if _, err = os.Stat(scratch); err != nil {
		t.Fatalf("cancelled waiter removed the active reader's copy: %v", err)
	}
	cancel()
	if err = <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("active read returned %v", err)
	}
	if _, err = os.Lstat(scratch); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("cancelled read left its scratch file: %v", err)
	}
	_, err = files.independentSnapshotVersions(context.Background())
	var app *Error
	if !errors.As(err, &app) || app.Code != "insufficient_storage" {
		t.Fatalf("subsequent reader did not observe the configured reserve: %v", err)
	}
	if _, err = os.Lstat(scratch); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("reserve failure left a partial snapshot: %v", err)
	}
}

func TestProtectionScratchRetainsUnexpectedLink(t *testing.T) {
	dir := t.TempDir()
	original := filepath.Join(dir, "unrelated")
	if err := os.WriteFile(original, []byte("synthetic retained content"), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "snapshot.sqlite-wal")
	if err := os.Symlink("unrelated", link); err != nil {
		t.Skip("runner cannot create the synthetic link")
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	var app *Error
	if err = clearProtectionScratch(root); !errors.As(err, &app) || app.Code != "snapshot_scratch_occupied" {
		t.Fatalf("scratch cleanup accepted an unexpected link: %v", err)
	}
	if _, err = os.Readlink(link); err != nil {
		t.Fatal("scratch cleanup removed the link", err)
	}
	if data, err := os.ReadFile(original); err != nil || string(data) != "synthetic retained content" {
		t.Fatal("scratch cleanup changed an unrelated file", err)
	}
}
