package service

import (
	"context"
	"errors"
	"time"

	"qimeng.local/next/server/internal/engagement"
	"qimeng.local/next/server/internal/events"
	"qimeng.local/next/server/internal/model"
	"qimeng.local/next/server/internal/store"
)

type reactionSetter func(context.Context, store.Querier, model.Asset, bool, time.Time) (model.Asset, error)

func (s *Service) SetAssetFavorite(ctx context.Context, w *Write, id string, value bool) (model.Asset, error) {
	return s.setReaction(ctx, w, id, events.TopicFavoriteChanged, engagement.SetFavorite, value, nil)
}

func (s *Service) SetAssetTodayLike(ctx context.Context, w *Write, id string, value bool) (model.Asset, error) {
	return s.setReaction(ctx, w, id, events.TopicLikeChanged, engagement.SetTodayLike, value, nil)
}

// The old request always carried a cached favorite. A mismatched value on a
// like could mean either an old like form or an actual favorite change.
func (s *Service) Reaction(ctx context.Context, w *Write, id string, favorite, liked bool) (model.Asset, error) {
	if !liked {
		return s.SetAssetFavorite(ctx, w, id, favorite)
	}
	return s.setReaction(ctx, w, id, events.TopicLikeChanged, engagement.SetTodayLike, true, &favorite)
}

func (s *Service) setReaction(ctx context.Context, w *Write, id, topic string, set reactionSetter, value bool, legacyFavorite *bool) (model.Asset, error) {
	if err := Authorize(w.Principal, "admin", ""); err != nil {
		return model.Asset{}, err
	}
	q, err := w.Q()
	if err != nil {
		return model.Asset{}, err
	}
	at := time.Now()
	a, err := readAssetOnDay(ctx, q, id, at.Format(time.DateOnly))
	if err != nil {
		return a, err
	}
	if a.Trashed {
		return a, Fail(409, "asset_trashed", "请先恢复媒体。")
	}
	if legacyFavorite != nil && *legacyFavorite != a.Favorite {
		return a, Fail(409, "reaction_intent_required", "此客户端同时提交了不一致的收藏与点赞。请升级客户端并单独提交本次操作。")
	}
	a, err = set(ctx, q, a, value, at)
	switch {
	case errors.Is(err, engagement.ErrCountFull):
		return a, Fail(409, "like_count_capacity", "累计计数已达到上限。")
	case errors.Is(err, engagement.ErrDayUnknown):
		return a, Fail(409, "legacy_like_day_unknown", "旧累计没有可核实的当日去重边界，原累计已保留。")
	}
	if err == nil {
		// Favorite and vote are engagement signals, not library membership:
		// they never publish library.changed or advance the asset set revision.
		w.changed(topic, a.ID)
	}
	return a, err
}
