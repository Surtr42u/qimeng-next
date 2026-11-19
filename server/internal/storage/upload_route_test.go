package storage

import (
	"os"
	"path/filepath"
	"testing"
)

func TestUploadRoutesResolveTemporaryDurableAndLegacyPaths(t *testing.T) {
	data, media := t.TempDir(), t.TempDir()
	r, err := Open(data, map[string]string{"media": media})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	data, media = r.locations["data"].absolute, r.locations["media"].absolute
	if err = r.RegisterLibrary("library", "media", "originals"); err != nil {
		t.Fatal(err)
	}
	lookups := 0
	r.SetUploadLibraryLookup(func(id string) (string, error) {
		lookups++
		if id == "durable" {
			return "library", nil
		}
		return "", nil
	})
	resolved := func(name, want string) {
		t.Helper()
		got, err := r.AbsolutePath(name)
		if err != nil || got != want {
			t.Fatalf("resolve %s: %s, %v, want %s", name, got, err, want)
		}
	}
	staging := filepath.Join(media, "originals", ".qimeng-internal", "uploads")
	r.RegisterUpload("temporary", "library")
	resolved("uploads/temporary.part", filepath.Join(staging, "temporary.part"))
	if lookups != 0 {
		t.Fatal("uncommitted upload consulted the catalog")
	}
	r.UnregisterUpload("temporary")
	resolved("uploads/temporary.part", filepath.Join(data, "uploads", "temporary.part"))
	resolved("uploads/durable.part", filepath.Join(staging, "durable.part"))
	r.DetachLibrary("library", true)
	resolved("uploads/durable.part", filepath.Join(staging, "durable.part"))
	resolved("retired/durable", filepath.Join(media, "originals", ".qimeng-internal", "retired", "durable"))
	if lookups != 4 || len(r.uploads) != 0 {
		t.Fatalf("durable routes accumulated in memory: lookups=%d, routes=%d", lookups, len(r.uploads))
	}
	legacy := filepath.Join(data, "uploads", "durable.part")
	if err = os.MkdirAll(filepath.Dir(legacy), 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(legacy, []byte("legacy prefix"), 0600); err != nil {
		t.Fatal(err)
	}
	resolved("uploads/durable.part", legacy)
	if lookups != 4 {
		t.Fatal("legacy prefix used the newer storage route")
	}
}
