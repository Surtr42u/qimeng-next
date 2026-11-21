// portable_import_apply.go 是便携旧备份计划的落库阶段。
//
// 所有写入与预览计划在同一个调用方事务里完成；只有计划与落库之间没有任何
// 并发窗口，预览计数才能当作实际写入计数。任何一段失败都回滚整次导入。
// 重复执行时各段按现有行重新判断，不再增加标签/作者关联或资产修订。
package service

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"qimeng.local/next/server/internal/engagement"
	legacy "qimeng.local/next/server/internal/migration"
	"qimeng.local/next/server/internal/model"
	"qimeng.local/next/server/internal/store"
)

func (s *Service) applyPortablePlan(ctx context.Context, q store.Querier, w *Write, plan *portableImportPlan) error {
	now := time.Now().UTC()
	if e := s.applyPortableAuthors(ctx, q, plan, now); e != nil {
		return e
	}
	if e := s.applyPortableTags(ctx, q, plan, now); e != nil {
		return e
	}
	if e := s.applyPortableAuthorLinks(ctx, q, plan, now); e != nil {
		return e
	}
	if e := s.applyPortableTxt(ctx, q, plan, now); e != nil {
		return e
	}
	if e := s.applyPortableLinkProjections(ctx, q, plan, now); e != nil {
		return e
	}
	if e := s.applyPortableFavorites(ctx, q, plan, now); e != nil {
		return e
	}
	if e := s.applyPortableLikes(ctx, q, plan, now); e != nil {
		return e
	}
	if e := s.applyPortableEvents(ctx, q, plan); e != nil {
		return e
	}
	if e := s.applyPortableTimeline(ctx, q, plan); e != nil {
		return e
	}
	if e := s.applyPortableFollows(ctx, q, plan, now); e != nil {
		return e
	}
	if e := s.applyPortablePreferences(ctx, q, plan); e != nil {
		return e
	}
	return nil
}

func (s *Service) applyPortableAuthors(ctx context.Context, q store.Querier, plan *portableImportPlan, now time.Time) error {
	for _, author := range plan.authors {
		if !author.Create || author.SkipReason != "" || author.Placeholder {
			continue
		}
		if author.Local.Kind == "cos" {
			raw, e := store.Encode(author.Local)
			if e != nil {
				return e
			}
			if _, e = q.ExecContext(ctx, "INSERT INTO authors(id,name,following,data) VALUES(?,?,?,?)",
				author.Local.ID, author.Local.Name, author.Local.Following, raw); e != nil {
				return e
			}
			continue
		}
		if e := saveNewRegularAuthor(ctx, q, author.Local); e != nil {
			return e
		}
	}
	for _, profile := range plan.profiles {
		// 同一份资料已经归档过就不再处理：避免重导时把已忽略的候选重新挂回
		// pending，也避免把“候选已创建”重复计成新的写入。
		var archived int
		if e := q.QueryRowContext(ctx, "SELECT COUNT(*) FROM author_profile_archives WHERE id=?", profile.ArchiveID).Scan(&archived); e != nil {
			return e
		}
		if archived > 0 {
			continue
		}
		result, e := RestoreImportedAuthorProfile(ctx, q, profile.LocalAuthorID, profile.AuthorName, profile.ArchiveID, profile.Prepared)
		if e != nil {
			return e
		}
		plan.report.Authors.ProfileBlocksRestored += len(result.RestoredBlocks)
		plan.report.Authors.ProfileCandidatesCreated += len(result.CandidateBlocks)
		plan.report.Authors.ProfileIssues += len(result.Issues)
		if result.AvatarRestored {
			plan.report.Authors.AvatarsRestored++
		}
		for _, issue := range result.Issues {
			plan.warn("作者资料 %s：%s", profile.AuthorName, issue)
		}
	}
	return nil
}

func (s *Service) applyPortableTags(ctx context.Context, q store.Querier, plan *portableImportPlan, now time.Time) error {
	stamp := dateKey(now)
	for _, tag := range plan.tags {
		if !tag.Create {
			continue
		}
		if _, e := q.ExecContext(ctx, store.InsertAvailableTag, tag.Name, stamp, stamp); e != nil {
			return e
		}
		var id string
		if e := q.QueryRowContext(ctx, "SELECT id FROM tags WHERE name=? AND deleted=0", tag.Name).Scan(&id); e != nil {
			return e
		}
		tag.ID = id
	}
	for assetID, names := range plan.tagIncoming {
		asset, ok := plan.assets[assetID]
		if !ok {
			continue
		}
		existing := plan.tagExisting[assetID]
		desired := []string{}
		for _, name := range existingOrder(plan, assetID) {
			desired = append(desired, name)
		}
		added := 0
		for _, name := range names {
			tag := plan.tags[name]
			if tag == nil || tag.Unavailable || existing[name] || containsString(desired, name) {
				continue
			}
			desired = append(desired, name)
			added++
		}
		if added == 0 {
			continue
		}
		asset.Tags = desired
		asset.TagsRevision++
		asset.Revision++
		asset.UpdatedAt = now
		if asset.TagSetUpdatedAt == nil {
			if value := portableMillisTime(plan.assetMedia[assetID].TagsUpdatedAtMillis, now); value != nil {
				asset.TagSetUpdatedAt = value
			} else {
				asset.TagSetUpdatedAt = &now
			}
		}
		if asset.TagSetSource == "" {
			asset.TagSetSource = "import"
		}
		if e := store.SaveAssetTags(ctx, q, asset); e != nil {
			return e
		}
		if e := protectHumanField(ctx, q, asset.ID, "tags"); e != nil {
			return e
		}
		plan.assets[assetID] = asset
	}
	return nil
}

