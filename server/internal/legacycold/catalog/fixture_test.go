package catalog

import (
	"database/sql"
	"path"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"qimeng.local/next/server/internal/model"
	"qimeng.local/next/server/internal/store"
)

const sampleTime = "2025-02-03T04:05:06.007Z"
const sampleSource = "synthetic-cold-source"

// This is the public v26 column projection consumed by the catalog, including
// nullable pre-provenance timestamps, opaque identities and per-set clocks.
const coldTables = `
CREATE TABLE libraries(id TEXT PRIMARY KEY,root_path TEXT NOT NULL,kind TEXT NOT NULL);
CREATE TABLE assets(asset_id TEXT PRIMARY KEY,library_id TEXT NOT NULL REFERENCES libraries(id),
 rel_path TEXT NOT NULL,file_name TEXT NOT NULL,source TEXT,cos_work TEXT,tag_set_updated_at TEXT NOT NULL DEFAULT '');
CREATE TABLE authors(id TEXT PRIMARY KEY,display_name TEXT NOT NULL,type TEXT NOT NULL,
 created_at TEXT NOT NULL,followed INTEGER NOT NULL,origin TEXT NOT NULL);
CREATE TABLE asset_authors(asset_id TEXT NOT NULL REFERENCES assets(asset_id),author_id TEXT NOT NULL REFERENCES authors(id),
 created_at TEXT,origin TEXT NOT NULL,PRIMARY KEY(asset_id,author_id));
CREATE TABLE asset_characters(asset_id TEXT NOT NULL REFERENCES assets(asset_id),character_name TEXT NOT NULL,
 created_at TEXT,origin TEXT NOT NULL,PRIMARY KEY(asset_id,character_name));
CREATE TABLE tags(id TEXT PRIMARY KEY,name TEXT NOT NULL UNIQUE,created_at TEXT NOT NULL);
CREATE TABLE asset_tags(asset_id TEXT NOT NULL REFERENCES assets(asset_id),tag_id TEXT NOT NULL REFERENCES tags(id),
 created_at TEXT NOT NULL,origin TEXT NOT NULL,PRIMARY KEY(asset_id,tag_id));
CREATE TABLE kv_settings(key TEXT PRIMARY KEY,value TEXT NOT NULL,updated_at TEXT NOT NULL);
`

type fixture struct {
	t       *testing.T
	path    string
	source  *sql.DB
	target  *store.Store
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{t: t, path: filepath.Join(t.TempDir(), "fixture-v26.db")}
	var err error
	f.source, err = store.OpenDatabase(f.path, store.CatalogDatabase)
	if err != nil {
		t.Fatal(err)
	}
	f.target, err = store.Open(filepath.Join(t.TempDir(), "fixture-native.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.target.DB.Close(); f.source.Close() })
	f.old(coldTables)
	f.old("INSERT INTO libraries VALUES('library','/synthetic-mount','normal'),('cos-library','/other-synthetic-mount','cos')")
	f.native(`INSERT INTO libraries(id,name,kind,data) VALUES
		('library','Library','regular','{"id":"library","name":"Library","kind":"regular","enabled":true}'),
		('cos-library','Cos','cos','{"id":"cos-library","name":"Cos","kind":"cos","enabled":true}')`)
	a := model.Author{ID: "unknown", Name: "其他搁置", Placeholder: true, Revision: 1, FieldStatus: map[string]string{"identity": "missing"}}
	raw, err := store.Encode(a)
	if err != nil {
		t.Fatal(err)
	}
	f.native("INSERT INTO authors(id,name,data) VALUES(?,?,?)", a.ID, a.Name, raw)
	return f
}

func (f *fixture) old(statement string, args ...any) {
	f.t.Helper()
	if _, err := f.source.ExecContext(f.t.Context(), statement, args...); err != nil {
		f.t.Fatal(err)
	}
}
func (f *fixture) native(statement string, args ...any) {
	f.t.Helper()
	if _, err := f.target.DB.ExecContext(f.t.Context(), statement, args...); err != nil {
		f.t.Fatal(err)
	}
}
func (f *fixture) setting(key, text string) {
	f.t.Helper()
	f.old("INSERT INTO kv_settings VALUES(?,?,?)", key, text, sampleTime)
}
func (f *fixture) author(id, name, kind string, followed int) {
	f.t.Helper()
	f.old("INSERT INTO authors VALUES(?,?,?,?,?,'client')", id, name, kind, sampleTime, followed)
}
func (f *fixture) asset(id, lib, relative string, source, cosWork any, tagSet string) {
	f.t.Helper()
	name := path.Base(relative)
	f.old("INSERT INTO assets VALUES(?,?,?,?,?,?,?)", id, lib, relative, name, source, cosWork, tagSet)
	at, err := time.Parse(time.RFC3339, sampleTime)
	if err != nil {
		f.t.Fatal(err)
	}
	a := model.Asset{ID: id, LibraryID: lib, FileName: name, OriginalFileName: name, RelativePath: relative, MediaType: "image",
		MimeType: "image/png", Size: 10, Hash: strings.Repeat("b", 64), Revision: 1, TagsRevision: 1, AuthorsRevision: 1,
		ContentVersion: 1, AuthorID: "unknown", AuthorIDs: []string{"unknown"}, AuthorStatus: "unknown", MetadataSource: "migration",
		CreatedAt: at, UpdatedAt: at, ProbeState: "ready"}
	if err := store.InsertAsset(f.t.Context(), f.target.DB, a); err != nil {
		f.t.Fatal(err)
	}
}
func (f *fixture) run() error {
	f.t.Helper()
	if err := f.source.Close(); err != nil {
		f.t.Fatal(err)
	}
	var err error
	f.source, err = store.OpenDatabase(f.path, store.SnapshotDatabase)
	if err != nil {
		f.t.Fatal(err)
	}
	tx, err := f.target.DB.BeginTx(f.t.Context(), nil)
	if err != nil {
		f.t.Fatal(err)
	}
	defer tx.Rollback()
	if err := Import(f.t.Context(), f.source, tx, sampleSource); err != nil {
		return err
	}
	return tx.Commit()
}
func (f *fixture) scalar(query string, args ...any) string {
	f.t.Helper()
	var value string
	if err := f.target.DB.QueryRowContext(f.t.Context(), query, args...).Scan(&value); err != nil {
		f.t.Fatal(err)
	}
	return value
}
func (f *fixture) expect(want, query string, args ...any) {
	f.t.Helper()
	if got := f.scalar(query, args...); got != want {
		f.t.Fatalf("native readback got %q, want %q for %s", got, want, query)
	}
}
