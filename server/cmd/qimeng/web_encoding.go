package main

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
)

type webEncoding struct {
	original, compressed   fs.FileInfo
	identityETag, gzipETag string
}

type webEncodings map[string]webEncoding

var fingerprintedEntry = regexp.MustCompile(`^/assets/index-[A-Za-z0-9_-]{8}\.(js|css)$`)

func loadWebEncodings(web string) webEncodings {
	root, err := os.OpenRoot(web)
	if err != nil {
		return nil
	}
	defer root.Close()
	names, _ := fs.Glob(root.FS(), "assets/index-????????.*.gz")
	entries := make(webEncodings)
	for _, name := range names {
		original := strings.TrimSuffix(name, ".gz")
		if !fingerprintedEntry.MatchString("/" + original) {
			continue
		}
		entry, err := verifyWebEncoding(root, original)
		if err != nil {
			slog.Warn("ignoring an invalid precompressed Web entry")
			continue
		}
		entries["/"+original] = entry
	}
	return entries
}

// Verify each installed pair once. A reader of an unchanged package pays no
// compression or checksum cost when requesting either representation.
func verifyWebEncoding(root *os.Root, name string) (webEncoding, error) {
	var entry webEncoding
	original, err := root.Open(name)
	if err != nil {
		return entry, err
	}
	defer original.Close()
	compressed, err := root.Open(name + ".gz")
	if err != nil {
		return entry, err
	}
	defer compressed.Close()
	if entry.original, err = original.Stat(); err != nil {
		return entry, err
	}
	if entry.compressed, err = compressed.Stat(); err != nil {
		return entry, err
	}
	if !entry.original.Mode().IsRegular() || !entry.compressed.Mode().IsRegular() {
		return entry, errors.New("not a regular Web entry")
	}
	wireHash := sha256.New()
	decoded, err := gzip.NewReader(io.TeeReader(compressed, wireHash))
	if err != nil {
		return entry, err
	}
	defer decoded.Close()
	decodedHash, originalHash := sha256.New(), sha256.New()
	n, err := io.Copy(decodedHash, io.LimitReader(decoded, entry.original.Size()+1))
	if err != nil {
		return entry, err
	}
	if n != entry.original.Size() {
		return entry, errors.New("precompressed Web entry has a different decoded size")
	}
	if _, err := io.Copy(originalHash, original); err != nil {
		return entry, err
	}
	if !bytes.Equal(decodedHash.Sum(nil), originalHash.Sum(nil)) {
		return entry, errors.New("precompressed Web entry has different content")
	}
	entry.identityETag = fmt.Sprintf(`"%x"`, originalHash.Sum(nil))
	entry.gzipETag = fmt.Sprintf(`"%x"`, wireHash.Sum(nil))
	return entry, nil
}

func sameWebFile(a, b fs.FileInfo) bool {
	return os.SameFile(a, b) && a.Size() == b.Size() && a.ModTime().Equal(b.ModTime())
}

func (entries webEncodings) serve(w http.ResponseWriter, r *http.Request, web string, info fs.FileInfo) bool {
	if !fingerprintedEntry.MatchString(r.URL.Path) || !info.Mode().IsRegular() {
		return false
	}
	w.Header().Add("Vary", "Accept-Encoding")
	gzipQ, identityQ := webEncodingWeights(r.Header.Values("Accept-Encoding"))
	entry, ok := entries[r.URL.Path]
	if ok && sameWebFile(entry.original, info) {
		if gzipQ > 0 && gzipQ >= identityQ && (r.Header.Get("Range") == "" || identityQ == 0) {
			if entry.serveGzip(w, r, web) {
				return true
			}
		}
		if identityQ != 0 {
			w.Header().Set("ETag", entry.identityETag)
		}
	}
	if identityQ == 0 {
		http.Error(w, "no acceptable encoding", http.StatusNotAcceptable)
		return true
	}
	return false
}

func (entry webEncoding) serveGzip(w http.ResponseWriter, r *http.Request, web string) bool {
	file, err := os.OpenInRoot(web, strings.TrimPrefix(r.URL.Path, "/")+".gz")
	if err != nil {
		return false
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !sameWebFile(entry.compressed, info) {
		return false
	}
	if r.Header.Get("Range") != "" {
		// Identity is forbidden. The client can decode a whole gzip stream.
		r = r.Clone(r.Context())
		r.Header.Del("Range")
	}
	w.Header().Set("Content-Encoding", "gzip")
	w.Header().Set("Content-Length", strconv.FormatInt(info.Size(), 10))
	w.Header().Set("ETag", entry.gzipETag)
	http.ServeContent(w, r, entry.original.Name(), info.ModTime(), file)
	return true
}

var qualityWeight = regexp.MustCompile(`(?i)^q=(0(?:\.[0-9]{0,3})?|1(?:\.0{0,3})?)$`)

// A missing identity weight leaves it as the fallback. An explicit weight
// ranks the two offers. An explicit coding takes precedence over '*'.
func webEncodingWeights(fields []string) (gzipQ, identityQ float64) {
	gzipQ, identityQ, wildcard := -1.0, -1.0, -1.0
	for _, field := range fields {
		for item := range strings.SplitSeq(field, ",") {
			coding, weight, weighted := strings.Cut(item, ";")
			q := 1.0
			if weighted {
				match := qualityWeight.FindStringSubmatch(strings.TrimSpace(weight))
				if match == nil {
					q = 0
				} else {
					q, _ = strconv.ParseFloat(match[1], 64)
				}
			}
			switch strings.ToLower(strings.TrimSpace(coding)) {
			case "gzip", "x-gzip":
				gzipQ = max(gzipQ, q)
			case "identity":
				identityQ = max(identityQ, q)
			case "*":
				wildcard = max(wildcard, q)
			}
		}
	}
	if gzipQ < 0 {
		gzipQ = wildcard
	}
	if identityQ < 0 && wildcard == 0 {
		identityQ = 0
	}
	return gzipQ, identityQ
}
