ALTER TABLE asset_sources ADD COLUMN ordinal INTEGER NOT NULL DEFAULT 0;
ALTER TABLE asset_characters ADD COLUMN ordinal INTEGER NOT NULL DEFAULT 0;
-- Membership was already used by filters. The old arrays supply ordering evidence only.
UPDATE asset_sources AS x SET ordinal=coalesce((SELECT min(CAST(j.key AS INTEGER))
 FROM asset_records r,json_each(r.data,'$.sourceIds') j WHERE r.id=x.asset_id AND j.value=x.source_id),2147483647);
UPDATE asset_characters AS x SET ordinal=coalesce((SELECT min(CAST(j.key AS INTEGER))
 FROM asset_records r,json_each(r.data,'$.characterIds') j WHERE r.id=x.asset_id AND j.value=x.character_id),2147483647);
CREATE INDEX source_assets ON asset_sources(source_id,asset_id);
CREATE INDEX character_assets ON asset_characters(character_id,asset_id);
-- Published upload/plan DTOs include character parents. Retain the original selection
-- separately while these declarations are staged. Older declarations keep their recorded IDs.
ALTER TABLE uploads ADD COLUMN declared_source_ids TEXT CHECK(declared_source_ids IS NULL OR json_type(declared_source_ids)='array');
ALTER TABLE plans ADD COLUMN declared_source_ids TEXT CHECK(declared_source_ids IS NULL OR json_type(declared_source_ids)='object');

CREATE TABLE asset_taxonomy_state(
 asset_id TEXT PRIMARY KEY REFERENCES asset_records(id) ON DELETE CASCADE,
 source_text TEXT NOT NULL, character_text TEXT NOT NULL CHECK(json_type(character_text)='array'),
 derived INTEGER NOT NULL CHECK(derived IN (0,1)), candidates TEXT NOT NULL CHECK(json_type(candidates)='array')
);
INSERT INTO asset_taxonomy_state(asset_id,source_text,character_text,derived,candidates)
 SELECT r.id,
 CASE WHEN NOT EXISTS(SELECT 1 FROM asset_sources x WHERE x.asset_id=r.id)
   AND coalesce(json_array_length(r.data,'$.sourceIds'),0)=0 THEN coalesce(json_extract(r.data,'$.source'),'') ELSE '' END,
 CASE WHEN NOT EXISTS(SELECT 1 FROM asset_characters x WHERE x.asset_id=r.id)
   AND coalesce(json_array_length(r.data,'$.characterIds'),0)=0 AND json_type(r.data,'$.characters')='array'
   THEN json_extract(r.data,'$.characters') ELSE '[]' END,
 coalesce(json_extract(r.data,'$.taxonomyDerived'),0),
 CASE WHEN json_type(r.data,'$.taxonomyCandidates')='array' THEN json_extract(r.data,'$.taxonomyCandidates') ELSE '[]' END
 FROM asset_records r;

DROP VIEW assets;
DROP TRIGGER pending_asset_update;
DROP TRIGGER backup_asset_u;
UPDATE asset_records SET data=json_remove(data,'$.source','$.sourceIds','$.characters','$.characterIds',
 '$.taxonomyDerived','$.taxonomyCandidates','$.workKeys','$.roleKeys','$.workFacets','$.roleFacets');
CREATE TRIGGER pending_asset_update BEFORE UPDATE ON asset_records WHEN EXISTS(
 SELECT 1 FROM file_intents WHERE state='prepared' AND json_extract(data,'$.After.id')=OLD.id
) BEGIN SELECT RAISE(ABORT,'qimeng_pending_asset'); END;
CREATE TRIGGER backup_asset_u AFTER UPDATE ON asset_records
 WHEN OLD.data IS NOT NEW.data OR OLD.favorite!=NEW.favorite OR OLD.favorite_updated_at IS NOT NEW.favorite_updated_at
 BEGIN UPDATE backup_business_state SET revision=revision+1; END;
