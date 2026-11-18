package httpapi_test

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"

	"qimeng.local/next/server/internal/httpapi"
	"qimeng.local/next/server/internal/model"
	"qimeng.local/next/server/internal/service"
)

func makeNativeVideo(t *testing.T, size string) []byte {
	t.Helper()
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("the media integration runner supplies ffmpeg")
	}
	if _, err = exec.LookPath("ffprobe"); err != nil {
		t.Skip("the media integration runner supplies ffprobe")
	}
	data, err := exec.Command(ffmpeg, "-nostdin", "-v", "error", "-f", "lavfi", "-i", "testsrc2=size="+size+":rate=6",
		"-t", "3", "-threads", "1", "-c:v", "libx264", "-pix_fmt", "yuv420p",
		"-movflags", "frag_keyframe+empty_moov", "-f", "mp4", "pipe:1").Output()
	if err != nil {
		t.Fatal("creating synthetic video", err)
	}
	return data
}

func nativeVideoUpload(t *testing.T, f *fixture, name string, body []byte, previous *model.Asset) model.Upload {
	t.Helper()
	input := map[string]any{"libraryId": f.library, "fileName": name, "mimeType": "video/mp4", "size": len(body), "mode": "ordinary"}
	if previous != nil {
		input["replaceAssetId"] = previous.ID
		input["expectedContentVersion"] = previous.ContentVersion
	}
	var upload model.Upload
	decode(t, f.json("POST", "/uploads", "declare-native-"+name, input, 201), &upload)
	r := f.request("PUT", "/uploads/"+upload.ID+"/content", "", testSecret, body, map[string]string{"Content-Type": "application/octet-stream", "Upload-Offset": "0"})
	if r.Code != http.StatusOK {
		t.Fatal("transferring synthetic original", r.Code, r.Body.String())
	}
	return upload
}

func nativeCheckpointCopy(t *testing.T, f *fixture, stage string) *fixture {
	t.Helper()
	var b service.Backup
	decode(t, f.json("POST", "/backups", "native-checkpoint-"+stage, struct{}{}, 201), &b)
	f.json("POST", "/backups/"+b.ID+"/export", "native-export-"+stage, struct{}{}, 202)
	b = awaitBackup(t, f, b.ID)
	if b.ExportState != "completed" || b.ArchiveHash == "" {
		t.Fatal("checkpoint export failed", b.ExportState, b.ExportError)
	}
	destination := filepath.Join(t.TempDir(), "isolated")
	result, err := service.RestoreBackup(context.Background(), service.RestoreOptions{
		ArchivePath: filepath.Join(f.s.Config.DataDir, "backups", b.ID, "archive.qnext"), Destination: destination,
		ExpectedSHA256: b.ArchiveHash, ReserveBytes: 1,
	})
	if err != nil || result.AssetCount != 2 || result.InstanceID == f.s.InstanceID || result.ArchiveHash != b.ArchiveHash {
		t.Fatal("isolated restore", result, err)
	}
	restored, err := service.New(service.Config{DataDir: destination, AdminToken: testSecret, ReserveBytes: 1})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { restored.Close() })
	return &fixture{t: t, s: restored, h: httpapi.New(restored), library: f.library}
}

func assertNativeOriginal(t *testing.T, f *fixture, id string, version int64, original []byte) {
	t.Helper()
	r := f.request("GET", "/assets/"+id+"/content?version="+strconv.FormatInt(version, 10), "", testSecret, nil, nil)
	hash := sha256.Sum256(original)
	if r.Code != http.StatusOK || len(r.Body.Bytes()) != len(original) || sha256.Sum256(r.Body.Bytes()) != hash {
		t.Fatalf("restored original %s v%d has unexpected status, length or SHA-256: %d", id, version, r.Code)
	}
	t.Logf("verified original %s v%d bytes=%d sha256=%x", id, version, len(original), hash)
}

