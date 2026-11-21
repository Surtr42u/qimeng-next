package assetmerge

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"qimeng.local/next/server/internal/backup"
	"qimeng.local/next/server/internal/service"
	"qimeng.local/next/server/internal/store"
)

func TestReserveFailureAfterClaimKeepsCatalogAndSelectedOriginal(t *testing.T) {
	ctx := t.Context()
	parent := t.TempDir()
	a, err := openArea(parent)
	if err != nil {
		t.Fatal(err)
	}
	defer a.root.Close()
	if err := a.mkdir("work"); err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(filepath.Join(parent, "work", "qimeng.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.DB.Close(); err != nil {
		t.Fatal(err)
	}
	put := func(name string, data []byte) string {
		t.Helper()
		if err := os.WriteFile(filepath.Join(parent, name), data, 0600); err != nil {
			t.Fatal(err)
		}
		h := sha256.Sum256(data)
		return hex.EncodeToString(h[:])
	}
	privatePlan := put("owner.json", []byte("{}"))
	put("archive.qnext", []byte("synthetic archive stand-in after the archive verification boundary"))
	signed := service.RestoreResult{InstanceID: "fixture-restore", State: "restored"}
	raw, err := json.Marshal(signed)
	if err != nil {
		t.Fatal(err)
	}
	put("receipt.json", raw)
	put("work/selection.png", []byte("synthetic bytes for the failure path"))
	oldFile, err := a.readFile(ctx, "work/selection.png", nil)
	if err != nil {
		t.Fatal(err)
	}
	oldCatalog, err := a.readFile(ctx, "work/qimeng.sqlite", nil)
	if err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(parent, "archive.qnext")
	archiveInfo, err := os.Stat(archive)
	if err != nil {
		t.Fatal(err)
	}
	stageInfo, err := a.inspect("work")
	if err != nil {
		t.Fatal(err)
	}
	op := operation{area: a, opts: Options{Plan: filepath.Join(parent, "owner.json"), RestoreReceipt: filepath.Join(parent, "receipt.json"),
		Archive: archive, ReserveBytes: 1 << 62}, req: request{Stage: "work", Preimage: "preimage", Destination: "final", PlanHash: privatePlan, Restore: signed},
		archiveInfo: archiveInfo, moves: []relocation{{Before: "selection.png", Copy: "originals/00000001", Stamp: oldFile}}}
	if err := op.apply(ctx, stageInfo); FailurePhase(err) != "preserve_selected_preimages" {
		t.Fatalf("wanted a private preimage failure, got %v", err)
	}
	if _, err := a.inspect("work/RESTORE_INCOMPLETE"); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"final", "preimage/ready.json"} {
		if _, err := a.inspect(name); !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("unexpected publication during disk failure: %v", err)
		}
	}
	check := func(name string, expected stamp) {
		current, err := a.readFile(ctx, name, nil)
		if err != nil || !expected.matches(current) {
			t.Fatalf("original changed: %v", err)
		}
	}
	check("work/selection.png", oldFile)
	check("work/qimeng.sqlite", oldCatalog)
}

func TestMetadataArchiveVerifiesLiveOriginals(t *testing.T) {
	data := []byte("synthetic original bytes")
	digest := sha256.Sum256(data)
	parent := t.TempDir()
	a, err := openArea(parent)
	if err != nil {
		t.Fatal(err)
	}
	defer a.root.Close()
	if err = a.mkdir("stage"); err != nil {
		t.Fatal(err)
	}
	if err = a.mkdirAll("stage/libraries/lib"); err != nil {
		t.Fatal(err)
	}
	name := filepath.Join(parent, "stage", "libraries", "lib", "a.png")
	if err = os.WriteFile(name, data, 0600); err != nil {
		t.Fatal(err)
	}
	c := catalog{files: map[string]payload{"asset/a": {
		File: backup.File{Kind: "asset", ObjectID: "a", ContentVersion: 1,
			StoragePath: "libraries/lib/a.png", SHA256: hex.EncodeToString(digest[:]), Size: int64(len(data))},
		Physical: "libraries/lib/a.png",
	}}, dirs: map[string]bool{"libraries": true, "libraries/lib": true}}
	manifest := backup.Manifest{Scope: backup.ScopeMetadata}
	files, err := a.verifyBaseline(context.Background(), "stage", manifest, c)
	if err != nil || len(files) != 1 {
		t.Fatalf("records-only baseline did not verify the live original: %v %d", err, len(files))
	}
	damaged := append([]byte(nil), data...)
	damaged[0] ^= 1
	if err = os.WriteFile(name, damaged, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = a.verifyBaseline(context.Background(), "stage", manifest, c); err == nil {
		t.Fatal("records-only baseline accepted a damaged original")
	}
}
