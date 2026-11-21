package service

import (
	"context"
	"database/sql"
	"time"

	"qimeng.local/next/server/internal/model"
	"qimeng.local/next/server/internal/store"
)

// The latest view is the maximum (created_at,event_id) pair for an asset. The
// concatenated key keeps the exact row_number tie-break while letting one pass
// over events_asset aggregate per asset without a temporary sort. Event
// timestamps come from dateKey (or migration 0014's normalisation) and never
// contain the unit separator used to split the two fields back apart.
const historyLatest = `latest AS (SELECT asset_id,latest_key,
 substr(latest_key,1,instr(latest_key,char(31))-1) created_at,
 substr(latest_key,instr(latest_key,char(31))+1) event_id
 FROM (SELECT asset_id,max(CASE WHEN json_extract(data,'$.kind')='view' THEN created_at||char(31)||event_id END) latest_key FROM events GROUP BY asset_id))`

func (s *Service) FilteredHistory(ctx context.Context, filter AssetFilter) (model.Page[model.Event], error) {
	page := model.Page[model.Event]{Items: []model.Event{}}
	f, e := normalizeAssetFilter(filter)
	if e != nil {
		return page, e
	}
	n, e := limited(f.Limit, 50)
	if e != nil {
		return page, e
	}
	predicate, e := assetPredicate(f)
	if e != nil {
		return page, e
	}
	order := browseSort{Expression: "ev.created_at", Name: "history_desc", Descending: true}
	cursor, e := readSeek(f.Cursor, predicate.FilterKey, order.Name)
	if e != nil {
		return page, e
	}
	seek, values, e := seekClause(cursor, order, "ev.event_id")
	if e != nil {
		return page, e
	}
	activity := activityForFilter(f)
	cte := browsePrefix(activity) + historyLatest
	from := browseSource(activity)
	from += "JOIN latest ev ON ev.asset_id=a.id AND ev.latest_key IS NOT NULL "
	tx, e := s.DB.DB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if e != nil {
		return page, e
	}
	defer tx.Rollback()
	var count int64
	switch {
	case f.DateCounts && f.Cursor == "":
		args := append([]any{f.TZOffsetMinutes * 60}, predicate.Args...)
		buckets, e := tx.QueryContext(ctx, cte+" SELECT date(unixepoch(substr(ev.created_at,1,19)||'Z')+?,'unixepoch') day,count(*)"+from+"WHERE "+predicate.Where+" GROUP BY day ORDER BY day DESC", args...)
		if e != nil {
			return page, e
		}
		dates := []model.DateCountBucket{}
		for buckets.Next() {
			var item model.DateCountBucket
			if e = buckets.Scan(&item.Date, &item.FileCount); e != nil {
				buckets.Close()
				return page, e
			}
			dates = append(dates, item)
			count += item.FileCount
		}
		e = buckets.Err()
		buckets.Close()
		if e != nil {
			return page, e
		}
		page.DateCounts = &dates
	case f.Cursor != "" && cursor.Total != nil && *cursor.Total >= 0:
		count = *cursor.Total
	default:
		if e = tx.QueryRowContext(ctx, cte+" SELECT COUNT(*)"+from+"WHERE "+predicate.Where, predicate.Args...).Scan(&count); e != nil {
			return page, e
		}
	}
	page.TotalMatched = &count
	args := append([]any{}, predicate.Args...)
	args = append(args, values...)
	args = append(args, n+1, time.Now().Format("2006-01-02"))
	query := cte + ", page AS (SELECT ev.event_id,ev.created_at,a.id" + from + "WHERE " + predicate.Where + seek + " ORDER BY ev.created_at DESC,ev.event_id DESC LIMIT ?) SELECT json_set(e.data,'$.asset',json(" + assetJSON + ")) FROM page p JOIN assets a ON a.id=p.id JOIN authors au ON au.id=a.author_id JOIN events e ON e.event_id=p.event_id ORDER BY p.created_at DESC,p.event_id DESC"
	valuesPage, e := store.List[model.Event](ctx, tx, query, args...)
	if e != nil {
		return page, e
	}
	page.Items = valuesPage
	if len(valuesPage) > n {
		page.Items = valuesPage[:n]
		last := page.Items[n-1]
		page.NextCursor = makeSeekTotal(predicate.FilterKey, order.Name, dateKey(last.CreatedAt), last.EventID, count)
	}
	return page, nil
}
