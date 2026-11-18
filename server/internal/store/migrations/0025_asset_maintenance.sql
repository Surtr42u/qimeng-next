-- Tasks retain their catalog identity and research evidence even after an owner
-- removes an asset. Only a current, separately scoped credential can act.
CREATE TABLE asset_maintenance_tasks(id TEXT PRIMARY KEY,asset_id TEXT NOT NULL,data TEXT NOT NULL);
CREATE INDEX asset_maintenance_target ON asset_maintenance_tasks(asset_id,id);
CREATE TABLE asset_maintenance_evidence(task_id TEXT NOT NULL REFERENCES asset_maintenance_tasks(id),revision INTEGER NOT NULL,data TEXT NOT NULL,PRIMARY KEY(task_id,revision));
CREATE TABLE asset_human_fields(asset_id TEXT NOT NULL REFERENCES assets(id) ON DELETE CASCADE,field TEXT NOT NULL,PRIMARY KEY(asset_id,field));
-- Older rows only recorded a general human metadata edit. Preserve its empty
-- fields conservatively. Future edits record the specific affected field.
INSERT INTO asset_human_fields SELECT id,'authors' FROM assets WHERE json_extract(data,'$.metadataSource')='human';
INSERT INTO asset_human_fields SELECT id,'taxonomy' FROM assets WHERE json_extract(data,'$.metadataSource')='human';
INSERT INTO asset_human_fields SELECT id,'tags' FROM assets WHERE json_extract(data,'$.tagSetSource') IN ('human','migration','import');
INSERT OR IGNORE INTO asset_human_fields SELECT asset_id,'authors' FROM asset_author_overrides WHERE removed=1;
INSERT OR IGNORE INTO asset_human_fields SELECT a.id,'rename' FROM assets a JOIN file_intents f ON a.id=json_extract(f.data,'$.After.id') WHERE f.kind='move' AND f.state IN ('done','committed');
CREATE TRIGGER backup_asset_task_i AFTER INSERT ON asset_maintenance_tasks BEGIN UPDATE backup_business_state SET revision=revision+1; END;
CREATE TRIGGER backup_asset_task_u AFTER UPDATE OF data ON asset_maintenance_tasks WHEN OLD.data!=NEW.data BEGIN UPDATE backup_business_state SET revision=revision+1; END;
