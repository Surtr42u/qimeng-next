package httpapi_test

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"qimeng.local/next/server/internal/model"
	"qimeng.local/next/server/internal/service"
)

func v2Document(t *testing.T, a model.Asset, data map[string]any) []byte {
	t.Helper()
	data["mediaFiles"] = []any{map[string]any{"recordKey": "source-record", "assetId": "original-source-asset", "fileName": a.FileName, "mediaType": a.MediaType, "sizeBytes": a.Size, "tagsUpdatedAtMillis": time.Now().Add(time.Second).UnixMilli()}}
	if _, ok := data["viewEvents"]; !ok {
		data["viewEvents"] = []any{}
	}
	raw, e := json.Marshal(map[string]any{"format": "qimeng_backup", "schemaVersion": 2, "appIdentifier": "synthetic.legacy", "data": data})
	if e != nil {
		t.Fatal(e)
	}
	return raw
}
func stageV2(t *testing.T, f *fixture, key string, raw []byte) service.V2Import {
	t.Helper()
	r := f.request("POST", "/migration/v2/imports?sourceId=synthetic-installation", key, testSecret, raw, map[string]string{"Content-Type": "application/octet-stream"})
	if r.Code != 201 {
		t.Fatalf("stage: %d %s", r.Code, r.Body.String())
	}
	var input service.V2Import
	decode(t, r, &input)
	return input
}
func planV2(t *testing.T, f *fixture, input service.V2Import, key string) service.V2MigrationPlan {
	t.Helper()
	r := f.json("POST", "/migration/v2/imports/"+input.ID+"/plans", key, service.V2PlanCreate{}, 201)
	var plan service.V2MigrationPlan
	decode(t, r, &plan)
	return plan
}

