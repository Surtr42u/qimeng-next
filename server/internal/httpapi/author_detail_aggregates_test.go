package httpapi_test

import (
	"fmt"
	"strconv"
	"testing"

	"qimeng.local/next/server/internal/model"
)

// TestAuthorDetailDerivesCountsAndTextOrder covers the single-author query:
// live assets count, trashed assets do not, and txtOrder comes from the
// earliest (list position, ordinal) membership across every list.
func TestAuthorDetailDerivesCountsAndTextOrder(t *testing.T) {
	f := setup(t)
	listed := createAuthor(t, f, "Detail listed")
	trashed := createAuthor(t, f, "Detail trashed")
	unlisted := createAuthor(t, f, "Detail unlisted")

	u, _ := f.upload("author-detail-live.png", 201, map[string]any{"authorIds": []string{listed.ID}})
	live := *f.complete(u).Asset
	for index := 0; index < 2; index++ {
		f.json("POST", "/assets/"+live.ID+"/events", "", map[string]any{
			"eventId":        fmt.Sprintf("author-detail-view-%d", index),
			"sessionId":      fmt.Sprintf("author-detail-session-%d", index),
			"kind":           "view",
			"contentVersion": live.ContentVersion,
		}, 200)
	}
	u, _ = f.upload("author-detail-trashed.png", 202, map[string]any{"authorIds": []string{trashed.ID}})
	trashedAsset := *f.complete(u).Asset
	f.json("POST", "/assets/"+trashedAsset.ID+"/events", "", map[string]any{
		"eventId":        "author-detail-trashed-view",
		"sessionId":      "author-detail-trashed-session",
		"kind":           "view",
		"contentVersion": trashedAsset.ContentVersion,
	}, 200)
	f.json("DELETE", "/assets/"+trashedAsset.ID+"?expectedRevision="+strconv.FormatInt(trashedAsset.Revision, 10), "author-detail-trash-asset", struct{}{}, 200)

	if _, e := f.s.DB.DB.Exec(`INSERT INTO author_lists(id,name,position,revision,data) SELECT 'detail-list-one','Detail list one',COALESCE(MAX(position),0)+1,1,'{}' FROM author_lists`); e != nil {
		t.Fatal(e)
	}
	if _, e := f.s.DB.DB.Exec(`INSERT INTO author_lists(id,name,position,revision,data) SELECT 'detail-list-two','Detail list two',COALESCE(MAX(position),0)+1,1,'{}' FROM author_lists`); e != nil {
		t.Fatal(e)
	}
	for _, member := range []struct {
		list    string
		ordinal int
		author  string
	}{
		{"detail-list-one", 1, trashed.ID},
		{"detail-list-one", 2, listed.ID},
		{"detail-list-two", 1, listed.ID},
	} {
		if _, e := f.s.DB.DB.Exec(`INSERT INTO author_list_members(list_id,ordinal,author_id) VALUES(?,?,?)`, member.list, member.ordinal, member.author); e != nil {
			t.Fatal(e)
		}
	}

	var listedDetail model.Author
	decode(t, f.json("GET", "/authors/"+listed.ID, "", nil, 200), &listedDetail)
	if listedDetail.FileCount != 1 || listedDetail.ViewCount != 2 || listedDetail.TXTOrder == nil || *listedDetail.TXTOrder != 2 {
		t.Fatalf("listed author aggregates = %+v", listedDetail)
	}
	var trashedDetail model.Author
	decode(t, f.json("GET", "/authors/"+trashed.ID, "", nil, 200), &trashedDetail)
	if trashedDetail.FileCount != 0 || trashedDetail.ViewCount != 0 || trashedDetail.TXTOrder == nil || *trashedDetail.TXTOrder != 1 {
		t.Fatalf("trashed author aggregates = %+v", trashedDetail)
	}
	var unlistedDetail model.Author
	decode(t, f.json("GET", "/authors/"+unlisted.ID, "", nil, 200), &unlistedDetail)
	if unlistedDetail.FileCount != 0 || unlistedDetail.ViewCount != 0 || unlistedDetail.TXTOrder != nil {
		t.Fatalf("unlisted author aggregates = %+v", unlistedDetail)
	}
	f.json("GET", "/authors/detail-author-missing", "", nil, 404)
}
