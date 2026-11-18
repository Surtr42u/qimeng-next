// Package engagement owns an asset's favorite state and dated votes.
// The caller supplies an authorized transaction and one server-local instant.
package engagement

import (
	"context"
	"errors"
	"math"
	"time"

	"qimeng.local/next/server/internal/model"
	"qimeng.local/next/server/internal/store"
)

var ErrDayUnknown = errors.New("imported vote day is unknown")
var ErrCountFull = errors.New("vote count is at capacity")

// SetFavorite only changes the favorite projection. Repeating a choice
// preserves its date and the asset revision.
func SetFavorite(ctx context.Context, q store.Querier, a model.Asset, value bool, at time.Time) (model.Asset, error) {
	if a.Favorite == value {
		return a, nil
	}
	now := at.UTC()
	a.Favorite = value
	a.FavoriteUpdatedAt = &now
	a.FavoritedAt = nil
	var favoriteDate any
	if value {
		a.FavoritedAt = &now
		favoriteDate = timestamp(now)
	}
	a.Revision++
	a.UpdatedAt = now
	_, err := q.ExecContext(ctx, `UPDATE asset_records SET favorite=?,favorited_at=?,favorite_updated_at=?,revision=?,updated_at=?,
		data=json_set(data,'$.revision',?,'$.updatedAt',?) WHERE id=?`,
		value, favoriteDate, timestamp(now), a.Revision, timestamp(now), a.Revision, timestamp(now), a.ID)
	return a, err
}

// SetTodayLike changes only the vote on the supplied day. Earlier votes
// and the part of an imported total with no known date keep their identity.
func SetTodayLike(ctx context.Context, q store.Querier, a model.Asset, value bool, at time.Time) (model.Asset, error) {
	if a.Liked == value {
		return a, nil
	}
	day := at.Format(time.DateOnly)
	now := at.UTC()
	if value {
		if a.LikeCount == math.MaxInt64 {
			return a, ErrCountFull
		}
		var unknown bool
		err := q.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM like_baselines WHERE asset_id=? AND ((last_day='' AND observed_day>=?) OR last_day>?))",
			a.ID, day, day).Scan(&unknown)
		if err != nil {
			return a, err
		}
		if unknown {
			return a, ErrDayUnknown
		}
		if _, err = q.ExecContext(ctx, "INSERT INTO likes(asset_id,day,created_at) VALUES(?,?,?)", a.ID, day, now.Format(time.RFC3339Nano)); err != nil {
			return a, err
		}
		a.LikeCount++
	} else {
		if _, err := q.ExecContext(ctx, "DELETE FROM likes WHERE asset_id=? AND day=?", a.ID, day); err != nil {
			return a, err
		}
		a.LikeCount--
	}
	a.Liked = value
	a.Revision++
	a.UpdatedAt = now
	_, err := q.ExecContext(ctx, "UPDATE asset_records SET revision=?,updated_at=?,data=json_set(data,'$.revision',?,'$.updatedAt',?) WHERE id=?",
		a.Revision, timestamp(now), a.Revision, timestamp(now), a.ID)
	return a, err
}

func timestamp(t time.Time) string { return t.Format("2006-01-02T15:04:05.000000000Z") }
