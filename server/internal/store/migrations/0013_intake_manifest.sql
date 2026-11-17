CREATE TABLE batch_inputs(
 batch_id TEXT NOT NULL REFERENCES batches(id),
 input_id TEXT NOT NULL,
 ordinal INTEGER NOT NULL,
 cancelled INTEGER NOT NULL DEFAULT 0 CHECK(cancelled IN (0,1)),
 upload_id TEXT UNIQUE REFERENCES uploads(id) DEFERRABLE INITIALLY DEFERRED,
 data TEXT NOT NULL,
 PRIMARY KEY(batch_id,input_id),
 UNIQUE(batch_id,ordinal)
);
-- Past uploads cannot prove the original intended selection. Retain their
-- records, but never manufacture a complete manifest from what happened to arrive.
UPDATE batches SET data=json_set(data,
 '$.manifestRevision',1,'$.manifestState','legacy_unverified',
 '$.manifestFingerprint','','$.expectedInputs',0,
 '$.state',CASE WHEN json_extract(data,'$.state')='completed' THEN 'partial' ELSE json_extract(data,'$.state') END);
