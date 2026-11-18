package service

import (
	"context"
	"slices"

	"qimeng.local/next/server/internal/model"
	"qimeng.local/next/server/internal/store"
)

func uploadDeclaredSources(ctx context.Context, q store.Querier, u model.Upload) ([]string, error) {
	fallback, err := store.Encode(append([]string{}, u.SourceIDs...))
	if err != nil {
		return nil, err
	}
	return store.Get[[]string](ctx, q, "SELECT coalesce(declared_source_ids,?) FROM uploads WHERE id=?", fallback, u.ID)
}

func editAssetTaxonomy(ctx context.Context, q store.Querier, a *model.Asset, in AssetUpdate) (*store.TaxonomyEdits, error) {
	if in.SourceIDs == nil && in.CharacterIDs == nil && in.Source == nil && in.Characters == nil {
		return nil, nil
	}
	edit := &store.TaxonomyEdits{}
	var err error
	if in.SourceIDs != nil || in.CharacterIDs != nil {
		sources := a.SourceIDs
		characters := a.CharacterIDs
		if in.SourceIDs != nil {
			edit.SourceIDs, err = cleanStrings(*in.SourceIDs, 20, 100)
			sources = edit.SourceIDs
		} else {
			// A currently implied parent must not become an owner's explicit source when editing roles.
			sources, err = store.Get[[]string](ctx, q, "SELECT json_group_array(source_id ORDER BY ordinal,source_id) FROM asset_sources WHERE asset_id=?", a.ID)
		}
		if err != nil {
			return nil, err
		}
		if in.CharacterIDs != nil {
			edit.CharacterIDs, err = cleanStrings(*in.CharacterIDs, 50, 100)
			if err != nil {
				return nil, err
			}
			characters = edit.CharacterIDs
		}
		if err = resolveTaxonomyOn(ctx, q, a, sources, characters); err != nil {
			return nil, err
		}
	}
	if in.Source != nil {
		if len(*in.Source) > 200 {
			return nil, Fail(400, "invalid_source", "出处名称过长。")
		}
		if len(a.SourceIDs) > 0 && *in.Source != a.Source {
			return nil, Fail(409, "taxonomy_ids_required", "出处已关联词条。请使用 sourceIds 修改，并核对所选角色的所属出处。")
		}
		if len(a.SourceIDs) == 0 {
			a.Source = *in.Source
			edit.SourceIDs = []string{}
		}
	}
	if in.Characters != nil {
		values, err := cleanStrings(*in.Characters, 50, 200)
		if err != nil {
			return nil, err
		}
		if len(a.CharacterIDs) > 0 && !slices.Equal(values, a.Characters) {
			return nil, Fail(409, "taxonomy_ids_required", "角色已关联词条。请使用 characterIds 修改或清空。")
		}
		if len(a.CharacterIDs) == 0 {
			a.Characters = values
			edit.CharacterIDs = []string{}
		}
	}
	a.TaxonomyDerived = false
	a.TaxonomyCandidates = nil
	return edit, nil
}
