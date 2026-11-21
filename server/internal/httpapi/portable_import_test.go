package httpapi_test

import (
	"encoding/json"
	"testing"
	"time"

	"qimeng.local/next/server/internal/model"
	"qimeng.local/next/server/internal/service"
)

func TestPortableImportEndpointPreviewsAppliesAndDeduplicates(t *testing.T) {
	f := setup(t)
	upload, _ := f.upload("portable.png", 42, nil)
	asset := *f.complete(upload).Asset
	const recordKey = "legacy-record"
	document := map[string]any{
		"format": "qimeng_backup", "schemaVersion": 2, "appIdentifier": "com.qimeng.media",
		"exportedAtMillis": time.Now().UnixMilli(),
		"data": map[string]any{
			"mediaFiles": []any{map[string]any{
				"recordKey": recordKey, "libraryName": "Synthetic", "relPath": asset.RelativePath,
				"fileName": asset.FileName, "mediaType": asset.MediaType, "sizeBytes": asset.Size,
				"modifiedAtMillis": asset.CreatedAt.UnixMilli(),
			}},
			// 真实旧导出的 open 事件没有 seconds 键。
			"viewEvents": []any{map[string]any{
				"eventId": "endpoint-open-event", "recordKey": recordKey, "kind": "open",
				"sessionId": "endpoint-session", "startedAt": "2025-10-10T01:02:03Z", "day": "2025-10-10",
			}},
			"authors":         []any{map[string]any{"authorId": "endpoint-author", "displayName": "接口作者"}},
			"authorMediaRefs": []any{map[string]any{"authorId": "endpoint-author", "recordKey": recordKey}},
			"tags":            []any{map[string]any{"name": "接口标签"}},
			"mediaTagRefs":    []any{map[string]any{"recordKey": recordKey, "tagName": "接口标签"}},
			"likes":           []any{map[string]any{"recordKey": recordKey, "likeCount": 1, "lastLikeDate": "2025-10-10"}},
			"favorites":       []string{recordKey},
			"timelineTags":    []any{},
			"txtFragments":    []any{},
		},
	}
	raw, e := json.Marshal(document)
	if e != nil {
		t.Fatal(e)
	}

	preview := f.request("POST", "/migration/portable/imports?dryRun=true", "", testSecret, raw, nil)
	if preview.Code != 200 {
		t.Fatalf("preview: %d %s", preview.Code, preview.Body.String())
	}
	var report service.PortableImportReport
	decode(t, preview, &report)
	if !report.DryRun || report.Media.Matched != 1 || report.Events.Imported != 1 {
		t.Fatalf("preview report: %+v", report)
	}
	var before model.Asset
	decode(t, f.json("GET", "/assets/"+asset.ID, "", nil, 200), &before)
	if before.Favorite || before.AuthorID != "unknown" || len(before.Tags) != 0 {
		t.Fatal("dry run wrote metadata")
	}

	applied := f.request("POST", "/migration/portable/imports", "", testSecret, raw, nil)
	if applied.Code != 200 {
		t.Fatalf("apply: %d %s", applied.Code, applied.Body.String())
	}
	decode(t, applied, &report)
	if report.DryRun || report.AlreadyImported || !report.Changed {
		t.Fatalf("apply report: %+v", report)
	}
	var after model.Asset
	decode(t, f.json("GET", "/assets/"+asset.ID, "", nil, 200), &after)
	if !after.Favorite || after.AuthorID == "unknown" || len(after.Tags) != 1 || after.Tags[0] != "接口标签" {
		t.Fatalf("applied asset: %+v", after)
	}

	replayed := f.request("POST", "/migration/portable/imports", "", testSecret, raw, nil)
	if replayed.Code != 200 {
		t.Fatalf("replay: %d %s", replayed.Code, replayed.Body.String())
	}
	decode(t, replayed, &report)
	if !report.AlreadyImported || report.Changed {
		t.Fatalf("replay report: %+v", report)
	}

	unauthenticated := f.request("POST", "/migration/portable/imports", "", "", raw, nil)
	if unauthenticated.Code != 401 {
		t.Fatalf("unauthenticated: %d", unauthenticated.Code)
	}

	invalid := f.request("POST", "/migration/portable/imports", "", testSecret,
		[]byte(`{"format":"other","schemaVersion":2,"appIdentifier":"com.qimeng.media","data":{"viewEvents":[]}}`), nil)
	if invalid.Code != 400 {
		t.Fatalf("invalid: %d %s", invalid.Code, invalid.Body.String())
	}
	var failure struct {
		Code string `json:"code"`
	}
	decode(t, invalid, &failure)
	if failure.Code != "unsupported_portable_format" {
		t.Fatalf("invalid code: %s", failure.Code)
	}
}
