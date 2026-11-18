package service

import (
	"context"
	"math"

	"qimeng.local/next/server/internal/domain"
	"qimeng.local/next/server/internal/model"
	"qimeng.local/next/server/internal/store"
)

type CollectionMember struct {
	CollectionID     string `json:"collectionId"`
	AssetID          string `json:"assetId"`
	Revision         int64  `json:"revision"`
	Position         *int   `json:"position,omitempty"`
	ReportedPosition *int   `json:"reportedPosition,omitempty"`
}

const memberJSON = `json_object('collectionId',collection_id,'assetId',asset_id,'revision',revision,
 'position',position,'reportedPosition',reported_position)`

func (s *Service) CollectionMembers(ctx context.Context, id, cursor string, limit int) (model.Page[CollectionMember], error) {
	n, err := limited(limit, 100)
	if err != nil {
		return model.Page[CollectionMember]{}, err
	}
	var exists int
	if err = s.DB.DB.QueryRowContext(ctx, "SELECT 1 FROM collections WHERE id=?", id).Scan(&exists); err != nil {
		return model.Page[CollectionMember]{}, Normalize(err)
	}
	key := "collection-members:" + id
	offset, err := pageOffset(cursor, key)
	if err != nil {
		return model.Page[CollectionMember]{}, err
	}
	items, err := store.List[CollectionMember](ctx, s.DB.DB, "SELECT "+memberJSON+` FROM collection_members
		WHERE collection_id=? ORDER BY position IS NULL,position,asset_id LIMIT ? OFFSET ?`, id, n+1, offset)
	return paged(items, n, offset, key), err
}

func (s *Service) RenameCollection(ctx context.Context, w *Write, id, name string, revision int64) (model.Collection, error) {
	if err := Authorize(w.Principal, "admin", ""); err != nil {
		return model.Collection{}, err
	}
	if !domain.ValidName(name) || !domain.ValidMediaPath(name) {
		return model.Collection{}, Fail(400, "invalid_collection", "集合名称无效。")
	}
	q, err := w.Q()
	if err != nil {
		return model.Collection{}, err
	}
	c, err := store.Get[model.Collection](ctx, q, "SELECT data FROM collections WHERE id=?", id)
	if err != nil {
		return c, Normalize(err)
	}
	if c.Revision != revision {
		return c, Conflict(c.Revision)
	}
	if c.Name == name {
		return c, nil
	}
	c.Name, c.Revision = name, c.Revision+1
	_, err = q.ExecContext(ctx, "UPDATE collections SET name=?,data=json_set(data,'$.name',?,'$.revision',?) WHERE id=?", name, name, c.Revision, id)
	return c, err
}

func (s *Service) SetCollectionMemberPosition(ctx context.Context, w *Write, id, assetID string, position int, revision int64) (CollectionMember, error) {
	if err := Authorize(w.Principal, "admin", ""); err != nil {
		return CollectionMember{}, err
	}
	if position < 1 {
		return CollectionMember{}, Fail(400, "invalid_position", "集合成员位置须为正整数。")
	}
	q, err := w.Q()
	if err != nil {
		return CollectionMember{}, err
	}
	member, err := store.Get[CollectionMember](ctx, q, "SELECT "+memberJSON+" FROM collection_members WHERE collection_id=? AND asset_id=?", id, assetID)
	if err != nil {
		return member, Normalize(err)
	}
	if member.Revision != revision {
		return member, Conflict(member.Revision)
	}
	if member.Position != nil && *member.Position == position {
		return member, nil
	}
	if err = checkCollectionSlot(ctx, q, id, assetID, position); err != nil {
		return member, err
	}
	member, err = store.Get[CollectionMember](ctx, q, `UPDATE collection_members SET position=?,reported_position=NULL,revision=revision+1
		WHERE collection_id=? AND asset_id=? RETURNING `+memberJSON, position, id, assetID)
	return member, Normalize(err)
}

// The writer lock serializes admission. A durable ingest may release that
// lock before committing, so its promised position also occupies the slot.
func checkCollectionSlot(ctx context.Context, q store.Querier, collection, asset string, position int) error {
	var occupied bool
	err := q.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM collection_members
		WHERE collection_id=?1 AND position=?2 AND asset_id!=?3) OR EXISTS(SELECT 1 FROM file_intents
		WHERE state='prepared' AND kind='ingest' AND json_extract(data,'$.After.collectionId')=?1
		AND json_extract(data,'$.After.page')=?2 AND json_extract(data,'$.After.id')!=?3)`, collection, position, asset).Scan(&occupied)
	if err != nil {
		return err
	}
	if occupied {
		return Fail(409, "collection_position_occupied", "集合中的这个位置已有成员或正在发布的文件。")
	}
	return nil
}

func (s *Service) maximumCollectionPosition(ctx context.Context, id string) (int, error) {
	var n int
	err := s.DB.DB.QueryRowContext(ctx, `SELECT max(
		coalesce((SELECT max(position) FROM collection_members WHERE collection_id=?1 AND position IS NOT NULL),0),
		coalesce((SELECT max(reported_position) FROM collection_members WHERE collection_id=?1 AND reported_position IS NOT NULL),0),
		coalesce((SELECT max(json_extract(data,'$.After.page')) FROM file_intents WHERE state='prepared' AND kind='ingest'
		AND json_extract(data,'$.After.collectionId')=?1),0))`, id).Scan(&n)
	if n == math.MaxInt {
		return 0, Fail(409, "collection_position_exhausted", "此集合已用尽可表示的位置。")
	}
	return n, err
}
