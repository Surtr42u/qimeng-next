-- Earlier development versions accepted an optional replay key for these two
-- one-time grants. Revoke only authorizations whose plaintext was recorded.
CREATE TEMP TABLE exposed_credential_ids (id TEXT PRIMARY KEY);
INSERT OR IGNORE INTO exposed_credential_ids
 SELECT json_extract(CAST(response AS TEXT), '$.id')
 FROM idempotency WHERE operation='POST /api/v1/credentials'
 AND json_type(CAST(response AS TEXT), '$.token')='text';
INSERT OR IGNORE INTO exposed_credential_ids
 SELECT json_extract(CAST(response AS TEXT), '$.credential.id')
 FROM idempotency WHERE operation='POST /api/v1/ai/delegations'
 AND json_type(CAST(response AS TEXT), '$.credential.token')='text';
UPDATE ai_delegations SET state='revoked',data=json_set(data,'$.state','revoked')
 WHERE id IN (SELECT id FROM exposed_credential_ids);
DELETE FROM credentials WHERE id IN (SELECT id FROM exposed_credential_ids)
 OR json_extract(data,'$.parentDelegationId') IN (SELECT id FROM exposed_credential_ids);
DELETE FROM idempotency WHERE operation IN ('POST /api/v1/credentials','POST /api/v1/ai/delegations');
DROP TABLE exposed_credential_ids;
