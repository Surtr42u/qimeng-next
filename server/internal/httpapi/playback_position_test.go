package httpapi_test

import (
	"fmt"
	"net/http"
	"os/exec"
	"strings"
	"testing"
	"time"

	"qimeng.local/next/server/internal/model"
)

func TestVideoSeeksOwnOnlyVersionedPlaybackState(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("the media integration runner supplies ffmpeg")
	}
	f := setup(t)
	video, err := exec.Command(ffmpeg, "-nostdin", "-v", "error", "-f", "lavfi", "-i", "color=c=blue:s=32x24:r=4",
		"-t", "3", "-c:v", "libx264", "-pix_fmt", "yuv420p", "-movflags", "frag_keyframe+empty_moov", "-f", "mp4", "pipe:1").Output()
	if err != nil {
		t.Fatal("creating synthetic video", err)
	}
	var u model.Upload
	decode(t, f.json("POST", "/uploads", "create-seek-video", map[string]any{
		"libraryId": f.library, "fileName": "seeks.mp4", "mimeType": "video/mp4", "size": len(video), "mode": "ordinary",
	}, 201), &u)
	response := f.request("PUT", "/uploads/"+u.ID+"/content", "", testSecret, video, map[string]string{"Content-Type": "application/octet-stream", "Upload-Offset": "0"})
	if response.Code != http.StatusOK {
		t.Fatal(response.Code, response.Body.String())
	}
	a := *f.complete(u).Asset
	root := "/assets/" + a.ID
	var source, character struct{ ID string }
	decode(t, f.json("POST", "/sources", "seek-source", map[string]any{"name": "Seek source", "aliases": []string{}}, 201), &source)
	decode(t, f.json("POST", "/characters", "seek-character", map[string]any{"name": "Seek character", "sourceId": source.ID, "aliases": []string{}}, 201), &character)
	f.json("PATCH", root, "set-seek-taxonomy", map[string]any{"expectedRevision": a.Revision, "sourceIds": []string{source.ID}, "characterIds": []string{character.ID}}, 200)
	f.json("PUT", root+"/tags", "set-seek-tag", map[string]any{"tags": []string{"测试标签"}, "expectedRevision": a.TagsRevision}, 200)

	// Count actual SQLite row mutations. All four relations are populated before recording.
	db := f.s.DB.DB
	if _, err := db.Exec("CREATE TABLE test_seek_writes(table_name TEXT NOT NULL)"); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"asset_authors", "asset_tags", "asset_sources", "asset_characters"} {
		var count int
		if err := db.QueryRow("SELECT count(*) FROM "+table+" WHERE asset_id=?", a.ID).Scan(&count); err != nil || count < 1 {
			t.Fatal("expected a populated relation", table, count, err)
		}
		for _, op := range []string{"INSERT", "UPDATE", "DELETE"} {
			ddl := fmt.Sprintf("CREATE TRIGGER test_seek_%s_%s AFTER %s ON %s BEGIN INSERT INTO test_seek_writes VALUES('%s'); END",
				table, strings.ToLower(op), op, table, table)
			if _, err := db.Exec(ddl); err != nil {
				t.Fatal(err)
			}
		}
	}
	var initial model.Asset
	decode(t, f.json("GET", root, "", nil, 200), &initial)
	start := time.Now().Add(-time.Minute)
	event := func(n int, kind string, position, second int) {
		f.json("POST", root+"/events", "", map[string]any{"eventId": fmt.Sprintf("seek-event-%d", n), "sessionId": "seek-session",
			"kind": kind, "contentVersion": a.ContentVersion, "positionMs": position, "occurredAt": start.Add(time.Duration(second) * time.Second)}, 200)
	}
	event(0, "play", 0, 0)
	event(1, "progress", 650, 1)
	event(2, "progress", 2100, 2)
	event(3, "progress", 1200, 3)
	event(3, "progress", 1200, 3)
	event(4, "progress", 900, 1)
	f.json("POST", root+"/events", "", map[string]any{"eventId": "next-page-play", "sessionId": "next-page", "kind": "play", "contentVersion": a.ContentVersion}, 200)
	f.json("POST", root+"/events", "", map[string]any{"eventId": "play-at-offset", "sessionId": "third-page", "kind": "play", "positionMs": 300, "contentVersion": a.ContentVersion}, 200)
	f.json("POST", root+"/events", "", map[string]any{"eventId": "missing-seek-offset", "kind": "progress", "contentVersion": a.ContentVersion}, 400)
	var reloaded model.Asset
	decode(t, f.json("GET", root, "", nil, 200), &reloaded)
	if reloaded.PositionMs != 1200 || reloaded.PositionContentVersion != a.ContentVersion || reloaded.Revision != initial.Revision {
		t.Fatal("late delivery or replay changed the last seek or metadata revision", reloaded.PositionMs, reloaded.Revision)
	}
	var writes, copies int
	if err := db.QueryRow("SELECT count(*) FROM test_seek_writes").Scan(&writes); err != nil || writes != 0 {
		t.Fatal("playback wrote relationship records", writes, err)
	}
	if err := db.QueryRow("SELECT count(*) FROM assets WHERE id=? AND (json_type(data,'$.positionMs') IS NOT NULL OR json_type(data,'$.positionContentVersion') IS NOT NULL)", a.ID).Scan(&copies); err != nil || copies != 0 {
		t.Fatal("asset metadata contains a second writable copy of playback", copies, err)
	}
	event(5, "progress", 0, 5)
	decode(t, f.json("GET", root, "", nil, 200), &reloaded)
	if reloaded.PositionMs != 0 || reloaded.PositionContentVersion != a.ContentVersion {
		t.Fatal("explicit seek to the beginning was lost", reloaded.PositionMs, reloaded.PositionContentVersion)
	}
}
