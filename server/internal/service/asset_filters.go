package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"qimeng.local/next/server/internal/domain"
	"qimeng.local/next/server/internal/model"
)

const activityCTE = `activity AS (SELECT asset_id,sum(views) views,sum(plays) plays FROM asset_daily_stats GROUP BY asset_id)`

// The activity aggregate is only joined for sorts and filters that read
// view/play counts. Everything else (including the asset projection, which
// sums per asset in assetJSON) stays on the assets view alone.
func activityForFilter(f AssetFilter) bool {
	return f.MinViews != nil || f.MaxViews != nil || f.MinPlays != nil || f.MaxPlays != nil
}
func activityForPage(f AssetFilter) bool {
	return activityForFilter(f) || f.Sort == "views" || f.Sort == "plays"
}

// browsePrefix opens a statement whose remaining body starts with a CTE.
func browsePrefix(useActivity bool) string {
	if useActivity {
		return "WITH " + activityCTE + ","
	}
	return "WITH "
}

// browseStatementPrefix opens a statement whose next token is SELECT.
func browseStatementPrefix(useActivity bool) string {
	if useActivity {
		return "WITH " + activityCTE + " "
	}
	return ""
}
func browseSource(useActivity bool) string {
	if useActivity {
		return ` FROM assets a LEFT JOIN activity st ON st.asset_id=a.id `
	}
	return ` FROM assets a `
}

// assetSearchAliasRows is the live alias channel as a one-column compound
// select. It joins the materialised probe inside one IN so SQLite builds the
// token's candidate set once instead of evaluating two OR-connected scans.
const assetSearchAliasRows = `SELECT at.asset_id FROM tag_aliases ta JOIN asset_tags at ON at.tag_id=ta.tag_id
 WHERE qimeng_search_token(ta.alias,?)=1
 UNION ALL
 SELECT cm.asset_id FROM collections c CROSS JOIN collection_members cm ON cm.collection_id=c.id
 WHERE qimeng_search_token(c.name,?)=1
 UNION ALL
 SELECT cm.asset_id FROM collection_aliases ca CROSS JOIN collection_members cm ON cm.collection_id=ca.collection_id
 WHERE qimeng_search_token(ca.alias,?)=1`

// assetSearchProbe turns one token into the asset_search WHERE term.
// asset_search.text is ASCII-folded by migration 0048; brackets is the same
// text with every opening bracket form normalised to '('. A plain token is a
// literal instr on text. A page token is a literal instr on brackets. A digit
// token must be a complete run: the GLOBs require a non-digit (or the string
// edge) on both sides, and the bracket-normalised preceding-character class
// rejects a run directly after '(' — exactly domain.SearchToken's branches.
func assetSearchProbe(token string) (string, []any) {
	kind, folded, page := domain.SearchTokenPattern(token)
	switch kind {
	case domain.SearchTokenPage:
		return `instr(brackets,?)>0`, []any{page}
	case domain.SearchTokenNumber:
		// SearchTokenPattern classifies only all-digit runs here. Re-check it:
		// a GLOB pattern is safe only when the token cannot contain GLOB
		// metacharacters.
		if !domain.SearchTokenDigits(folded) {
			return `instr(text,?)>0`, []any{folded}
		}
		// brackets has every opening bracket form normalised to '(' so the
		// digit-run check only has to exclude '('; this also keeps the GLOB
		// character class free of '['.
		before := "*[^0-9(]" + folded
		return `instr(brackets,?)>0 AND (` +
				`brackets GLOB ? OR brackets GLOB ? OR brackets GLOB ? OR brackets GLOB ?)`,
			[]any{folded, folded + "[^0-9]*", folded, before + "[^0-9]*", before}
	}
	return `instr(text,?)>0`, []any{folded}
}

