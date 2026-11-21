package httpapi_test

import (
	"encoding/json"
	"testing"
	"time"

	"qimeng.local/next/server/internal/model"
)

// TestHistoryLatestHandlesWholeAndFractionalTimestamps verifies the
// created_at||char(31)||event_id comparison after qimeng_time_key has made both
// whole-second and fractional inputs fixed width. The later fractional event
// must win for one asset while a second asset keeps its own latest view.
func TestHistoryLatestHandlesWholeAndFractionalTimestamps(t *testing.T) {
	f := setup(t)
	u, _ := f.upload("history-width-mixed.png", 211, nil)
	mixed := *f.complete(u).Asset
	u, _ = f.upload("history-width-whole.png", 212, nil)
	whole := *f.complete(u).Asset

	base := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	insert := func(asset model.Asset, eventID string, at time.Time) {
		stamp := at.Format(time.RFC3339Nano)
		raw, e := json.Marshal(map[string]any{
			"eventId":        eventID,
			"assetId":        asset.ID,
			"fileName":       asset.FileName,
			"kind":           "view",
			"sessionId":      "history-width-" + eventID,
			"contentVersion": asset.ContentVersion,
			"occurredAt":     stamp,
			"createdAt":      stamp,
		})
		if e != nil {
			t.Fatal(e)
		}
		if _, e = f.s.DB.DB.Exec(`INSERT INTO events(event_id,asset_id,created_at,data) VALUES(?,?,qimeng_time_key(?),?)`, eventID, asset.ID, stamp, string(raw)); e != nil {
			t.Fatal(e)
		}
	}
	insert(mixed, "history-width-whole-event", base)
	insert(mixed, "history-width-fraction-event", base.Add(500*time.Millisecond))
	insert(whole, "history-width-later-event", base.Add(2*time.Second))

	page := historyPage(t, f, "?limit=10")
	if len(page.Items) != 2 || page.TotalMatched == nil || *page.TotalMatched != 2 {
		t.Fatalf("history width page = %+v", page)
	}
	latest := map[string]string{}
	for _, item := range page.Items {
		latest[item.AssetID] = item.EventID
	}
	if latest[mixed.ID] != "history-width-fraction-event" {
		t.Fatalf("latest event for mixed-width asset = %q", latest[mixed.ID])
	}
	if latest[whole.ID] != "history-width-later-event" {
		t.Fatalf("latest event for whole-second asset = %q", latest[whole.ID])
	}
	if page.Items[0].AssetID != whole.ID || page.Items[1].AssetID != mixed.ID {
		t.Fatalf("history order = %s,%s", page.Items[0].AssetID, page.Items[1].AssetID)
	}
}
