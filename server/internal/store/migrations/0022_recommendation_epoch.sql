-- An order is derived from a catalog generation. High-frequency views, plays,
-- dwell events and impressions accumulate for the next order without replacing
-- the current page while someone is browsing.
DROP TRIGGER recommendation_asset_insert;
DROP TRIGGER recommendation_asset_delete;
DROP TRIGGER recommendation_asset_change;
DELETE FROM recommendation_snapshots;
CREATE TABLE recommendation_epoch(id INTEGER PRIMARY KEY CHECK (id=1), generation INTEGER NOT NULL);
INSERT INTO recommendation_epoch VALUES(1,1);
CREATE TRIGGER order_asset_i AFTER INSERT ON assets BEGIN UPDATE recommendation_epoch SET generation=generation+1; END;
CREATE TRIGGER order_asset_d AFTER DELETE ON assets BEGIN UPDATE recommendation_epoch SET generation=generation+1; END;
CREATE TRIGGER order_asset_u AFTER UPDATE ON assets WHEN OLD.revision!=NEW.revision OR OLD.trashed!=NEW.trashed
 BEGIN UPDATE recommendation_epoch SET generation=generation+1; END;
CREATE TRIGGER order_library_u AFTER UPDATE ON libraries WHEN json_extract(OLD.data,'$.enabled') IS NOT json_extract(NEW.data,'$.enabled')
 BEGIN UPDATE recommendation_epoch SET generation=generation+1; END;
CREATE TRIGGER order_author_kind AFTER UPDATE ON authors WHEN json_extract(OLD.data,'$.kind') IS NOT json_extract(NEW.data,'$.kind')
 BEGIN UPDATE recommendation_epoch SET generation=generation+1; END;
CREATE TRIGGER order_migration AFTER UPDATE OF data ON migration_plans WHEN json_extract(NEW.data,'$.state') IN ('committed','partial') AND OLD.data IS NOT NEW.data
 BEGIN UPDATE recommendation_epoch SET generation=generation+1; END;
