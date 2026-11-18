package httpapi

import (
	"net/http"
	"strconv"

	"qimeng.local/next/server/internal/service"
	"qimeng.local/next/server/internal/transport/generated"
)

func assetFilterRequest(r *http.Request) (service.AssetFilter, error) {
	q := r.URL.Query()
	f := service.AssetFilter{LibraryID: q.Get("libraryId"), Q: q.Get("q"), MediaType: q.Get("mediaType"), Kind: q.Get("kind"), AuthorID: q.Get("authorId"), CollectionID: q.Get("collectionId"), Tag: q.Get("tag"), TagMode: q.Get("tagMode"), Sort: q.Get("sort"), Order: q.Get("order"), TimeRange: q.Get("timeRange"), Cursor: q.Get("cursor"), AuthorIDs: q["authorIds"], SourceIDs: q["sourceIds"], CharacterIDs: q["characterIds"], CosWorkIDs: q["cosWorkIds"], WorkKeys: q["workKeys"], RoleKeys: q["roleKeys"], Tags: q["tags"]}
	var e error
	f.Limit, e = limit(r)
	if e != nil {
		return f, e
	}
	for _, field := range []struct {
		name   string
		target **bool
	}{{"favorite", &f.Favorite}, {"liked", &f.Liked}, {"history", &f.History}} {
		*field.target, e = boolQuery(r, field.name)
		if e != nil {
			return f, e
		}
	}
	f.ViewRange = q.Get("viewRange")
	f.PlayRange = q.Get("playRange")
	f.SizeRange = q.Get("sizeRange")
	if q.Has("directory") {
		if len(q["directory"]) != 1 {
			return f, service.Fail(400, "invalid_path", "一次只能查询一个明确目录。")
		}
		directory := q.Get("directory")
		f.Directory = &directory
	}
	recursive, e := boolQuery(r, "recursive")
	if e != nil {
		return f, e
	}
	f.Recursive = recursive != nil && *recursive
	includeDisabled, e := boolQuery(r, "includeDisabled")
	if e != nil {
		return f, e
	}
	f.IncludeDisabled = includeDisabled != nil && *includeDisabled
	dateCounts, e := boolQuery(r, "dateCounts")
	if e != nil {
		return f, e
	}
	f.DateCounts = dateCounts != nil && *dateCounts
	if value := q.Get("tzOffsetMinutes"); value != "" {
		f.TZOffsetMinutes, e = strconv.Atoi(value)
		if e != nil {
			return f, service.Fail(400, "invalid_timezone_offset", "客户端时区偏移必须是整数分钟。")
		}
	}
	for _, field := range []struct {
		name   string
		target **int64
	}{{"minViews", &f.MinViews}, {"maxViews", &f.MaxViews}, {"minPlays", &f.MinPlays}, {"maxPlays", &f.MaxPlays}, {"minSize", &f.MinSize}, {"maxSize", &f.MaxSize}} {
		if value := q.Get(field.name); value != "" {
			parsed, e := strconv.ParseInt(value, 10, 64)
			if e != nil || parsed < 0 {
				return f, service.Fail(400, "invalid_filter", "次数或文件大小必须为非负整数。")
			}
			*field.target = &parsed
		}
	}
	for _, field := range []struct {
		name   string
		target *int
	}{{"fromYear", &f.FromYear}, {"toYear", &f.ToYear}} {
		if value := q.Get(field.name); value != "" {
			parsed, e := strconv.Atoi(value)
			if e != nil {
				return f, service.Fail(400, "invalid_year_range", "年份必须是整数。")
			}
			*field.target = parsed
		}
	}
	return f, nil
}
func (a *API) GetAssetFacets(w http.ResponseWriter, r *http.Request, params generated.GetAssetFacetsParams) {
	a.readJSON(w, r, func(r *http.Request) (any, error) {
		f, e := assetFilterRequest(r)
		if e != nil {
			return nil, e
		}
		return a.s.AssetFacets(r.Context(), f, r.URL.Query().Get("row"), r.URL.Query().Get("facetQ"))
	})
}

func (a *API) GetSearchSuggestions(w http.ResponseWriter, r *http.Request, params generated.GetSearchSuggestionsParams) {
	a.readJSON(w, r, func(r *http.Request) (any, error) {
		n, e := limit(r)
		if e != nil {
			return nil, e
		}
		recommended, e := boolQuery(r, "recommend")
		if e != nil {
			return nil, e
		}
		return a.s.SearchSuggestions(r.Context(), r.URL.Query().Get("q"), recommended != nil && *recommended, n)
	})
}

func (a *API) ListCosWorks(w http.ResponseWriter, r *http.Request, params generated.ListCosWorksParams) {
	a.readJSON(w, r, func(r *http.Request) (any, error) {
		n, e := limit(r)
		if e != nil {
			return nil, e
		}
		q := r.URL.Query()
		return a.s.CosWorks(r.Context(), q.Get("libraryId"), q.Get("authorId"), q.Get("q"), q.Get("cursor"), n)
	})
}
