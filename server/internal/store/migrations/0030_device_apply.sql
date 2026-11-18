CREATE TABLE device_apply_tasks(
 id TEXT PRIMARY KEY, device_id TEXT NOT NULL, state TEXT NOT NULL,
 created_at TEXT NOT NULL, expires_at TEXT NOT NULL, data TEXT NOT NULL
);
CREATE INDEX device_apply_order ON device_apply_tasks(device_id,created_at DESC,id DESC);
CREATE INDEX device_apply_live ON device_apply_tasks(expires_at,device_id) WHERE state IN ('pending','claimed');
CREATE TABLE device_apply_chunks(
 task_id TEXT NOT NULL REFERENCES device_apply_tasks(id) ON DELETE CASCADE,
 kind TEXT NOT NULL, ordinal INTEGER NOT NULL, payload BLOB NOT NULL,
 PRIMARY KEY(task_id,kind,ordinal)
);
CREATE TABLE device_apply_requests(
 task_id TEXT NOT NULL REFERENCES device_apply_tasks(id) ON DELETE CASCADE,
 principal_id TEXT NOT NULL, operation TEXT NOT NULL, request_key TEXT NOT NULL, input_hash TEXT NOT NULL,
 PRIMARY KEY(task_id,principal_id,operation,request_key)
);
CREATE UNIQUE INDEX current_device_credential ON credentials(json_extract(data,'$.deviceId'))
 WHERE json_extract(data,'$.deviceId') IS NOT NULL;
CREATE TRIGGER backup_device_task_insert AFTER INSERT ON device_apply_tasks BEGIN
 UPDATE backup_business_state SET revision=revision+1; END;
CREATE TRIGGER backup_device_task_update AFTER UPDATE ON device_apply_tasks WHEN OLD.data IS NOT NEW.data BEGIN
 UPDATE backup_business_state SET revision=revision+1; END;
