CREATE TABLE collection_members (
 asset_id TEXT PRIMARY KEY REFERENCES asset_records(id) ON DELETE CASCADE,
 collection_id TEXT NOT NULL REFERENCES collections(id), position INTEGER CHECK(position IS NULL OR position>0),
 reported_position INTEGER CHECK(reported_position IS NULL OR reported_position>0),
 revision INTEGER NOT NULL CHECK(revision>0),
 CHECK(position IS NULL OR reported_position IS NULL)
);
CREATE UNIQUE INDEX collection_member_slot ON collection_members(collection_id,position) WHERE position IS NOT NULL;
CREATE INDEX collection_member_order ON collection_members(collection_id,position IS NULL,position,asset_id);
CREATE INDEX collection_reported_slot ON collection_members(collection_id,reported_position) WHERE reported_position IS NOT NULL;
CREATE INDEX pending_collection_slot ON file_intents(json_extract(data,'$.After.collectionId'),json_extract(data,'$.After.page'))
 WHERE state='prepared' AND kind='ingest';
CREATE TRIGGER collection_member_library BEFORE INSERT ON collection_members WHEN NOT EXISTS(
 SELECT 1 FROM collections c JOIN asset_locations l ON l.library_id=c.library_id
 WHERE c.id=NEW.collection_id AND l.asset_id=NEW.asset_id
) BEGIN SELECT RAISE(ABORT,'member and collection belong to different libraries'); END;

-- Preserve every membership. An absent ordinal stays unknown. If several
-- old records claim one slot, retain each reported number for explicit review.
WITH observed AS (
 SELECT id,collection_id,CASE WHEN json_type(data,'$.page')='integer' AND json_extract(data,'$.page')>0
 THEN json_extract(data,'$.page') END AS n FROM asset_records WHERE collection_id!=''
), counts AS (
 SELECT *,count(*) OVER (PARTITION BY collection_id,n) AS claims FROM observed
)
INSERT INTO collection_members(asset_id,collection_id,position,reported_position,revision)
 SELECT id,collection_id,CASE WHEN claims=1 THEN n END,CASE WHEN claims>1 THEN n END,1 FROM counts;
DROP VIEW assets;
DROP INDEX assets_collection;
ALTER TABLE asset_records DROP COLUMN collection_id;
DROP TRIGGER pending_asset_update;
DROP TRIGGER backup_asset_u;
UPDATE asset_records SET data=json_remove(data,'$.collectionId','$.page')
 WHERE json_type(data,'$.collectionId') IS NOT NULL OR json_type(data,'$.page') IS NOT NULL;
CREATE TRIGGER pending_asset_update BEFORE UPDATE ON asset_records WHEN EXISTS(
 SELECT 1 FROM file_intents WHERE state='prepared' AND json_extract(data,'$.After.id')=OLD.id
) BEGIN SELECT RAISE(ABORT,'qimeng_pending_asset'); END;
CREATE TRIGGER backup_asset_u AFTER UPDATE ON asset_records
 WHEN OLD.data IS NOT NEW.data OR OLD.favorite!=NEW.favorite OR OLD.favorite_updated_at IS NOT NEW.favorite_updated_at
 BEGIN UPDATE backup_business_state SET revision=revision+1; END;
CREATE TRIGGER backup_member_i AFTER INSERT ON collection_members BEGIN UPDATE backup_business_state SET revision=revision+1; END;
CREATE TRIGGER backup_member_u AFTER UPDATE ON collection_members BEGIN UPDATE backup_business_state SET revision=revision+1; END;
CREATE TRIGGER backup_member_d AFTER DELETE ON collection_members BEGIN UPDATE backup_business_state SET revision=revision+1; END;
CREATE TRIGGER pending_member_position BEFORE UPDATE ON collection_members WHEN EXISTS(
 SELECT 1 FROM file_intents WHERE state='prepared' AND json_extract(data,'$.After.id')=OLD.asset_id
) BEGIN SELECT RAISE(ABORT,'qimeng_pending_asset'); END;

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
         'tags',json((SELECT json_group_array(x.tag ORDER BY x.ordinal,x.tag) FROM asset_tag_names x WHERE x.asset_id=r.id)))) AS data
 FROM asset_locations l JOIN asset_records r ON r.id=l.asset_id
 JOIN asset_content_versions v ON v.asset_id=l.asset_id AND v.version=l.current_version
 LEFT JOIN collection_members m ON m.asset_id=r.id;
