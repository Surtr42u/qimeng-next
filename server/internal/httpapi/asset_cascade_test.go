package httpapi_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"qimeng.local/next/server/internal/model"
	"qimeng.local/next/server/internal/store"
)

func TestExplicitTagRemovalAndAssetPurgeRespectFactOwnership(t *testing.T) {
	f := setup(t)
	var c model.Collection
	decode(t, f.json("POST", "/collections", "create-cascade-group", map[string]string{"libraryId": f.library,
		"kind": "image_set", "name": "Cascade group"}, 201), &c)
	firstUpload, _ := f.upload("cascade-first.png", 81, map[string]any{"collectionId": c.ID})
	first := *f.complete(firstUpload).Asset
	secondUpload, original := f.upload("cascade-second.png", 82, map[string]any{"collectionId": c.ID})
	second := *f.complete(secondUpload).Asset
	// Start with machine-supplied associations so that only the later human
	// removal, not a prior whole-set editor, can create its protection record.
	ctx := context.Background()
	db := f.s.DB.DB
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	for _, a := range []*model.Asset{&first, &second} {
		a.Tags, a.TagSetSource = []string{"machine-tag"}, "ai"
		a.UpdatedAt = time.Now().UTC()
		a.TagSetUpdatedAt = &a.UpdatedAt
		a.TagsRevision++
		a.Revision++
		if err = store.SaveAssetTags(ctx, tx, *a); err != nil {
			t.Fatal(err)
		}
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	base := "/assets/" + first.ID
	f.json("DELETE", fmt.Sprintf("%s/tags/machine-tag?expectedRevision=%d", base, first.TagsRevision), "remove-single-native-tag", struct{}{}, 200)
	f.json("DELETE", "/tags/machine-tag?expectedRevision=1", "remove-global-native-tag", struct{}{}, 204)
	for _, id := range []string{first.ID, second.ID} {
		var n int
		if err = db.QueryRow("SELECT count(*) FROM asset_human_fields WHERE asset_id=? AND field='tags'", id).Scan(&n); err != nil || n != 1 {
			t.Fatal("explicit human removal lost its protection record", id, n, err)
		}
		var read model.Asset
		decode(t, f.json("GET", "/assets/"+id, "", nil, 200), &read)
		if len(read.Tags) != 0 || read.TagSetSource != "human" || read.TagSetUpdatedAt == nil {
			t.Fatal("explicit empty set lost its source or time", id, read.Tags)
		}
	}
	replace, _ := f.upload("cascade-replace.png", 83, map[string]any{"replaceAssetId": first.ID, "expectedContentVersion": 1})
	a := *f.complete(replace).Asset
	f.json("POST", base+"/events", "", map[string]any{"eventId": "cascade-historical-view", "kind": "view", "contentVersion": a.ContentVersion}, 200)
	f.json("DELETE", fmt.Sprintf("/trash/%s?expectedRevision=%d", a.ID, a.Revision), "cannot-purge-live", struct{}{}, 409)
	decode(t, f.json("DELETE", fmt.Sprintf("%s?expectedRevision=%d", base, a.Revision), "trash-before-native-purge", struct{}{}, 200), &a)
	var versions, memberships int
	if err = db.QueryRow(`SELECT (SELECT count(*) FROM asset_content_versions WHERE asset_id=?),
		(SELECT count(*) FROM collection_members WHERE asset_id=?)`, a.ID, a.ID).Scan(&versions, &memberships); err != nil || versions != 2 || memberships != 1 {
		t.Fatal("moving to trash discarded restorable facts", versions, memberships, err)
	}
	f.json("DELETE", fmt.Sprintf("/trash/%s?expectedRevision=%d", a.ID, a.Revision), "purge-native-asset", struct{}{}, 204)
	for _, table := range []string{"asset_content_versions", "asset_locations", "collection_members", "asset_authors", "asset_human_fields"} {
		var n int
		if err = db.QueryRow("SELECT count(*) FROM "+table+" WHERE asset_id=?", a.ID).Scan(&n); err != nil || n != 0 {
			t.Fatal("permanently removed asset left a dependent fact", table, n, err)
		}
	}
	var events int
	if err = db.QueryRow("SELECT count(*) FROM events WHERE asset_id=? AND event_id='cascade-historical-view'", a.ID).Scan(&events); err != nil || events != 1 {
		t.Fatal("purge removed an historical event", events, err)
	}
	var sibling model.Asset
	decode(t, f.json("GET", "/assets/"+second.ID, "", nil, 200), &sibling)
	if sibling.CollectionID != c.ID || sibling.Page != 2 {
		t.Fatal("purge renumbered a sibling", sibling.CollectionID, sibling.Page)
	}
	assertNativeOriginal(t, f, sibling.ID, 1, original)
}
