ALTER TABLE batch_inputs ADD COLUMN rejection_code TEXT NOT NULL DEFAULT ''
 CHECK(rejection_code IN ('','empty_input','unsupported_media')
       AND (rejection_code='' OR (upload_id IS NULL AND cancelled=0)));

-- Only an unreceived, uncancelled selection can acquire a pre-transfer
-- disposition. Keep the original manifest, upload and operation receipts.
WITH supported(extension) AS (
 VALUES ('.jpg'),('.jpeg'),('.png'),('.gif'),('.webp'),('.avif'),
        ('.mp4'),('.m4v'),('.mov'),('.webm'),('.mkv'),('.avi')
), originals AS (
 SELECT i.batch_id,i.input_id,json_extract(i.data,'$.size') AS size,
        CASE WHEN coalesce(json_extract(i.data,'$.nameState'),'known')='known'
             THEN lower(json_extract(i.data,'$.originalFileName')) END AS name
 FROM batch_inputs i JOIN batches b ON b.id=i.batch_id
 WHERE i.upload_id IS NULL AND i.cancelled=0 AND json_extract(b.data,'$.manifestState')='sealed'
 AND json_extract(b.data,'$.state')!='cancelled'
), rejected AS (
 SELECT batch_id,input_id,CASE WHEN size=0 THEN 'empty_input' ELSE 'unsupported_media' END AS code
 FROM originals WHERE size=0 OR (instr(name,'.')>0 AND substr(name,-1)!='.'
 AND NOT EXISTS(SELECT 1 FROM supported WHERE substr(name,-length(extension))=extension))
)
UPDATE batch_inputs AS i SET rejection_code=r.code FROM rejected AS r
WHERE i.batch_id=r.batch_id AND i.input_id=r.input_id;

-- The disposition is derived from the original selection. Prepared plans for
-- already received media still have the same inputs and batch precondition.
UPDATE batches SET data=json_set(data,'$.state','partial')
WHERE EXISTS(SELECT 1 FROM batch_inputs i WHERE i.batch_id=batches.id AND i.rejection_code!='');
UPDATE backup_business_state SET revision=revision+1
WHERE EXISTS(SELECT 1 FROM batch_inputs WHERE rejection_code!='');
