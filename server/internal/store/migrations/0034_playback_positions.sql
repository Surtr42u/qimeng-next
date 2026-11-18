CREATE TABLE asset_playback_positions (
    asset_id TEXT NOT NULL REFERENCES assets(id) ON DELETE CASCADE,
    content_version INTEGER NOT NULL CHECK(content_version > 0),
    position_ms INTEGER NOT NULL CHECK(position_ms >= 0),
    occurred_at TEXT NOT NULL,
    PRIMARY KEY (asset_id, content_version)
) WITHOUT ROWID;

-- Older Next rows recorded the version and offset, but not always a seek time.
-- Backfill a time only from a matching dated event. An unknown time sorts first.
INSERT INTO asset_playback_positions (asset_id, content_version, position_ms, occurred_at)
SELECT a.id, json_extract(a.data,'$.positionContentVersion'), json_extract(a.data,'$.positionMs'),
       coalesce((SELECT max(e.created_at) FROM events e WHERE e.asset_id=a.id
                 AND json_extract(e.data,'$.kind') IN ('play','progress')
                 AND json_extract(e.data,'$.contentVersion')=json_extract(a.data,'$.positionContentVersion')
                 AND json_extract(e.data,'$.positionMs')=json_extract(a.data,'$.positionMs')), '')
FROM assets a WHERE json_extract(a.data,'$.positionContentVersion') > 0
                AND json_extract(a.data,'$.positionMs') >= 0;

UPDATE assets SET data=json_remove(data,'$.positionMs','$.positionContentVersion')
WHERE json_type(data,'$.positionMs') IS NOT NULL OR json_type(data,'$.positionContentVersion') IS NOT NULL;
