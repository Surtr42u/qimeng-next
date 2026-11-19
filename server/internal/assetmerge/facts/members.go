package facts

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path"

	"qimeng.local/next/server/internal/domain"
	"qimeng.local/next/server/internal/model"
)

// The normal asset read view supplies current names. These additional rows
// preserve exact relationship identities, ordering and explicit selections,
// including source terms which were only inferred from a character.
const originalRelations = `json_object(
	'authors',(SELECT json_group_array(json_object('id',author_id,'ordinal',ordinal) ORDER BY ordinal,author_id) FROM asset_authors WHERE asset_id=?1),
	'tags',(SELECT json_group_array(json_object('id',tag_id,'ordinal',ordinal) ORDER BY ordinal,tag_id) FROM asset_tags WHERE asset_id=?1),
	'explicitSources',(SELECT json_group_array(source_id ORDER BY ordinal,source_id) FROM asset_sources WHERE asset_id=?1),
	'sourceRows',(SELECT json_group_array(json_object('id',source_id,'ordinal',ordinal) ORDER BY ordinal,source_id) FROM asset_sources WHERE asset_id=?1),
	'characters',(SELECT json_group_array(json_object('id',character_id,'ordinal',ordinal) ORDER BY ordinal,character_id) FROM asset_characters WHERE asset_id=?1),
	'taxonomy',(SELECT json_object('sourceText',source_text,'characterText',json(character_text),'derived',derived,'candidates',json(candidates))
		FROM asset_taxonomy_state WHERE asset_id=?1),
	'authorOverrides',(SELECT json_group_array(json_object('authorId',author_id,'removed',removed,'listId',list_id) ORDER BY author_id)
		FROM asset_author_overrides WHERE asset_id=?1),
	'listMatches',(SELECT json_group_array(json_object('listId',list_id,'authorId',author_id,'work',work) ORDER BY list_id,author_id,work)
		FROM author_list_matches WHERE asset_id=?1),
	'humanFields',(SELECT json_group_array(field ORDER BY field) FROM asset_human_fields WHERE asset_id=?1))`

type originalMember struct {
	plan      Member
	asset     model.Asset
	original  string
	relations string
	native    struct {
		ExplicitSources []string `json:"explicitSources"`
		Taxonomy        struct {
			SourceText    string   `json:"sourceText"`
			CharacterText []string `json:"characterText"`
			Derived       int      `json:"derived"`
			Candidates    []string `json:"candidates"`
		} `json:"taxonomy"`
	}
}

func (p projection) loadMembers() ([]originalMember, error) {
	result := make([]originalMember, 1, len(p.group.Members))
	var firstSize int64
	_, destinationMIME, _ := domain.MediaType(path.Base(p.group.RelativePath))
	for index, item := range p.group.Members {
		member := originalMember{plan: item}
		var claimed, sourceBound, extraVersion bool
		err := p.tx.QueryRowContext(p.ctx, `SELECT a.data,`+originalRelations+`,
			EXISTS(SELECT 1 FROM selected_image_merge_members WHERE physical_asset_id=?1),
			EXISTS(SELECT 1 FROM migration_media_aliases WHERE source_id=?2 AND source_asset_id=?1 AND asset_id=?1)
			AND EXISTS(SELECT 1 FROM migration_relation_origins WHERE input_id=?2 AND kind='cold26/asset' AND identity=?1),
			EXISTS(SELECT 1 FROM asset_content_versions WHERE asset_id=?1 AND version!=1)
			FROM assets a WHERE a.id=?1`, item.AssetID, p.source).Scan(&member.original, &member.relations, &claimed, &sourceBound, &extraVersion)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("member %d is missing", index+1)
		}
		if err != nil {
			return nil, err
		}
		if claimed || !sourceBound {
			return nil, fmt.Errorf("member %d has a conflicting group or lacks its original v26 identity", index+1)
		}
		if err := json.Unmarshal([]byte(member.original), &member.asset); err != nil {
			return nil, errors.New("invalid native asset record")
		}
		if err := json.Unmarshal([]byte(member.relations), &member.native); err != nil {
			return nil, errors.New("invalid native relationship record")
		}
		a := member.asset
		if a.Trashed || a.LibraryID != p.group.LibraryID || a.MediaType != "image" || a.MimeType != destinationMIME ||
			!domain.ValidMediaPath(a.RelativePath) || a.FileName != path.Base(a.RelativePath) || path.Dir(a.RelativePath) != path.Dir(p.group.RelativePath) {
			return nil, fmt.Errorf("member %d is not a live image of the reviewed type in the same library and directory", index+1)
		}
		if a.ContentVersion != 1 || extraVersion || a.StorageLayoutVersion != 2 || a.ContentMtime == nil || a.SourceMtime == nil || a.Hash != p.group.SHA256 ||
			(index > 0 && a.Size != firstSize) || (item.ExpectedSize != nil && *item.ExpectedSize != a.Size) ||
			(item.ExpectedVersion != nil && *item.ExpectedVersion != a.ContentVersion) ||
			(item.ExpectedRelativePath != "" && item.ExpectedRelativePath != a.RelativePath) {
			return nil, fmt.Errorf("member %d differs from its reviewed original or version", index+1)
		}
		if a.Revision != 1 || a.MetadataSource != "migration" {
			return nil, fmt.Errorf("member %d is no longer a fresh cold projection", index+1)
		}
		firstSize = a.Size
		if a.ID == p.group.RepresentativeID {
			result[0] = member
		} else {
			result = append(result, member)
		}
	}
	return result, nil
}
