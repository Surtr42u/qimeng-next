CREATE TABLE ai_delegations (
 id TEXT PRIMARY KEY,
 state TEXT NOT NULL,
 expires_at TEXT NOT NULL,
 data TEXT NOT NULL
);
CREATE TABLE delegated_tasks (
 task_id TEXT PRIMARY KEY,
 delegation_id TEXT NOT NULL REFERENCES ai_delegations(id),
 credential_id TEXT NOT NULL,
 kind TEXT NOT NULL,
 created_day TEXT NOT NULL,
 calls INTEGER NOT NULL DEFAULT 0,
 max_calls INTEGER NOT NULL,
 deadline TEXT NOT NULL
);
CREATE INDEX delegated_tasks_daily ON delegated_tasks(delegation_id,created_day);