func TestV2PlanIsReadOnlyAndCommitPreservesIdentitiesAndEmptyTags(t *testing.T) {
	f := setup(t)
	u, _ := f.upload("migrate.png", 71, nil)
	a := *f.complete(u).Asset
	r := f.json("PUT", "/assets/"+a.ID+"/tags", "v2-initial-tags", map[string]any{"expectedRevision": a.TagsRevision, "tags": []string{"old-tag"}}, 200)
	decode(t, r, &a)
	sourceProfile := `{"authorId":"source-author","status":"verified","nickname":"Public creator","biography":"Imported public summary","homepageUrl":"https://example.org/creator"}`
	raw := v2Document(t, a, map[string]any{
		"authors":         []any{map[string]any{"authorId": "source-author", "displayName": "Synthetic creator", "profileJson": sourceProfile}},
		"authorMediaRefs": []any{map[string]any{"authorId": "source-author", "recordKey": "source-record", "fileName": a.FileName, "origin": "client", "createdAtMillis": int64(1700000000000)}},
		"mediaTagRefs":    []any{}, "tags": []any{map[string]any{"name": "unused-vocabulary"}}, "favorites": []string{"source-record"}, "followedAuthorIds": []string{"source-author"},
		"likes":        []any{map[string]any{"recordKey": "source-record", "likeCount": 4, "lastLikeDate": "2025-10-10"}},
		"viewEvents":   []any{map[string]any{"eventId": "legacy:portable-original", "recordKey": "source-record", "kind": "open", "sessionId": "legacy-session", "startedAt": "2025-10-10T01:02:03.000Z", "seconds": nil, "day": "2025-10-10"}, map[string]any{"eventId": "portable:original-dwell", "recordKey": "source-record", "kind": "dwell", "sessionId": "legacy-session", "startedAt": "2025-10-10T01:02:16.000Z", "seconds": 13}},
		"mediaStats":   []any{map[string]any{"recordKey": "source-record", "viewCount": 999}},
		"txtFragments": []any{map[string]any{"filename": "synthetic.txt", "content": "原始片段\r\n内容不改写\n", "importedAtMillis": int64(1700000000000)}},
	})
	input := stageV2(t, f, "stage-v2-original", raw)
	if input.SourceHashesAvailable {
		t.Fatal("v2 claimed nonexistent source hashes")
	}
	plan := planV2(t, f, input, "plan-v2-original")
	if plan.MatchedCount != 1 || plan.SkippedCount != 0 || plan.RequiresReview == nil || !*plan.RequiresReview {
		t.Fatalf("plan: %+v", plan)
	}
	r = f.json("GET", "/assets/"+a.ID, "", nil, 200)
	var before model.Asset
	decode(t, r, &before)
	if len(before.Tags) != 1 || before.AuthorID != "unknown" {
		t.Fatal("read-only plan changed metadata")
	}
	r = f.json("GET", "/migration/v2/plans/"+plan.ID+"/items", "", nil, 200)
	var items model.Page[service.V2MigrationItem]
	decode(t, r, &items)
	if items.Items[0].TagAction != "apply" {
		t.Fatal("newer explicit empty tag set was not planned")
	}
	r = f.json("POST", "/migration/v2/plans/"+plan.ID+"/commit", "commit-v2-original", map[string]any{"fingerprint": plan.Fingerprint, "acceptSkipped": false}, 200)
	decode(t, r, &plan)
	if plan.State != "committed" || plan.CheckpointID == "" || plan.Counts["eventsImported"] != 2 || plan.Counts["profileBlocksRestored"] < 1 {
		t.Fatalf("commit: %+v", plan)
	}
	r = f.json("GET", "/assets/"+a.ID, "", nil, 200)
	var after model.Asset
	decode(t, r, &after)
	if after.ID != a.ID || after.Hash != a.Hash || len(after.Tags) != 0 || after.AuthorID == "unknown" || !after.Favorite || after.LikeCount != 4 || after.UnattributedLikeCount != 3 {
		t.Fatalf("lost imported metadata or asset identity: %+v", after)
	}
	var views, seconds int64
	if e := f.s.DB.DB.QueryRow("SELECT sum(views),sum(seconds) FROM asset_daily_stats WHERE asset_id=?", a.ID).Scan(&views, &seconds); e != nil {
		t.Fatal(e)
	}
	if views != 1 || seconds != 13 {
		t.Fatalf("aggregate replay: views=%d seconds=%d", views, seconds)
	}
	var version int64
	if e := f.s.DB.DB.QueryRow("SELECT json_extract(data,'$.contentVersion') FROM events WHERE event_id='legacy:portable-original'").Scan(&version); e != nil || version != 0 {
		t.Fatal("legacy event invented a current content version")
	}
	var original string
	if e := f.s.DB.DB.QueryRow("SELECT original FROM migration_event_origins WHERE event_id='legacy:portable-original'").Scan(&original); e != nil {
		t.Fatal(e)
	}
	var event map[string]any
	if e := json.Unmarshal([]byte(original), &event); e != nil {
		t.Fatal(e)
	}
	if event["seconds"] != nil || event["sessionId"] != "legacy-session" || event["day"] != "2025-10-10" {
		t.Fatal("original event facts were rewritten")
	}
	r = f.request("GET", "/migration/v2/imports/"+input.ID+"/document", "", testSecret, nil, nil)
	if r.Code != 200 || !bytes.Equal(raw, r.Body.Bytes()) {
		t.Fatal("raw input bytes were not conserved")
	}
	if _, e := f.s.Root.Stat("backups/" + plan.CheckpointID + "/snapshot.sqlite"); e != nil {
		t.Fatal("pre-import checkpoint missing", e)
	}
	repeated := planV2(t, f, input, "plan-v2-repeat")
	r = f.json("POST", "/migration/v2/plans/"+repeated.ID+"/commit", "commit-v2-repeat", map[string]any{"fingerprint": repeated.Fingerprint, "acceptSkipped": false}, 200)
	decode(t, r, &repeated)
	if repeated.Counts["eventsImported"] != 0 || repeated.Counts["eventsAlreadyPresent"] != 2 {
		t.Fatal("repeated source created new historical identities")
	}
}

