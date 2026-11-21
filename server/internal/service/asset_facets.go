package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"strconv"
	"strings"

	"qimeng.local/next/server/internal/model"
	"qimeng.local/next/server/internal/store"
)

type AssetFacet struct {
	Key   string `json:"key"`
	Kind  string `json:"kind"`
	ID    string `json:"id"`
	Name  string `json:"name"`
	Count int64  `json:"count"`
}

func fixedFacetPosition(id string) int {
	switch id {
	case "all":
		return 0
	case "regular", "image":
		return 1
	case "cos", "animated_image":
		return 2
	default:
		return 3
	}
}

// facetCandidates builds the dynamic option set. Fixed partitions/types are
// produced by fixedFacetCounts from one aggregate instead. The relationship
// tables are keyed by (asset, owner) and the source view emits each pair once,
// so a candidate's matched assets are exactly its joined rows: count(*) equals
// the previous count(DISTINCT matched.id) without a temp B-tree per group.
func facetCandidates(row string) (string, error) {
	switch row {
	case "works":
		return `SELECT 'source:'||s.id key,'source' kind,s.id,s.name,count(*) n FROM matched m JOIN asset_source_memberships x ON x.asset_id=m.id JOIN sources s ON s.id=x.source_id WHERE m.kind='regular' GROUP BY s.id UNION ALL SELECT 'source:__other__','source','__other__','其他',count(*) FROM matched m WHERE m.kind='regular' AND NOT EXISTS(SELECT 1 FROM asset_source_memberships x WHERE x.asset_id=m.id) HAVING count(*)>0 UNION ALL SELECT 'author:'||au.id,'author',au.id,au.name,count(*) FROM matched m JOIN asset_authors x ON x.asset_id=m.id JOIN authors au ON au.id=x.author_id WHERE m.kind='cos' AND json_extract(au.data,'$.kind')='cos' GROUP BY au.id`, nil
	case "roles":
		return `SELECT 'character:'||c.id key,'character' kind,c.id,c.name,count(*) n FROM matched m JOIN asset_characters x ON x.asset_id=m.id JOIN characters c ON c.id=x.character_id WHERE m.kind='regular' GROUP BY c.id UNION ALL SELECT 'work:'||w.id,'work',w.id,w.name,count(*) FROM matched m JOIN cos_works w ON w.id=m.cos_work_id WHERE m.kind='cos' GROUP BY w.id`, nil
	default:
		return "", Fail(400, "invalid_facet_row", "相册维度必须是分区、作者、角色或类型。")
	}
}

// fixedFacetCounts returns one scan over the match set that yields both the
// total and the counts of the fixed, small dimensions. The old candidate CTE
// recounted matched once per option.
func fixedFacetCounts(row string) (columns, values, countCase string, e error) {
	switch row {
	case "partitions":
		return `count(*) total,coalesce(sum(CASE WHEN ` + assetPartition + `='regular' THEN 1 ELSE 0 END),0) regular,coalesce(sum(CASE WHEN ` + assetPartition + `='cos' THEN 1 ELSE 0 END),0) cos`,
			`(0,'all','partition','all','全部'),(1,'regular','partition','regular','常规'),(2,'cos','partition','cos','COS')`,
			`CASE v.position WHEN 0 THEN c.total WHEN 1 THEN c.regular ELSE c.cos END`, nil
	case "types":
		return `count(*) total,coalesce(sum(CASE WHEN a.media_type='image' THEN 1 ELSE 0 END),0) image,coalesce(sum(CASE WHEN a.media_type='animated_image' THEN 1 ELSE 0 END),0) animated_image,coalesce(sum(CASE WHEN a.media_type='video' THEN 1 ELSE 0 END),0) video`,
			`(0,'all','type','all','全部'),(1,'image','type','image','图片'),(2,'animated_image','type','animated_image','动图'),(3,'video','type','video','视频')`,
			`CASE v.position WHEN 0 THEN c.total WHEN 1 THEN c.image WHEN 2 THEN c.animated_image ELSE c.video END`, nil
	}
	return "", "", "", Fail(400, "invalid_facet_row", "相册维度必须是分区、作者、角色或类型。")
}

