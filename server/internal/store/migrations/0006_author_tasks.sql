CREATE TABLE author_tasks (
 id TEXT PRIMARY KEY,
 author_id TEXT NOT NULL REFERENCES authors(id),
 state TEXT NOT NULL,
 expires_at TEXT NOT NULL,
 data TEXT NOT NULL
);
CREATE INDEX author_tasks_author ON author_tasks(author_id,state);
