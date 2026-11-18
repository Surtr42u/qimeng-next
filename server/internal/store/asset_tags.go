package store

import (
	"context"

	"qimeng.local/next/server/internal/model"
)

// SaveAssetTags applies the complete tag-set intent to an existing asset.
// The caller owns the transaction and the tag revision it just checked.
func SaveAssetTags(ctx context.Context, q Querier, a model.Asset) error {
	raw, err := Encode(map[string]any{"tags": a.Tags, "tagsRevision": a.TagsRevision, "tagSetUpdatedAt": a.TagSetUpdatedAt,
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
	if replace {
		raw, err := Encode(a.Tags)
		if err != nil {
			return err
		}
		if _, err = q.ExecContext(ctx, "DELETE FROM asset_tags WHERE asset_id=? AND NOT EXISTS(SELECT 1 FROM json_each(?) WHERE value=tag)", a.ID, raw); err != nil {
			return err
		}
	}
	stamp := a.UpdatedAt.Format("2006-01-02T15:04:05.000000000Z")
	for _, name := range a.Tags {
		if _, err := q.ExecContext(ctx, "INSERT INTO tags(name,revision,created_at,updated_at) VALUES(?,1,?,?) ON CONFLICT(name) DO NOTHING", name, stamp, stamp); err != nil {
			return err
		}
		if _, err := q.ExecContext(ctx, "INSERT INTO asset_tags(asset_id,tag) VALUES(?,?) ON CONFLICT(asset_id,tag) DO NOTHING", a.ID, name); err != nil {
			return err
		}
	}
	return nil
}
