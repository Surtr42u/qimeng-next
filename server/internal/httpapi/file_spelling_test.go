package httpapi_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"qimeng.local/next/server/internal/model"
	"qimeng.local/next/server/internal/service"
)

func TestCaseOnlyRenamePreservesOriginalAndReplaysWithoutAnotherFile(t *testing.T) {
	f := setup(t)
	upload, original := f.upload("example.png", 197, nil)
	asset := *f.complete(upload).Asset
	body := map[string]any{"relativePath": "Example.png", "expectedRevision": asset.Revision}
	for i := 0; i < 2; i++ {
		var moved model.Asset
		decode(t, f.json("POST", "/assets/"+asset.ID+"/move", "rename-letter-case", body, 200), &moved)
		if moved.ID != asset.ID || moved.RelativePath != "Example.png" || moved.ContentVersion != asset.ContentVersion {
			t.Fatal("changing case changed the asset identity, spelling or content version")
		}
	}
	root, err := f.s.Root.LibraryPath(f.library)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		count++
		if entry.Name() != "Example.png" {
			t.Fatal("original directory entry was left at its old spelling")
		}
	}
	if count != 1 {
		t.Fatalf("rename left %d originals", count)
	}
	content := f.request("GET", "/assets/"+asset.ID+"/content", "", testSecret, nil, nil)
	if content.Code != 200 || !bytes.Equal(content.Body.Bytes(), original) {
		t.Fatal("case-only cleanup removed the original")
	}
}

func TestExplicitMoveRejectsExistingHardLinkBeforeRecordingIntent(t *testing.T) {
	f := setup(t)
	upload, original := f.upload("held.png", 198, nil)
	asset := *f.complete(upload).Asset
	root, err := f.s.Root.LibraryPath(f.library)
	if err != nil {
		t.Fatal(err)
	}
	occupied := filepath.Join(root, "already-present.png")
	if err := os.Link(filepath.Join(root, "held.png"), occupied); err != nil {
		t.Fatal(err)
	}
	f.json("POST", "/assets/"+asset.ID+"/move", "do-not-adopt-another-link", map[string]any{"relativePath": "already-present.png", "expectedRevision": asset.Revision}, 409)
	for _, name := range []string{"held.png", "already-present.png"} {
		actual, err := os.ReadFile(filepath.Join(root, name))
		if err != nil || !bytes.Equal(actual, original) {
			t.Fatal("conflicting move changed an existing directory entry")
		}
	}
	var pending int
	if err := f.s.DB.DB.QueryRow("SELECT count(*) FROM file_intents WHERE state='prepared'").Scan(&pending); err != nil || pending != 0 {
		t.Fatal("known conflict left a startup recovery intent")
	}
}

func TestCaseRenameResumesAfterOldEntryWasRemoved(t *testing.T) {
	f := setup(t)
	u, data := f.upload("original.png", 199, nil)
	before := *f.complete(u).Asset
	after := before
	after.RelativePath = "Original.png"
	after.FileName = "Original.png"
	after.Revision++
	root, _ := f.s.Root.LibraryPath(f.library)
	source := filepath.Join(root, before.FileName)
	if _, err := os.Stat(filepath.Join(root, after.FileName)); errors.Is(err, os.ErrNotExist) {
		t.Skip("requires a case-insensitive filesystem")
	}
	key := "synthetic-spelling-hold"
	hold := filepath.Join(root, ".qimeng-internal", "rename-holds", key)
	if err := os.MkdirAll(filepath.Dir(hold), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(source, hold); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(source); err != nil {
		t.Fatal(err)
	}
	record := map[string]any{"ID": key, "Kind": "move", "Source": "libraries/" + f.library + "/" + before.FileName, "Target": "libraries/" + f.library + "/" + after.FileName, "Before": before, "After": after, "State": "prepared", "spellingOnly": true}
	raw, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.DB.DB.Exec("INSERT INTO file_intents(id,kind,state,data) VALUES(?,?,?,?)", key, "move", "prepared", string(raw)); err != nil {
		t.Fatal(err)
	}
	config := f.s.Config
	if err := f.s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := service.New(config)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	actual, err := os.ReadFile(filepath.Join(root, after.FileName))
	if err != nil || !bytes.Equal(actual, data) {
		t.Fatal("recovery failed to publish the only held original")
	}
	if _, err = os.Stat(hold); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("committed rename hold was not released")
	}
}
