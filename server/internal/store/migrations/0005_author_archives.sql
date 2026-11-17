CREATE TABLE author_archives(id TEXT PRIMARY KEY,author_id TEXT NOT NULL,reason TEXT NOT NULL,created_at TEXT NOT NULL,data TEXT NOT NULL);
CREATE INDEX author_archives_author ON author_archives(author_id,created_at);
INSERT INTO schema_version VALUES(5);
