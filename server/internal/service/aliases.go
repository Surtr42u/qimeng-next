package service

import (
	"context"
	"slices"
	"time"

	"qimeng.local/next/server/internal/model"
	"qimeng.local/next/server/internal/store"
)

func revisedAliases(current []string, revision, expected int64, input []string) ([]string, bool, error) {
	if revision != expected {
		return nil, false, Conflict(revision)
	}
	values, err := cleanStrings(input, 100, 200)
	if err != nil {
		return nil, false, err
	}
	return values, !slices.Equal(current, values), nil
}

func (s *Service) SetTagAliases(ctx context.Context, w *Write, id string, revision int64, input []string) (Tag, error) {
	if err := Authorize(w.Principal, "admin", ""); err != nil {
		return Tag{}, err
	}
	q, err := w.Q()
	if err != nil {
		return Tag{}, err
	}
	t, err := tagByID(ctx, q, id)
	if err != nil {
		return t, err
	}
	aliases, changed, err := revisedAliases(t.Aliases, t.Revision, revision, input)
	if err != nil || !changed {
		return t, err
	}
	if err = store.ReplaceTagAliases(ctx, q, id, aliases); err != nil {
		return t, err
	}
	t.Aliases, t.Revision, t.UpdatedAt = aliases, t.Revision+1, time.Now().UTC().Format(time.RFC3339Nano)
	_, err = q.ExecContext(ctx, "UPDATE tags SET revision=?,updated_at=? WHERE id=?", t.Revision, t.UpdatedAt, id)
	return t, err
}

func (s *Service) SetCollectionAliases(ctx context.Context, w *Write, id string, revision int64, input []string) (model.Collection, error) {
	if err := Authorize(w.Principal, "admin", ""); err != nil {
		return model.Collection{}, err
	}
	q, err := w.Q()
	if err != nil {
		return model.Collection{}, err
	}
	c, err := store.Get[model.Collection](ctx, q, "SELECT data FROM collection_details WHERE id=?", id)
	if err != nil {
		return c, Normalize(err)
	}
	aliases, changed, err := revisedAliases(c.Aliases, c.Revision, revision, input)
	if err != nil || !changed {
		return c, err
	}
	if err = store.ReplaceCollectionAliases(ctx, q, id, aliases); err != nil {
		return c, err
	}
	c.Aliases, c.Revision = aliases, c.Revision+1
	_, err = q.ExecContext(ctx, "UPDATE collections SET data=json_set(data,'$.revision',?) WHERE id=?", c.Revision, id)
	return c, err
}
