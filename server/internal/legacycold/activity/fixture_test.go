package activity_test

import (
	"context"
	"crypto/sha256"
	"database/sql"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"qimeng.local/next/server/internal/legacycold/activity"
	"qimeng.local/next/server/internal/model"
	"qimeng.local/next/server/internal/store"
)

//go:embed testdata/activity-v26.sql
var legacySchema string

//go:embed testdata/activity-rows.sql
var legacyRows string

func syntheticSource(t *testing.T, change string) (*sql.DB, string) {
	t.Helper()
	file := filepath.Join(t.TempDir(), "synthetic.sqlite")
	write, err := store.OpenDatabase(file, store.CatalogDatabase)
	if err != nil {
		t.Fatal(err)
	}
	defer write.Close()
	for _, query := range []string{legacySchema, legacyRows, change} {
		if query != "" {
			if _, err := write.Exec(query); err != nil {
				t.Fatal(err)
			}
		}
	}
	day := time.Now().Format(time.DateOnly)
	if _, err := write.Exec("INSERT INTO daily_shown VALUES('clip-a',?,900),('clip-b',?,0)", day, day); err != nil {
		t.Fatal(err)
	}
	if err := write.Close(); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	read, err := store.OpenDatabase(file, store.SnapshotDatabase)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { read.Close() })
	return read, fmt.Sprintf("legacy-cold-v26:%x", sha256.Sum256(content))
}

func syntheticTarget(t *testing.T) (string, *sql.DB) {
	t.Helper()
	root := t.TempDir()
	const lib = "fixture-library"
	if err := os.MkdirAll(filepath.Join(root, "libraries", lib), 0700); err != nil {
		t.Fatal(err)
	}
	target, err := store.Open(filepath.Join(root, "qimeng.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	db := target.DB
	t.Cleanup(func() { db.Close() })
	created := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	library, err := store.Encode(model.Library{ID: lib, Name: "Synthetic", Kind: "regular", Enabled: true,
		StorageID: "data", RootRelativePath: "libraries/" + lib, ScanState: "idle", Revision: 1, CreatedAt: created})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("INSERT INTO libraries VALUES(?,'Synthetic','regular',?)", lib, library); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO authors(id,name,data) VALUES('fixture-author','Synthetic creator',
		'{"id":"fixture-author","name":"Synthetic creator","kind":"regular","revision":1}')`); err != nil {
		t.Fatal(err)
	}
	for i, id := range []string{"clip-a", "clip-b", "still"} {
		name, media, mime := "scene.mp4", "video", "video/mp4"
		if id == "still" {
			name, media, mime = "still.png", "image", "image/png"
		}
		a := model.Asset{ID: id, LibraryID: lib, FileName: name, OriginalFileName: name, RelativePath: id + "/" + name,
			MediaType: media, MimeType: mime, Hash: strings.Repeat(string("abc"[i]), 64), Size: int64(10 * (i + 1)),
			ContentVersion: 1, Revision: 1, AuthorID: "fixture-author", AuthorIDs: []string{"fixture-author"},
			Tags: []string{}, TagsRevision: 1, CreatedAt: created, UpdatedAt: created}
		if err := store.InsertAsset(t.Context(), db, a); err != nil {
			t.Fatal(err)
		}
	}
	return root, db
}

func sourceLocation(t *testing.T) *time.Location {
	t.Helper()
	zone, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	return zone
}

func transfer(ctx context.Context, source, target *sql.DB, id string, zone *time.Location) error {
	tx, err := target.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := activity.Import(ctx, source, tx, id, zone); err != nil {
		return err
	}
	return tx.Commit()
}

func checkValue[T comparable](t *testing.T, db *sql.DB, want T, query string, args ...any) {
	t.Helper()
	var got T
	if err := db.QueryRow(query, args...).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("%s: got %v, want %v", query, got, want)
	}
}