// assetSearchMatches returns the complete candidate set for one token: the
// materialised probe UNION ALL every live alias branch.
func assetSearchMatches(token string) (string, []any) {
	probe, values := assetSearchProbe(token)
	args := append(values, token, token, token)
	return "a.id IN (SELECT s.asset_id FROM asset_search s WHERE " + probe +
		" UNION ALL " + assetSearchAliasRows + ")", args
}

type browsePredicate struct {
	Where     string
	Args      []any
	FilterKey string
}

type browseSort struct {
	Expression, Name              string
	Descending, Numeric, Nullable bool
	// NullSentinel is set when Expression replaces NULL with a sentinel so the
	// ordering can use a plain index and a row-value seek. It also accepts a
	// pre-upgrade "~null" cursor.
	NullSentinel string
}

func normalizeAssetFilter(in AssetFilter) (AssetFilter, error) {
	if in.TimeRange == "year_range" && in.FromYear > in.ToYear {
		in.FromYear, in.ToYear = in.ToYear, in.FromYear
	}
	if in.TZOffsetMinutes < -840 || in.TZOffsetMinutes > 840 {
		return in, Fail(400, "invalid_timezone_offset", "客户端时区偏移必须在 UTC−14 至 UTC+14 之间。")
	}
	in.Q = strings.Join(strings.Fields(in.Q), " ")
	if utf8.RuneCountInString(in.Q) > 200 {
		return in, Fail(400, "query_too_long", "搜索词最多 200 字。")
	}
	if in.Kind != "" && in.Kind != "regular" && in.Kind != "cos" {
		return in, Fail(400, "invalid_filter", "来源分区无效。")
	}
	switch in.MediaType {
	case "", "image", "animated_image", "video":
	default:
		return in, Fail(400, "invalid_filter", "媒体类型无效。")
	}
	for _, field := range []*[]string{&in.AuthorIDs, &in.SourceIDs, &in.CharacterIDs, &in.CosWorkIDs, &in.WorkKeys, &in.RoleKeys, &in.Tags} {
		max := 50
		if field == &in.Tags {
			max = 100
		}
		var e error
		*field, e = cleanStrings(append([]string{}, (*field)...), max, 512)
		if e != nil {
			return in, e
		}
		sort.Strings(*field)
	}
	if in.Tag != "" {
		names, e := cleanStrings([]string{in.Tag}, 1, 100)
		if e != nil {
			return in, e
		}
		in.Tag = names[0]
	}
	if in.TagMode == "" {
		in.TagMode = "any"
	}
	if in.TagMode != "any" && in.TagMode != "all" {
		return in, Fail(400, "invalid_filter", "标签匹配模式无效。")
	}
	if in.Directory != nil {
		if in.LibraryID == "" {
			return in, Fail(400, "directory_requires_library", "目录文件分页必须指定媒体库。")
		}
		if !domain.ValidMediaPath(*in.Directory) {
			return in, Fail(400, "invalid_path", "目录须为库内合法相对路径。")
		}
	}
	if in.Order == "" {
		in.Order = "desc"
	}
	if in.Order != "asc" && in.Order != "desc" {
		return in, Fail(400, "invalid_sort", "排序方向无效。")
	}
	if in.Sort == "" {
		in.Sort = "default"
	}
	switch in.Sort {
	case "added_desc":
		in.Sort = "addedDate"
		in.Order = "desc"
	case "name_asc":
		in.Sort = "name"
		in.Order = "asc"
	case "updated_desc":
		in.Order = "desc"
	case "default", "fileDate", "addedDate", "views", "plays", "size", "name", "favorited":
	default:
		return in, Fail(400, "invalid_sort", "不支持该排序方式。")
	}
	if in.Sort == "favorited" && (in.Favorite == nil || !*in.Favorite) {
		return in, Fail(400, "favorite_sort_requires_subset", "收藏时间排序须限定收藏子集。")
	}
	switch in.DateCountsBy {
	case "", "fileDate":
	case "favoritedAt":
		if in.Favorite == nil || !*in.Favorite {
			return in, Fail(400, "favorite_dates_require_subset", "收藏日期分组须限定收藏子集。")
		}
	default:
		return in, Fail(400, "invalid_date_counts_by", "不支持该日期分组方式。")
	}
	for _, rangeValue := range []struct {
		name      string
		low, high **int64
		size      bool
	}{{in.ViewRange, &in.MinViews, &in.MaxViews, false}, {in.PlayRange, &in.MinPlays, &in.MaxPlays, false}, {in.SizeRange, &in.MinSize, &in.MaxSize, true}} {
		low, high, ok := domain.CountInterval(rangeValue.name)
		if rangeValue.size {
			low, high, ok = domain.SizeInterval(rangeValue.name)
		}
		if !ok {
			return in, Fail(400, "invalid_filter_range", "播放、观看或文件大小区间无效。")
		}
		if low != nil && (*rangeValue.low == nil || **rangeValue.low < *low) {
			*rangeValue.low = low
		}
		if high != nil && (*rangeValue.high == nil || **rangeValue.high > *high) {
			*rangeValue.high = high
		}
	}
	for _, pair := range [][2]*int64{{in.MinViews, in.MaxViews}, {in.MinPlays, in.MaxPlays}, {in.MinSize, in.MaxSize}} {
		for _, value := range pair {
			if value != nil && *value < 0 {
				return in, Fail(400, "invalid_filter", "次数或文件大小不能为负。")
			}
		}
		if pair[0] != nil && pair[1] != nil && *pair[0] > *pair[1] {
			return in, Fail(400, "invalid_filter", "筛选下限不能大于上限。")
		}
	}
	return in, nil
}
func queryArray(values []string) string { raw, _ := json.Marshal(values); return string(raw) }
func sourceSelection(ids []string) (string, []any) {
	selected := []string{}
	other := false
	for _, id := range ids {
		if id == "__other__" {
			other = true
		} else {
			selected = append(selected, id)
		}
	}
	parts := []string{}
	args := []any{}
	if len(selected) > 0 {
		parts = append(parts, "EXISTS(SELECT 1 FROM asset_source_memberships x WHERE x.asset_id=a.id AND x.source_id IN (SELECT value FROM json_each(?)))")
		args = append(args, queryArray(selected))
	}
	if other {
		parts = append(parts, "(NOT "+assetIsCOS+" AND NOT EXISTS(SELECT 1 FROM asset_source_memberships x WHERE x.asset_id=a.id))")
	}
	return "(" + strings.Join(parts, " OR ") + ")", args
}
func mixedSelections(keys []string, row string) (string, []any, error) {
	groups := map[string][]string{}
	for _, key := range keys {
		kind, id, ok := strings.Cut(key, ":")
		if !ok || id == "" || (row == "works" && kind != "source" && kind != "author") || (row == "roles" && kind != "character" && kind != "work") {
			return "", nil, Fail(400, "invalid_facet_key", "筛选键不属于指定的相册维度。")
		}
		groups[kind] = append(groups[kind], id)
	}
	parts := []string{}
	args := []any{}
	for _, kind := range []string{"source", "author", "character", "work"} {
		ids := groups[kind]
		if len(ids) == 0 {
			continue
		}
		switch kind {
		case "source":
			clause, values := sourceSelection(ids)
			parts = append(parts, "(NOT "+assetIsCOS+" AND "+clause+")")
			args = append(args, values...)
		case "author":
			parts = append(parts, "EXISTS(SELECT 1 FROM asset_authors x JOIN authors selected_author ON selected_author.id=x.author_id WHERE x.asset_id=a.id AND json_extract(selected_author.data,'$.kind')='cos' AND x.author_id IN (SELECT value FROM json_each(?)))")
			args = append(args, queryArray(ids))
		case "character":
			parts = append(parts, "(NOT "+assetIsCOS+" AND EXISTS(SELECT 1 FROM asset_characters x WHERE x.asset_id=a.id AND x.character_id IN (SELECT value FROM json_each(?))))")
			args = append(args, queryArray(ids))
		case "work":
			parts = append(parts, "("+assetIsCOS+" AND a.cos_work_id IN (SELECT value FROM json_each(?)))")
			args = append(args, queryArray(ids))
		}
	}
	return "(" + strings.Join(parts, " OR ") + ")", args, nil
}
func fileDateBounds(f AssetFilter) (*time.Time, *time.Time, error) {
	zone := time.FixedZone("client", f.TZOffsetMinutes*60)
	now := time.Now().In(zone)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, zone)
	start, end := today, today.AddDate(0, 0, 1)
	switch f.TimeRange {
	case "", "all":
		return nil, nil, nil
	case "today":
	case "week":
		weekday := (int(today.Weekday()) + 6) % 7
		start = today.AddDate(0, 0, -weekday)
	case "month":
		start = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, zone)
	case "three_months":
		start = today.AddDate(0, -3, 0)
	case "year":
		start = time.Date(now.Year(), 1, 1, 0, 0, 0, 0, zone)
	case "year_range":
		if f.FromYear < 1970 || f.ToYear < f.FromYear || f.ToYear > 9999 {
			return nil, nil, Fail(400, "invalid_year_range", "年份范围无效。")
		}
		start = time.Date(f.FromYear, 1, 1, 0, 0, 0, 0, zone)
		end = time.Date(f.ToYear+1, 1, 1, 0, 0, 0, 0, zone)
	default:
		return nil, nil, Fail(400, "invalid_time_range", "时间范围无效。")
	}
	return &start, &end, nil
}
func assetPredicate(f AssetFilter) (browsePredicate, error) {
	// trashed is a two-valued filter, not user text, so it is inlined. The full
	// expression indexes added by migration 0047 live on asset_records and serve
	// the ORDER BY (including keyset seeks); trashed is a separate asset_locations
	// column, so the predicate stays an ordinary filter, not a partial-index
	// predicate.
	trashed := "0"
	if f.Trashed {
		trashed = "1"
	}
	where := []string{"a.trashed=" + trashed}
	args := []any{}
	if f.Trashed {
		where = append(where, "a.id NOT IN "+selectedImageDonors)
	}
	if !f.IncludeDisabled && !f.Trashed {
		// EXISTS keeps the sort-driving table (asset_records) first. A joined
		// libraries row forced the planner into an asset_locations scan plus a
		// temp B-tree for the default order.
		where = append(where, "EXISTS(SELECT 1 FROM libraries l WHERE l.id=a.library_id AND COALESCE(json_extract(l.data,'$.enabled'),1)=1)")
	}
	if f.Directory != nil {
		directory := *f.Directory
		if directory == "" {
			if !f.Recursive {
				where = append(where, "instr(a.relative_path,'/')=0")
			}
		} else {
			prefix := directory + "/"
			where = append(where, "substr(a.relative_path,1,length(?))=?")
			args = append(args, prefix, prefix)
			if !f.Recursive {
				where = append(where, "instr(substr(a.relative_path,length(?)+1),'/')=0")
				args = append(args, prefix)
			}
		}
	}
	for _, value := range []struct{ column, value string }{{"a.library_id", f.LibraryID}, {"a.media_type", f.MediaType}, {"a.collection_id", f.CollectionID}, {assetPartition, f.Kind}} {
		if value.value != "" {
			where = append(where, value.column+"=?")
			args = append(args, value.value)
		}
	}
	if f.AuthorID != "" {
		where = append(where, "EXISTS(SELECT 1 FROM asset_authors aa WHERE aa.asset_id=a.id AND aa.author_id=?)")
		args = append(args, f.AuthorID)
	}
	for _, dimension := range []struct {
		table, column string
		values        []string
	}{{"asset_authors", "author_id", f.AuthorIDs}, {"asset_characters", "character_id", f.CharacterIDs}} {
		if len(dimension.values) > 0 {
			where = append(where, "EXISTS(SELECT 1 FROM "+dimension.table+" x WHERE x.asset_id=a.id AND x."+dimension.column+" IN (SELECT value FROM json_each(?)))")
			args = append(args, queryArray(dimension.values))
		}
	}
	if len(f.SourceIDs) > 0 {
		clause, values := sourceSelection(f.SourceIDs)
		where = append(where, clause)
		args = append(args, values...)
	}
	if len(f.CosWorkIDs) > 0 {
		where = append(where, "a.cos_work_id IN (SELECT value FROM json_each(?))")
		args = append(args, queryArray(f.CosWorkIDs))
	}
	for _, row := range []struct {
		name string
		keys []string
	}{{"works", f.WorkKeys}, {"roles", f.RoleKeys}} {
		if len(row.keys) > 0 {
			clause, values, e := mixedSelections(row.keys, row.name)
			if e != nil {
				return browsePredicate{}, e
			}
			where = append(where, clause)
			args = append(args, values...)
		}
	}
	for _, token := range strings.Fields(f.Q) {
		// One candidate set per token: materialised text UNION ALL live aliases.
		match, values := assetSearchMatches(token)
		where = append(where, match)
		args = append(args, values...)
	}
	if f.Tag != "" {
		where = append(where, "EXISTS(SELECT 1 FROM asset_tag_names WHERE asset_id=a.id AND tag=?)")
		args = append(args, f.Tag)
	}
	if len(f.Tags) > 0 {
		if f.TagMode == "all" {
			where = append(where, "(SELECT COUNT(DISTINCT tag) FROM asset_tag_names WHERE asset_id=a.id AND tag IN (SELECT value FROM json_each(?)))=?")
			args = append(args, queryArray(f.Tags), len(f.Tags))
		} else {
			where = append(where, "EXISTS(SELECT 1 FROM asset_tag_names WHERE asset_id=a.id AND tag IN (SELECT value FROM json_each(?)))")
			args = append(args, queryArray(f.Tags))
		}
	}
	if f.Favorite != nil {
		where = append(where, "a.favorite=?")
		args = append(args, *f.Favorite)
	}
	if f.Liked != nil {
		where = append(where, "(EXISTS(SELECT 1 FROM likes WHERE asset_id=a.id) OR EXISTS(SELECT 1 FROM like_baselines WHERE asset_id=a.id))=?")
		args = append(args, *f.Liked)
	}
	if f.History != nil {
		where = append(where, "EXISTS(SELECT 1 FROM events WHERE asset_id=a.id AND json_extract(data,'$.kind')='view')=?")
		args = append(args, *f.History)
	}
	for _, rangeValue := range []struct {
		expression string
		low, high  *int64
	}{{"COALESCE(st.views,0)", f.MinViews, f.MaxViews}, {"COALESCE(st.plays,0)", f.MinPlays, f.MaxPlays}, {"a.size", f.MinSize, f.MaxSize}} {
		if rangeValue.low != nil {
			where = append(where, rangeValue.expression+">=?")
			args = append(args, *rangeValue.low)
		}
		if rangeValue.high != nil {
			where = append(where, rangeValue.expression+"<=?")
			args = append(args, *rangeValue.high)
		}
	}
	start, end, e := fileDateBounds(f)
	if e != nil {
		return browsePredicate{}, e
	}
	if start != nil {
		where = append(where, "a.file_date>=? AND a.file_date<?")
		args = append(args, start.UnixMilli(), end.UnixMilli())
	}
	keyFilter := f
	keyFilter.Cursor = ""
	keyFilter.Limit = 0
	keyFilter.DateCounts = false
	if start == nil {
		keyFilter.TZOffsetMinutes = 0
	}
	key, _ := json.Marshal(keyFilter)
	boundary := ""
	if start != nil {
		boundary = start.String() + "/" + end.String()
	}
	return browsePredicate{Where: strings.Join(where, " AND "), Args: args, FilterKey: "file-date-2:" + string(key) + boundary}, nil
}
func assetSort(f AssetFilter) browseSort {
	value := browseSort{Name: f.Sort + ":" + f.Order, Descending: f.Order == "desc"}
	switch f.Sort {
	case "default", "fileDate":
		// SQLite sorts NULL below every value. Replacing it with a sentinel keeps
		// NULL rows last in both directions without the non-indexable
		// "file_date IS NULL" ORDER BY prefix, so assets_file_date_null_last_desc
		// and _asc serve the whole scan including keyset continuations.
		if value.Descending {
			value.Expression = "COALESCE(a.file_date,-9223372036854775808)"
			value.NullSentinel = "-9223372036854775808"
		} else {
			value.Expression = "COALESCE(a.file_date,9223372036854775807)"
			value.NullSentinel = "9223372036854775807"
		}
		value.Numeric = true
	case "addedDate":
		value.Expression = "a.created_at"
	case "updated_desc":
		value.Expression = "a.updated_at"
	case "name":
		value.Expression = "a.file_name"
	case "size":
		value.Expression = "a.size"
		value.Numeric = true
	case "views":
		value.Expression = "COALESCE(st.views,0)"
		value.Numeric = true
	case "plays":
		value.Expression = "COALESCE(st.plays,0)"
		value.Numeric = true
	case "favorited":
		value.Expression = "a.favorited_at"
		value.Nullable = true
	}
	return value
}
func seekClause(key seekCursor, order browseSort, idExpression string) (string, []any, error) {
	if key.ID == "" {
		return "", nil, nil
	}
	comparison := ">"
	if order.Descending {
		comparison = "<"
	}
	if order.NullSentinel != "" {
		raw := key.Value
		if raw == "~null" {
			// Cursors issued before NULL was folded into the sort key.
			raw = order.NullSentinel
		}
		var value any = raw
		if order.Numeric {
			n, e := strconv.ParseInt(raw, 10, 64)
			if e != nil {
				return "", nil, Fail(400, "invalid_cursor", "排序游标数值无效。")
			}
			value = n
		}
		return " AND (" + order.Expression + "," + idExpression + ")" + comparison + "(?,?)", []any{value, key.ID}, nil
	}
	if !order.Nullable {
		var value any = key.Value
		if order.Numeric {
			n, e := strconv.ParseInt(key.Value, 10, 64)
			if e != nil {
				return "", nil, Fail(400, "invalid_cursor", "排序游标数值无效。")
			}
			value = n
		}
		// Row-value comparison is exactly (key < cursor OR (key = cursor AND id < id)).
		// Keeping it as one term lets the planner keep scanning the sort index.
		return " AND (" + order.Expression + "," + idExpression + ")" + comparison + "(?,?)", []any{value, key.ID}, nil
	}
	if key.Value == "~null" && order.Nullable {
		return " AND " + order.Expression + " IS NULL AND " + idExpression + comparison + "?", []any{key.ID}, nil
	}
	var value any = key.Value
	if order.Numeric {
		n, e := strconv.ParseInt(key.Value, 10, 64)
		if e != nil {
			return "", nil, Fail(400, "invalid_cursor", "排序游标数值无效。")
		}
		value = n
	}
	clause := " AND (" + order.Expression + comparison + "? OR (" + order.Expression + "=? AND " + idExpression + comparison + "?)"
	if order.Nullable {
		clause += " OR " + order.Expression + " IS NULL"
	}
	return clause + ")", []any{value, value, key.ID}, nil
}
func sortSQL(order browseSort, key, id string) string {
	direction := " ASC"
	if order.Descending {
		direction = " DESC"
	}
	prefix := ""
	if order.Nullable {
		prefix = key + " IS NULL ASC,"
	}
	return prefix + key + direction + "," + id + direction
}
func (s *Service) Assets(ctx context.Context, filter AssetFilter) (model.Page[model.Asset], error) {
	page := model.Page[model.Asset]{Items: []model.Asset{}}
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
	order := assetSort(f)
	cursor, e := readSeek(f.Cursor, predicate.FilterKey, order.Name)
	if e != nil {
		return page, e
	}
	seek, values, e := seekClause(cursor, order, "a.id")
	if e != nil {
		return page, e
	}
	tx, e := s.DB.DB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if e != nil {
		return page, e
	}
	defer tx.Rollback()
	// The whole-set count and date buckets are first-page facts. Later pages
	// carry the number in the opaque cursor (seekCursor.Total) instead of
	// rescanning the match set; they still return the same totalMatched.
	filterActivity := activityForFilter(f)
	var count int64
	switch {
	case f.DateCounts && f.Cursor == "":
		buckets := []model.DateCountBucket{}
		unknown := int64(0)
		args := append([]any{f.TZOffsetMinutes * 60}, predicate.Args...)
		epoch := "a.file_date/1000.0"
		if f.DateCountsBy == "favoritedAt" {
			// SQLite can round a fraction near midnight into the next second.
			epoch = "unixepoch(substr(a.favorited_at,1,19)||'Z')"
		}
		rows, e := tx.QueryContext(ctx, browseStatementPrefix(filterActivity)+"SELECT date("+epoch+"+?,'unixepoch') day,count(*)"+browseSource(filterActivity)+"WHERE "+predicate.Where+" GROUP BY day ORDER BY day DESC", args...)
		if e != nil {
			return page, e
		}
		for rows.Next() {
			var day sql.NullString
			var n int64
			if e = rows.Scan(&day, &n); e != nil {
				rows.Close()
				return page, e
			}
			count += n
			if day.Valid {
				buckets = append(buckets, model.DateCountBucket{Date: day.String, FileCount: n})
			} else {
				unknown += n
			}
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return page, e
		}
		page.DateCounts = &buckets
		page.UnknownDateCount = &unknown
	case f.Cursor != "" && cursor.Total != nil && *cursor.Total >= 0:
		count = *cursor.Total
	default:
		if e = tx.QueryRowContext(ctx, browseStatementPrefix(filterActivity)+"SELECT COUNT(*)"+browseSource(filterActivity)+"WHERE "+predicate.Where, predicate.Args...).Scan(&count); e != nil {
			return page, e
		}
	}
	page.TotalMatched = &count
	pageActivity := activityForPage(f)
	args := append([]any{}, predicate.Args...)
	args = append(args, values...)
	args = append(args, n+1, time.Now().Format("2006-01-02"))
	query := browsePrefix(pageActivity) + "page AS (SELECT a.id," + order.Expression + " sort_key" + browseSource(pageActivity) + "WHERE " + predicate.Where + seek + " ORDER BY " + sortSQL(order, order.Expression, "a.id") + " LIMIT ?) SELECT " + assetJSON + ",CAST(p.sort_key AS TEXT) FROM page p JOIN assets a ON a.id=p.id JOIN authors au ON au.id=a.author_id ORDER BY " + sortSQL(order, "p.sort_key", "p.id")
	rows, e := tx.QueryContext(ctx, query, args...)
	if e != nil {
		return page, e
	}
	defer rows.Close()
	keys := []string{}
	for rows.Next() {
		var raw string
		var key sql.NullString
		var a model.Asset
		if e = rows.Scan(&raw, &key); e != nil {
			return page, e
		}
		if e = json.Unmarshal([]byte(raw), &a); e != nil {
			return page, e
		}
		page.Items = append(page.Items, a)
		value := "~null"
		if key.Valid {
			value = key.String
		}
		keys = append(keys, value)
	}
	if e = rows.Err(); e != nil {
		return page, e
	}
	if len(page.Items) > n {
		page.Items = page.Items[:n]
		last := page.Items[n-1]
		page.NextCursor = makeSeekTotal(predicate.FilterKey, order.Name, keys[n-1], last.ID, count)
	}
	return page, nil
}
