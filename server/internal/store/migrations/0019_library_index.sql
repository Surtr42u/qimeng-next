CREATE INDEX scans_latest_for_library ON scans(library_id,json_extract(data,'$.createdAt') DESC,id DESC);
CREATE INDEX assets_live_directory ON assets(library_id,substr(relative_path,1,max(0,length(relative_path)-length(file_name)-1))) WHERE trashed=0;
-- Cancelled/duplicate upload prefixes have independent backup coverage. Keep
-- only their trusted storage binding once the browsable index is removed.
CREATE TABLE detached_upload_roots (
 library_id TEXT PRIMARY KEY,
 storage_id TEXT NOT NULL,
 relative_path TEXT NOT NULL
);

-- A hash match against today's asset cannot establish which content version an
-- old upload installed. Read committed publication evidence only. A late
-- /complete response can contain a newer asset; its version must also match
-- the original command (new = 1, replace = expected prior version + 1).
WITH receipts AS MATERIALIZED (
 SELECT operation,CAST(response AS TEXT) body FROM idempotency
 WHERE status=200 AND (operation LIKE 'POST /api/v1/uploads/%/complete' OR operation LIKE 'commitPlanItem:%')
 AND json_valid(CAST(response AS TEXT))
), evidence(upload_id,asset_id,hash,size,version) AS (
 SELECT json_extract(data,'$.UploadID'),json_extract(data,'$.After.id'),json_extract(data,'$.After.hash'),
        json_extract(data,'$.After.size'),json_extract(data,'$.After.contentVersion') FROM file_intents
 WHERE kind IN ('ingest','replace') AND state IN ('committed','done')
 UNION ALL
 SELECT json_extract(body,'$.upload.id'),json_extract(body,'$.asset.id'),json_extract(body,'$.asset.hash'),
        json_extract(body,'$.asset.size'),json_extract(body,'$.asset.contentVersion') FROM receipts
 WHERE json_extract(body,'$.upload.state')='ingested'
 AND (operation='POST /api/v1/uploads/'||json_extract(body,'$.upload.id')||'/complete'
   OR operation LIKE 'commitPlanItem:%:'||json_extract(body,'$.upload.id'))
), proven AS (
 SELECT u.id,MIN(e.version) version FROM evidence e JOIN uploads u ON u.id=e.upload_id
 WHERE u.state='ingested' AND e.asset_id=json_extract(u.data,'$.assetId') AND e.hash=json_extract(u.data,'$.hash')
 AND e.size=json_extract(u.data,'$.size') AND typeof(e.version)='integer' AND e.version>0
 AND ((COALESCE(json_extract(u.data,'$.replaceAssetId'),'')='' AND e.version=1)
 OR (json_extract(u.data,'$.replaceAssetId')=e.asset_id AND json_extract(u.data,'$.expectedContentVersion')>0
 AND e.version=json_extract(u.data,'$.expectedContentVersion')+1))
 GROUP BY u.id
)
UPDATE uploads AS u SET data=json_set(u.data,'$.publishedContentVersion',p.version)
 FROM proven p WHERE u.id=p.id;
