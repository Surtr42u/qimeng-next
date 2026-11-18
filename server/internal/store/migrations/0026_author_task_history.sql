-- A completed task describes its original author even after deletion or merge.
-- Keep its immutable author_id and archive the author separately. New tasks
-- still require a live author; outstanding grants are cancelled on deletion.
ALTER TABLE author_tasks RENAME TO author_tasks_with_live_owner;
CREATE TABLE author_tasks (
 id TEXT PRIMARY KEY,
 author_id TEXT NOT NULL,
 state TEXT NOT NULL,
 expires_at TEXT NOT NULL,
 data TEXT NOT NULL
);
INSERT INTO author_tasks(id,author_id,state,expires_at,data)
 SELECT id,author_id,CASE state WHEN 'revoked' THEN 'cancelled' ELSE state END,expires_at,
 CASE state WHEN 'revoked' THEN json_set(data,'$.state','cancelled') ELSE data END FROM author_tasks_with_live_owner;
DROP TABLE author_tasks_with_live_owner;
CREATE INDEX author_tasks_author ON author_tasks(author_id,state);
CREATE TRIGGER author_task_live_creation BEFORE INSERT ON author_tasks
 WHEN NOT EXISTS(SELECT 1 FROM authors WHERE id=NEW.author_id) BEGIN
 SELECT RAISE(ABORT,'author task requires a live author');
END;
CREATE TRIGGER author_task_close_on_delete AFTER DELETE ON authors BEGIN
 UPDATE author_tasks SET state='cancelled',data=json_set(data,'$.state','cancelled') WHERE author_id=OLD.id AND state='authorized';
END;
