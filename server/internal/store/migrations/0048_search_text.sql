-- Materialised asset search text.
--
-- The search predicate used to rebuild the whole text (file name, directory,
-- tags, authors and aliases, sources, characters) with correlated subqueries
-- for every candidate row, then run the shared token function once per token.
-- The text is now written once per asset and the predicate is a literal
-- substring probe over that row.
--
-- FTS5 is deliberately not used: the Android driver (mattn/go-sqlite3) only
-- compiles FTS5 with the sqlite_fts5 build tag, and a trigram index cannot
-- answer one or two character CJK tokens or the bracketed page forms without
-- falling back to a scan anyway. This table works with both drivers.
--
-- asset_search_source is the only definition of the text (also used by the
-- backfill). Triggers copy one row after every mutation that can change it.
-- Text is ASCII-folded because the shared token function only folds A-Z;
-- brackets keeps （ [ 【 ） ] 】 normalised for page tokens.
CREATE VIEW asset_search_source AS
WITH raw AS (
 SELECT l.asset_id AS asset_id,
  l.file_name||char(10)||substr(l.relative_path,1,length(l.relative_path)-length(l.file_name))||char(10)||
  COALESCE((SELECT group_concat(tag,char(10)) FROM asset_tag_names WHERE asset_id=l.asset_id),'')||char(10)||
  COALESCE((SELECT group_concat(au.name||' '||COALESCE(json_extract(au.data,'$.aliases'),''),char(10))
   FROM asset_authors aa JOIN authors au ON au.id=aa.author_id WHERE aa.asset_id=l.asset_id),'')||char(10)||
  -- asset_source_memberships cannot be used here: its inferred branch is not
  -- correlated with asset_id by the planner, so every trigger evaluation
  -- would scan all membership rows. The UNION ALL reproduces the same
  -- explicit-plus-inferred set with per-asset indexes.
  COALESCE((SELECT group_concat(s.name,char(10)) FROM (
    SELECT x.source_id AS source_id FROM asset_sources x WHERE x.asset_id=l.asset_id
    UNION ALL
    SELECT c.source_id FROM asset_characters x JOIN characters c ON c.id=x.character_id
     WHERE x.asset_id=l.asset_id
      AND NOT EXISTS(SELECT 1 FROM asset_sources e WHERE e.asset_id=x.asset_id AND e.source_id=c.source_id)
     GROUP BY c.source_id) m JOIN sources s ON s.id=m.source_id),
   (SELECT source_text FROM asset_taxonomy_state t WHERE t.asset_id=l.asset_id),'')||char(10)||
  COALESCE((SELECT group_concat(c.name,char(10))
   FROM asset_characters x JOIN characters c ON c.id=x.character_id WHERE x.asset_id=l.asset_id),
   (SELECT json(coalesce(character_text,'[]')) FROM asset_taxonomy_state t WHERE t.asset_id=l.asset_id),'[]')
  AS raw_text
 FROM asset_locations l
)
-- The asset_records join is the trigger bodies' orphan guard: an
-- asset_locations row without its parent (possible only if foreign keys were
-- bypassed historically) must not abort the backfill or create a row that the
-- assets view can never return.
SELECT asset_id,lower(raw_text) AS text,
 replace(replace(replace(replace(replace(replace(lower(raw_text),'（','('),'[','('),'【','('),'）',')'),']',')'),'】',')') AS brackets
FROM raw
JOIN asset_records r ON r.id=raw.asset_id;

CREATE TABLE asset_search(
 asset_id TEXT PRIMARY KEY REFERENCES asset_records(id) ON DELETE CASCADE,
 text TEXT NOT NULL,
 brackets TEXT NOT NULL
) WITHOUT ROWID;

-- Existing assets. The migration runner executes this file in one
-- transaction, so an interrupted upgrade leaves neither the table nor a
-- partial index behind and the retry starts from the old schema.
INSERT INTO asset_search(asset_id,text,brackets) SELECT asset_id,text,brackets FROM asset_search_source;

-- Every trigger uses the same two steps: drop the stale row, then copy the
-- current view row. The join to asset_records also stops a cascading asset
-- delete from re-inserting a row after the parent is gone.

