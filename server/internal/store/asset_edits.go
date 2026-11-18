package store

import (
	"context"

	"qimeng.local/next/server/internal/model"
)

// SaveAssetEdits writes editable catalog fields. Only an explicit author or
// taxonomy edit can touch its respective membership rows.
func SaveAssetEdits(ctx context.Context, q Querier, a model.Asset, authors bool, taxonomy *TaxonomyEdits) error {
	patch := map[string]any{"revision": a.Revision, "updatedAt": a.UpdatedAt, "metadataSource": a.MetadataSource,
		"note": a.Note, "acquisitionSources": a.AcquisitionSources}
	set := "revision=?,updated_at=?,data=json_patch(data,?)"
	if authors {
		patch["authorsRevision"] = a.AuthorsRevision
		patch["authorStatus"] = a.AuthorStatus
		set += ",author_id=?"
	}
	raw, err := Encode(patch)
	if err != nil {
		return err
	}
	args := []any{a.Revision, a.UpdatedAt.Format("2006-01-02T15:04:05.000000000Z"), raw}
	if authors {
		args = append(args, a.AuthorID)
	}
	if _, err = q.ExecContext(ctx, "UPDATE asset_records SET "+set+" WHERE id=?", append(args, a.ID)...); err != nil {
		return err
	}
	if authors {
		if err = saveAssetIDs(ctx, q, a.ID, "asset_authors", "author_id", a.AuthorIDs, false); err != nil {
			return err
		}
	}
	if taxonomy != nil {
		return SaveAssetTaxonomy(ctx, q, a, taxonomy)
	}
	return nil
}
