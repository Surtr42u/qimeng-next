package activity_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"qimeng.local/next/server/internal/httpapi"
	"qimeng.local/next/server/internal/model"
	"qimeng.local/next/server/internal/service"
)

const fixtureToken = "synthetic-cold-activity-test-owner-credential"

func get[T any](t *testing.T, handler http.Handler, path string) T {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, "/api/v1"+path, nil)
	request.Header.Set("Authorization", "Bearer "+fixtureToken)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("GET %s: %d %s", path, response.Code, response.Body.String())
	}
	var result T
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func TestSealedActivityThroughCurrentNextHTTP(t *testing.T) {
	source, id := syntheticSource(t, "")
	root, target := syntheticTarget(t)
	if err := transfer(t.Context(), source, target, id, sourceLocation(t)); err != nil {
		t.Fatal(err)
	}
	if err := target.Close(); err != nil {
		t.Fatal(err)
	}
	s, err := service.New(service.Config{DataDir: root, AdminToken: fixtureToken})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	handler := httpapi.New(s)

	clip := get[model.Asset](t, handler, "/assets/clip-a")
	if clip.ID != "clip-a" || clip.ViewCount != 4 || clip.PlayCount != 1 || clip.LikeCount != 2 || clip.UnattributedLikeCount != 0 || !clip.Favorite {
		t.Fatalf("asset activity: %+v", clip)
	}
	if clip.FavoritedAt == nil || clip.FavoritedAt.UTC().Format(time.RFC3339Nano) != "2025-03-09T05:00:00.123Z" ||
		clip.ContentVersion != 1 || clip.PositionContentVersion != 0 || clip.PositionMs != 0 {
		t.Fatalf("favorite time or unproven position: %+v", clip)
	}
	marks := get[model.Page[service.TimelineMark]](t, handler, "/assets/clip-a/timeline")
	if len(marks.Items) != 2 || marks.Items[0].ID != "mark-first" || marks.Items[1].ID != "mark-second" {
		t.Fatalf("marks merged by position or label: %+v", marks)
	}
	for _, mark := range marks.Items {
		if mark.AssetID != clip.ID || mark.PositionMs != 1500 || mark.Label != "chapter" || mark.Color != "#bBcCdD" || mark.ContentVersion != 0 {
			t.Fatalf("mark mapped to the wrong content: %+v", mark)
		}
	}
	if marks.Items[0].CreatedAt.UTC().Format(time.RFC3339Nano) != "2025-02-02T01:01:01.111Z" {
		t.Fatal("missing original mark creation time")
	}
	otherMarks := get[model.Page[service.TimelineMark]](t, handler, "/assets/clip-b/timeline")
	if len(otherMarks.Items) != 1 || otherMarks.Items[0].ID != "mark-other" || otherMarks.Items[0].ContentVersion != 0 {
		t.Fatal("another original's same-time mark was rebound")
	}

	overview := get[service.Overview](t, handler, "/stats/overview?range=all")
	if overview.Totals != (service.ActivityTotals{Views: 6, Plays: 1, Seconds: 27}) || overview.Inventory["events"] != 13 ||
		overview.Inventory["assets"] != 3 || overview.ViewedAssets != 2 || overview.ActiveDays != 3 {
		t.Fatalf("global activity did not conserve the orphan: %+v", overview)
	}
	review := get[service.Review](t, handler, "/stats/review?range=all")
	if review.ViewCount != 5 || review.DistinctAssets != 2 || review.FirstViews != 2 || review.ReturnViews != 3 ||
		len(review.Hours) != 24 || review.Hours[23].Views != 1 || review.Hours[0].Views != 4 ||
		review.Hours[3].Seconds != 27 || review.Hours[2].Seconds != 0 || review.VideoSeconds != 27 {
		t.Fatalf("source calendar and DST review: %+v", review)
	}
	history := get[model.Page[model.Event]](t, handler, "/history")
	if len(history.Items) != 2 || history.TotalMatched == nil || *history.TotalMatched != 2 {
		t.Fatalf("history includes a missing file or omitted a live one: %+v", history)
	}
	for _, event := range history.Items {
		if event.ContentVersion != 0 || event.Asset == nil || event.Asset.ID != event.AssetID || event.Asset.ContentVersion != 1 ||
			event.CreatedAt.UTC().Format(time.RFC3339Nano) != "2025-03-09T05:00:00Z" {
			t.Fatalf("latest historical open: %+v", event)
		}
	}
	ranks := get[model.Page[service.RankedAsset]](t, handler, "/rankings?period=all")
	if len(ranks.Items) != 3 || ranks.Items[0].Asset.ID != clip.ID || ranks.Items[0].Score != 7 {
		t.Fatalf("lifetime views + plays + known votes: %+v", ranks)
	}
	recommendation := get[service.RecommendationPage](t, handler, "/recommendations?kind=regular&mediaType=video&seed=7531&limit=1")
	if len(recommendation.Items) != 1 || recommendation.Items[0].ID != "clip-b" {
		t.Fatalf("exposure penalty missing from the live recommendation: %+v", recommendation)
	}
	checkValue(t, s.DB.DB, 1, "SELECT count FROM recommendation_shown WHERE asset_id='clip-b' AND day=?", time.Now().Format(time.DateOnly))
}