func TestNativeVideoRenameReplacementAndIsolatedRestore(t *testing.T) {
	low, high := makeNativeVideo(t, "160x90"), makeNativeVideo(t, "1280x720")
	f := setup(t)
	controlUpload, controlBytes := f.upload("control.png", 148, nil)
	control := f.complete(controlUpload).Asset
	a := *f.complete(nativeVideoUpload(t, f, "camera.mp4", low, nil)).Asset
	if a.Width != 160 || a.Height != 90 || a.ContentVersion != 1 || a.DurationMs < 1300 || a.ProbeState != "ready" {
		t.Fatal("first content was not measured by ffprobe", a.Width, a.Height, a.DurationMs, a.ProbeState)
	}
	root := "/assets/" + a.ID
	decode(t, f.json("PATCH", root, "attach-native-author", map[string]any{"expectedRevision": a.Revision,
		"expectedAuthorsRevision": a.AuthorsRevision, "newAuthorNames": []string{"Native fixture author"}}, 200), &a)
	author := a.AuthorID
	decode(t, f.json("PUT", root+"/favorite", "native-favorite", map[string]bool{"favorite": true}, 200), &a)
	var mark service.TimelineMark
	decode(t, f.json("POST", root+"/timeline", "native-old-mark", map[string]any{"positionMs": 700, "label": "Old cut", "contentVersion": 1}, 201), &mark)
	f.json("POST", root+"/events", "", map[string]any{"eventId": "native-seek-first", "kind": "progress", "contentVersion": 1, "positionMs": 1200}, 200)
	const moved = "season/renamed.mp4"
	before := a.ID
	decode(t, f.json("POST", root+"/move", "native-video-move", map[string]any{"expectedRevision": a.Revision, "relativePath": moved}, 200), &a)
	if a.ID != before || a.ContentVersion != 1 || a.RelativePath != moved || a.Hash != fmt.Sprintf("%x", sha256.Sum256(low)) {
		t.Fatal("rename changed the content or identity", a.ID, a.ContentVersion, a.RelativePath)
	}
	firstCopy := nativeCheckpointCopy(t, f, "before")
	assertNativeOriginal(t, firstCopy, a.ID, 1, low)
	assertNativeOriginal(t, firstCopy, control.ID, 1, controlBytes)
	var first model.Asset
	decode(t, firstCopy.json("GET", root, "", nil, 200), &first)
	if first.AuthorID != author || author == "unknown" || !first.Favorite || first.PositionMs != 1200 || first.PositionContentVersion != 1 {
		t.Fatal("first restore changed authors, favorite or version-one seek", first.AuthorID, first.Favorite, first.PositionMs, first.PositionContentVersion)
	}
	replacement := nativeVideoUpload(t, f, "camera-hd.mp4", high, &a)
	latest := f.complete(replacement).Asset
	replayed := f.complete(replacement).Asset
	if latest.ID != a.ID || latest.ContentVersion != 2 || replayed.ID != a.ID || replayed.ContentVersion != 2 ||
		latest.Width != 1280 || latest.Height != 720 || latest.DurationMs < 1300 || latest.ProbeState != "ready" {
		t.Fatal("high-resolution replacement/replay did not publish exactly v2")
	}
	secondCopy := nativeCheckpointCopy(t, f, "after")
	for _, instance := range []*fixture{f, secondCopy} {
		var current model.Asset
		decode(t, instance.json("GET", root, "", nil, 200), &current)
		if current.ID != a.ID || current.ContentVersion != 2 || current.RelativePath != moved || !current.Favorite ||
			len(current.AuthorIDs) != 1 || current.AuthorIDs[0] != author || current.PositionMs != 1200 || current.PositionContentVersion != 1 {
			t.Fatal("current content inherited an old seek or lost its business identity", current.ID, current.RelativePath, current.PositionContentVersion)
		}
		var timeline model.Page[service.TimelineMark]
		decode(t, instance.json("GET", root+"/timeline", "", nil, 200), &timeline)
		if len(timeline.Items) != 1 || timeline.Items[0].ID != mark.ID || timeline.Items[0].ContentVersion != 1 || timeline.Items[0].PositionMs != 700 {
			t.Fatal("old timeline was discarded or applied to the replacement", timeline)
		}
		instance.json("PUT", root+"/timeline/"+mark.ID, "cannot-rebase-native-mark", map[string]any{"expectedRevision": mark.Revision,
			"contentVersion": 2, "positionMs": 700, "label": "Old cut"}, 409)
		instance.json("POST", root+"/events", "", map[string]any{"eventId": "native-stale-seek", "kind": "progress", "contentVersion": 1, "positionMs": 300}, 409)
		if r := instance.request("GET", root+"/content?version=1", "", testSecret, nil, nil); r.Code != http.StatusConflict {
			t.Fatal("old playback opened different bytes", r.Code)
		}
		assertNativeOriginal(t, instance, a.ID, 2, high)
		assertNativeOriginal(t, instance, control.ID, 1, controlBytes)
		for i, item := range []struct {
			data          []byte
			width, height int
		}{{low, 160, 90}, {high, 1280, 720}} {
			var count int
			err := instance.s.DB.DB.QueryRow(`SELECT count(*) FROM asset_content_versions WHERE asset_id=? AND version=?
				AND hash=? AND size=? AND media_type='video' AND mime_type='video/mp4' AND width=? AND height=? AND duration_ms>0`,
				a.ID, i+1, fmt.Sprintf("%x", sha256.Sum256(item.data)), len(item.data), item.width, item.height).Scan(&count)
			if err != nil || count != 1 {
				t.Fatal("missing immutable video evidence", i+1, count, err)
			}
		}
	}
}