CREATE TRIGGER backup_taxonomy_state_i AFTER INSERT ON asset_taxonomy_state BEGIN UPDATE backup_business_state SET revision=revision+1; END;
CREATE TRIGGER backup_taxonomy_state_u AFTER UPDATE ON asset_taxonomy_state BEGIN UPDATE backup_business_state SET revision=revision+1; END;
CREATE TRIGGER backup_taxonomy_state_d AFTER DELETE ON asset_taxonomy_state BEGIN UPDATE backup_business_state SET revision=revision+1; END;
CREATE TRIGGER backup_asset_source_i AFTER INSERT ON asset_sources BEGIN UPDATE backup_business_state SET revision=revision+1; END;
CREATE TRIGGER backup_asset_source_u AFTER UPDATE ON asset_sources BEGIN UPDATE backup_business_state SET revision=revision+1; END;
CREATE TRIGGER backup_asset_source_d AFTER DELETE ON asset_sources BEGIN UPDATE backup_business_state SET revision=revision+1; END;
CREATE TRIGGER backup_asset_character_i AFTER INSERT ON asset_characters BEGIN UPDATE backup_business_state SET revision=revision+1; END;
CREATE TRIGGER backup_asset_character_u AFTER UPDATE ON asset_characters BEGIN UPDATE backup_business_state SET revision=revision+1; END;
CREATE TRIGGER backup_asset_character_d AFTER DELETE ON asset_characters BEGIN UPDATE backup_business_state SET revision=revision+1; END;

-- Explicitly recorded sources retain their order. A character's current parent follows
-- them once, in the order of its earliest selected character.
CREATE VIEW asset_source_memberships AS
 SELECT x.asset_id,x.source_id,0 AS inferred,x.ordinal FROM asset_sources x
 UNION ALL
 SELECT x.asset_id,c.source_id,1,min(x.ordinal) FROM asset_characters x JOIN characters c ON c.id=x.character_id
 WHERE NOT EXISTS(SELECT 1 FROM asset_sources s WHERE s.asset_id=x.asset_id AND s.source_id=c.source_id)
 GROUP BY x.asset_id,c.source_id;
CREATE VIEW assets AS
 SELECT l.rowid AS rowid,r.id,l.library_id,l.relative_path,l.file_name,v.media_type,r.author_id,m.collection_id,
        v.hash,v.size,r.revision,r.created_at,r.updated_at,r.favorite,l.trashed,r.file_date,r.favorited_at,r.cos_work_id,
        json_patch(r.data,json_object('id',r.id,'libraryId',l.library_id,'relativePath',l.relative_path,'fileName',l.file_name,
         'collectionId',m.collection_id,'page',m.position,'contentVersion',v.version,'hash',v.hash,'size',v.size,'mediaType',v.media_type,'mimeType',v.mime_type,
         'width',v.width,'height',v.height,'durationMs',v.duration_ms,'probeState',v.probe_state,
         'trashed',json(CASE l.trashed WHEN 1 THEN 'true' ELSE 'false' END),
         'contentMtime',l.content_mtime,'storageLayoutVersion',nullif(l.storage_layout,0),
         'favorite',json(CASE r.favorite WHEN 1 THEN 'true' ELSE 'false' END),'favoriteUpdatedAt',r.favorite_updated_at,'favoritedAt',r.favorited_at,
         'authorId',r.author_id,'authorName',(SELECT name FROM authors WHERE id=r.author_id),
         'authorIds',json((SELECT json_group_array(aa.author_id ORDER BY aa.ordinal,aa.author_id) FROM asset_authors aa WHERE aa.asset_id=r.id)),
         'tags',json((SELECT json_group_array(x.tag ORDER BY x.ordinal,x.tag) FROM asset_tag_names x WHERE x.asset_id=r.id)),
         'sourceIds',json((SELECT json_group_array(x.source_id ORDER BY x.inferred,x.ordinal,x.source_id) FROM asset_source_memberships x WHERE x.asset_id=r.id)),
         'source',coalesce((SELECT group_concat(s.name,'+' ORDER BY x.inferred,x.ordinal,x.source_id)
             FROM asset_source_memberships x JOIN sources s ON s.id=x.source_id WHERE x.asset_id=r.id),t.source_text,''),
         'characterIds',json((SELECT json_group_array(x.character_id ORDER BY x.ordinal,x.character_id) FROM asset_characters x WHERE x.asset_id=r.id)),
         'characters',json(coalesce((SELECT json_group_array(c.name ORDER BY x.ordinal,x.character_id)
             FROM asset_characters x JOIN characters c ON c.id=x.character_id WHERE x.asset_id=r.id HAVING count(*)>0),t.character_text,'[]')),
         'taxonomyDerived',json(CASE t.derived WHEN 1 THEN 'true' ELSE 'false' END),'taxonomyCandidates',json(coalesce(t.candidates,'[]')))) AS data
 FROM asset_locations l JOIN asset_records r ON r.id=l.asset_id
 JOIN asset_content_versions v ON v.asset_id=l.asset_id AND v.version=l.current_version
 LEFT JOIN collection_members m ON m.asset_id=r.id LEFT JOIN asset_taxonomy_state t ON t.asset_id=r.id;
