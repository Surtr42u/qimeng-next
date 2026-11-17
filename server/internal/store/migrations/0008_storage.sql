UPDATE libraries SET data=json_set(data,'$.storageId','data','$.rootRelativePath','libraries/'||id) WHERE json_type(data,'$.storageId') IS NULL;
CREATE TABLE scans(id TEXT PRIMARY KEY,library_id TEXT NOT NULL REFERENCES libraries(id),state TEXT NOT NULL,data TEXT NOT NULL);
INSERT INTO schema_version VALUES(8);
