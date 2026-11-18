-- Poll only sealed intelligent intakes in the permitted libraries. The view
-- derives readiness from the same manifest and verified uploads used by intake.
CREATE INDEX batches_intelligent_intake ON batches(json_extract(data,'$.libraryId'),created_at,id)
 WHERE json_extract(data,'$.mode')='intelligent' AND json_extract(data,'$.manifestState')='sealed'
 AND json_extract(data,'$.state') IN ('receiving','waiting_executor','ready','partial');
CREATE INDEX prepared_intake_upload ON file_intents(json_extract(data,'$.UploadID')) WHERE kind='ingest' AND state='prepared';
CREATE INDEX credential_intake_scope ON credentials(json_extract(data,'$.batchId')) WHERE json_extract(data,'$.batchId') IS NOT NULL;

CREATE VIEW ready_intake_batches AS
 SELECT b.id AS batch_id,b.created_at,json_extract(b.data,'$.libraryId') AS library_id,
        json_extract(b.data,'$.manifestFingerprint') AS manifest_fingerprint,
        json_extract(b.data,'$.maxResearchCalls') AS max_research_calls
 FROM batches b JOIN libraries l ON l.id=json_extract(b.data,'$.libraryId')
 WHERE json_extract(b.data,'$.mode')='intelligent' AND json_extract(b.data,'$.manifestState')='sealed'
 AND json_extract(b.data,'$.state') IN ('receiving','waiting_executor','ready','partial') AND json_extract(l.data,'$.enabled')=1
 AND length(json_extract(b.data,'$.manifestFingerprint'))=64
 AND json_extract(b.data,'$.expectedInputs') BETWEEN 1 AND 100
 AND NOT EXISTS (SELECT 1 FROM delegated_tasks t WHERE t.task_id=b.id)
 AND NOT EXISTS (SELECT 1 FROM credentials c WHERE json_extract(c.data,'$.batchId')=b.id AND julianday(c.expires_at)>julianday('now')
                 AND EXISTS (SELECT 1 FROM json_each(c.data,'$.scopes') WHERE value IN ('ingest','organize','research')))
 AND (SELECT count(*) FROM batch_inputs i WHERE i.batch_id=b.id)=json_extract(b.data,'$.expectedInputs')
 AND NOT EXISTS (
   SELECT 1 FROM batch_inputs i LEFT JOIN uploads u ON u.id=i.upload_id AND u.batch_id=i.batch_id
   WHERE i.batch_id=b.id AND i.cancelled=0 AND i.rejection_code=''
   AND (u.id IS NULL OR u.state NOT IN ('staged','duplicate','ingested','cancelled','failed')
     OR (u.state='staged' AND (coalesce(length(json_extract(u.data,'$.hash')),0)!=64
       OR EXISTS (SELECT 1 FROM file_intents f WHERE f.kind='ingest' AND f.state='prepared' AND json_extract(f.data,'$.UploadID')=u.id)))))
 AND EXISTS (
   SELECT 1 FROM batch_inputs i JOIN uploads u ON u.id=i.upload_id AND u.batch_id=i.batch_id
   WHERE i.batch_id=b.id AND i.cancelled=0 AND i.rejection_code='' AND u.state='staged'
   AND coalesce(json_extract(u.data,'$.errorCode'),'')='' AND coalesce(json_extract(u.data,'$.replaceAssetId'),'')=''
   AND NOT EXISTS (SELECT 1 FROM research r WHERE r.upload_id=u.id AND json_extract(r.data,'$.outcome')='conflict'));
