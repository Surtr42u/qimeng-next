package httpapi_test

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"qimeng.local/next/server/internal/model"
	"qimeng.local/next/server/internal/service"
)

func TestCollectionMemberOrderAndRenameDoNotWriteAssetFacts(t *testing.T) {
	f := setup(t)
	var c model.Collection
	decode(t, f.json("POST", "/collections", "create-member-group", map[string]string{
		"libraryId": f.library, "kind": "image_set", "name": "Members"}, 201), &c)
	aUpload, original := f.upload("member-a.png", 78, map[string]any{"collectionId": c.ID})
	first := *f.complete(aUpload).Asset
	bUpload, _ := f.upload("member-b.png", 79, map[string]any{"collectionId": c.ID})
	second := *f.complete(bUpload).Asset
	f.json("PUT", "/assets/"+first.ID+"/tags", "tag-member-file", map[string]any{"expectedRevision": first.TagsRevision, "tags": []string{"keep"}}, 200)
	db := f.s.DB.DB
	for _, table := range []string{"asset_records", "asset_locations", "asset_content_versions", "asset_tags", "asset_authors"} {
		for _, op := range []string{"INSERT", "UPDATE", "DELETE"} {
			query := fmt.Sprintf("CREATE TRIGGER order_guard_%s_%s BEFORE %s ON %s BEGIN SELECT RAISE(ABORT,'collection wrote to asset'); END",
				table, strings.ToLower(op), op, table)
			if _, err := db.Exec(query); err != nil {
				t.Fatal(err)
			}
		}
	}
	base := "/collections/" + c.ID
	var renamed model.Collection
	decode(t, f.json("PATCH", base, "rename-member-group", map[string]any{"expectedRevision": c.Revision, "name": "Renamed group"}, 200), &renamed)
	if renamed.ID != c.ID || renamed.Name != "Renamed group" || renamed.Revision != c.Revision+1 {
		t.Fatal("collection identity or revision changed unexpectedly", renamed)
	}
	f.json("PATCH", base, "stale-rename-group", map[string]any{"expectedRevision": c.Revision, "name": "Stale"}, 409)
	route := func(id string) string { return base + "/members/" + id + "/position" }
	if _, err := db.Exec(`INSERT INTO file_intents(id,kind,state,data) VALUES('pending-member-publication','ingest','prepared',
		json_object('After',json_object('id','upcoming','collectionId',?,'page',6)))`, c.ID); err != nil {
		t.Fatal(err)
	}
	f.json("PUT", route(first.ID), "pending-member-position", map[string]int{"expectedRevision": 1, "position": 6}, 409)
	if _, err := db.Exec("DELETE FROM file_intents WHERE id='pending-member-publication'"); err != nil {
		t.Fatal(err)
	}
	f.json("PUT", route(first.ID), "occupied-member-position", map[string]int{"expectedRevision": 1, "position": 2}, 409)
	var moved service.CollectionMember
	move := map[string]int{"expectedRevision": 1, "position": 7}
	decode(t, f.json("PUT", route(first.ID), "move-member-position", move, 200), &moved)
	f.json("PUT", route(first.ID), "move-member-position", move, 200)
	if moved.AssetID != first.ID || moved.Revision != 2 || moved.Position == nil || *moved.Position != 7 {
		t.Fatal("member position or revision lost", moved)
	}
	var page model.Page[service.CollectionMember]
	decode(t, f.json("GET", base+"/members?limit=1", "", nil, 200), &page)
	if len(page.Items) != 1 || page.Items[0].AssetID != second.ID || page.NextCursor == "" {
		t.Fatal("members not ordered by their recorded positions", page)
	}
	var last model.Page[service.CollectionMember]
	decode(t, f.json("GET", base+"/members?limit=1&cursor="+url.QueryEscape(page.NextCursor), "", nil, 200), &last)
	if len(last.Items) != 1 || last.Items[0].AssetID != first.ID || last.Items[0].Position == nil || *last.Items[0].Position != 7 {
		t.Fatal("pagination lost the member which changed position", last)
	}
	var asset model.Asset
	decode(t, f.json("GET", "/assets/"+first.ID, "", nil, 200), &asset)
	if asset.CollectionID != c.ID || asset.Page != 7 || asset.RelativePath != first.RelativePath || asset.ContentVersion != first.ContentVersion ||
		len(asset.Tags) != 1 || asset.Tags[0] != "keep" {
		t.Fatal("editing the member touched its original or tags", asset.RelativePath, asset.Page, asset.Tags)
	}
	assertNativeOriginal(t, f, first.ID, 1, original)
	type attempt struct {
		id       string
		revision int
	}
	replies := make(chan *httptest.ResponseRecorder, 2)
	start := make(chan struct{})
	for i, entry := range []attempt{{first.ID, 2}, {second.ID, 1}} {
		go func() {
			<-start
			raw, _ := json.Marshal(map[string]int{"expectedRevision": entry.revision, "position": 9})
			replies <- f.request("PUT", route(entry.id), fmt.Sprintf("claim-member-slot-%d", i), testSecret, raw, nil)
		}()
	}
	close(start)
	var successes, conflicts int
	for range 2 {
		reply := <-replies
		switch {
		case reply.Code == 200:
			successes++
		case reply.Code == 409 && strings.Contains(reply.Body.String(), "collection_position_occupied"):
			conflicts++
		default:
			t.Fatal(reply.Code, reply.Body.String())
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatal("one free slot had multiple winners", successes, conflicts)
	}
	t.Log("collection rename and position edits wrote zero asset, content, location, author or tag rows; one of two simultaneous slot claims succeeded")
}

func TestShortDramaRenameKeepsPublishedEpisodeAndUsesCurrentSeriesForStagedVideo(t *testing.T) {
	firstVideo, secondVideo := makeNativeVideo(t, "80x48"), makeNativeVideo(t, "112x64")
	f := setup(t)
	var c model.Collection
	decode(t, f.json("POST", "/collections", "create-native-drama", map[string]string{
		"libraryId": f.library, "kind": "short_drama", "name": "Original series"}, 201), &c)
	stage := func(name string, body []byte) model.Upload {
		t.Helper()
		var u model.Upload
		decode(t, f.json("POST", "/uploads", "create-drama-"+name, map[string]any{"libraryId": f.library, "collectionId": c.ID,
			"fileName": name, "mimeType": "video/mp4", "size": len(body), "mode": "ordinary", "note": "Original note"}, 201), &u)
		r := f.request("PUT", "/uploads/"+u.ID+"/content", "", testSecret, body, map[string]string{"Content-Type": "application/octet-stream", "Upload-Offset": "0"})
		if r.Code != 200 {
			t.Fatal("stage drama episode", r.Code, r.Body.String())
		}
		return u
	}
	first := *f.complete(stage("episode-a.mp4", firstVideo)).Asset
	pending := stage("episode-b.mp4", secondVideo)
	base := "/collections/" + c.ID
	f.json("PUT", base+"/members/"+first.ID+"/position", "edit-drama-ordinal", map[string]int{"position": 4, "expectedRevision": 1}, 200)
	f.json("PATCH", base, "rename-native-drama", map[string]any{"name": "Renamed series", "expectedRevision": c.Revision}, 200)
	second := *f.complete(pending).Asset
	var read model.Asset
	decode(t, f.json("GET", "/assets/"+first.ID, "", nil, 200), &read)
	if read.CollectionID != c.ID || read.Page != 4 || read.RelativePath != first.RelativePath || read.Note != "Original note" ||
		second.CollectionID != c.ID || second.Page != 5 || !strings.HasPrefix(second.RelativePath, "Renamed series/") {
		t.Fatal("episode order or published file was derived from a renamed collection", read.Page, read.RelativePath, second.Page, second.RelativePath)
	}
	assertNativeOriginal(t, f, first.ID, 1, firstVideo)
	assertNativeOriginal(t, f, second.ID, 1, secondVideo)
}
