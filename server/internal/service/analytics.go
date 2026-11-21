package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"qimeng.local/next/server/internal/analytics"
	"qimeng.local/next/server/internal/model"
	"qimeng.local/next/server/internal/store"
)

type RecommendationPreferences struct {
	Revision int64                        `json:"revision"`
	Weights  analytics.Weights            `json:"weights"`
	Presets  map[string]analytics.Weights `json:"presets"`
}
type RecommendationUpdate struct {
	ExpectedRevision int64             `json:"expectedRevision"`
	Weights          analytics.Weights `json:"weights"`
}

func (s *Service) RecommendationPreferences(ctx context.Context) (RecommendationPreferences, error) {
	value, e := store.Get[RecommendationPreferences](ctx, s.DB.DB, "SELECT value FROM settings WHERE key='recommendation_preferences'")
	if errors.Is(e, sql.ErrNoRows) {
		value = RecommendationPreferences{Revision: 1, Weights: analytics.DefaultWeights}
	} else if e != nil {
		return value, e
	}
	value.Presets = analytics.Presets()
	return value, nil
}
func (s *Service) UpdateRecommendationPreferences(ctx context.Context, w *Write, in RecommendationUpdate) (RecommendationPreferences, error) {
	if e := Authorize(w.Principal, "admin", ""); e != nil {
		return RecommendationPreferences{}, e
	}
	current, e := s.RecommendationPreferences(ctx)
	if e != nil {
		return current, e
	}
	if current.Revision != in.ExpectedRevision {
		return current, Conflict(current.Revision)
	}
	if !in.Weights.Valid() {
		return current, Fail(400, "invalid_weights", "推荐权重必须为0到1的有限数。")
	}
	current.Revision++
	current.Weights = in.Weights
	data, e := store.Encode(current)
	if e != nil {
		return current, e
	}
	q, e := w.Q()
	if e != nil {
		return current, e
	}
	if _, e = q.ExecContext(ctx, "INSERT INTO settings(key,value) VALUES('recommendation_preferences',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value", data); e != nil {
		return current, e
	}
	_, e = q.ExecContext(ctx, "DELETE FROM recommendation_snapshots")
	return current, e
}

type RecommendationFilter struct {
	Kind, MediaType, Cursor string
	Seed                    int64
	Limit                   int
}
type RecommendationPage struct {
	Items      []model.Asset `json:"items"`
	NextCursor string        `json:"nextCursor,omitempty"`
	SnapshotID string        `json:"snapshotId"`
}

func (s *Service) recommendationInputs(ctx context.Context, f RecommendationFilter, day string) ([]analytics.Candidate, error) {
	rows, e := s.DB.DB.QueryContext(ctx, `WITH totals AS (SELECT asset_id,sum(views) AS views,sum(plays) AS plays,sum(seconds) AS seconds FROM asset_daily_stats GROUP BY asset_id), last_views AS (SELECT asset_id,max(created_at) AS last_view FROM events WHERE json_extract(data,'$.kind')='view' GROUP BY asset_id), likes_total AS (SELECT asset_id,count(*) AS likes FROM likes GROUP BY asset_id)
 SELECT a.id,a.media_type,a.created_at,json_extract(a.data,'$.tags'),coalesce(t.views,0),coalesce(t.plays,0),coalesce(t.seconds,0),coalesce(l.likes,0)+coalesce(lb.unattributed_count,0),coalesce(v.last_view,''),coalesce(sh.count,0)
 FROM assets a JOIN libraries lib ON lib.id=a.library_id LEFT JOIN totals t ON t.asset_id=a.id LEFT JOIN likes_total l ON l.asset_id=a.id LEFT JOIN like_baselines lb ON lb.asset_id=a.id LEFT JOIN last_views v ON v.asset_id=a.id LEFT JOIN recommendation_shown sh ON sh.asset_id=a.id AND sh.day=?
 WHERE a.trashed=0 AND coalesce(json_extract(lib.data,'$.enabled'),1)=1 AND (?='' OR `+assetPartition+`=?) AND (?='' OR a.media_type=?) ORDER BY a.id`, day, f.Kind, f.Kind, f.MediaType, f.MediaType)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	items := []analytics.Candidate{}
	for rows.Next() {
		var a analytics.Candidate
		var added, last, tags string
		if e = rows.Scan(&a.ID, &a.MediaType, &added, &tags, &a.Views, &a.Plays, &a.Seconds, &a.Likes, &last, &a.Shown); e != nil {
			return nil, e
		}
		if e = json.Unmarshal([]byte(tags), &a.Tags); e != nil {
			return nil, e
		}
		if a.Added, e = time.Parse(time.RFC3339Nano, added); e != nil {
			return nil, e
		}
		if last != "" {
			if a.LastViewed, e = time.Parse(time.RFC3339Nano, last); e != nil {
				return nil, e
			}
		}
		items = append(items, a)
	}
	return items, rows.Err()
}

