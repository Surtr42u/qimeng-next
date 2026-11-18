package httpapi_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"qimeng.local/next/server/internal/model"
	"qimeng.local/next/server/internal/transport/generated"
)

func TestContractRoutesBindBeforeMutationAndAuthenticateBeforeErrors(t *testing.T) {
	f := setup(t)
	for _, tc := range []struct {
		method, path, token string
		want                int
	}{
		{"GET", "/instance", "", 200},
		{"PATCH", "/uploads/absent/content", testSecret, 405},
		{"GET", "/uploads/absent/undeclared", testSecret, 404},
		{"GET", "/libraries?limit=invalid", testSecret, 400},
		{"PUT", "/uploads/absent/content", "", 401},
		{"PUT", "/uploads/absent/content", testSecret, 400},
		{"GET", "/device/apply-tasks", testSecret, 400},
	} {
		response := f.request(tc.method, tc.path, "", tc.token, nil, nil)
		if response.Code != tc.want {
			t.Fatalf("%s %s returned %d, want %d", tc.method, tc.path, response.Code, tc.want)
		}
		if tc.want == 400 {
			var problem generated.Error
			decode(t, response, &problem)
			if problem.Code == "" {
				t.Fatal("parameter response omitted its public error code", tc.path)
			}
		}
	}
}

func TestGeneratedClientUploadsOriginalAndStreamsVersionedRange(t *testing.T) {
	f := setup(t)
	server := httptest.NewServer(f.h)
	t.Cleanup(server.Close)
	client, err := generated.NewClient(server.URL+"/api/v1", generated.WithRequestEditorFn(func(_ context.Context, r *http.Request) error {
		r.Header.Set("Authorization", "Bearer "+testSecret)
		return nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	frame := image.NewNRGBA(image.Rect(0, 0, 512, 256))
	if _, err = rand.Read(frame.Pix); err != nil {
		t.Fatal(err)
	}
	var original bytes.Buffer
	if err = png.Encode(&original, frame); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	input := generated.UploadCreate{LibraryId: f.library, FileName: "stream.png", Size: int64(original.Len()), MimeType: "image/png", Mode: "ordinary"}
	response, err := client.CreateUpload(ctx, &generated.CreateUploadParams{IdempotencyKey: "contract-create-upload"}, input)
	readJSON := func(response *http.Response, err error, want int, out any) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		if response.StatusCode != want {
			t.Fatalf("unexpected HTTP status %d, want %d", response.StatusCode, want)
		}
		if err := json.NewDecoder(response.Body).Decode(out); err != nil {
			t.Fatal(err)
		}
	}
	var upload model.Upload
	readJSON(response, err, 201, &upload)
	cut := original.Len() / 2
	for _, part := range [][2]int{{0, cut}, {cut, original.Len()}} {
		params := &generated.AppendUploadParams{UploadOffset: int64(part[0])}
		response, err = client.AppendUploadWithBody(ctx, upload.ID, params, "application/octet-stream", bytes.NewReader(original.Bytes()[part[0]:part[1]]))
		readJSON(response, err, 200, &upload)
	}
	response, err = client.CompleteUpload(ctx, upload.ID, &generated.CompleteUploadParams{IdempotencyKey: "contract-complete-upload"})
	var completed model.UploadResult
	readJSON(response, err, 200, &completed)
	if completed.Asset == nil || completed.Upload.State != "ingested" {
		t.Fatal("upload did not publish an original", completed.Upload.State)
	}
	offset := 137
	byteRange := fmt.Sprintf("bytes=%d-", offset)
	params := &generated.GetAssetContentParams{Range: &byteRange, Version: &completed.Asset.ContentVersion}
	response, err = client.GetAssetContent(ctx, completed.Asset.ID, params)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	wantRange := fmt.Sprintf("bytes %d-%d/%d", offset, original.Len()-1, original.Len())
	if response.StatusCode != 206 || response.Header.Get("Content-Range") != wantRange || response.Header.Get("ETag") == "" {
		t.Fatal(response.StatusCode, response.Header.Get("Content-Range"))
	}
	chunk := make([]byte, 32<<10)
	for at := offset; at < original.Len(); {
		n := min(len(chunk), original.Len()-at)
		if _, err := io.ReadFull(response.Body, chunk[:n]); err != nil || !bytes.Equal(chunk[:n], original.Bytes()[at:at+n]) {
			t.Fatal("streamed original differs at byte", at, err)
		}
		at += n
	}
	if n, err := response.Body.Read(chunk); n != 0 || err != io.EOF {
		t.Fatal("unexpected data after the byte range", n, err)
	}
}
