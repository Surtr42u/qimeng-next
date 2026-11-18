CREATE TABLE local_sync_sources (
 root_id TEXT NOT NULL, path TEXT NOT NULL, stamp TEXT NOT NULL, state TEXT NOT NULL,
 attempts INTEGER NOT NULL, upload_id TEXT NOT NULL DEFAULT '', data TEXT NOT NULL,
 seen_cycle TEXT NOT NULL, completed_at TEXT NOT NULL DEFAULT '',
 PRIMARY KEY(root_id,path)
);
CREATE INDEX local_sync_recent ON local_sync_sources(root_id,state,completed_at DESC,path);