type RankedAsset struct {
	Asset model.Asset `json:"asset"`
	Score int64       `json:"score"`
}

func (s *Service) Rankings(ctx context.Context, period, kind, cursor string, count int, mediaTypes ...string) (model.Page[RankedAsset], error) {
	result := model.Page[RankedAsset]{Items: []RankedAsset{}}
	n, e := limited(count, 50)
	if e != nil {
		return result, e
	}
	if kind != "" && kind != "regular" && kind != "cos" {
		return result, Fail(400, "invalid_filter", "来源分区无效。")
	}
	mediaType := ""
	if len(mediaTypes) > 0 {
		mediaType = mediaTypes[0]
	}
	if mediaType != "" && mediaType != "image" && mediaType != "animated_image" && mediaType != "video" {
		return result, Fail(400, "invalid_filter", "媒体类型无效。")
	}
	days, ok := map[string]int{"day": 1, "week": 7, "month": 30, "quarter": 90, "year": 365, "all": 0}[period]
	if !ok {
		return result, Fail(400, "invalid_period", "排行周期无效。")
	}
	cutoff := ""
	if days > 0 {
		cutoff = time.Now().AddDate(0, 0, -days).Format("2006-01-02")
	}
	scope := "rank/" + period + "/" + kind + "/" + cutoff + "/" + mediaType
	offset, e := pageOffset(cursor, scope)
	if e != nil {
		return result, e
	}
	rows, e := s.DB.DB.QueryContext(ctx, `WITH activity AS (SELECT asset_id,sum(views+plays) AS score FROM asset_daily_stats WHERE day>=? GROUP BY asset_id),votes AS(SELECT asset_id,count(*) AS score FROM likes WHERE day>=? GROUP BY asset_id)
 SELECT a.id,coalesce(activity.score,0)+coalesce(votes.score,0)+CASE WHEN ?='all' THEN coalesce(lb.unattributed_count,0) ELSE 0 END AS heat FROM assets a JOIN libraries lib ON lib.id=a.library_id LEFT JOIN activity ON activity.asset_id=a.id LEFT JOIN votes ON votes.asset_id=a.id LEFT JOIN like_baselines lb ON lb.asset_id=a.id
 WHERE a.trashed=0 AND coalesce(json_extract(lib.data,'$.enabled'),1)=1 AND (?='' OR `+assetPartition+`=?) AND (?='' OR a.media_type=?) AND (?='all' OR coalesce(activity.score,0)+coalesce(votes.score,0)>0)
 ORDER BY heat DESC,a.file_date DESC,a.id LIMIT ? OFFSET ?`, cutoff, cutoff, period, kind, kind, mediaType, mediaType, period, n+1, offset)
	if e != nil {
		return result, e
	}
	type row struct {
		id    string
		score int64
	}
	values := []row{}
	for rows.Next() {
		var item row
		if e = rows.Scan(&item.id, &item.score); e != nil {
			rows.Close()
			return result, e
		}
		values = append(values, item)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return result, e
	}
	if len(values) > 0 {
		// The ranking statement above already fixed the page and its scores. Load
		// the whole page with the shared asset projection in one statement instead
		// of one read per ranked id; page.key restores that exact order.
		scores := make(map[string]int64, len(values))
		ids := make([]string, len(values))
		for i, item := range values {
			scores[item.id] = item.score
			ids[i] = item.id
		}
		encoded, err := json.Marshal(ids)
		if err != nil {
			return result, err
		}
		// assetJSON binds today's like day before json_each binds the id page.
		rows, e = s.DB.DB.QueryContext(ctx, "SELECT "+assetJSON+`,page.value,a.id FROM json_each(?) page
		 JOIN assets a ON a.id=coalesce((SELECT representative_id FROM selected_image_merge_members WHERE physical_asset_id=page.value),page.value)
		 JOIN authors au ON au.id=a.author_id ORDER BY page.key`, time.Now().Format(time.DateOnly), string(encoded))
		if e != nil {
			return result, e
		}
		for rows.Next() {
			var raw, rankedID, resolvedID string
			if e = rows.Scan(&raw, &rankedID, &resolvedID); e != nil {
				rows.Close()
				return result, e
			}
			if resolvedID != rankedID {
				rows.Close()
				return result, mergedAssetError(resolvedID)
			}
			var a model.Asset
			if e = json.Unmarshal([]byte(raw), &a); e != nil {
				rows.Close()
				return result, e
			}
			result.Items = append(result.Items, RankedAsset{a, scores[rankedID]})
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return result, e
		}
		if len(result.Items) != len(values) {
			// Only a concurrent delete between the two statements can remove a
			// ranked row; keep the per-row read's 404 contract for that case.
			return result, Normalize(sql.ErrNoRows)
		}
	}
	return paged(result.Items, n, offset, scope), nil
}
