package httpapi_test

import (
	"bytes"
	"io"
	"os"
	"strconv"
	"strings"
	"testing"

	"qimeng.local/next/server/internal/model"
)

func TestAppendUploadRejectsMissingAcknowledgedPrefix(t *testing.T) {
	f := setup(t)
	body := pngBytes(t, 40)
	half := len(body) / 2

	var upload model.Upload
	decode(t, f.json("POST", "/uploads", "create-prefix-test", map[string]any{
		"libraryId": f.library, "fileName": "prefix.png", "size": len(body), "mimeType": "image/png", "mode": "ordinary",
	}, 201), &upload)
	endpoint := "/uploads/" + upload.ID
	appendChunk := func(offset int, content []byte, want int, code string) {
		t.Helper()
		res := f.request("PUT", endpoint+"/content", "", testSecret, content, map[string]string{
			"Content-Type": "application/octet-stream", "Upload-Offset": strconv.Itoa(offset),
		})
		if res.Code != want || !strings.Contains(res.Body.String(), code) {
			t.Fatalf("append at %d: status %d, body %s", offset, res.Code, res.Body.String())
		}
	}
	appendChunk(0, body[:half], 200, "")

	staging, err := f.s.Root.OpenFile("uploads/"+upload.ID+".part", os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer staging.Close()
	if err := staging.Truncate(int64(half - 1)); err != nil {
		t.Fatal(err)
	}
	assertStaging := func(want []byte) {
		t.Helper()
		if _, err := staging.Seek(0, io.SeekStart); err != nil {
			t.Fatal(err)
		}
		got, err := io.ReadAll(staging)
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("unexpected staging bytes (size %d, want %d): %v", len(got), len(want), err)
		}
	}
	appendChunk(half, body[half:], 409, "upload_prefix_missing")
	appendChunk(0, body[:half-1], 409, "upload_prefix_missing")
	assertStaging(body[:half-1])
	decode(t, f.json("GET", endpoint, "", nil, 200), &upload)
	if upload.Offset != int64(half) {
		t.Fatalf("missing prefix advanced offset to %d", upload.Offset)
	}

	// Recreate the correct prefix plus a longer, uncommitted tail.
	if _, err := staging.WriteAt(append(append([]byte(nil), body[:half]...), make([]byte, len(body))...), 0); err != nil {
		t.Fatal(err)
	}
	appendChunk(0, body[:half], 200, "")
	changed := append([]byte(nil), body[:half]...)
	changed[0] ^= 1
	appendChunk(0, changed, 409, "chunk_conflict")
	appendChunk(half, body[half:], 200, "")
	assertStaging(body)
	decode(t, f.json("GET", endpoint, "", nil, 200), &upload)
	if upload.Offset != int64(len(body)) {
		t.Fatalf("offset after successful resume was %d", upload.Offset)
	}
}
