package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"qimeng.local/next/server/internal/model"
	"qimeng.local/next/server/internal/store"
)

func portableTestService(t *testing.T) *Service {
	t.Helper()
	s, e := New(Config{DataDir: t.TempDir(), AdminToken: "synthetic-portable-owner-credential-000000", ReserveBytes: 1})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func insertPortableLibrary(t *testing.T, s *Service, id, name, kind string) {
	t.Helper()
	now := time.Now().UTC()
	library := model.Library{ID: id, Name: name, Kind: kind, Enabled: true, StorageID: "data", RootRelativePath: "libraries/" + id, Revision: 1, CreatedAt: now}
	raw, e := store.Encode(library)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.DB.DB.Exec("INSERT INTO libraries(id,name,kind,data) VALUES(?,?,?,?)", id, name, kind, raw); e != nil {
		t.Fatal(e)
	}
}

func insertPortableAsset(t *testing.T, s *Service, id, libraryID, fileName, relPath, mediaType string) model.Asset {
	t.Helper()
	now := time.Now().UTC()
	mime := "image/png"
	if mediaType == "video" {
		mime = "video/mp4"
	}
	asset := model.Asset{ID: id, LibraryID: libraryID, FileName: fileName, OriginalFileName: fileName,
		RelativePath: relPath, MediaType: mediaType, MimeType: mime, Size: 10, Hash: strings.Repeat("a", 64),
		Revision: 1, TagsRevision: 1, AuthorsRevision: 1, ContentVersion: 1, AuthorID: "unknown",
		AuthorIDs: []string{"unknown"}, AuthorStatus: "unknown", Tags: []string{},
		CreatedAt: now, UpdatedAt: now, ProbeState: "ok"}
	if e := store.InsertAsset(context.Background(), s.DB.DB, asset); e != nil {
		t.Fatal(e)
	}
	return asset
}

func portableMediaEntry(recordKey, libraryName, relPath, fileName, mediaType, assetID string) map[string]any {
	return map[string]any{"recordKey": recordKey, "libraryName": libraryName, "relPath": relPath,
		"fileName": fileName, "mediaType": mediaType, "sizeBytes": 10,
		"modifiedAtMillis": 1700000000000, "assetId": assetID}
}

func portableTestDocument(t *testing.T, exportedAt int64) []byte {
	t.Helper()
	avatar := base64.StdEncoding.EncodeToString(syntheticPNG(t, 7))
	profile := `{"authorId":"source-a","revision":1,"status":"verified","nickname":"Alice",` +
		`"biography":"Bio","avatarUrl":"https://example.org/a.png","homepageUrl":"https://example.org/a",` +
		`"evidenceUrls":["https://example.org/e"]}`
	data := map[string]any{
		"mediaFiles": []any{
			// 旧导出偶尔带反斜杠；匹配前按 / 归一，空格和大小写保持原样。
			portableMediaEntry("path", "1  图集", "folder\\unique.png", "unique.png", "image", ""),
			portableMediaEntry("id", "1  图集", "stale/id-target.png", "id-target.png", "image", "asset-id"),
			portableMediaEntry("name", "2  cos图集", "stale/only.png", "only.png", "image", ""),
			portableMediaEntry("amb", "1  图集", "missing/same.png", "same.png", "image", ""),
			portableMediaEntry("clip", "1  3D", "clip.mp4", "clip.mp4", "video", ""),
			portableMediaEntry("dup-a", "1  3D", "dup-target.png", "dup-target.png", "image", ""),
			portableMediaEntry("dup-b", "1  3D", "dup-target.png", "dup-target.png", "image", ""),
			portableMediaEntry("missing-lib", "9  不存在", "x.png", "x.png", "image", ""),
			portableMediaEntry("missing-file", "1  图集", "nope.png", "nope.png", "image", ""),
		},
		"viewEvents": []any{
			// 真实导出的 open 事件没有 seconds 键，dwell 事件多数没有 day 键。
			map[string]any{"eventId": "event-open-path", "recordKey": "path", "kind": "open", "sessionId": "s1", "startedAt": "2025-10-10T01:02:03Z", "day": "2025-10-10"},
			map[string]any{"eventId": "event-dwell-clip", "recordKey": "clip", "kind": "dwell", "sessionId": "s2", "startedAt": "2025-10-10T02:00:00Z", "seconds": 13},
			map[string]any{"eventId": "event-open-missing", "recordKey": "missing-lib", "kind": "open", "sessionId": "s3", "startedAt": "2025-10-10T03:00:00Z", "day": "2025-10-10"},
		},
		"authors": []any{
			map[string]any{"authorId": "source-a", "displayName": "测试作者甲", "createdAtMillis": int64(1700000000000), "profileJson": profile, "avatarData": avatar},
			map[string]any{"authorId": "source-b", "displayName": "测试作者乙"},
			map[string]any{"authorId": "cos_test", "displayName": "测试目录"},
			map[string]any{"authorId": "其他搁置", "displayName": "其他搁置"},
		},
		"authorMediaRefs": []any{
			map[string]any{"authorId": "source-a", "recordKey": "path"},
			map[string]any{"authorId": "source-b", "recordKey": "name"},
			map[string]any{"authorId": "cos_test", "recordKey": "name"},
			map[string]any{"authorId": "source-a", "recordKey": "missing-lib"},
			map[string]any{"authorId": "其他搁置", "recordKey": "path"},
		},
		"tags": []any{map[string]any{"name": "旧标签"}, map[string]any{"name": "已删除标签"}},
		"mediaTagRefs": []any{
			map[string]any{"recordKey": "path", "tagName": "旧标签"},
			map[string]any{"recordKey": "name", "tagName": "旧标签"},
			map[string]any{"recordKey": "path", "tagName": "已删除标签"},
			map[string]any{"recordKey": "missing-lib", "tagName": "旧标签"},
		},
		"likes": []any{
			map[string]any{"recordKey": "path", "likeCount": 4, "lastLikeDate": "2025-10-10"},
			map[string]any{"recordKey": "clip", "likeCount": 2},
			map[string]any{"recordKey": "missing-lib", "likeCount": 1, "lastLikeDate": "2025-10-10"},
		},
		"favorites": []string{"path", "missing-lib"},
		"timelineTags": []any{
			map[string]any{"recordKey": "clip", "fileName": "clip.mp4", "timeMillis": 5000, "name": "标记", "createdAtMillis": int64(1700000000000)},
			map[string]any{"recordKey": "path", "fileName": "unique.png", "timeMillis": 1000, "name": "图片标记", "createdAtMillis": int64(1700000000000)},
		},
		"followedAuthorIds": []string{"source-a"},
		"txtFragments": []any{
			map[string]any{"filename": "作者名单.txt", "content": "1  测试作者甲\n作品\nunique.png\n", "importedAtMillis": int64(1700000000000)},
		},
		"appPrefs": map[string]any{"version": 1, "recommendationPrefs": map[string]any{
			"tagRelevance": 0.3, "tagCollection": 0.15, "engagement": 0.1, "recency": 0.15,
			"likeScore": 0.05, "discovery": 0.2, "freshness": 0.05, "browseDepth": 0.03, "maxRandom": 0.3,
		}},
		"mediaStats":      []any{map[string]any{"recordKey": "path", "viewCount": 1}},
		"dailyBrowse":     []any{map[string]any{"recordKey": "path", "dayStartMillis": int64(1), "viewCount": 1}},
		"history":         []any{map[string]any{"recordKey": "path"}, map[string]any{"recordKey": "clip"}},
		"authorMasterTxt": "主表汇总",
		"settings":        []any{},
		"scanSources":     []any{},
		"albumRules":      []any{},
		"cosWorks":        []any{},
	}
	raw, e := json.Marshal(map[string]any{
		"format": "qimeng_backup", "schemaVersion": 2, "appIdentifier": "com.qimeng.media",
		"exportedAtMillis": exportedAt, "data": data,
	})
	if e != nil {
		t.Fatal(e)
	}
	return raw
}

func portableAdmin() model.Principal {
	return model.Principal{ID: "owner", Scopes: []string{"admin"}}
}

func portablePreview(t *testing.T, s *Service, raw []byte) PortableImportReport {
	t.Helper()
	report, e := s.PreviewPortableImport(context.Background(), portableAdmin(), raw)
	if e != nil {
		t.Fatal(e)
	}
	return report
}

func portableApply(t *testing.T, s *Service, raw []byte) PortableImportReport {
	t.Helper()
	out, e := s.Run(context.Background(), portableAdmin(), "portable-import", "", raw, func(w *Write) (any, int, error) {
		report, e := s.ApplyPortableImport(context.Background(), w, raw)
		return report, 200, e
	})
	if e != nil {
		t.Fatal(e)
	}
	var report PortableImportReport
	if e = json.Unmarshal(out.Body, &report); e != nil {
		t.Fatal(e)
	}
	return report
}

func portableCount(t *testing.T, s *Service, query string, args ...any) int {
	t.Helper()
	var value int
	if e := s.DB.DB.QueryRow(query, args...).Scan(&value); e != nil {
		t.Fatal(e)
	}
	return value
}

func assertPortablePreviewCounts(t *testing.T, report PortableImportReport) {
	t.Helper()
	if !report.DryRun || report.AlreadyImported || !report.Changed {
		t.Fatalf("preview flags: %+v", report)
	}
	if report.Media.Total != 9 || report.Media.Matched != 4 || report.Media.MatchedByLibraryPath != 2 ||
		report.Media.MatchedByAssetID != 1 || report.Media.MatchedByFileName != 1 ||
		report.Media.Ambiguous != 1 || report.Media.Unmatched != 2 || report.Media.DuplicateTargets != 2 {
		t.Fatalf("media: %+v", report.Media)
	}
	if len(report.Media.MissingLibraries) != 1 || report.Media.MissingLibraries[0].LibraryName != "9  不存在" ||
		report.Media.MissingLibraries[0].Reason != "library_not_found" || report.Media.MissingLibraries[0].Count != 1 {
		t.Fatalf("missing libraries: %+v", report.Media.MissingLibraries)
	}
	if report.Authors.Total != 4 || report.Authors.Created != 3 || report.Authors.Placeholder != 1 ||
		report.Authors.ProfilesPrepared != 1 || report.Authors.AvatarsPrepared != 1 {
		t.Fatalf("authors: %+v", report.Authors)
	}
	if report.Tags.VocabularyTotal != 2 || report.Tags.VocabularyCreated != 1 ||
		report.Tags.VocabularyUnavailable != 1 || report.Tags.LinksAdded != 2 || report.Tags.LinksSkipped != 2 {
		t.Fatalf("tags: %+v", report.Tags)
	}
	if report.AssetAuthors.Total != 5 || report.AssetAuthors.LinksAdded != 3 ||
		report.AssetAuthors.SkippedUnmatched != 1 || report.AssetAuthors.SkippedPlaceholder != 1 {
		t.Fatalf("author links: %+v", report.AssetAuthors)
	}
	if report.Favorites.Applied != 1 || report.Favorites.SkippedUnmatched != 1 {
		t.Fatalf("favorites: %+v", report.Favorites)
	}
	if report.Likes.Applied != 2 || report.Likes.SkippedUnmatched != 1 {
		t.Fatalf("likes: %+v", report.Likes)
	}
	if report.Events.Total != 3 || report.Events.Imported != 2 || report.Events.SkippedUnmatched != 1 ||
		report.Events.Views != 1 || report.Events.DwellSeconds != 13 {
		t.Fatalf("events: %+v", report.Events)
	}
	if report.Timeline.Total != 2 || report.Timeline.Imported != 1 || report.Timeline.SkippedNotVideo != 1 {
		t.Fatalf("timeline: %+v", report.Timeline)
	}
	if report.Follows.Marked != 1 || report.Txt.FragmentsImported != 1 || !report.Preferences.Imported {
		t.Fatalf("follows/txt/prefs: %+v %+v %+v", report.Follows, report.Txt, report.Preferences)
	}
	if len(report.Skipped) != 4 {
		t.Fatalf("skipped sections: %+v", report.Skipped)
	}
	if len(report.Samples.Ambiguous) != 1 || len(report.Samples.Unmatched) != 2 || len(report.Samples.DuplicateTargets) != 2 {
		t.Fatalf("samples: %+v", report.Samples)
	}
}

func TestPortableImportMatchesByLibraryPathAndIsIdempotent(t *testing.T) {
	s := portableTestService(t)
	insertPortableLibrary(t, s, "lib-main", "1  图集", "regular")
	insertPortableLibrary(t, s, "lib-cos", "2  cos图集", "cos")
	insertPortableLibrary(t, s, "lib-other", "1  3D", "regular")
	insertPortableAsset(t, s, "asset-path", "lib-main", "unique.png", "folder/unique.png", "image")
	insertPortableAsset(t, s, "asset-id", "lib-main", "id-target.png", "id-target.png", "image")
	insertPortableAsset(t, s, "asset-name", "lib-cos", "only.png", "测试目录/only.png", "image")
	insertPortableAsset(t, s, "asset-amb-a", "lib-main", "same.png", "a/same.png", "image")
	insertPortableAsset(t, s, "asset-amb-b", "lib-main", "same.png", "b/same.png", "image")
	insertPortableAsset(t, s, "asset-clip", "lib-other", "clip.mp4", "clip.mp4", "video")
	insertPortableAsset(t, s, "asset-dup", "lib-other", "dup-target.png", "dup-target.png", "image")
	now := dateKey(time.Now().UTC())
	if _, e := s.DB.DB.Exec("INSERT INTO tags(id,name,revision,deleted,created_at,updated_at) VALUES('deleted-tag-id','已删除标签',1,1,?,?)", now, now); e != nil {
		t.Fatal(e)
	}
	raw := portableTestDocument(t, 1700000000000)

	preview := portablePreview(t, s, raw)
	assertPortablePreviewCounts(t, preview)
	// 预览不写任何行。
	if got := portableCount(t, s, "SELECT COUNT(*) FROM authors"); got != 1 {
		t.Fatalf("authors changed during preview: %d", got)
	}
	if got := portableCount(t, s, "SELECT COUNT(*) FROM tags WHERE deleted=0"); got != 0 {
		t.Fatalf("live tags changed during preview: %d", got)
	}
	if got := portableCount(t, s, "SELECT COUNT(*) FROM asset_tags"); got != 0 {
		t.Fatalf("asset tags changed during preview: %d", got)
	}
	if got := portableCount(t, s, "SELECT COUNT(*) FROM asset_authors WHERE author_id!='unknown'"); got != 0 {
		t.Fatalf("author links changed during preview: %d", got)
	}
	if got := portableCount(t, s, "SELECT COUNT(*) FROM events"); got != 0 {
		t.Fatalf("events changed during preview: %d", got)
	}
	if got := portableCount(t, s, "SELECT COUNT(*) FROM likes"); got != 0 {
		t.Fatalf("likes changed during preview: %d", got)
	}
	if got := portableCount(t, s, "SELECT COUNT(*) FROM like_baselines"); got != 0 {
		t.Fatalf("like baselines changed during preview: %d", got)
	}
	if got := portableCount(t, s, "SELECT COUNT(*) FROM timeline_marks"); got != 0 {
		t.Fatalf("timeline changed during preview: %d", got)
	}
	if got := portableCount(t, s, "SELECT COUNT(*) FROM author_txt_fragments"); got != 0 {
		t.Fatalf("txt fragments changed during preview: %d", got)
	}
	if got := portableCount(t, s, "SELECT COUNT(*) FROM author_lists"); got != 0 {
		t.Fatalf("author lists changed during preview: %d", got)
	}
	if got := portableCount(t, s, "SELECT COUNT(*) FROM author_profile_blocks"); got != 0 {
		t.Fatalf("profiles changed during preview: %d", got)
	}
	if got := portableCount(t, s, "SELECT COUNT(*) FROM author_profile_avatars"); got != 0 {
		t.Fatalf("avatars changed during preview: %d", got)
	}
	if got := portableCount(t, s, "SELECT COUNT(*) FROM asset_human_fields"); got != 0 {
		t.Fatalf("human fields changed during preview: %d", got)
	}
	if got := portableCount(t, s, "SELECT COUNT(*) FROM settings WHERE key='recommendation_preferences'"); got != 0 {
		t.Fatalf("preferences changed during preview: %d", got)
	}
	if got := portableCount(t, s, "SELECT COUNT(*) FROM asset_records WHERE favorite=1"); got != 0 {
		t.Fatalf("favorites changed during preview: %d", got)
	}
	if got := portableCount(t, s, "SELECT COUNT(*) FROM migration_relation_origins WHERE kind='portable-v2/import'"); got != 0 {
		t.Fatalf("preview wrote an import receipt: %d", got)
	}

	applied := portableApply(t, s, raw)
	if applied.DryRun || applied.AlreadyImported || !applied.Changed {
		t.Fatalf("applied flags: %+v", applied)
	}
	if applied.Authors.ProfileBlocksRestored != 3 || applied.Authors.ProfileCandidatesCreated != 0 ||
		applied.Authors.AvatarsRestored != 1 || applied.Txt.ListsActivated != 1 {
		t.Fatalf("applied profile/txt: %+v %+v", applied.Authors, applied.Txt)
	}
	if applied.Events.Imported != 2 || applied.Events.Views != 1 || applied.Events.DwellSeconds != 13 ||
		applied.Timeline.Imported != 1 || applied.Follows.Marked != 1 || !applied.Preferences.Imported {
		t.Fatalf("applied records: %+v %+v %+v %+v", applied.Events, applied.Timeline, applied.Follows, applied.Preferences)
	}
	if got := portableCount(t, s, "SELECT COUNT(*) FROM authors"); got != 4 {
		t.Fatalf("authors after apply: %d", got)
	}
	var authorID, authorData string
	if e := s.DB.DB.QueryRow("SELECT id,data FROM authors WHERE name='测试作者甲'").Scan(&authorID, &authorData); e != nil {
		t.Fatal(e)
	}
	var author model.Author
	if e := json.Unmarshal([]byte(authorData), &author); e != nil {
		t.Fatal(e)
	}
	if author.Kind != "regular" || !author.Following {
		t.Fatalf("author: %+v", author)
	}
	if got := portableCount(t, s, "SELECT COUNT(*) FROM author_profile_blocks WHERE author_id=?", authorID); got != 3 {
		t.Fatalf("profile blocks: %d", got)
	}
	if got := portableCount(t, s, "SELECT COUNT(*) FROM author_profile_avatars"); got != 1 {
		t.Fatalf("avatars: %d", got)
	}
	if got := portableCount(t, s, "SELECT COUNT(*) FROM authors WHERE name='测试目录' AND json_extract(data,'$.kind')='cos'"); got != 1 {
		t.Fatalf("cos author: %d", got)
	}
	if got := portableCount(t, s, "SELECT COUNT(*) FROM tags WHERE name='旧标签' AND deleted=0"); got != 1 {
		t.Fatalf("tag vocabulary: %d", got)
	}
	if got := portableCount(t, s, "SELECT COUNT(*) FROM asset_tags WHERE asset_id='asset-path'"); got != 1 {
		t.Fatalf("path tags: %d", got)
	}
	if got := portableCount(t, s, "SELECT COUNT(*) FROM asset_tags WHERE asset_id='asset-name'"); got != 1 {
		t.Fatalf("cos tags: %d", got)
	}
	if got := portableCount(t, s, "SELECT COUNT(*) FROM asset_authors WHERE asset_id='asset-path'"); got != 1 {
		t.Fatalf("path authors: %d", got)
	}
	if got := portableCount(t, s, "SELECT COUNT(*) FROM asset_authors WHERE asset_id='asset-name'"); got != 2 {
		t.Fatalf("cos authors: %d", got)
	}
	if got := portableCount(t, s, "SELECT favorite FROM asset_records WHERE id='asset-path'"); got != 1 {
		t.Fatalf("favorite: %d", got)
	}
	if got := portableCount(t, s, "SELECT COUNT(*) FROM likes WHERE asset_id='asset-path' AND day='2025-10-10'"); got != 1 {
		t.Fatalf("dated like: %d", got)
	}
	if got := portableCount(t, s, "SELECT unattributed_count FROM like_baselines WHERE asset_id='asset-path'"); got != 3 {
		t.Fatalf("path baseline: %d", got)
	}
	if got := portableCount(t, s, "SELECT source_count FROM like_baselines WHERE asset_id='asset-clip'"); got != 2 {
		t.Fatalf("clip baseline: %d", got)
	}
	if got := portableCount(t, s, "SELECT COUNT(*) FROM events"); got != 2 {
		t.Fatalf("events: %d", got)
	}
	if got := portableCount(t, s, "SELECT views FROM asset_daily_stats WHERE asset_id='asset-path' AND day='2025-10-10'"); got != 1 {
		t.Fatalf("path views: %d", got)
	}
	if got := portableCount(t, s, "SELECT sum(seconds) FROM asset_daily_stats WHERE asset_id='asset-clip'"); got != 13 {
		t.Fatalf("clip seconds: %d", got)
	}
	if got := portableCount(t, s, "SELECT COUNT(*) FROM timeline_marks WHERE asset_id='asset-clip'"); got != 1 {
		t.Fatalf("timeline marks: %d", got)
	}
	if got := portableCount(t, s, "SELECT COUNT(*) FROM author_txt_fragments"); got != 1 {
		t.Fatalf("txt fragments: %d", got)
	}
	if got := portableCount(t, s, "SELECT COUNT(*) FROM author_lists"); got != 1 {
		t.Fatalf("author lists: %d", got)
	}
	if got := portableCount(t, s, "SELECT COUNT(*) FROM settings WHERE key='recommendation_preferences'"); got != 1 {
		t.Fatalf("preferences: %d", got)
	}
	if got := portableCount(t, s, "SELECT COUNT(*) FROM migration_relation_origins WHERE kind='portable-v2/import'"); got != 1 {
		t.Fatalf("import receipt: %d", got)
	}
	if got := portableCount(t, s, "SELECT COUNT(*) FROM asset_author_overrides WHERE asset_id='asset-path'"); got != 1 {
		t.Fatalf("path override: %d", got)
	}
	if got := portableCount(t, s, "SELECT COUNT(*) FROM asset_author_overrides WHERE asset_id='asset-name'"); got != 1 {
		t.Fatalf("cos override: %d", got)
	}

	revisions := map[string]int64{}
	rows, e := s.DB.DB.Query("SELECT id,revision FROM asset_records")
	if e != nil {
		t.Fatal(e)
	}
	for rows.Next() {
		var id string
		var revision int64
		if e = rows.Scan(&id, &revision); e != nil {
			rows.Close()
			t.Fatal(e)
		}
		revisions[id] = revision
	}
	rows.Close()
	if e = rows.Err(); e != nil {
		t.Fatal(e)
	}

	// 相同文件第二次提交：回执命中，不产生新写入。
	again := portableApply(t, s, raw)
	if !again.AlreadyImported || again.Changed {
		t.Fatalf("second apply: %+v", again)
	}
	for id, revision := range revisions {
		if got := portableCount(t, s, "SELECT revision FROM asset_records WHERE id=?", id); int64(got) != revision {
			t.Fatalf("asset %s revision changed on receipt replay: %d -> %d", id, revision, got)
		}
	}

	// 不同导出时刻导致摘要变化，但语义相同：各段只报已有，不再增加关联或修订。
	semantic := portableApply(t, s, portableTestDocument(t, 1700000000001))
	if semantic.AlreadyImported || semantic.Changed {
		t.Fatalf("semantic replay flags: %+v", semantic)
	}
	if semantic.Authors.Created != 0 || semantic.Authors.Merged != 3 ||
		semantic.Authors.ProfileBlocksRestored != 0 || semantic.Authors.AvatarsRestored != 0 {
		t.Fatalf("semantic authors: %+v", semantic.Authors)
	}
	if semantic.Tags.VocabularyCreated != 0 || semantic.Tags.LinksAdded != 0 ||
		semantic.Tags.LinksPresent != 2 || semantic.Tags.VocabularyExisting != 1 {
		t.Fatalf("semantic tags: %+v", semantic.Tags)
	}
	if semantic.AssetAuthors.LinksAdded != 0 || semantic.AssetAuthors.LinksPresent != 3 {
		t.Fatalf("semantic author links: %+v", semantic.AssetAuthors)
	}
	if semantic.Favorites.Already != 1 || semantic.Likes.Already != 2 ||
		semantic.Events.Already != 2 || semantic.Timeline.Already != 1 ||
		semantic.Follows.Already != 1 || semantic.Txt.FragmentsExisting != 1 ||
		semantic.Txt.ListsActivated != 0 || !semantic.Preferences.Kept {
		t.Fatalf("semantic records: %+v %+v %+v %+v %+v %+v %+v", semantic.Favorites, semantic.Likes,
			semantic.Events, semantic.Timeline, semantic.Follows, semantic.Txt, semantic.Preferences)
	}
	current := map[string]int64{}
	rows, e = s.DB.DB.Query("SELECT id,revision FROM asset_records")
	if e != nil {
		t.Fatal(e)
	}
	for rows.Next() {
		var id string
		var revision int64
		if e = rows.Scan(&id, &revision); e != nil {
			rows.Close()
			t.Fatal(e)
		}
		current[id] = revision
	}
	rows.Close()
	if e = rows.Err(); e != nil {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(revisions, current) {
		t.Fatalf("asset revisions changed on semantic replay: %+v -> %+v", revisions, current)
	}
	if got := portableCount(t, s, "SELECT COUNT(*) FROM asset_tags"); got != 2 {
		t.Fatalf("duplicate tag links: %d", got)
	}
	if got := portableCount(t, s, "SELECT COUNT(*) FROM asset_authors WHERE author_id!='unknown'"); got != 3 {
		t.Fatalf("duplicate author links: %d", got)
	}
	if got := portableCount(t, s, "SELECT COUNT(*) FROM events"); got != 2 {
		t.Fatalf("duplicate events: %d", got)
	}
	if got := portableCount(t, s, "SELECT COUNT(*) FROM likes"); got != 1 {
		t.Fatalf("duplicate likes: %d", got)
	}
	if got := portableCount(t, s, "SELECT COUNT(*) FROM timeline_marks"); got != 1 {
		t.Fatalf("duplicate timeline marks: %d", got)
	}
}

func TestPortableImportAcceptsRealShapeWithoutOptionalKeys(t *testing.T) {
	s := portableTestService(t)
	// 真实导出的 open 事件没有 seconds，多数事件没有 day；followedAuthorIds、
	// favorites、authors 等段可能缺省。这里确认缺省不报错、不误计数。
	document := map[string]any{
		"format": "qimeng_backup", "schemaVersion": 2, "appIdentifier": "com.qimeng.media",
		"data": map[string]any{
			"mediaFiles": []any{portableMediaEntry("record", "", "x.png", "x.png", "image", "")},
			"viewEvents": []any{
				map[string]any{"eventId": "event-no-seconds", "recordKey": "record", "kind": "open", "sessionId": "session", "startedAt": "2025-10-10T01:02:03Z"},
			},
		},
	}
	raw, e := json.Marshal(document)
	if e != nil {
		t.Fatal(e)
	}
	report := portablePreview(t, s, raw)
	if report.Media.Total != 1 || report.Media.Unmatched != 1 {
		t.Fatalf("media: %+v", report.Media)
	}
	if report.Events.Total != 1 || report.Events.SkippedUnmatched != 1 {
		t.Fatalf("events: %+v", report.Events)
	}
	if report.Follows.Total != 0 || report.Favorites.Total != 0 || report.Authors.Total != 0 {
		t.Fatalf("absent sections: %+v %+v %+v", report.Follows, report.Favorites, report.Authors)
	}
}

func TestPortableImportRejectsUnrecognisedEnvelope(t *testing.T) {
	s := portableTestService(t)
	cases := []struct{ name, body, code string }{
		{"format", `{"format":"other","schemaVersion":2,"appIdentifier":"com.qimeng.media","data":{"viewEvents":[]}}`, "unsupported_portable_format"},
		{"schema", `{"format":"qimeng_backup","schemaVersion":3,"appIdentifier":"com.qimeng.media","data":{"viewEvents":[]}}`, "unsupported_schema_version"},
		{"app", `{"format":"qimeng_backup","schemaVersion":2,"appIdentifier":"other","data":{"viewEvents":[]}}`, "unsupported_app_identifier"},
		{"truncated", `{"format":"qimeng_backup","schemaVersion":2,"appIdentifier":"com.qimeng.media","data":{"mediaFiles":[`, "invalid_json"},
		{"no-events", `{"format":"qimeng_backup","schemaVersion":2,"appIdentifier":"com.qimeng.media","data":{"mediaFiles":[]}}`, "invalid_portable_import"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			_, e := s.PreviewPortableImport(context.Background(), portableAdmin(), []byte(test.body))
			var app *Error
			if !errors.As(e, &app) || app.Code != test.code {
				t.Fatalf("got %v, want %s", e, test.code)
			}
		})
	}
}
