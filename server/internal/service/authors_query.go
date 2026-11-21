package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"

	"qimeng.local/next/server/internal/model"
)

const authorCountsCTE = `WITH author_counts AS (SELECT aa.author_id,COUNT(DISTINCT a.id) files,COALESCE(sum(st.views),0) views FROM asset_authors aa JOIN assets a ON a.id=aa.asset_id LEFT JOIN (SELECT asset_id,sum(views) views FROM asset_daily_stats GROUP BY asset_id) st ON st.asset_id=a.id WHERE a.trashed=0 GROUP BY aa.author_id),
 member_order AS (SELECT m.author_id,l.position,m.ordinal,row_number() OVER(PARTITION BY m.author_id ORDER BY l.position,m.ordinal) occurrence FROM author_list_members m JOIN author_lists l ON l.id=m.list_id),
 txt_order AS (SELECT author_id,row_number() OVER(ORDER BY position,ordinal,author_id) rank FROM member_order WHERE occurrence=1)`
const authorFrom = ` FROM authors au LEFT JOIN author_counts ac ON ac.author_id=au.id LEFT JOIN txt_order txt ON txt.author_id=au.id `
const authorJSON = `json_set(au.data,'$.fileCount',COALESCE(ac.files,0),'$.viewCount',COALESCE(ac.views,0),'$.txtOrder',txt.rank,'$.acquisitionSources',json(COALESCE(json_extract(au.data,'$.acquisitionSources'),'[]')))`

// authorDetailSQL is the single-author form of authorCountsCTE+authorJSON.
// Counts read only this author's asset_authors rows. author_lists.position is
// unique, so an author's earliest (position,ordinal) totally orders its
// txtOrder slot; counting authors whose earliest membership precedes that tuple
// reproduces txt_order's global row_number without materializing every author.
// The author_id tie-break mirrors the window even though unique list positions
// make it unreachable.
const authorDetailSQL = `WITH target AS (SELECT m.list_id,l.position,m.ordinal FROM author_list_members m JOIN author_lists l ON l.id=m.list_id WHERE m.author_id=? ORDER BY l.position,m.ordinal LIMIT 1)
 SELECT json_set(au.data,'$.fileCount',COALESCE((SELECT COUNT(DISTINCT a.id) FROM asset_authors aa JOIN assets a ON a.id=aa.asset_id WHERE aa.author_id=au.id AND a.trashed=0),0),'$.viewCount',COALESCE((SELECT sum(st.views) FROM asset_authors aa JOIN assets a ON a.id=aa.asset_id LEFT JOIN asset_daily_stats st ON st.asset_id=a.id WHERE aa.author_id=au.id AND a.trashed=0),0),'$.txtOrder',CASE WHEN NOT EXISTS(SELECT 1 FROM target) THEN NULL ELSE 1+(SELECT COUNT(DISTINCT m2.author_id) FROM author_list_members m2 JOIN author_lists l2 ON l2.id=m2.list_id,target t WHERE l2.position<t.position OR (l2.position=t.position AND m2.ordinal<t.ordinal) OR (l2.position=t.position AND m2.ordinal=t.ordinal AND m2.author_id<au.id)) END,'$.acquisitionSources',json(COALESCE(json_extract(au.data,'$.acquisitionSources'),'[]')))
 FROM authors au WHERE au.id=?`

func (s *Service) Authors(ctx context.Context, query, cursor string, limit int, following bool, options ...string) (model.Page[model.Author], error) {
	result := model.Page[model.Author]{Items: []model.Author{}}
	kind, sorting := "", "default"
	if len(options) > 0 {
		kind = options[0]
	}
	if len(options) > 1 && options[1] != "" {
		sorting = options[1]
	}
	if kind != "" && kind != "regular" && kind != "cos" {
		return result, Fail(400, "invalid_author_kind", "作者分区无效。")
	}
	if len(query) > 800 {
		return result, Fail(400, "query_too_long", "作者搜索词过长。")
	}
	n, e := limited(limit, 20)
	if e != nil {
		return result, e
	}
	order := browseSort{Name: sorting, Expression: "au.name"}
	switch sorting {
	case "default":
	case "views":
		order.Expression = "COALESCE(ac.views,0)"
		order.Numeric = true
		order.Descending = true
	case "files":
		order.Expression = "COALESCE(ac.files,0)"
		order.Numeric = true
		order.Descending = true
	case "addedAsc", "addedDesc":
		order.Nullable = true
		order.Descending = sorting == "addedDesc"
		prefix := "'0'"
		if order.Descending {
			prefix = "'2'"
		}
		order.Expression = "CASE WHEN txt.rank IS NOT NULL THEN " + prefix + "||printf('%020d',txt.rank) ELSE '1'||qimeng_time_key(json_extract(au.data,'$.createdAt')) END"
	default:
		return result, Fail(400, "invalid_sort", "作者排序无效。")
	}
	exact := ""
	if len(options) > 2 {
		exact = options[2]
	}
	if len([]rune(exact)) > 200 {
		return result, Fail(400, "query_too_long", "作者名称过长。")
	}
	keyRaw, _ := json.Marshal(struct {
		Q, Kind, Sort string
		Following     bool
	}{strings.Join(strings.Fields(query), " "), kind, sorting, following})
	key := string(keyRaw)
	if exact != "" {
		key += "|exact=" + exact
	}
	seek, e := readSeek(cursor, key, sorting)
	if e != nil {
		return result, e
	}
	where := []string{"(?='' OR json_extract(au.data,'$.kind')=? OR au.id='unknown')", "(?=0 OR au.following=1)"}
	args := []any{kind, kind, following}
	if exact != "" {
		where = append(where, "au.name=?")
		args = append(args, exact)
	}
	for _, token := range strings.Fields(query) {
		where = append(where, "qimeng_search_token(au.name||char(10)||COALESCE(json_extract(au.data,'$.aliases'),''),?)=1")
		args = append(args, token)
	}
	clause, values, e := seekClause(seek, order, "au.id")
	if e != nil {
		return result, e
	}
	tx, e := s.DB.DB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if e != nil {
		return result, e
	}
	defer tx.Rollback()
	var count int64
	// Filters reference authors only, and both CTE joins are at most one row per
	// author, so the count does not need the library-wide aggregates.
	if e = tx.QueryRowContext(ctx, "SELECT count(*) FROM authors au WHERE "+strings.Join(where, " AND "), args...).Scan(&count); e != nil {
		return result, e
	}
	result.TotalMatched = &count
	args = append(args, values...)
	args = append(args, n+1)
	rows, e := tx.QueryContext(ctx, authorCountsCTE+" SELECT "+authorJSON+",CAST("+order.Expression+" AS TEXT)"+authorFrom+"WHERE "+strings.Join(where, " AND ")+clause+" ORDER BY "+sortSQL(order, order.Expression, "au.id")+" LIMIT ?", args...)
	if e != nil {
		return result, e
	}
	defer rows.Close()
	keys := []string{}
	for rows.Next() {
		var raw string
		var value sql.NullString
		var author model.Author
		if e = rows.Scan(&raw, &value); e != nil {
			return result, e
		}
		if e = json.Unmarshal([]byte(raw), &author); e != nil {
			return result, e
		}
		result.Items = append(result.Items, author)
		key := "~null"
		if value.Valid {
			key = value.String
		}
		keys = append(keys, key)
	}
	if e = rows.Err(); e != nil {
		return result, e
	}
	if len(result.Items) > n {
		result.Items = result.Items[:n]
		last := result.Items[n-1]
		result.NextCursor = makeSeek(key, sorting, keys[n-1], last.ID)
	}
	return result, nil
}
