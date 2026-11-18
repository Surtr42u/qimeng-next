CREATE TABLE tag_aliases (
 tag_id TEXT NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
 ordinal INTEGER NOT NULL CHECK(ordinal>=0), alias TEXT NOT NULL,
 PRIMARY KEY(tag_id,ordinal)
) WITHOUT ROWID;
CREATE TABLE collection_aliases (
 collection_id TEXT NOT NULL REFERENCES collections(id) ON DELETE CASCADE,
 ordinal INTEGER NOT NULL CHECK(ordinal>=0), alias TEXT NOT NULL,
 PRIMARY KEY(collection_id,ordinal)
) WITHOUT ROWID;

-- Preserve the exact order of each old collection's JSON strings. Missing,
-- null and empty lists have no rows. Invalid shapes fail the migration
-- transaction before their source field can be removed.
INSERT INTO collection_aliases(collection_id,ordinal,alias)
 SELECT c.id,CAST(j.key AS INTEGER),
        CASE WHEN json_type(c.data,'$.aliases')='array' AND j.type='text' THEN j.value END
 FROM collections c LEFT JOIN json_each(c.data,'$.aliases') j ON true
 WHERE coalesce(json_type(c.data,'$.aliases'),'null')!='null'
 AND (json_type(c.data,'$.aliases')!='array' OR j.key IS NOT NULL);

-- Previously a collection's revision only changed when its name changed;
-- existing pending plans fingerprinted that number. Aliases have no bearing
-- on a plan which already identifies its target collection.
ALTER TABLE collections ADD COLUMN naming_revision INTEGER NOT NULL DEFAULT 1 CHECK(naming_revision>=1);
DROP TRIGGER backup_collection_u;
UPDATE collections SET naming_revision=coalesce(json_extract(data,'$.revision'),1),data=json_remove(data,'$.aliases')
 WHERE json_type(data,'$.aliases') IS NOT NULL OR coalesce(json_extract(data,'$.revision'),1)!=1;
CREATE TRIGGER backup_collection_u AFTER UPDATE ON collections WHEN OLD.data IS NOT NEW.data
 BEGIN UPDATE backup_business_state SET revision=revision+1; END;

CREATE INDEX collections_library_name ON collections(library_id,name,id);
CREATE INDEX collections_name ON collections(name,id);
CREATE VIEW tag_details AS
 SELECT t.id,t.name,t.deleted,
 json_object('id',t.id,'name',t.name,'aliases',json((SELECT json_group_array(a.alias ORDER BY a.ordinal)
  FROM tag_aliases a WHERE a.tag_id=t.id)),'revision',t.revision,'createdAt',t.created_at,'updatedAt',t.updated_at) AS data
 FROM tags t;
CREATE VIEW collection_details AS
 SELECT c.id,c.library_id,c.name,c.kind,
 json_set(c.data,'$.aliases',json((SELECT json_group_array(a.alias ORDER BY a.ordinal)
  FROM collection_aliases a WHERE a.collection_id=c.id))) AS data FROM collections c;