// existingOrder 返回已存在标签的稳定顺序。tagExisting 只存集合，顺序来自
// 当前资产数据（assets 视图按 ordinal 输出）。
func existingOrder(plan *portableImportPlan, assetID string) []string {
	asset, ok := plan.assets[assetID]
	if !ok {
		return nil
	}
	return asset.Tags
}

func (s *Service) applyPortableAuthorLinks(ctx context.Context, q store.Querier, plan *portableImportPlan, now time.Time) error {
	for assetID, incoming := range plan.authorIncoming {
		asset, ok := plan.assets[assetID]
		if !ok {
			continue
		}
		existing := plan.authorExisting[assetID]
		desired := []string{}
		for _, id := range existing {
			if id != "unknown" {
				desired = appendUnique(desired, id)
			}
		}
		for _, id := range incoming {
			desired = appendUnique(desired, id)
		}
		if len(desired) == 0 {
			desired = []string{"unknown"}
		}
		if strings.Join(existing, "\x00") == strings.Join(desired, "\x00") {
			continue
		}
		authors := make([]model.Author, 0, len(desired))
		for _, id := range desired {
			if author, ok := plan.localAuthors[id]; ok {
				authors = append(authors, author)
				continue
			}
			author, e := store.Get[model.Author](ctx, q, "SELECT data FROM authors WHERE id=?", id)
			if e != nil {
				return e
			}
			plan.localAuthors[id] = author
			authors = append(authors, author)
		}
		setAuthors(&asset, authors)
		asset.AuthorsRevision++
		asset.Revision++
		asset.UpdatedAt = now
		if e := store.SaveAssetEdits(ctx, q, asset, true, nil); e != nil {
			return e
		}
		if e := protectHumanField(ctx, q, asset.ID, "authors"); e != nil {
			return e
		}
		plan.assets[assetID] = asset
	}
	return nil
}

func (s *Service) applyPortableTxt(ctx context.Context, q store.Querier, plan *portableImportPlan, now time.Time) error {
	if len(plan.fragments) == 0 {
		return nil
	}
	mapped := []V2MigrationAuthor{}
	for index, author := range plan.authors {
		if author.Placeholder || author.SkipReason != "" || author.Local.Kind != "regular" {
			continue
		}
		mapped = append(mapped, V2MigrationAuthor{Ordinal: index, SourceAuthorID: author.Source.AuthorID,
			Name: author.Source.DisplayName, Kind: "regular", AuthorID: author.Local.ID, State: "existing"})
	}
	fragments := make([]legacy.Fragment, 0, len(plan.fragments))
	for _, fragment := range plan.fragments {
		fragments = append(fragments, fragment.legacy())
	}
	metadata := V2Import{ID: portableSourceID(plan.sourceDigest), SourceID: plan.sourceID, SHA256: plan.sourceDigest, CreatedAt: now,
		MediaCount: len(plan.input.Data.MediaFiles), AuthorCount: len(plan.input.Data.Authors),
		EventCount: len(plan.input.Data.ViewEvents), FragmentCount: len(plan.fragments)}
	textPlan := V2MigrationPlan{ID: plan.sourceID + ":txt", ImportID: metadata.ID, State: "prepared",
		CreatedAt: now, Issues: []string{}, Counts: map[string]int64{}}
	if e := restoreV2TextLists(ctx, q, metadata, fragments, mapped, &textPlan); e != nil {
		// 片段是辅助资料：业务校验失败时保留已归档的片段并继续导入记录，
		// 只有真正的存储错误才中止整次导入。
		if code := portableErrorCode(e); code != "" {
			plan.warn("作者 TXT 片段未能激活清单（%s），片段已按归档状态保留。", code)
			plan.report.Txt.Issues = append(plan.report.Txt.Issues, code)
			return nil
		}
		return e
	}
	plan.report.Txt.ListsActivated = int(textPlan.Counts["txtListsActivated"])
	if len(textPlan.Issues) > 20 {
		plan.report.Txt.Issues = append([]string{}, textPlan.Issues[:20]...)
	} else {
		plan.report.Txt.Issues = append([]string{}, textPlan.Issues...)
	}
	for _, issue := range plan.report.Txt.Issues {
		plan.warn("作者 TXT：%s", issue)
	}
	return nil
}

