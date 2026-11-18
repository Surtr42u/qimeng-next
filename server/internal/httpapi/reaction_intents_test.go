package httpapi_test

import (
	"testing"
	"time"

	"qimeng.local/next/server/internal/model"
)

func TestIndependentFavoriteAndDailyVoteKeepHistoryAcrossStaleViewsAndReplays(t *testing.T) {
	f := setup(t)
	upload, _ := f.upload("intent-test.png", 81, nil)
	asset := *f.complete(upload).Asset
	day := time.Now().AddDate(0, 0, -1).Format(time.DateOnly)
	seedRecordedLikes(t, f, asset.ID, 3, day)
	root := "/assets/" + asset.ID
	read := func() model.Asset {
		var current model.Asset
		decode(t, f.json("GET", root, "", nil, 200), &current)
		return current
	}
	a, b := read(), read()
	if a.Favorite || b.Liked || b.LikeCount != 4 {
		t.Fatal("expected two unrefreshed views with four historical votes")
	}
	f.json("PUT", root+"/favorite", "first-page-favorite", map[string]bool{"favorite": !a.Favorite}, 200)
	vote := map[string]bool{"liked": !b.Liked}
	first := f.json("PUT", root+"/like/today", "second-page-vote", vote, 200)
	second := f.json("PUT", root+"/like/today", "second-page-vote", vote, 200)
	if first.Body.String() != second.Body.String() {
		t.Fatal("a lost response was not replayed")
	}
	rated := read()
	if !rated.Favorite || !rated.Liked || rated.LikeCount != 5 || rated.UnattributedLikeCount != 3 {
		t.Fatal("an independent intent changed the other field or its history")
	}
	f.json("PUT", root+"/reaction", "legacy-ambiguous-stale", map[string]bool{"favorite": false, "liked": true}, 409)
	f.json("PUT", root+"/like/today", "second-page-vote", map[string]bool{"liked": false}, 409)
	f.json("PUT", root+"/like/today", "cancel-today-vote", map[string]bool{"liked": false}, 200)
	f.json("PUT", root+"/like/today", "second-page-vote", vote, 200)
	f.json("PUT", root+"/like/today", "cancel-today-vote", map[string]bool{"liked": false}, 200)
	f.json("PUT", root+"/favorite", "favorite-same-value", map[string]bool{"favorite": true}, 200)
	undone := read()
	if !undone.Favorite || undone.Liked || undone.LikeCount != 4 || undone.UnattributedLikeCount != 3 ||
		undone.Revision != rated.Revision+1 || undone.FavoritedAt == nil || !undone.FavoritedAt.Equal(*rated.FavoritedAt) {
		t.Fatal("cancelling today's vote changed favorite, historical votes or replay state")
	}
	var earlier int
	if err := f.s.DB.DB.QueryRow("SELECT count(*) FROM likes WHERE asset_id=? AND day=?", asset.ID, day).Scan(&earlier); err != nil || earlier != 1 {
		t.Fatal("the earlier dated vote must survive cancellation", err)
	}
	for _, suffix := range []string{"/favorite", "/like/today"} {
		f.json("PUT", root+suffix, "missing-intent-value", map[string]any{}, 400)
		f.json("PUT", root+suffix, "extra-intent-field", map[string]bool{"favorite": false, "liked": true}, 400)
	}
	f.json("PUT", root+"/like/today", "null-is-not-false", map[string]any{"liked": nil}, 400)
	f.json("PUT", root+"/favorite", "null-is-not-false", map[string]any{"favorite": nil}, 400)
}
