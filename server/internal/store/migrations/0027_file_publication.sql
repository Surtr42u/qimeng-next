CREATE INDEX pending_file_assets ON file_intents(json_extract(data,'$.After.id')) WHERE state IN ('prepared','committed');
-- A prepared file publication owns the prior business snapshot. The publishing
-- transaction first claims its own row as 'committing' and ends in 'committed'.
-- A rollback also rolls back that claim; 'committing' is never durable.
CREATE TRIGGER pending_asset_insert BEFORE INSERT ON assets WHEN EXISTS(
 SELECT 1 FROM file_intents WHERE state='prepared' AND json_extract(data,'$.After.id')=NEW.id
) BEGIN SELECT RAISE(ABORT,'qimeng_pending_asset'); END;
CREATE TRIGGER pending_asset_update BEFORE UPDATE ON assets WHEN EXISTS(
 SELECT 1 FROM file_intents WHERE state='prepared' AND json_extract(data,'$.After.id')=OLD.id
) BEGIN SELECT RAISE(ABORT,'qimeng_pending_asset'); END;
CREATE TRIGGER pending_asset_delete BEFORE DELETE ON assets WHEN EXISTS(
 SELECT 1 FROM file_intents WHERE state='prepared' AND json_extract(data,'$.After.id')=OLD.id
) BEGIN SELECT RAISE(ABORT,'qimeng_pending_asset'); END;
CREATE TRIGGER pending_asset_author_delete BEFORE DELETE ON authors WHEN EXISTS(
 SELECT 1 FROM file_intents f JOIN json_each(f.data,'$.After.authorIds') proposed ON proposed.value=OLD.id WHERE f.state='prepared'
) BEGIN SELECT RAISE(ABORT,'qimeng_pending_asset'); END;

-- Plan result provenance belongs to the business snapshot even though runtime
-- write tokens and filesystem intents are discarded on isolated restoration.
CREATE TABLE published_plan_items(
 upload_id TEXT PRIMARY KEY REFERENCES uploads(id),
 plan_id TEXT NOT NULL REFERENCES plans(id),
 asset_id TEXT NOT NULL, content_version INTEGER NOT NULL, authors_revision INTEGER NOT NULL,
 authors TEXT NOT NULL
);
INSERT INTO published_plan_items(upload_id,plan_id,asset_id,content_version,authors_revision,authors)
 SELECT u.id,p.id,json_extract(f.data,'$.After.id'),json_extract(f.data,'$.After.contentVersion'),
 COALESCE(json_extract(f.data,'$.After.authorsRevision'),1),json_extract(f.data,'$.After.authorIds')
 FROM file_intents f JOIN uploads u ON u.id=json_extract(f.data,'$.UploadID')
 JOIN plans p ON p.batch_id=u.batch_id AND json_extract(f.data,'$.Operation')='commitPlanItem:'||p.id||':'||u.id
 WHERE f.state IN ('committed','done') AND u.state='ingested' AND json_type(f.data,'$.After.authorIds')='array';
