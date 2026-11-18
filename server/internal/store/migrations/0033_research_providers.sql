ALTER TABLE delegated_tasks ADD COLUMN research_providers TEXT NOT NULL DEFAULT '[]';
ALTER TABLE delegated_tasks ADD COLUMN provider_limit INTEGER NOT NULL DEFAULT 0;
ALTER TABLE delegated_tasks ADD COLUMN provider_calls INTEGER NOT NULL DEFAULT 0;
-- Receipts contain only public, bounded evidence and the input fingerprint.
-- A grant can be revoked, or its runtime task removed during isolated restore,
-- without losing the provenance of an accepted research submission.
CREATE TABLE research_provider_calls(
 id TEXT PRIMARY KEY, task_id TEXT NOT NULL, subject_id TEXT NOT NULL, subject_hash TEXT NOT NULL,
 provider TEXT NOT NULL, request_key TEXT NOT NULL, input_hash TEXT NOT NULL,
 state TEXT NOT NULL, started_at TEXT NOT NULL, data TEXT NOT NULL,
 UNIQUE(task_id,request_key)
);
CREATE INDEX research_provider_rate ON research_provider_calls(provider,started_at);
-- A finished evidence record is durable business history. Polling or replaying
-- the same key must not displace a generation of the owner's backup.
CREATE TRIGGER backup_provider_evidence AFTER UPDATE OF state ON research_provider_calls
WHEN OLD.state='pending' AND NEW.state!='pending'
BEGIN UPDATE backup_business_state SET revision=revision+1; END;
