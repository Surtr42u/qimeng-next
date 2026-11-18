-- Older cancellations stopped only the batch. Keep every transfer record and
-- accepted byte prefix, while releasing abandoned receiving/staged reservations.
UPDATE uploads SET state='cancelled',data=json_set(data,'$.state','cancelled')
WHERE state IN ('receiving','staged')
  AND batch_id IN (SELECT id FROM batches WHERE json_extract(data,'$.state')='cancelled')
  AND NOT EXISTS (SELECT 1 FROM file_intents f
                  WHERE f.state='prepared' AND json_extract(f.data,'$.UploadID')=uploads.id);

UPDATE batch_inputs SET cancelled=1
WHERE cancelled=0
  AND batch_id IN (SELECT id FROM batches WHERE json_extract(data,'$.state')='cancelled')
  AND (upload_id IS NULL OR upload_id IN (SELECT id FROM uploads WHERE state='cancelled'));
