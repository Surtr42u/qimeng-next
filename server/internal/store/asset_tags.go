package store

import (
	"context"

	"qimeng.local/next/server/internal/model"
)

// SaveAssetTags applies the complete tag-set intent to an existing asset.
// The caller owns the transaction and the tag revision it just checked.
func SaveAssetTags(ctx context.Context, q Querier, a model.Asset) error {
	raw, err := Encode(map[string]any{"tagsRevision": a.TagsRevision, "tagSetUpdatedAt": a.TagSetUpdatedAt,
		"tagSetSource": a.TagSetSource, "revision": a.Revision, "updatedAt": a.UpdatedAt})
	if err != nil {
		return err
	}
	if _, err = q.ExecContext(ctx, `UPDATE asset_records SET revision=?,updated_at=?,data=json_patch(data,?) WHERE id=?`,
		a.Revision, a.UpdatedAt.Format("2006-01-02T15:04:05.000000000Z"), raw, a.ID); err != nil {
		return err
	}
	return writeAssetTags(ctx, q, a, true)
}

func writeAssetTags(ctx context.Context, q Querier, a model.Asset, replace bool) error {
	if !replace && len(a.Tags) == 0 {
		return nil
	}
	raw, err := Encode(a.Tags)
	if err != nil {
		return err
	}
	if replace {
		if _, err = q.ExecContext(ctx, `DELETE FROM asset_tags WHERE asset_id=? AND NOT EXISTS(
			SELECT 1 FROM json_each(?) j JOIN tags t ON t.name=j.value AND t.deleted=0 WHERE t.id=asset_tags.tag_id)`, a.ID, raw); err != nil {
			return err
		}
	}
	stamp := a.UpdatedAt.Format("2006-01-02T15:04:05.000000000Z")
	for _, name := range a.Tags {
		if _, err := q.ExecContext(ctx, InsertAvailableTag, name, stamp, stamp); err != nil {
			return err
		}
	}
	_, err = q.ExecContext(ctx, `INSERT INTO asset_tags(asset_id,tag_id,ordinal)
		SELECT ?, (SELECT id FROM tags WHERE name=j.value AND deleted=0),CAST(j.key AS INTEGER)
		FROM json_each(?) j WHERE j.type='text' ON CONFLICT(asset_id,tag_id) DO UPDATE SET ordinal=excluded.ordinal
		WHERE ordinal!=excluded.ordinal`, a.ID, raw)
	return err
}

const InsertAvailableTag = `INSERT INTO tags(name,revision,created_at,updated_at) SELECT ?1,1,?2,?3
 WHERE NOT EXISTS(SELECT 1 FROM tags WHERE name=?1) AND NOT EXISTS(SELECT 1 FROM retired_tag_names WHERE name=?1)`

const TagUnavailable = `SELECT NOT EXISTS(SELECT 1 FROM tags WHERE name=?1 AND deleted=0)
 AND (EXISTS(SELECT 1 FROM tags WHERE name=?1 AND deleted=1) OR EXISTS(SELECT 1 FROM retired_tag_names WHERE name=?1))`