CREATE TRIGGER asset_search_location_i AFTER INSERT ON asset_locations BEGIN
 DELETE FROM asset_search WHERE asset_id=NEW.asset_id;
 INSERT INTO asset_search(asset_id,text,brackets)
  SELECT s.asset_id,s.text,s.brackets FROM asset_search_source s JOIN asset_records r ON r.id=s.asset_id
  WHERE s.asset_id=NEW.asset_id;
END;
CREATE TRIGGER asset_search_location_u AFTER UPDATE OF relative_path,file_name ON asset_locations
 WHEN OLD.relative_path IS NOT NEW.relative_path OR OLD.file_name IS NOT NEW.file_name BEGIN
 DELETE FROM asset_search WHERE asset_id=NEW.asset_id;
 INSERT INTO asset_search(asset_id,text,brackets)
  SELECT s.asset_id,s.text,s.brackets FROM asset_search_source s JOIN asset_records r ON r.id=s.asset_id
  WHERE s.asset_id=NEW.asset_id;
END;
CREATE TRIGGER asset_search_location_d AFTER DELETE ON asset_locations BEGIN
 DELETE FROM asset_search WHERE asset_id=OLD.asset_id;
END;
CREATE TRIGGER asset_search_record_d AFTER DELETE ON asset_records BEGIN
 DELETE FROM asset_search WHERE asset_id=OLD.id;
END;

CREATE TRIGGER asset_search_author_i AFTER INSERT ON asset_authors BEGIN
 DELETE FROM asset_search WHERE asset_id=NEW.asset_id;
 INSERT INTO asset_search(asset_id,text,brackets)
  SELECT s.asset_id,s.text,s.brackets FROM asset_search_source s JOIN asset_records r ON r.id=s.asset_id
  WHERE s.asset_id=NEW.asset_id;
END;
CREATE TRIGGER asset_search_author_u AFTER UPDATE OF asset_id,author_id ON asset_authors
 WHEN OLD.asset_id IS NOT NEW.asset_id OR OLD.author_id IS NOT NEW.author_id BEGIN
 DELETE FROM asset_search WHERE asset_id IN (OLD.asset_id,NEW.asset_id);
 INSERT INTO asset_search(asset_id,text,brackets)
  SELECT s.asset_id,s.text,s.brackets FROM asset_search_source s JOIN asset_records r ON r.id=s.asset_id
  WHERE s.asset_id IN (OLD.asset_id,NEW.asset_id);
END;
CREATE TRIGGER asset_search_author_d AFTER DELETE ON asset_authors BEGIN
 DELETE FROM asset_search WHERE asset_id=OLD.asset_id;
 INSERT INTO asset_search(asset_id,text,brackets)
  SELECT s.asset_id,s.text,s.brackets FROM asset_search_source s JOIN asset_records r ON r.id=s.asset_id
  WHERE s.asset_id=OLD.asset_id;
END;

CREATE TRIGGER asset_search_tag_i AFTER INSERT ON asset_tags BEGIN
 DELETE FROM asset_search WHERE asset_id=NEW.asset_id;
 INSERT INTO asset_search(asset_id,text,brackets)
  SELECT s.asset_id,s.text,s.brackets FROM asset_search_source s JOIN asset_records r ON r.id=s.asset_id
  WHERE s.asset_id=NEW.asset_id;
END;
CREATE TRIGGER asset_search_tag_u AFTER UPDATE OF asset_id,tag_id ON asset_tags
 WHEN OLD.asset_id IS NOT NEW.asset_id OR OLD.tag_id IS NOT NEW.tag_id BEGIN
 DELETE FROM asset_search WHERE asset_id IN (OLD.asset_id,NEW.asset_id);
 INSERT INTO asset_search(asset_id,text,brackets)
  SELECT s.asset_id,s.text,s.brackets FROM asset_search_source s JOIN asset_records r ON r.id=s.asset_id
  WHERE s.asset_id IN (OLD.asset_id,NEW.asset_id);