func (s *Service) applyPortableLinkProjections(ctx context.Context, q store.Querier, plan *portableImportPlan, now time.Time) error {
	if len(plan.linkPlans) == 0 {
		return nil
	}
	byAuthor := map[string][]portableAuthorWork{}
	for _, work := range plan.linkPlans {
		byAuthor[work.AuthorID] = append(byAuthor[work.AuthorID], work)
	}
	for authorID, works := range byAuthor {
		author, ok := plan.localAuthors[authorID]
		if !ok {
			var e error
			author, e = store.Get[model.Author](ctx, q, "SELECT data FROM authors WHERE id=?", authorID)
			if e != nil {
				return e
			}
		}
		if author.Placeholder || author.Kind != "regular" {
			continue
		}
		list, e := s.authorTextHome(ctx, q, author)
		if e != nil {
			if portableErrorCode(e) == "author_text_identity_invalid" {
				// 名称无法写入 TXT 语法时只保留显式关联，不制造错误的清单块。
				for _, work := range works {
					if _, e = q.ExecContext(ctx, "INSERT INTO asset_author_overrides(asset_id,author_id,removed,list_id) VALUES(?,?,0,'') ON CONFLICT(asset_id,author_id) DO UPDATE SET removed=0,list_id=''", work.AssetID, authorID); e != nil {
						return e
					}
				}
				continue
			}
			return e
		}
		changed := false
		seenWork := map[string]bool{}
		for _, work := range works {
			if seenWork[work.FileName] {
				continue
			}
			seenWork[work.FileName] = true
			if addListWork(&list, author.ID, work.FileName) {
				changed = true
			}
		}
		if changed {
			list.Revision++
			list.UpdatedAt = now
			if e = saveAuthorList(ctx, q, &list); e != nil {
				return e
			}
		}
		seenPair := map[string]bool{}
		for _, work := range works {
			key := work.AssetID + "\x00" + authorID
			if seenPair[key] {
				continue
			}
			seenPair[key] = true
			if _, e = q.ExecContext(ctx, "INSERT INTO asset_author_overrides(asset_id,author_id,removed,list_id) VALUES(?,?,0,?) ON CONFLICT(asset_id,author_id) DO UPDATE SET removed=0,list_id=excluded.list_id", work.AssetID, authorID, list.ID); e != nil {
				return e
			}
		}
	}
	return nil
}

func (s *Service) applyPortableFavorites(ctx context.Context, q store.Querier, plan *portableImportPlan, now time.Time) error {
	for _, favorite := range plan.favoritePlans {
		asset, ok := plan.assets[favorite.AssetID]
		if !ok {
			continue
		}
		if asset.Favorite {
			plan.report.Favorites.Applied--
			plan.report.Favorites.Already++
			continue
		}
		if asset.FavoriteUpdatedAt != nil {
			plan.report.Favorites.Applied--
			plan.report.Favorites.SkippedLocal++
			continue
		}
		updated, e := engagement.SetFavorite(ctx, q, asset, true, now)
		if e != nil {
			return e
		}
		plan.assets[favorite.AssetID] = updated
	}
	return nil
}

func (s *Service) applyPortableLikes(ctx context.Context, q store.Querier, plan *portableImportPlan, now time.Time) error {
	for _, like := range plan.likePlans {
		if like.Action != "apply" {
			continue
		}
		action, _, e := decideLegacyLikes(ctx, q, like.AssetID, plan.sourceID, likeSourceKey(like.Media.legacy()), like.Like.legacy())
		if e != nil {
			return e
		}
		if action != "apply" {
			plan.report.Likes.Applied--
			if action == "keep" {
				plan.report.Likes.Already++
			} else {
				plan.report.Likes.SkippedConflict++
			}
			continue
		}
		day := legacyLikeDay(like.Like.legacy())
		unattributed := like.Like.LikeCount
		if day != "" {
			r, e := q.ExecContext(ctx, "INSERT OR IGNORE INTO likes(asset_id,day,created_at) VALUES(?,?,?)", like.AssetID, day, dateKey(now))
			if e != nil {
				return e
			}
			affected, e := r.RowsAffected()
			if e != nil {
				return e
			}
			if affected == 0 {
				plan.report.Likes.Applied--
				plan.report.Likes.Already++
				continue
			}
			unattributed--
		}
		if unattributed > 0 {
			if _, e = q.ExecContext(ctx, `INSERT INTO like_baselines(asset_id,source_id,source_key,source_count,unattributed_count,last_day,observed_day,import_id)
 VALUES(?,?,?,?,?,?,?,?)`, like.AssetID, plan.sourceID, likeSourceKey(like.Media.legacy()), like.Like.LikeCount, unattributed,
				day, now.Format("2006-01-02"), plan.sourceDigest); e != nil {
				return e
			}
		}
		asset := plan.assets[like.AssetID]
		asset.Revision++
		asset.UpdatedAt = now
		if _, e = q.ExecContext(ctx, "UPDATE asset_records SET revision=?,updated_at=?,data=json_set(data,'$.revision',?,'$.updatedAt',?) WHERE id=?",
			asset.Revision, dateKey(now), asset.Revision, dateKey(now), asset.ID); e != nil {
			return e
		}
		plan.assets[like.AssetID] = asset
	}
	return nil
}

