package httpapi_test

import (
	"bytes"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"testing"

	"qimeng.local/next/server/internal/httpapi"
	"qimeng.local/next/server/internal/model"
	"qimeng.local/next/server/internal/service"
)

func TestExternalLogicalRootKeepsOriginalsOutsideDatabaseDirectory(t *testing.T) {
	t.Run("configured root", func(t *testing.T) { checkExternalLibrary(t, "") })
	t.Run("configured subdirectory", func(t *testing.T) { checkExternalLibrary(t, "existing") })
}

func checkExternalLibrary(t *testing.T, relative string) {
	t.Helper()
	mediaRoot := t.TempDir()
	dataRoot := t.TempDir()
	existing := filepath.Join(mediaRoot, relative)
	if e := os.MkdirAll(existing, 0700); e != nil {
		t.Fatal(e)
	}
	original := pngBytes(t, 200)
	if e := os.WriteFile(filepath.Join(existing, "preexisting.png"), original, 0600); e != nil {
		t.Fatal(e)
	}
	config := service.Config{DataDir: dataRoot, AdminToken: testSecret, StorageRoots: map[string]string{"media": mediaRoot}, ReserveBytes: 1}
	s, e := service.New(config)
	if e != nil {
		t.Fatal(e)
	}
	f := &fixture{t: t, s: s, h: httpapi.New(s)}
	t.Cleanup(func() { f.s.Close() })
	r := f.json("POST", "/libraries", "external-library-create", map[string]any{"name": "External synthetic", "kind": "regular", "storageId": "media", "rootRelativePath": relative}, 201)
	var library model.Library
	decode(t, r, &library)
	f.library = library.ID
	f.json("POST", "/libraries", "overlapping-library-create", map[string]any{"name": "Overlap", "kind": "regular", "storageId": "media", "rootRelativePath": path.Join(relative, "child")}, 409)
	upload, body := f.upload("external-upload.png", 201, map[string]any{"originalFileName": "source�.png", "sourceRelativePath": "download/source�.png"})
	a := *f.complete(upload).Asset
	if a.OriginalFileName != "source�.png" || a.SourceRelativePath != "download/source�.png" {
		t.Fatal("original clues were replaced by destination filename")
	}
	actual, e := os.ReadFile(filepath.Join(existing, "external-upload.png"))
	if e != nil || !bytes.Equal(actual, body) {
		t.Fatal("original was not published into the configured root")
	}
	if _, e = os.Stat(filepath.Join(dataRoot, "libraries", library.ID, "external-upload.png")); !os.IsNotExist(e) {
		t.Fatal("original was copied into the database directory")
	}
	untouched, e := os.ReadFile(filepath.Join(existing, "preexisting.png"))
	if e != nil || !bytes.Equal(untouched, original) {
		t.Fatal("registering existing root modified its contents")
	}
	r = f.json("DELETE", "/assets/"+a.ID+"?expectedRevision="+strconv.FormatInt(a.Revision, 10), "external-trash", struct{}{}, 200)
	decode(t, r, &a)
	if _, e = os.Stat(filepath.Join(existing, ".qimeng-internal", "trash", a.ID, a.FileName)); e != nil {
		t.Fatal("trash was not kept on the original storage")
	}
	if e = f.s.Close(); e != nil {
		t.Fatal(e)
	}
	f.s, e = service.New(config)
	if e != nil {
		t.Fatal(e)
	}
	f.h = httpapi.New(f.s)
	r = f.json("POST", "/trash/"+a.ID+"/restore", "external-restore", map[string]any{"expectedRevision": a.Revision}, 200)
	decode(t, r, &a)
	if a.Trashed {
		t.Fatal("external storage did not restore after restart")
	}
	r = f.request("GET", "/assets/"+a.ID+"/thumbnail?version=1", "", testSecret, nil, nil)
	if r.Code != 200 {
		t.Fatalf("external thumbnail: %d %s", r.Code, r.Body.String())
	}
	f.json("POST", "/uploads", "reserved-internal-directory", map[string]any{"libraryId": f.library, "fileName": "invalid.png", "size": 100, "mimeType": "image/png", "mode": "ordinary", "relativeDirectory": ".qimeng-internal"}, 400)
	f.json("POST", "/uploads", "absolute-source-clue", map[string]any{"libraryId": f.library, "fileName": "invalid.png", "originalFileName": "invalid.png", "sourceRelativePath": "/private/source.png", "size": 100, "mimeType": "image/png", "mode": "ordinary"}, 400)
}
