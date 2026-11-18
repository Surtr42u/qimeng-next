-- Keep existing foreign keys and the stable asset identity. Content and
-- location have separate lifetimes; only the read view joins them.
ALTER TABLE assets RENAME TO asset_records;
CREATE TABLE asset_content_versions (
 asset_id TEXT NOT NULL REFERENCES asset_records(id) ON DELETE CASCADE,
 version INTEGER NOT NULL CHECK(version>0),
 hash TEXT CHECK(hash IS NULL OR (length(hash)=64 AND hash NOT GLOB '*[^0-9a-f]*')),
 size INTEGER NOT NULL CHECK(size>=0), media_type TEXT, mime_type TEXT,
 width INTEGER, height INTEGER, duration_ms INTEGER, probe_state TEXT,
 PRIMARY KEY(asset_id,version)
) WITHOUT ROWID;
CREATE TABLE asset_locations (
 asset_id TEXT PRIMARY KEY REFERENCES asset_records(id) ON DELETE CASCADE,
 library_id TEXT NOT NULL REFERENCES libraries(id), relative_path TEXT NOT NULL, file_name TEXT NOT NULL,
 current_version INTEGER NOT NULL, trashed INTEGER NOT NULL CHECK(trashed IN (0,1)),
 storage_layout INTEGER NOT NULL, content_mtime TEXT,
 FOREIGN KEY(asset_id,current_version) REFERENCES asset_content_versions(asset_id,version)
);

INSERT INTO asset_content_versions
 SELECT id,json_extract(data,'$.contentVersion'),nullif(hash,''),size,media_type,json_extract(data,'$.mimeType'),
        json_extract(data,'$.width'),json_extract(data,'$.height'),json_extract(data,'$.durationMs'),json_extract(data,'$.probeState')
 FROM asset_records;
INSERT INTO asset_locations
 SELECT id,library_id,relative_path,file_name,json_extract(data,'$.contentVersion'),trashed,
        coalesce(json_extract(data,'$.storageLayoutVersion'),0),json_extract(data,'$.contentMtime')
 FROM asset_records;

-- A committed file operation retains the inspected before/after facts. A
-- proven upload receipt can survive a restore which discarded runtime intents.
-- It proves bytes and a version, but supplies no measured media parameters.
WITH evidence(record) AS (
 SELECT json_extract(data,'$.Before') FROM file_intents WHERE state IN ('committed','done')
 UNION ALL SELECT json_extract(data,'$.After') FROM file_intents WHERE state IN ('committed','done')
 UNION ALL SELECT json_object('id',json_extract(data,'$.assetId'),'contentVersion',json_extract(data,'$.publishedContentVersion'),
                              'hash',json_extract(data,'$.hash'),'size',json_extract(data,'$.size'))
 FROM uploads WHERE state='ingested' AND json_extract(data,'$.publishedContentVersion')>0
), reported AS (
 SELECT l.asset_id,json_extract(e.record,'$.contentVersion') AS version,json_extract(e.record,'$.hash') AS hash,
        json_extract(e.record,'$.size') AS size,json_extract(e.record,'$.mediaType') AS media_type,
        json_extract(e.record,'$.mimeType') AS mime_type,json_extract(e.record,'$.width') AS width,
        json_extract(e.record,'$.height') AS height,json_extract(e.record,'$.durationMs') AS duration_ms,
        json_extract(e.record,'$.probeState') AS probe_state
 FROM evidence e JOIN asset_locations l ON l.asset_id=json_extract(e.record,'$.id')
 WHERE json_type(e.record,'$.contentVersion')='integer' AND json_extract(e.record,'$.contentVersion') BETWEEN 1 AND l.current_version-1
       AND json_type(e.record,'$.size')='integer' AND json_extract(e.record,'$.size')>=0
       AND json_type(e.record,'$.hash')='text' AND length(json_extract(e.record,'$.hash'))=64
       AND json_extract(e.record,'$.hash') NOT GLOB '*[^0-9a-f]*'
)
INSERT INTO asset_content_versions
 SELECT asset_id,version,min(hash),min(size),
        CASE count(DISTINCT media_type) WHEN 1 THEN min(media_type) END,
        CASE count(DISTINCT mime_type) WHEN 1 THEN min(mime_type) END,
        CASE count(DISTINCT width) WHEN 1 THEN min(width) END,
        CASE count(DISTINCT height) WHEN 1 THEN min(height) END,
        CASE count(DISTINCT duration_ms) WHEN 1 THEN min(duration_ms) END,
        CASE count(DISTINCT probe_state) WHEN 1 THEN min(probe_state) END
 FROM reported GROUP BY asset_id,version HAVING count(DISTINCT hash)=1 AND count(DISTINCT size)=1;

DROP TRIGGER author_mirror_asset_update;
DROP TRIGGER order_asset_u;
DROP TRIGGER pending_asset_update;
DROP TRIGGER backup_asset_u;
DROP INDEX assets_live_path;
DROP INDEX assets_hash;
DROP INDEX assets_live_directory;
DROP INDEX assets_added;
DROP INDEX assets_author;
DROP INDEX assets_collection;
DROP INDEX assets_file_date;
DROP INDEX assets_library_file_date;
DROP INDEX assets_favorite_time;
DROP INDEX assets_cos_work;
ALTER TABLE asset_records DROP COLUMN library_id;
ALTER TABLE asset_records DROP COLUMN relative_path;
ALTER TABLE asset_records DROP COLUMN file_name;
ALTER TABLE asset_records DROP COLUMN media_type;
ALTER TABLE asset_records DROP COLUMN hash;
ALTER TABLE asset_records DROP COLUMN size;
ALTER TABLE asset_records DROP COLUMN trashed;
UPDATE asset_records SET data=json_remove(data,'$.id','$.hash','$.size','$.contentVersion','$.mediaType','$.mimeType',
 '$.width','$.height','$.durationMs','$.probeState','$.libraryId','$.relativePath','$.fileName',
 '$.contentMtime','$.storageLayoutVersion','$.trashed','$.positionMs','$.positionContentVersion');