END;
CREATE TRIGGER asset_search_tag_d AFTER DELETE ON asset_tags BEGIN
 DELETE FROM asset_search WHERE asset_id=OLD.asset_id;
 INSERT INTO asset_search(asset_id,text,brackets)
  SELECT s.asset_id,s.text,s.brackets FROM asset_search_source s JOIN asset_records r ON r.id=s.asset_id
  WHERE s.asset_id=OLD.asset_id;
END;

CREATE TRIGGER asset_search_asset_source_i AFTER INSERT ON asset_sources BEGIN
 DELETE FROM asset_search WHERE asset_id=NEW.asset_id;
 INSERT INTO asset_search(asset_id,text,brackets)
  SELECT s.asset_id,s.text,s.brackets FROM asset_search_source s JOIN asset_records r ON r.id=s.asset_id
  WHERE s.asset_id=NEW.asset_id;
END;
CREATE TRIGGER asset_search_asset_source_u AFTER UPDATE OF asset_id,source_id ON asset_sources
 WHEN OLD.asset_id IS NOT NEW.asset_id OR OLD.source_id IS NOT NEW.source_id BEGIN
 DELETE FROM asset_search WHERE asset_id IN (OLD.asset_id,NEW.asset_id);
 INSERT INTO asset_search(asset_id,text,brackets)
  SELECT s.asset_id,s.text,s.brackets FROM asset_search_source s JOIN asset_records r ON r.id=s.asset_id
  WHERE s.asset_id IN (OLD.asset_id,NEW.asset_id);
END;
CREATE TRIGGER asset_search_asset_source_d AFTER DELETE ON asset_sources BEGIN
 DELETE FROM asset_search WHERE asset_id=OLD.asset_id;
 INSERT INTO asset_search(asset_id,text,brackets)
  SELECT s.asset_id,s.text,s.brackets FROM asset_search_source s JOIN asset_records r ON r.id=s.asset_id
  WHERE s.asset_id=OLD.asset_id;
END;

CREATE TRIGGER asset_search_asset_character_i AFTER INSERT ON asset_characters BEGIN
 DELETE FROM asset_search WHERE asset_id=NEW.asset_id;
 INSERT INTO asset_search(asset_id,text,brackets)
  SELECT s.asset_id,s.text,s.brackets FROM asset_search_source s JOIN asset_records r ON r.id=s.asset_id
  WHERE s.asset_id=NEW.asset_id;
END;
CREATE TRIGGER asset_search_asset_character_u AFTER UPDATE OF asset_id,character_id ON asset_characters
 WHEN OLD.asset_id IS NOT NEW.asset_id OR OLD.character_id IS NOT NEW.character_id BEGIN
 DELETE FROM asset_search WHERE asset_id IN (OLD.asset_id,NEW.asset_id);
 INSERT INTO asset_search(asset_id,text,brackets)
  SELECT s.asset_id,s.text,s.brackets FROM asset_search_source s JOIN asset_records r ON r.id=s.asset_id
  WHERE s.asset_id IN (OLD.asset_id,NEW.asset_id);
END;
CREATE TRIGGER asset_search_asset_character_d AFTER DELETE ON asset_characters BEGIN
 DELETE FROM asset_search WHERE asset_id=OLD.asset_id;
 INSERT INTO asset_search(asset_id,text,brackets)
  SELECT s.asset_id,s.text,s.brackets FROM asset_search_source s JOIN asset_records r ON r.id=s.asset_id
  WHERE s.asset_id=OLD.asset_id;
END;

CREATE TRIGGER asset_search_taxonomy_i AFTER INSERT ON asset_taxonomy_state BEGIN
 DELETE FROM asset_search WHERE asset_id=NEW.asset_id;
 INSERT INTO asset_search(asset_id,text,brackets)
  SELECT s.asset_id,s.text,s.brackets FROM asset_search_source s JOIN asset_records r ON r.id=s.asset_id
  WHERE s.asset_id=NEW.asset_id;
