DROP VIEW assets;
DROP TRIGGER asset_tag_requires_active_word;
DROP TRIGGER backup_tag_i;
DROP TRIGGER backup_tag_u;
DROP TRIGGER backup_tag_d;
DROP TRIGGER pending_asset_update;
DROP TRIGGER backup_asset_u;
DROP TRIGGER author_mirror_asset_update;
DROP INDEX tags_lookup;
ALTER TABLE tags RENAME TO previous_tag_words;
ALTER TABLE asset_tags RENAME TO previous_asset_tags;

CREATE TABLE tags (
 id TEXT PRIMARY KEY NOT NULL DEFAULT(lower(hex(randomblob(16)))), name TEXT NOT NULL,
 revision INTEGER NOT NULL, deleted INTEGER NOT NULL DEFAULT 0, created_at TEXT NOT NULL, updated_at TEXT NOT NULL
);
CREATE UNIQUE INDEX tag_live_name ON tags(name) WHERE deleted=0;
CREATE INDEX tag_by_name ON tags(name,deleted,revision);
CREATE TABLE retired_tag_names(name TEXT PRIMARY KEY,revision INTEGER NOT NULL);
CREATE TABLE asset_tags (
 asset_id TEXT NOT NULL REFERENCES asset_records(id) ON DELETE CASCADE,
 tag_id TEXT NOT NULL REFERENCES tags(id), ordinal INTEGER NOT NULL,
 PRIMARY KEY(asset_id,tag_id)
);
CREATE INDEX tags_lookup ON asset_tags(tag_id,asset_id);
-- The same pre-upgrade snapshot receives the same native identities on each
-- restore. Historically only the exact, case-sensitive name was recorded.
INSERT INTO tags(id,name,revision,deleted,created_at,updated_at)
 SELECT 'legacy-'||lower(hex(name)),name,revision,deleted,created_at,updated_at FROM previous_tag_words;
INSERT INTO asset_tags(asset_id,tag_id,ordinal)
 SELECT a.asset_id,t.id,coalesce((SELECT min(CAST(j.key AS INTEGER)) FROM json_each(r.data,'$.tags') j WHERE j.value=a.tag),2147483647)
 FROM previous_asset_tags a JOIN tags t ON t.name=a.tag JOIN asset_records r ON r.id=a.asset_id;
DROP TABLE previous_asset_tags;
DROP TABLE previous_tag_words;
CREATE VIEW asset_tag_names AS SELECT x.asset_id,x.tag_id,t.name AS tag,x.ordinal
 FROM asset_tags x JOIN tags t ON t.id=x.tag_id WHERE t.deleted=0;
CREATE TRIGGER asset_tag_requires_active_word BEFORE INSERT ON asset_tags
 WHEN NOT EXISTS(SELECT 1 FROM tags WHERE id=NEW.tag_id AND deleted=0)
 BEGIN SELECT RAISE(ABORT,'tag_not_active'); END;
CREATE TRIGGER stable_tag_id BEFORE UPDATE OF id ON tags WHEN OLD.id!=NEW.id
 BEGIN SELECT RAISE(ABORT,'tag identity is immutable'); END;
CREATE TRIGGER backup_tag_i AFTER INSERT ON tags BEGIN UPDATE backup_business_state SET revision=revision+1; END;
CREATE TRIGGER backup_tag_u AFTER UPDATE ON tags WHEN OLD.revision!=NEW.revision OR OLD.deleted!=NEW.deleted OR OLD.name!=NEW.name
 BEGIN UPDATE backup_business_state SET revision=revision+1; END;
CREATE TRIGGER backup_tag_d AFTER DELETE ON tags BEGIN UPDATE backup_business_state SET revision=revision+1; END;

ALTER TABLE asset_authors ADD COLUMN ordinal INTEGER NOT NULL DEFAULT 0;
UPDATE asset_authors AS aa SET ordinal=coalesce((SELECT min(CAST(j.key AS INTEGER)) FROM asset_records r,
 json_each(r.data,'$.authorIds') j WHERE r.id=aa.asset_id AND j.value=aa.author_id),2147483647);
ALTER TABLE asset_records ADD COLUMN favorite_updated_at TEXT;
ALTER TABLE asset_records DROP COLUMN liked;
UPDATE asset_records SET favorite_updated_at=json_extract(data,'$.favoriteUpdatedAt'),
 data=json_remove(data,'$.tags','$.authorId','$.authorIds','$.authorName','$.favorite','$.favoriteUpdatedAt','$.favoritedAt',
 '$.liked','$.likeCount','$.unattributedLikeCount');
CREATE TRIGGER pending_asset_update BEFORE UPDATE ON asset_records WHEN EXISTS(
 SELECT 1 FROM file_intents WHERE state='prepared' AND json_extract(data,'$.After.id')=OLD.id
) BEGIN SELECT RAISE(ABORT,'qimeng_pending_asset'); END;
CREATE TRIGGER backup_asset_u AFTER UPDATE ON asset_records
 WHEN OLD.data IS NOT NEW.data OR OLD.favorite!=NEW.favorite OR OLD.favorite_updated_at IS NOT NEW.favorite_updated_at
 BEGIN UPDATE backup_business_state SET revision=revision+1; END;
CREATE TRIGGER author_mirror_asset_update AFTER UPDATE ON asset_records
 WHEN OLD.author_id!=NEW.author_id OR json_extract(OLD.data,'$.authorsRevision') IS NOT json_extract(NEW.data,'$.authorsRevision') BEGIN
 UPDATE author_list_mirror SET state='pending',version=version+1 WHERE path!='' AND mode='master';
END;

CREATE VIEW assets AS
 SELECT l.rowid AS rowid,r.id,l.library_id,l.relative_path,l.file_name,v.media_type,r.author_id,r.collection_id,
        v.hash,v.size,r.revision,r.created_at,r.updated_at,r.favorite,l.trashed,r.file_date,r.favorited_at,r.cos_work_id,
        json_patch(r.data,json_object('id',r.id,'libraryId',l.library_id,'relativePath',l.relative_path,'fileName',l.file_name,
         'contentVersion',v.version,'hash',v.hash,'size',v.size,'mediaType',v.media_type,'mimeType',v.mime_type,
         'width',v.width,'height',v.height,'durationMs',v.duration_ms,'probeState',v.probe_state,
         'trashed',json(CASE l.trashed WHEN 1 THEN 'true' ELSE 'false' END),
         'contentMtime',l.content_mtime,'storageLayoutVersion',nullif(l.storage_layout,0),
         'favorite',json(CASE r.favorite WHEN 1 THEN 'true' ELSE 'false' END),'favoriteUpdatedAt',r.favorite_updated_at,'favoritedAt',r.favorited_at,
         'authorId',r.author_id,'authorName',(SELECT name FROM authors WHERE id=r.author_id),
         'authorIds',json((SELECT json_group_array(aa.author_id ORDER BY aa.ordinal,aa.author_id) FROM asset_authors aa WHERE aa.asset_id=r.id)),
         'tags',json((SELECT json_group_array(x.tag ORDER BY x.ordinal,x.tag) FROM asset_tag_names x WHERE x.asset_id=r.id)))) AS data
 FROM asset_locations l JOIN asset_records r ON r.id=l.asset_id
 JOIN asset_content_versions v ON v.asset_id=l.asset_id AND v.version=l.current_version;
