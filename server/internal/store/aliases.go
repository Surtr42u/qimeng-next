package store

import (
	"context"

	"qimeng.local/next/server/internal/model"
)

// Alias rows belong to one stable owner. An edit removes only trailing
// ordinals and updates only slots whose displayed text actually changed.
func ReplaceTagAliases(ctx context.Context, q Querier, id string, aliases []string) error {
	return replaceAliases(ctx, q, "tag_aliases", "tag_id", id, aliases)
}

func ReplaceCollectionAliases(ctx context.Context, q Querier, id string, aliases []string) error {
	return replaceAliases(ctx, q, "collection_aliases", "collection_id", id, aliases)
}

func replaceAliases(ctx context.Context, q Querier, table, owner, id string, aliases []string) error {
	if _, err := q.ExecContext(ctx, "DELETE FROM "+table+" WHERE "+owner+"=? AND ordinal>=?", id, len(aliases)); err != nil {
		return err
	}
	if len(aliases) == 0 {
		return nil
	}
	raw, err := Encode(aliases)
	if err != nil {
		return err
	}
	_, err = q.ExecContext(ctx, "INSERT INTO "+table+"("+owner+",ordinal,alias) SELECT ?,CAST(j.key AS INTEGER),j.value FROM json_each(?) j "+
		"WHERE true ON CONFLICT("+owner+",ordinal) DO UPDATE SET alias=excluded.alias WHERE alias!=excluded.alias", id, raw)
	return err
}

// InsertCollection is called in the same transaction as its optional aliases
// or the naming plan which creates an image set.
func InsertCollection(ctx context.Context, q Querier, c model.Collection) error {
	raw, err := Encode(c)
	if err != nil {
		return err
	}
	if _, err = q.ExecContext(ctx, `INSERT INTO collections(id,library_id,name,kind,data,naming_revision)
		VALUES(?,?,?,?,json_remove(?,'$.aliases'),?)`, c.ID, c.LibraryID, c.Name, c.Kind, raw, c.Revision); err != nil {
		return err
	}
	if len(c.Aliases) == 0 {
		return nil
	}
	return ReplaceCollectionAliases(ctx, q, c.ID, c.Aliases)
}
