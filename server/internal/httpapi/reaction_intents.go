package httpapi

import (
	"net/http"

	"qimeng.local/next/server/internal/service"
	"qimeng.local/next/server/internal/transport/generated"
)

func (a *API) setAssetFavorite(r *http.Request, w *service.Write, in generated.AssetFavoriteUpdate) (any, int, error) {
	asset, err := a.s.SetAssetFavorite(r.Context(), w, r.PathValue("id"), in.Favorite)
	return asset, http.StatusOK, err
}

func (a *API) setAssetTodayLike(r *http.Request, w *service.Write, in generated.AssetTodayLikeUpdate) (any, int, error) {
	asset, err := a.s.SetAssetTodayLike(r.Context(), w, r.PathValue("id"), in.Liked)
	return asset, http.StatusOK, err
}

func (a *API) updateReaction(r *http.Request, w *service.Write, in generated.ReactionUpdate) (any, int, error) {
	asset, err := a.s.Reaction(r.Context(), w, r.PathValue("id"), in.Favorite, in.Liked)
	return asset, http.StatusOK, err
}
