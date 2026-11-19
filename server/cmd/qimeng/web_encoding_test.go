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
	write(filepath.Join(root, "index.html"), []byte(`<script type="module" src="`+url+`"></script>`))
	check := func(entries webEncodings, accept string, status int, body []byte) {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, url, nil)
		req.Header.Set("Accept-Encoding", accept)
		w := httptest.NewRecorder()
		entry, ok := entries[url]
		if !ok || !entry.serve(w, req, root) {
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
	stale := filepath.Join(root, "assets", "index-12345678.js")
	write(stale, original)
	write(stale+".gz", encoded.Bytes())
	valid := loadWebEncodings(root)
	if len(valid) != 1 {
		t.Fatal("valid gzip pair was not registered")
	}
	entry := valid[url]
	for _, test := range []struct {
		encoding, ifRange string
		status            int
		body              []byte
	}{
		{"gzip, identity;q=0", entry.gzipETag, 206, encoded.Bytes()[:8]},
		{"gzip", entry.identityETag, 200, encoded.Bytes()},
		{"identity", entry.identityETag, 206, original[:8]},
	} {
		req := httptest.NewRequest(http.MethodGet, url, nil)
		req.Header.Set("Accept-Encoding", test.encoding)
		req.Header.Set("Range", "bytes=0-7")
		req.Header.Set("If-Range", test.ifRange)
		w := httptest.NewRecorder()
		if !entry.serve(w, req, root) || w.Code != test.status || !bytes.Equal(w.Body.Bytes(), test.body) {
			t.Fatalf("Range with %q returned %d, %d bytes", test.encoding, w.Code, w.Body.Len())
		}
	}
	changed := []byte("a new script")
	write(name, changed)
	check(valid, "gzip", 200, changed)
	check(loadWebEncodings(root), "gzip, identity;q=0", 200, changed)
	write(name, original)
	write(name+".gz", encoded.Bytes()[:8])
	check(valid, "gzip", 200, original)
	check(loadWebEncodings(root), "gzip", 200, original)
}
