package httpapi

import (
	"net/http"
	"strconv"

	"qimeng.local/next/server/internal/service"
)

func (a *API) analyticsRoutes() {
	a.get("/recommendations", func(r *http.Request) (any, error) {
		q := r.URL.Query()
		n, e := limit(r)
		if e != nil {
			return nil, e
		}
		var seed int64
		if q.Get("seed") != "" {
			seed, e = strconv.ParseInt(q.Get("seed"), 10, 64)
			if e != nil {
				return nil, service.Fail(400, "invalid_seed", "推荐种子必须是整数。")
			}
		}
		return a.s.Recommendations(r.Context(), service.RecommendationFilter{Kind: q.Get("kind"), MediaType: q.Get("mediaType"), Seed: seed, Cursor: q.Get("cursor"), Limit: n})
	})
	a.get("/rankings", func(r *http.Request) (any, error) {
		q := r.URL.Query()
		n, e := limit(r)
		if e != nil {
			return nil, e
		}
		period := q.Get("period")
		if period == "" {
			period = "week"
		}
		return a.s.Rankings(r.Context(), period, q.Get("kind"), q.Get("cursor"), n, q.Get("mediaType"))
	})
	a.get("/preferences/recommendation", func(r *http.Request) (any, error) { return a.s.RecommendationPreferences(r.Context()) })
	write[service.RecommendationUpdate](a, "PUT", "/preferences/recommendation", true, func(r *http.Request, w *service.Write, in service.RecommendationUpdate) (any, int, error) {
		v, e := a.s.UpdateRecommendationPreferences(r.Context(), w, in)
		return v, 200, e
	})
	a.get("/stats/overview", func(r *http.Request) (any, error) { return a.s.StatsOverview(r.Context(), statsRange(r)) })
	a.get("/stats/trends", func(r *http.Request) (any, error) { return a.s.StatsTrends(r.Context(), statsRange(r)) })
	a.get("/stats/review", func(r *http.Request) (any, error) { return a.s.BrowsingReview(r.Context(), statsRange(r)) })
	a.get("/stats/top-interests", func(r *http.Request) (any, error) { return a.s.TopInterests(r.Context(), statsRange(r)) })
	for path, kind := range map[string]string{"/stats/top-authors": "author", "/stats/top-tags": "tag", "/stats/most-viewed": "asset"} {
		a.get(path, func(r *http.Request) (any, error) {
			n, e := limit(r)
			if e != nil {
				return nil, e
			}
			q := r.URL.Query()
			return a.s.ActivityRanking(r.Context(), kind, statsRange(r), q.Get("metric"), q.Get("cursor"), n)
		})
	}
}

func statsRange(r *http.Request) string {
	value := r.URL.Query().Get("range")
	if value == "" {
		return "7d"
	}
	return value
}