func (s *Service) applyPortableEvents(ctx context.Context, q store.Querier, plan *portableImportPlan) error {
	views, plays, dwell := int64(0), int64(0), int64(0)
	for _, item := range plan.eventPlans {
		if item.Action != "imported" {
			continue
		}
		asset, ok := plan.assets[item.AssetID]
		if !ok {
			continue
		}
		added, e := importV2Event(ctx, q, plan.sourceID, item.Event.legacy(), asset)
		if e != nil {
			if portableErrorCode(e) == "event_identity_conflict" {
				plan.report.Events.Imported--
				plan.report.Events.SkippedConflict++
				continue
			}
			return e
		}
		if !added {
			plan.report.Events.Imported--
			plan.report.Events.Already++
			continue
		}
		switch item.Event.Kind {
		case "open":
			views++
		case "play":
			plays++
		case "dwell":
			if item.Event.Seconds != nil {
				dwell += *item.Event.Seconds
			}
		}
	}
	plan.report.Events.Views = views
	plan.report.Events.Plays = plays
	plan.report.Events.DwellSeconds = dwell
	return nil
}

func (s *Service) applyPortableTimeline(ctx context.Context, q store.Querier, plan *portableImportPlan) error {
	for _, item := range plan.timelinePlans {
		if item.Action != "imported" {
			continue
		}
		asset, ok := plan.assets[item.AssetID]
		if !ok {
			continue
		}
		added, e := importV2Mark(ctx, q, plan.sourceID, item.Mark.legacy(), asset)
		if e != nil {
			return e
		}
		if !added {
			plan.report.Timeline.Imported--
			plan.report.Timeline.Already++
		}
	}
	return nil
}

func (s *Service) applyPortableFollows(ctx context.Context, q store.Querier, plan *portableImportPlan, now time.Time) error {
	for _, follow := range plan.followPlans {
		if follow.Action != "marked" {
			continue
		}
		author, e := store.Get[model.Author](ctx, q, "SELECT data FROM authors WHERE id=?", follow.AuthorID)
		if e != nil {
			return e
		}
		if author.Following {
			plan.report.Follows.Marked--
			plan.report.Follows.Already++
			continue
		}
		if author.FollowingUpdatedAt != nil {
			plan.report.Follows.Marked--
			plan.report.Follows.SkippedLocal++
			continue
		}
		author.Following = true
		author.FollowingUpdatedAt = &now
		author.Revision++
		author.UpdatedAt = now
		raw, e := store.Encode(author)
		if e != nil {
			return e
		}
		if _, e = q.ExecContext(ctx, "UPDATE authors SET following=?,data=? WHERE id=?", author.Following, raw, author.ID); e != nil {
			return e
		}
		plan.localAuthors[author.ID] = author
	}
	return nil
}

func (s *Service) applyPortablePreferences(ctx context.Context, q store.Querier, plan *portableImportPlan) error {
	if plan.weights == nil {
		return nil
	}
	var existing string
	e := q.QueryRowContext(ctx, "SELECT value FROM settings WHERE key='recommendation_preferences'").Scan(&existing)
	switch {
	case errors.Is(e, sql.ErrNoRows):
		value := RecommendationPreferences{Revision: 2, Weights: *plan.weights}
		raw, e := store.Encode(value)
		if e != nil {
			return e
		}
		if _, e = q.ExecContext(ctx, "INSERT INTO settings(key,value) VALUES('recommendation_preferences',?)", raw); e != nil {
			return e
		}
		if _, e = q.ExecContext(ctx, "DELETE FROM recommendation_snapshots"); e != nil {
			return e
		}
	case e != nil:
		return e
	default:
		if plan.report.Preferences.Imported {
			plan.report.Preferences.Imported = false
			plan.report.Preferences.Kept = true
		}
	}
	return nil
}
