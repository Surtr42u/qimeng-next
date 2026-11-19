INSERT INTO libraries VALUES('fixture-library','Synthetic','synthetic-library','2025-01-01T00:00:00.000Z');
INSERT INTO assets(asset_id,library_id,rel_path,file_name,media_type,size_bytes,mtime,created_at,updated_at,last_position_seconds)
VALUES
 ('clip-a','fixture-library','one/scene.mp4','scene.mp4','video',10,'2025-01-01T00:00:00.000Z','2025-01-01T00:00:00.000Z','2025-04-01T00:00:00.000Z',12.3456789),
 ('clip-b','fixture-library','two/scene.mp4','scene.mp4','video',20,'2025-01-01T00:00:00.000Z','2025-01-01T00:00:00.000Z','2025-04-01T00:00:00.000Z',0),
 ('still','fixture-library','still.png','still.png','image',30,'2025-01-01T00:00:00.000Z','2025-01-01T00:00:00.000Z','2025-04-01T00:00:00.000Z',NULL);

INSERT INTO view_events(id,asset_id,kind,session_id,started_at,seconds,client_event_id,day) VALUES
 (1, 'clip-a','open','same','2025-03-09T04:59:59.500Z',NULL,'client-original','2025-03-08'),
 (2, 'clip-a','open','same','2025-03-09T05:00:00.000Z',NULL,'legacy:transferred','2025-03-09'),
 (3, 'clip-a','open','same','2025-03-09T05:00:00.000Z',NULL,NULL,NULL),
 (4, 'clip-a','open','same','2025-03-09T05:00:00.000Z',NULL,'second-historical-open',NULL),
 (5, 'clip-a','play','same','2025-03-09T07:00:00.000Z',NULL,'client-play-original','2025-03-09'),
 (6, 'clip-a','dwell','same','2025-03-09T07:00:00.000Z',17,'client-dwell-original',NULL),
 (7, 'clip-b','open','same','2025-03-09T05:00:00.000Z',NULL,'other-original','2025-03-09'),
 (8, 'deleted-original','open','same','2025-03-09T05:00:00.000Z',NULL,'deleted-original-event',NULL),
 (9, 'clip-a','dwell','same','2025-03-09T07:05:00.000Z',0,'client-zero-dwell',NULL),
 (10,'clip-a','dwell','same','2025-03-09T07:08:00.000Z',NULL,'client-null-dwell',NULL),
 (11,'clip-a','dwell','same','2025-03-09T07:00:00.000Z',4,NULL,NULL),
 (12,'clip-a','dwell','same','2025-03-09T07:00:00.000Z',4,NULL,NULL),
 (13,'clip-a','dwell','same','2025-03-09T07:00:00.000Z',2,'foreign-business-day','2025-03-10');
UPDATE portable_event_identity SET stable_id='portable:1234567890abcdef1234567890abcdef' WHERE event_id=3;
-- v26 normally supplies these side rows. Exercise a sealed source whose
-- historical side entries have been lost, without changing the event itself.
DELETE FROM portable_event_identity WHERE event_id IN (11,12);

INSERT INTO likes VALUES
 ('clip-a','2025-03-08','2025-03-09T04:55:11.456Z'),
 ('clip-a','2025-03-09','2025-03-09T05:01:22.789Z');
INSERT INTO favorites VALUES('clip-a','2025-03-09T05:00:00.123Z');
INSERT INTO timeline_tags(id,asset_id,time_millis,name,created_at,color) VALUES
 ('mark-first','clip-a',1500,'chapter','2025-02-02T01:01:01.111Z','#bBcCdD'),
 ('mark-second','clip-a',1500,'chapter','2025-02-02T01:01:01.222Z','#bBcCdD'),
 ('mark-other','clip-b',1500,'chapter','2025-02-02T01:01:01.111Z','');
INSERT INTO daily_shown VALUES('clip-a','2025-03-09',5);
-- An out-of-date materialization must survive as evidence without adding
-- 900 views, 700 plays or 600 seconds to the authoritative event stream.
INSERT INTO asset_daily_stats VALUES('clip-a','2025-03-09',900,700,600);