// facetItemJSON leaves a NULL item for the synthetic totals row so an empty
// filtered page still carries the exact first-page facts.
func facetItemJSON(alias string) string {
	return `CASE WHEN ` + alias + `.key IS NULL THEN NULL ELSE json_object('key',` + alias + `.key,'kind',` + alias + `.kind,'id',` + alias + `.id,'name',` + alias + `.name,'count',` + alias + `.n) END`
}

// scanFacetPage reads (item,totalOptions,totalMatched) rows. The LEFT JOIN in
// each query guarantees one row even when facetQ or the cursor matches nothing.
func scanFacetPage(rows *sql.Rows) ([]AssetFacet, int64, int64, error) {
	items := []AssetFacet{}
	var totalOptions, totalMatched int64
	for rows.Next() {
		var raw sql.NullString
		var options, matched sql.NullInt64
		if err := rows.Scan(&raw, &options, &matched); err != nil {
			return nil, 0, 0, err
		}
		if options.Valid {
			totalOptions = options.Int64
		}
		if matched.Valid {
			totalMatched = matched.Int64
		}
		if !raw.Valid {
			continue
		}
		var value AssetFacet
		if err := json.Unmarshal([]byte(raw.String), &value); err != nil {
			return nil, 0, 0, err
		}
		items = append(items, value)
	}
	return items, totalOptions, totalMatched, rows.Err()
}

