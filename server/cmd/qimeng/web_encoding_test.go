package main

import (
	"bytes"
	"compress/gzip"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestPackagedEntryCanFallBackToUncompressedContent(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "assets"), 0700); err != nil {
		t.Fatal(err)
	}
	url := "/assets/index-Ab_cd-89.js"
	name := filepath.Join(root, filepath.FromSlash(url[1:]))
	original := []byte("original script")
	write := func(path string, data []byte) {
		t.Helper()
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(name, original)
	check := func(entries webEncodings, accept string, status int, body []byte) {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, url, nil)
		req.Header.Set("Accept-Encoding", accept)
		info, err := os.Stat(name)
		if err != nil {
			t.Fatal(err)
		}
		w := httptest.NewRecorder()
		if !entries.serve(w, req, root, info) {
			http.FileServer(http.Dir(root)).ServeHTTP(w, req)
		}
		if w.Code != status || w.Header().Get("Content-Encoding") != "" || !bytes.Equal(w.Body.Bytes(), body) {
			t.Fatalf("identity fallback returned %d %q", w.Code, w.Body.Bytes())
		}
	}
	check(loadWebEncodings(root), "gzip", 200, original)
	var encoded bytes.Buffer
	writer := gzip.NewWriter(&encoded)
	if _, err := writer.Write(original); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	write(name+".gz", encoded.Bytes())
	valid := loadWebEncodings(root)
	if len(valid) != 1 {
		t.Fatal("valid gzip pair was not registered")
	}
	changed := []byte("a new script")
	write(name, changed)
	check(valid, "gzip", 200, changed)
	check(loadWebEncodings(root), "gzip, identity;q=0", 406, []byte("no acceptable encoding\n"))
	write(name, original)
	write(name+".gz", encoded.Bytes()[:8])
	check(valid, "gzip", 200, original)
	check(loadWebEncodings(root), "gzip", 200, original)
}