func TestV2EventConflictRollsBackAllBusinessChanges(t *testing.T) {
	f := setup(t)
	u, _ := f.upload("atomic-import.png", 74, nil)
	a := *f.complete(u).Asset
	f.json("POST", "/assets/"+a.ID+"/events", "existing-event-operation", map[string]any{"eventId": "shared-event-identity", "kind": "view", "sessionId": "same-source", "contentVersion": a.ContentVersion, "occurredAt": "2025-10-10T00:00:00Z"}, 200)
	raw := v2Document(t, a, map[string]any{"authors": []any{map[string]any{"authorId": "must-not-survive", "displayName": "Rolled-back author"}}, "authorMediaRefs": []any{map[string]any{"authorId": "must-not-survive", "recordKey": "source-record", "fileName": a.FileName}}, "mediaTagRefs": []any{map[string]any{"recordKey": "source-record", "tagName": "must-rollback"}}, "viewEvents": []any{map[string]any{"eventId": "shared-event-identity", "recordKey": "source-record", "kind": "open", "sessionId": "same-source", "startedAt": "2025-10-11T00:00:00Z", "seconds": nil}}})
	input := stageV2(t, f, "stage-v2-conflict", raw)
	plan := planV2(t, f, input, "plan-v2-conflict")
	f.json("POST", "/migration/v2/plans/"+plan.ID+"/commit", "commit-v2-conflict", map[string]any{"fingerprint": plan.Fingerprint, "acceptSkipped": false}, 409)
	var count int
	if e := f.s.DB.DB.QueryRow("SELECT count(*) FROM authors WHERE name='Rolled-back author'").Scan(&count); e != nil || count != 0 {
		t.Fatal("author survived failed atomic import")
	}
	r := f.json("GET", "/assets/"+a.ID, "", nil, 200)
	var current model.Asset
	decode(t, r, &current)
	if len(current.Tags) != 0 || current.AuthorID != "unknown" || current.Revision != a.Revision {
		t.Fatal("failed import partially changed metadata")
	}
}

func TestV2PlanRejectsChangedTargetsAndMultiSourceAmbiguity(t *testing.T) {
	f := setup(t)
	u, _ := f.upload("versioned-import.png", 76, nil)
	a := *f.complete(u).Asset
	input := stageV2(t, f, "stage-v2-stale", v2Document(t, a, map[string]any{}))
	plan := planV2(t, f, input, "plan-v2-stale")
	f.json("PATCH", "/assets/"+a.ID, "change-after-v2-plan", map[string]any{"expectedRevision": a.Revision, "note": "Changed independently"}, 200)
	f.json("POST", "/migration/v2/plans/"+plan.ID+"/commit", "commit-v2-stale", map[string]any{"fingerprint": plan.Fingerprint, "acceptSkipped": false}, 409)
	var document map[string]any
	if e := json.Unmarshal(v2Document(t, a, map[string]any{}), &document); e != nil {
		t.Fatal(e)
	}
	data := document["data"].(map[string]any)
	media := data["mediaFiles"].([]any)
	second := map[string]any{}
	for key, value := range media[0].(map[string]any) {
		second[key] = value
	}
	second["recordKey"] = "other-source-record"
	second["assetId"] = "other-source-asset"
	data["mediaFiles"] = append(media, second)
	raw, _ := json.Marshal(document)
	input = stageV2(t, f, "stage-v2-multiple", raw)
	plan = planV2(t, f, input, "plan-v2-multiple")
	if plan.MatchedCount != 0 || plan.SkippedCount != 2 {
		t.Fatal("multiple legacy records were silently folded into one asset")
	}
	f.json("POST", "/migration/v2/plans/"+plan.ID+"/commit", "commit-v2-unaccepted", map[string]any{"fingerprint": plan.Fingerprint, "acceptSkipped": false}, 409)
	r := f.json("GET", "/migration/v2/plans/"+plan.ID+"/items?limit=1", "", nil, 200)
	var page model.Page[service.V2MigrationItem]
	decode(t, r, &page)
	if page.NextCursor == "" || page.Items[0].Reason != "multiple_sources_one_target" {
		t.Fatal("ambiguous record report missing")
	}
	r = f.json("GET", "/migration/v2/plans/"+plan.ID+"/items?limit=1&cursor="+page.NextCursor, "", nil, 200)
	decode(t, r, &page)
	if len(page.Items) != 1 || page.Items[0].Ordinal != 1 {
		t.Fatal("stable plan cursor lost a source record")
	}
}