func (s *Service) AssetFacets(ctx context.Context, filter AssetFilter, row, query string) (model.Page[AssetFacet], error) {
	page := model.Page[AssetFacet]{Items: []AssetFacet{}}
	f, e := normalizeAssetFilter(filter)
	if e != nil {
		return page, e
	}
	n, e := limited(f.Limit, 50)
	if e != nil {
		return page, e
	}
	if len(query) > 800 {
		return page, Fail(400, "query_too_long", "候选搜索词过长。")
	}
	predicate, e := assetPredicate(f)
	if e != nil {
		return page, e
	}
	key := predicate.FilterKey + "\x00" + row + "\x00" + query
	fixed := row == "partitions" || row == "types"
	sorting := "facet_count"
	ordering := "f.n DESC,f.name,f.key"
	if fixed {
		sorting = "fixed_row"
		ordering = "f.position"
	}
	cursor, e := readSeek(f.Cursor, key, sorting)
	if e != nil {
		return page, e
	}
	tx, e := s.DB.DB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if e != nil {
		return page, e
	}
	defer tx.Rollback()

	activity := activityForFilter(f)
	// totalMatched is a first-page fact. Continuations carry it in the cursor so
	// they do not rescan the match set, while still returning the same number.
	var carriedTotal *int64
	if cursor.ID != "" && cursor.Total != nil && *cursor.Total >= 0 {
		carriedTotal = cursor.Total
	}
	var sqlQuery string
	args := append([]any{}, predicate.Args...)
	args = append(args, query)
	on := "1=1"
	if fixed {
		columns, fixedValues, countCase, e := fixedFacetCounts(row)
		if e != nil {
			return page, e
		}
		if cursor.ID != "" {
			position, e := strconv.Atoi(cursor.Value)
			if e != nil || position < 0 || position > 3 {
				return page, Fail(400, "invalid_cursor", "固定维度游标无效。")
			}
			on = "f.position>?"
			args = append(args, position)
		}
		args = append(args, n+1)
		sqlQuery = browsePrefix(activity) + "counts AS (SELECT " + columns + browseSource(activity) + "WHERE " + predicate.Where +
			"), fixed(position,key,kind,id,name) AS (VALUES " + fixedValues +
			"), fixed_rows AS (SELECT v.position,v.key,v.kind,v.id,v.name," + countCase +
			" n,c.total total_matched FROM fixed v CROSS JOIN counts c)" +
			", filtered AS (SELECT f.*,count(*) OVER () total_options FROM fixed_rows f WHERE instr(lower(f.name),lower(?))>0)" +
			" SELECT " + facetItemJSON("f") + ",COALESCE((SELECT total_options FROM filtered LIMIT 1),0),(SELECT total_matched FROM fixed_rows LIMIT 1)" +
			" FROM (SELECT 1) d LEFT JOIN filtered f ON " + on + " ORDER BY " + ordering + " LIMIT ?"
	} else {
		candidates, e := facetCandidates(row)
		if e != nil {
			return page, e
		}
		totals, totalExpression := "), totals AS (SELECT count(*) n FROM matched)", "(SELECT n FROM totals)"
		if carriedTotal != nil {
			totals, totalExpression = ")", "?"
			args = append(args, *carriedTotal)
		}
		if cursor.ID != "" {
			parts := strings.SplitN(cursor.Value, "\x00", 2)
			if len(parts) != 2 {
				return page, Fail(400, "invalid_cursor", "候选游标无效。")
			}
			count, e := strconv.ParseInt(parts[0], 10, 64)
			if e != nil || count < 0 {
				return page, Fail(400, "invalid_cursor", "候选游标计数无效。")
			}
			on = "(f.n<? OR (f.n=? AND (f.name>? OR (f.name=? AND f.key>?))))"
			args = append(args, count, count, parts[1], parts[1], cursor.ID)
		}
		args = append(args, n+1)
		sqlQuery = browsePrefix(activity) + "matched AS (SELECT a.id,a.library_id,a.media_type,a.cos_work_id," + assetPartition + " kind" + browseSource(activity) + "WHERE " + predicate.Where +
			"), candidates AS (" + candidates + totals +
			", filtered AS (SELECT c.*,count(*) OVER () total_options FROM candidates c WHERE instr(lower(c.name),lower(?))>0)" +
			" SELECT " + facetItemJSON("f") + ",COALESCE((SELECT total_options FROM filtered LIMIT 1),0)," + totalExpression +
			" FROM (SELECT 1) d LEFT JOIN filtered f ON " + on + " ORDER BY " + ordering + " LIMIT ?"
	}
	rows, e := tx.QueryContext(ctx, sqlQuery, args...)
	if e != nil {
		return page, e
	}
	defer rows.Close()
	values, options, matched, e := scanFacetPage(rows)
	if e != nil {
		return page, e
	}
	page.Items = values
	page.TotalOptions = &options
	page.TotalMatched = &matched
	if len(values) > n {
		page.Items = values[:n]
		last := page.Items[n-1]
		value := strconv.FormatInt(last.Count, 10) + "\x00" + last.Name
		if fixed {
			value = strconv.Itoa(fixedFacetPosition(last.ID))
		}
		page.NextCursor = makeSeekTotal(key, sorting, value, last.Key, matched)
	}
	return page, nil
}

type SearchSuggestion struct {
	ID   string `json:"id"`
	Type string `json:"type"`
	Name string `json:"name"`
}

