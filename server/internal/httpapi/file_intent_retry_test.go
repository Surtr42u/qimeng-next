package httpapi_test

import (
	"bytes"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"qimeng.local/next/server/internal/model"
	"qimeng.local/next/server/internal/service"
)

func TestInterruptedMoveBindsOriginalInputUntilReceipt(t *testing.T) {
	f := setup(t)
	u, original := f.upload("original.png", 115, nil)
	asset := *f.complete(u).Asset
	route := "/assets/" + asset.ID + "/move"
	const key = "retry-interrupted-move"
	move := func(name string, status int) *httptest.ResponseRecorder {
		t.Helper()
		return f.json("POST", route, key, map[string]any{"expectedRevision": asset.Revision, "relativePath": name}, status)
	}
	db := f.s.DB.DB
	// Interrupt the real move after installation, before the catalog and its
	// receipt commit. Dropping the test trigger lets an authorized retry proceed.
	if _, err := db.Exec(`CREATE TRIGGER interrupt_move BEFORE UPDATE OF state ON file_intents
		WHEN OLD.kind='move' AND NEW.state='committing' BEGIN SELECT RAISE(ABORT,'interrupt move'); END`); err != nil {
		t.Fatal(err)
	}
	move("accepted.png", 500)
	if _, err := db.Exec("DROP TRIGGER interrupt_move"); err != nil {
		t.Fatal(err)
	}
	pending := func() string {
		t.Helper()
		var data string
		if err := db.QueryRow("SELECT data FROM file_intents WHERE kind='move' AND state='prepared'").Scan(&data); err != nil {
			t.Fatal(err)
		}
		return data
	}
	intent := pending()
	root, err := f.s.Root.LibraryPath(f.library)
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(root, asset.RelativePath)
	before, err := os.Stat(source)
	if err != nil {
		t.Fatal(err)
	}
	for _, changed := range []string{"other.png", asset.RelativePath} {
		var problem service.Error
		decode(t, move(changed, 409), &problem)
		if problem.Code != "idempotency_conflict" {
			t.Fatal("changed input did not conflict with the prepared operation", problem.Code)
		}
	}
	var current model.Asset
	decode(t, f.json("GET", "/assets/"+asset.ID, "", nil, 200), &current)
	if pending() != intent || current.ID != asset.ID || current.RelativePath != asset.RelativePath || current.Revision != asset.Revision || current.ContentVersion != asset.ContentVersion {
		t.Fatal("changed input modified the pending intent or its catalog identity")
	}
	after, err := os.Stat(source)
	if err != nil || !os.SameFile(before, after) {
		t.Fatal("changed input displaced the source object", err)
	}
	if raw, err := os.ReadFile(source); err != nil || !bytes.Equal(raw, original) {
		t.Fatal("changed input modified the original", err)
	}
	if _, err := os.Stat(filepath.Join(root, "other.png")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("rejected target was installed", err)
	}
	first := move("accepted.png", 200).Body.Bytes()
	if replay := move("accepted.png", 200).Body.Bytes(); !bytes.Equal(replay, first) {
		t.Fatal("the accepted move did not replay its original receipt")
	}
	decode(t, f.json("GET", "/assets/"+asset.ID, "", nil, 200), &current)
	if current.ID != asset.ID || current.Revision != asset.Revision+1 || current.ContentVersion != asset.ContentVersion || current.RelativePath != "accepted.png" {
		t.Fatal("retry did not complete the original move exactly once")
	}
	content := f.request("GET", "/assets/"+asset.ID+"/content", "", testSecret, nil, nil)
	if content.Code != 200 || !bytes.Equal(content.Body.Bytes(), original) {
		t.Fatal("moved asset lost its original bytes")
	}
	if _, err := os.Stat(source); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("completed move retained its old name", err)
	}
}
