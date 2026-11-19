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
	"slices"
	"strconv"
	"strings"

	"golang.org/x/net/html"
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
	index, err := root.Open("index.html")
	if err != nil {
		return nil
	}
	defer index.Close()
	entries := make(webEncodings)
	tokens := html.NewTokenizer(index)
	for {
		kind := tokens.Next()
		if kind == html.ErrorToken {
			break
		}
		if kind != html.StartTagToken && kind != html.SelfClosingTagToken {
			continue
		}
		url := webEntryURL(tokens.Token())
		if !fingerprintedEntry.MatchString(url) {
			continue
		}
		entry, err := verifyWebEncoding(root, strings.TrimPrefix(url, "/"))
		if err == nil {
			entries[url] = entry
		} else if !errors.Is(err, fs.ErrNotExist) {
			slog.Warn("ignoring an invalid precompressed Web entry")
		}
	}
	return entries
}

func webEntryURL(tag html.Token) string {
	var url, kind string
	for _, attr := range tag.Attr {
		switch {
		case tag.Data == "script" && attr.Key == "src", tag.Data == "link" && attr.Key == "href":
			url = attr.Val
		case tag.Data == "script" && attr.Key == "type", tag.Data == "link" && attr.Key == "rel":
			kind = attr.Val
		}
	}
	if tag.Data == "script" && kind == "module" && strings.HasSuffix(url, ".js") ||
		tag.Data == "link" && slices.Contains(strings.Fields(kind), "stylesheet") && strings.HasSuffix(url, ".css") {
		return url
	}
	return ""
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

func openWebVersion(web, name string, expected fs.FileInfo) *os.File {
	file, err := os.OpenInRoot(web, name)
	if err != nil {
		return nil
	}
	info, err := file.Stat()
	if err != nil || !os.SameFile(expected, info) || expected.Size() != info.Size() || !expected.ModTime().Equal(info.ModTime()) {
		file.Close()
		return nil
	}
	return file
}

func (entry webEncoding) serve(w http.ResponseWriter, r *http.Request, web string) bool {
	name := strings.TrimPrefix(r.URL.Path, "/")
	original := openWebVersion(web, name, entry.original)
	if original == nil {
		return false
	}
	defer original.Close()
	w.Header().Add("Vary", "Accept-Encoding")
	gzipQ, identityQ := webEncodingWeights(r.Header.Values("Accept-Encoding"))
	if gzipQ > 0 && gzipQ >= identityQ {
		if compressed := openWebVersion(web, name+".gz", entry.compressed); compressed != nil {
			defer compressed.Close()
			if strings.Contains(r.Header.Get("Range"), ",") {
				// A multipart envelope would need per-part encoding metadata.
				r = r.Clone(r.Context())
				r.Header.Del("Range")
			}
			w.Header().Set("ETag", entry.gzipETag)
			http.ServeContent(gzipWebResponse{w}, r, name, entry.compressed.ModTime(), compressed)
			return true
		}
	}
	if identityQ == 0 {
		http.Error(w, "no acceptable encoding", http.StatusNotAcceptable)
	} else {
		w.Header().Set("ETag", entry.identityETag)
		http.ServeContent(w, r, name, entry.original.ModTime(), original)
	}
	return true
}

// ServeContent computes lengths and validates preconditions for the bytes in
// the file. Only a response carrying those bytes has a gzip content encoding.
type gzipWebResponse struct{ http.ResponseWriter }

func (w gzipWebResponse) WriteHeader(status int) {
	if status == http.StatusOK || status == http.StatusPartialContent {
		w.Header().Set("Content-Encoding", "gzip")
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w gzipWebResponse) ReadFrom(r io.Reader) (int64, error) {
	return io.Copy(w.ResponseWriter, r)
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