CREATE UNIQUE INDEX assets_live_path ON asset_locations(library_id,relative_path) WHERE trashed=0;
CREATE INDEX assets_live_directory ON asset_locations(library_id,substr(relative_path,1,max(0,length(relative_path)-length(file_name)-1))) WHERE trashed=0;
CREATE INDEX asset_locations_library ON asset_locations(library_id,trashed,asset_id);
CREATE INDEX asset_versions_hash ON asset_content_versions(hash,size);
CREATE INDEX assets_added ON asset_records(created_at DESC,id DESC);
CREATE INDEX assets_author ON asset_records(author_id);
CREATE INDEX assets_collection ON asset_records(collection_id);
CREATE INDEX assets_file_date ON asset_records(file_date DESC,id DESC);
CREATE INDEX assets_favorite_time ON asset_records(favorite,favorited_at DESC,id DESC);
CREATE INDEX assets_cos_work ON asset_records(cos_work_id,id);

CREATE TRIGGER pending_asset_update BEFORE UPDATE ON asset_records WHEN EXISTS(
 SELECT 1 FROM file_intents WHERE state='prepared' AND json_extract(data,'$.After.id')=OLD.id
) BEGIN SELECT RAISE(ABORT,'qimeng_pending_asset'); END;
CREATE TRIGGER order_asset_u AFTER UPDATE ON asset_records WHEN OLD.revision!=NEW.revision
 BEGIN UPDATE recommendation_epoch SET generation=generation+1; END;
CREATE TRIGGER order_asset_trash AFTER UPDATE OF trashed ON asset_locations WHEN OLD.trashed!=NEW.trashed
 BEGIN UPDATE recommendation_epoch SET generation=generation+1; END;
CREATE TRIGGER author_mirror_asset_update AFTER UPDATE ON asset_records
 WHEN json_extract(OLD.data,'$.authorIds') IS NOT json_extract(NEW.data,'$.authorIds') BEGIN
 UPDATE author_list_mirror SET state='pending',version=version+1 WHERE path!='' AND mode='master';
END;
CREATE TRIGGER author_mirror_location_update AFTER UPDATE ON asset_locations
 WHEN OLD.file_name!=NEW.file_name OR OLD.trashed!=NEW.trashed BEGIN
 UPDATE author_list_mirror SET state='pending',version=version+1 WHERE path!='' AND mode='master';
END;
CREATE TRIGGER backup_asset_u AFTER UPDATE OF data ON asset_records WHEN OLD.data IS NOT NEW.data
 BEGIN UPDATE backup_business_state SET revision=revision+1; END;
CREATE TRIGGER backup_location_u AFTER UPDATE ON asset_locations
 BEGIN UPDATE backup_business_state SET revision=revision+1; END;
CREATE TRIGGER backup_content_i AFTER INSERT ON asset_content_versions
 BEGIN UPDATE backup_business_state SET revision=revision+1; END;
CREATE TRIGGER asset_content_immutable BEFORE UPDATE ON asset_content_versions
 BEGIN SELECT RAISE(ABORT,'asset content version is immutable'); END;
CREATE TRIGGER asset_content_retained BEFORE DELETE ON asset_content_versions
 WHEN EXISTS(SELECT 1 FROM asset_records WHERE id=OLD.asset_id)
 BEGIN SELECT RAISE(ABORT,'asset content version belongs to an existing asset'); END;
CREATE TRIGGER asset_version_step BEFORE UPDATE OF current_version ON asset_locations
 WHEN NEW.current_version!=OLD.current_version AND NEW.current_version!=OLD.current_version+1
 BEGIN SELECT RAISE(ABORT,'asset content version must advance by one'); END;

CREATE VIEW assets AS
 SELECT l.rowid AS rowid,r.id,l.library_id,l.relative_path,l.file_name,v.media_type,r.author_id,r.collection_id,
        v.hash,v.size,r.revision,r.created_at,r.updated_at,r.favorite,r.liked,l.trashed,r.file_date,r.favorited_at,r.cos_work_id,
        json_patch(r.data,json_object('id',r.id,'libraryId',l.library_id,'relativePath',l.relative_path,'fileName',l.file_name,
         'contentVersion',v.version,'hash',v.hash,'size',v.size,'mediaType',v.media_type,'mimeType',v.mime_type,
         'width',v.width,'height',v.height,'durationMs',v.duration_ms,'probeState',v.probe_state,
         'trashed',json(CASE l.trashed WHEN 1 THEN 'true' ELSE 'false' END),
         'contentMtime',l.content_mtime,'storageLayoutVersion',nullif(l.storage_layout,0))) AS data
 FROM asset_locations l JOIN asset_records r ON r.id=l.asset_id
 JOIN asset_content_versions v ON v.asset_id=l.asset_id AND v.version=l.current_version;