// suggestionVocabulary reads the five materialised name dimensions and keeps a
// name only when an indexed relationship reaches a live asset in an enabled
// library. Work is bounded by vocabulary size, not by the number of assets.
const suggestionVocabulary = `names(type,id,name) AS (
 SELECT 'source',s.id,s.name FROM sources s WHERE
  EXISTS(SELECT 1 FROM asset_sources x JOIN asset_locations l ON l.asset_id=x.asset_id JOIN libraries lib ON lib.id=l.library_id
   WHERE x.source_id=s.id AND l.trashed=0 AND COALESCE(json_extract(lib.data,'$.enabled'),1)=1
     AND NOT EXISTS(SELECT 1 FROM asset_authors pa JOIN authors pax ON pax.id=pa.author_id WHERE pa.asset_id=x.asset_id AND json_extract(pax.data,'$.kind')='cos'))
  OR EXISTS(SELECT 1 FROM characters c JOIN asset_characters x ON x.character_id=c.id JOIN asset_locations l ON l.asset_id=x.asset_id JOIN libraries lib ON lib.id=l.library_id
   WHERE c.source_id=s.id AND l.trashed=0 AND COALESCE(json_extract(lib.data,'$.enabled'),1)=1
     AND NOT EXISTS(SELECT 1 FROM asset_authors pa JOIN authors pax ON pax.id=pa.author_id WHERE pa.asset_id=x.asset_id AND json_extract(pax.data,'$.kind')='cos'))
 UNION ALL
 SELECT 'character',c.id,c.name FROM characters c WHERE
  EXISTS(SELECT 1 FROM asset_characters x JOIN asset_locations l ON l.asset_id=x.asset_id JOIN libraries lib ON lib.id=l.library_id
   WHERE x.character_id=c.id AND l.trashed=0 AND COALESCE(json_extract(lib.data,'$.enabled'),1)=1
     AND NOT EXISTS(SELECT 1 FROM asset_authors pa JOIN authors pax ON pax.id=pa.author_id WHERE pa.asset_id=x.asset_id AND json_extract(pax.data,'$.kind')='cos'))
 UNION ALL
 SELECT 'cosAuthor',au.id,au.name FROM authors au WHERE json_extract(au.data,'$.kind')='cos' AND
  EXISTS(SELECT 1 FROM asset_authors x JOIN asset_locations l ON l.asset_id=x.asset_id JOIN libraries lib ON lib.id=l.library_id
   WHERE x.author_id=au.id AND l.trashed=0 AND COALESCE(json_extract(lib.data,'$.enabled'),1)=1)
 UNION ALL
 SELECT 'cosWork',w.id,w.name FROM cos_works w WHERE
  EXISTS(SELECT 1 FROM asset_records r JOIN asset_locations l ON l.asset_id=r.id JOIN libraries lib ON lib.id=l.library_id
   WHERE r.cos_work_id=w.id AND l.trashed=0 AND COALESCE(json_extract(lib.data,'$.enabled'),1)=1
     AND EXISTS(SELECT 1 FROM asset_authors pa JOIN authors pax ON pax.id=pa.author_id WHERE pa.asset_id=r.id AND json_extract(pax.data,'$.kind')='cos'))
 UNION ALL
 SELECT 'author',au.id,au.name FROM authors au WHERE json_extract(au.data,'$.kind')='regular' AND
  EXISTS(SELECT 1 FROM asset_authors x JOIN asset_locations l ON l.asset_id=x.asset_id JOIN libraries lib ON lib.id=l.library_id
   WHERE x.author_id=au.id AND l.trashed=0 AND COALESCE(json_extract(lib.data,'$.enabled'),1)=1)
)`

func (s *Service) SearchSuggestions(ctx context.Context, query string, recommend bool, limit int) (any, error) {
	if limit == 0 {
		limit = 10
	}
	if limit < 1 || limit > 50 || len(query) > 800 {
		return nil, Fail(400, "invalid_query", "建议数量或搜索词无效。")
	}
	query = strings.TrimSpace(query)
	out := struct {
		Items []SearchSuggestion `json:"items"`
	}{Items: []SearchSuggestion{}}
	if query == "" && !recommend {
		return out, nil
	}
	cte := `WITH ` + suggestionVocabulary + `, unique_names AS (SELECT CASE WHEN count(DISTINCT id)=1 THEN min(id) ELSE '' END id,type,name FROM names GROUP BY type,name)`
	args := []any{}
	sqlQuery := ""
	if query == "" {
		sqlQuery = cte + `, shuffled AS (SELECT id,type,name,row_number() OVER(PARTITION BY type ORDER BY random()) position FROM unique_names) SELECT json_object('id',id,'type',type,'name',name) FROM shuffled ORDER BY position,type LIMIT ?`
		args = append(args, limit)
	} else {
		sqlQuery = cte + ` SELECT json_object('id',id,'type',type,'name',name) FROM unique_names WHERE instr(lower(name),lower(?))>0 ORDER BY length(name),name,type LIMIT ?`
		args = append(args, query, limit)
	}
	values, e := store.List[SearchSuggestion](ctx, s.DB.DB, sqlQuery, args...)
	if e != nil {
		return nil, e
	}
	out.Items = values
	return out, nil
}
