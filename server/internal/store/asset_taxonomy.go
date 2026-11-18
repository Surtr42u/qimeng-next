package store

import (
	"context"

	"qimeng.local/next/server/internal/model"
)

// Nil leaves a dimension alone. A non-nil empty slice explicitly removes its memberships.
type TaxonomyEdits struct {
	SourceIDs, CharacterIDs []string
}

func CompleteTaxonomy(a model.Asset) *TaxonomyEdits {
	return &TaxonomyEdits{append([]string{}, a.SourceIDs...), append([]string{}, a.CharacterIDs...)}
}

func SaveAssetTaxonomy(ctx context.Context, q Querier, a model.Asset, edit *TaxonomyEdits) error {
	if edit.SourceIDs != nil {
		if err := saveAssetIDs(ctx, q, a.ID, "asset_sources", "source_id", edit.SourceIDs, false); err != nil {
			return err
		}
	}
	if edit.CharacterIDs != nil {
		if err := saveAssetIDs(ctx, q, a.ID, "asset_characters", "character_id", edit.CharacterIDs, false); err != nil {
			return err
		}
	}
	sourceText := ""
	if len(a.SourceIDs) == 0 {
		sourceText = a.Source
	}
	var characters []string
	if len(a.CharacterIDs) == 0 {
		characters = a.Characters
	}
	characterText, err := Encode(append([]string{}, characters...))
	if err != nil {
		return err
	}
	candidates, err := Encode(append([]string{}, a.TaxonomyCandidates...))
	if err != nil {
		return err
	}
	_, err = q.ExecContext(ctx, `INSERT INTO asset_taxonomy_state(asset_id,source_text,character_text,derived,candidates) VALUES(?1,?2,?3,?4,?5)
		ON CONFLICT(asset_id) DO UPDATE SET source_text=CASE WHEN ?6 THEN excluded.source_text ELSE source_text END,
			character_text=CASE WHEN ?7 THEN excluded.character_text ELSE character_text END,derived=excluded.derived,candidates=excluded.candidates
		WHERE (?6 AND source_text!=excluded.source_text) OR (?7 AND character_text!=excluded.character_text)
			OR derived!=excluded.derived OR candidates!=excluded.candidates`, a.ID, sourceText, characterText, a.TaxonomyDerived, candidates,
		edit.SourceIDs != nil, edit.CharacterIDs != nil)
	return err
}
