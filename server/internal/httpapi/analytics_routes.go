package httpapi

import (
	"net/http"
	"strconv"

	"qimeng.local/next/server/internal/service"
	"qimeng.local/next/server/internal/transport/generated"
)

func (a *API) GetRecommendations(w http.ResponseWriter, r *http.Request, params generated.GetRecommendationsParams) {
	a.readJSON(w, r, func(r *http.Request) (any, error) {
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
}

func (a *API) GetRankings(w http.ResponseWriter, r *http.Request, params generated.GetRankingsParams) {
	a.readJSON(w, r, func(r *http.Request) (any, error) {
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
}

func (a *API) GetRecommendationPreferences(w http.ResponseWriter, r *http.Request) {
	a.readJSON(w, r, func(r *http.Request) (any, error) { return a.s.RecommendationPreferences(r.Context()) })
}

func (a *API) UpdateRecommendationPreferences(w http.ResponseWriter, r *http.Request, params generated.UpdateRecommendationPreferencesParams) {
	write[service.RecommendationUpdate](a, w, r, true, func(r *http.Request, w *service.Write, in service.RecommendationUpdate) (any, int, error) {
		v, e := a.s.UpdateRecommendationPreferences(r.Context(), w, in)
		return v, 200, e
	})
}

func (a *API) GetStatsOverview(w http.ResponseWriter, r *http.Request, params generated.GetStatsOverviewParams) {
	a.readJSON(w, r, func(r *http.Request) (any, error) { return a.s.StatsOverview(r.Context(), statsRange(r)) })
}

func (a *API) GetStatsTrends(w http.ResponseWriter, r *http.Request, params generated.GetStatsTrendsParams) {
	a.readJSON(w, r, func(r *http.Request) (any, error) { return a.s.StatsTrends(r.Context(), statsRange(r)) })
}

func (a *API) GetBrowsingReview(w http.ResponseWriter, r *http.Request, params generated.GetBrowsingReviewParams) {
	a.readJSON(w, r, func(r *http.Request) (any, error) { return a.s.BrowsingReview(r.Context(), statsRange(r)) })
}

func (a *API) GetTopInterests(w http.ResponseWriter, r *http.Request, params generated.GetTopInterestsParams) {
	a.readJSON(w, r, func(r *http.Request) (any, error) { return a.s.TopInterests(r.Context(), statsRange(r)) })
}

func (a *API) GetTopAuthors(w http.ResponseWriter, r *http.Request, params generated.GetTopAuthorsParams) {
	a.writeActivityRanking(w, r, "author")
}

func (a *API) GetTopTags(w http.ResponseWriter, r *http.Request, params generated.GetTopTagsParams) {
	a.writeActivityRanking(w, r, "tag")
}

func (a *API) GetMostViewedAssets(w http.ResponseWriter, r *http.Request, params generated.GetMostViewedAssetsParams) {
	a.writeActivityRanking(w, r, "asset")
}

func statsRange(r *http.Request) string {
	value := r.URL.Query().Get("range")
	if value == "" {
		return "7d"
	}
	return value
}

func (a *API) writeActivityRanking(w http.ResponseWriter, r *http.Request, kind string) {
	a.readJSON(w, r, func(r *http.Request) (any, error) {
		n, err := limit(r)
		if err != nil {
			return nil, err
		}
		q := r.URL.Query()
		return a.s.ActivityRanking(r.Context(), kind, statsRange(r), q.Get("metric"), q.Get("cursor"), n)
	})
}
