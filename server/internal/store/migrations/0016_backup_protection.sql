CREATE TABLE batch_protections (
 id TEXT PRIMARY KEY,
 batch_id TEXT NOT NULL REFERENCES batches(id),
 state TEXT NOT NULL,
 created_at TEXT NOT NULL,
 data TEXT NOT NULL
);
CREATE INDEX batch_protections_batch ON batch_protections(batch_id,created_at DESC,id DESC);
CREATE TABLE source_cleanup_permits (
 id TEXT PRIMARY KEY,
 protection_id TEXT NOT NULL REFERENCES batch_protections(id),
 batch_id TEXT NOT NULL REFERENCES batches(id),
 input_id TEXT NOT NULL,
 principal_id TEXT NOT NULL,
 state TEXT NOT NULL,
 expires_at TEXT NOT NULL,
 data TEXT NOT NULL
);
CREATE INDEX source_cleanup_inputs ON source_cleanup_permits(batch_id,input_id);
CREATE TABLE source_cleanup_receipts (
 permit_id TEXT PRIMARY KEY REFERENCES source_cleanup_permits(id),
 data TEXT NOT NULL
);
