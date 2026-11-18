CREATE TABLE author_lists (
 id TEXT PRIMARY KEY,
 name TEXT NOT NULL UNIQUE,
 position INTEGER NOT NULL UNIQUE,
 revision INTEGER NOT NULL,
 data TEXT NOT NULL
);
CREATE TABLE author_list_members (
 list_id TEXT NOT NULL REFERENCES author_lists(id) ON DELETE CASCADE,
 ordinal INTEGER NOT NULL,
 author_id TEXT NOT NULL REFERENCES authors(id) ON DELETE CASCADE,
 PRIMARY KEY(list_id,ordinal)
);
CREATE INDEX author_list_members_author ON author_list_members(author_id,list_id);
CREATE TABLE author_list_rules (
 list_id TEXT NOT NULL REFERENCES author_lists(id) ON DELETE CASCADE,
 author_id TEXT NOT NULL REFERENCES authors(id) ON DELETE CASCADE,
 work TEXT NOT NULL,
 ordinal INTEGER NOT NULL,
 match_key TEXT NOT NULL,
 extension TEXT NOT NULL,
 pages INTEGER NOT NULL,
 PRIMARY KEY(list_id,author_id,work)
);
CREATE INDEX author_list_rules_lookup ON author_list_rules(match_key,extension,pages);
CREATE TABLE author_list_matches (
 list_id TEXT NOT NULL REFERENCES author_lists(id) ON DELETE CASCADE,
 asset_id TEXT NOT NULL REFERENCES assets(id) ON DELETE CASCADE,
 author_id TEXT NOT NULL REFERENCES authors(id) ON DELETE CASCADE,
 work TEXT NOT NULL,
 PRIMARY KEY(list_id,asset_id,author_id,work),
 FOREIGN KEY(list_id,author_id,work) REFERENCES author_list_rules(list_id,author_id,work) ON DELETE CASCADE
);
CREATE INDEX author_list_matches_asset ON author_list_matches(asset_id,author_id);
-- Explicit relation decisions survive automatic rematching. A positive decision
-- is projected into its owning list; a removed relation cannot be resurrected by
-- an unchanged or late text rule in another list.
CREATE TABLE asset_author_overrides (
 asset_id TEXT NOT NULL REFERENCES assets(id) ON DELETE CASCADE,
 author_id TEXT NOT NULL REFERENCES authors(id) ON DELETE CASCADE,
 removed INTEGER NOT NULL CHECK(removed IN (0,1)),
 list_id TEXT NOT NULL DEFAULT '',
 PRIMARY KEY(asset_id,author_id)
);
INSERT INTO asset_author_overrides(asset_id,author_id,removed)
 SELECT x.asset_id,x.author_id,0 FROM asset_authors x JOIN authors au ON au.id=x.author_id
 WHERE x.author_id!='unknown' AND json_extract(au.data,'$.kind')='regular';
CREATE TABLE author_source_overrides (
 author_id TEXT PRIMARY KEY REFERENCES authors(id) ON DELETE CASCADE
);
INSERT INTO author_source_overrides(author_id)
 SELECT id FROM authors WHERE id!='unknown' AND json_array_length(json_extract(data,'$.acquisitionSources'))>0;
CREATE TABLE author_list_tombstones(name TEXT PRIMARY KEY,revision INTEGER NOT NULL);
CREATE TABLE author_lookup_keys (
 identity_key TEXT NOT NULL,
 author_id TEXT NOT NULL REFERENCES authors(id) ON DELETE CASCADE,
 PRIMARY KEY(identity_key,author_id)
);
CREATE TABLE author_list_mirror (
 id INTEGER PRIMARY KEY CHECK(id=1), revision INTEGER NOT NULL, path TEXT NOT NULL,
 mode TEXT NOT NULL, list_id TEXT NOT NULL, version INTEGER NOT NULL,
 state TEXT NOT NULL, content_hash TEXT NOT NULL DEFAULT '', error_code TEXT NOT NULL DEFAULT '', published_at TEXT
);
-- Bookkeeping participates in the business transaction. Filesystem publication
-- happens later; an interrupted or coalesced notification cannot lose a change.
CREATE TRIGGER author_mirror_list_insert AFTER INSERT ON author_lists BEGIN
 UPDATE author_list_mirror SET state='pending',version=version+1 WHERE path!='';
END;
CREATE TRIGGER author_mirror_list_update AFTER UPDATE ON author_lists BEGIN
 UPDATE author_list_mirror SET state='pending',version=version+1 WHERE path!='';
END;
CREATE TRIGGER author_mirror_list_delete AFTER DELETE ON author_lists BEGIN
 UPDATE author_list_mirror SET state='pending',version=version+1 WHERE path!='';
END;
CREATE TRIGGER author_mirror_author_insert AFTER INSERT ON authors BEGIN
 UPDATE author_list_mirror SET state='pending',version=version+1 WHERE path!='' AND mode='master';
END;
CREATE TRIGGER author_mirror_author_update AFTER UPDATE ON authors WHEN OLD.name!=NEW.name OR json_extract(OLD.data,'$.acquisitionSources') IS NOT json_extract(NEW.data,'$.acquisitionSources') BEGIN
 UPDATE author_list_mirror SET state='pending',version=version+1 WHERE path!='';
END;
CREATE TRIGGER author_mirror_author_delete AFTER DELETE ON authors BEGIN
 UPDATE author_list_mirror SET state='pending',version=version+1 WHERE path!='' AND mode='master';
END;
CREATE TRIGGER author_mirror_profile_insert AFTER INSERT ON author_profile_blocks BEGIN
 UPDATE author_list_mirror SET state='pending',version=version+1 WHERE path!='';
END;
CREATE TRIGGER author_mirror_profile_update AFTER UPDATE ON author_profile_blocks BEGIN
 UPDATE author_list_mirror SET state='pending',version=version+1 WHERE path!='';
END;
CREATE TRIGGER author_mirror_profile_delete AFTER DELETE ON author_profile_blocks BEGIN
 UPDATE author_list_mirror SET state='pending',version=version+1 WHERE path!='';
END;
CREATE TRIGGER author_mirror_asset_update AFTER UPDATE ON assets WHEN OLD.file_name!=NEW.file_name OR OLD.trashed!=NEW.trashed OR json_extract(OLD.data,'$.authorIds') IS NOT json_extract(NEW.data,'$.authorIds') BEGIN
 UPDATE author_list_mirror SET state='pending',version=version+1 WHERE path!='' AND mode='master';
END;
CREATE TRIGGER author_mirror_asset_delete AFTER DELETE ON assets BEGIN
 UPDATE author_list_mirror SET state='pending',version=version+1 WHERE path!='' AND mode='master';
END;