END;
CREATE TRIGGER asset_search_taxonomy_u AFTER UPDATE OF source_text,character_text ON asset_taxonomy_state
 WHEN OLD.source_text IS NOT NEW.source_text OR OLD.character_text IS NOT NEW.character_text BEGIN
 DELETE FROM asset_search WHERE asset_id=NEW.asset_id;
 INSERT INTO asset_search(asset_id,text,brackets)
  SELECT s.asset_id,s.text,s.brackets FROM asset_search_source s JOIN asset_records r ON r.id=s.asset_id
  WHERE s.asset_id=NEW.asset_id;
END;
CREATE TRIGGER asset_search_taxonomy_d AFTER DELETE ON asset_taxonomy_state BEGIN
 DELETE FROM asset_search WHERE asset_id=OLD.asset_id;
 INSERT INTO asset_search(asset_id,text,brackets)
  SELECT s.asset_id,s.text,s.brackets FROM asset_search_source s JOIN asset_records r ON r.id=s.asset_id
  WHERE s.asset_id=OLD.asset_id;
END;

-- Vocabulary renames update every asset that references the word. Aliases
-- (tag_aliases, collection_aliases) and collection names are not part of the
-- materialised text: the query keeps the live alias channel, so they need no
-- trigger. COS work names, library enable/disable and trash/restore are also
-- live filter or display facts, not searchable text.
CREATE TRIGGER asset_search_author_word_u AFTER UPDATE OF name,data ON authors
 WHEN OLD.name IS NOT NEW.name OR json_extract(OLD.data,'$.aliases') IS NOT json_extract(NEW.data,'$.aliases') BEGIN
 DELETE FROM asset_search WHERE asset_id IN (SELECT asset_id FROM asset_authors WHERE author_id=OLD.id);
 INSERT INTO asset_search(asset_id,text,brackets)
  SELECT s.asset_id,s.text,s.brackets FROM asset_search_source s JOIN asset_records r ON r.id=s.asset_id
  WHERE s.asset_id IN (SELECT asset_id FROM asset_authors WHERE author_id=OLD.id);
END;

CREATE TRIGGER asset_search_tag_word_u AFTER UPDATE OF name,deleted ON tags
 WHEN OLD.name IS NOT NEW.name OR OLD.deleted IS NOT NEW.deleted BEGIN
 DELETE FROM asset_search WHERE asset_id IN (SELECT asset_id FROM asset_tags WHERE tag_id=OLD.id);
 INSERT INTO asset_search(asset_id,text,brackets)
  SELECT s.asset_id,s.text,s.brackets FROM asset_search_source s JOIN asset_records r ON r.id=s.asset_id
  WHERE s.asset_id IN (SELECT asset_id FROM asset_tags WHERE tag_id=OLD.id);
END;

CREATE TRIGGER asset_search_source_word_u AFTER UPDATE OF name ON sources
 WHEN OLD.name IS NOT NEW.name BEGIN
 DELETE FROM asset_search WHERE asset_id IN (
  SELECT x.asset_id FROM asset_sources x WHERE x.source_id=OLD.id
  UNION SELECT x.asset_id FROM asset_characters x JOIN characters c ON c.id=x.character_id WHERE c.source_id=OLD.id);
 INSERT INTO asset_search(asset_id,text,brackets)
  SELECT s.asset_id,s.text,s.brackets FROM asset_search_source s JOIN asset_records r ON r.id=s.asset_id
  WHERE s.asset_id IN (
   SELECT x.asset_id FROM asset_sources x WHERE x.source_id=OLD.id
   UNION SELECT x.asset_id FROM asset_characters x JOIN characters c ON c.id=x.character_id WHERE c.source_id=OLD.id);
END;

CREATE TRIGGER asset_search_character_word_u AFTER UPDATE OF name,source_id ON characters
 WHEN OLD.name IS NOT NEW.name OR OLD.source_id IS NOT NEW.source_id BEGIN
 DELETE FROM asset_search WHERE asset_id IN (SELECT asset_id FROM asset_characters WHERE character_id=OLD.id);
 INSERT INTO asset_search(asset_id,text,brackets)
  SELECT s.asset_id,s.text,s.brackets FROM asset_search_source s JOIN asset_records r ON r.id=s.asset_id
  WHERE s.asset_id IN (SELECT asset_id FROM asset_characters WHERE character_id=OLD.id);
END;
