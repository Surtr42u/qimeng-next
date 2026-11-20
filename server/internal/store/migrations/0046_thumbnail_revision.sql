-- A cheap monotonic counter for device prefetch skipping. Only facts that can
-- change the derived thumbnail set or its cache identity advance it; ordinary
-- browsing events, checkpoints and downloads must not. Do not reuse
-- backup_business_state, which also tracks user activity.
CREATE TABLE thumbnail_revision (id INTEGER PRIMARY KEY CHECK(id=1), revision INTEGER NOT NULL CHECK(revision>0));
INSERT INTO thumbnail_revision(id,revision) VALUES(1,1);

-- assets is a read view joining records, locations and content versions. The
-- triggers therefore belong to the real tables, never to that view.
CREATE TRIGGER thumbnail_revision_asset_i AFTER INSERT ON asset_records
 BEGIN UPDATE thumbnail_revision SET revision=revision+1 WHERE id=1; END;
CREATE TRIGGER thumbnail_revision_asset_u AFTER UPDATE OF data ON asset_records
 WHEN OLD.data IS NOT NEW.data
 BEGIN UPDATE thumbnail_revision SET revision=revision+1 WHERE id=1; END;
CREATE TRIGGER thumbnail_revision_asset_d AFTER DELETE ON asset_records
 BEGIN UPDATE thumbnail_revision SET revision=revision+1 WHERE id=1; END;
CREATE TRIGGER thumbnail_revision_content_i AFTER INSERT ON asset_content_versions
 BEGIN UPDATE thumbnail_revision SET revision=revision+1 WHERE id=1; END;
CREATE TRIGGER thumbnail_revision_location_i AFTER INSERT ON asset_locations
 BEGIN UPDATE thumbnail_revision SET revision=revision+1 WHERE id=1; END;
CREATE TRIGGER thumbnail_revision_location_u AFTER UPDATE OF library_id,trashed,current_version ON asset_locations
 WHEN OLD.library_id IS NOT NEW.library_id OR OLD.trashed IS NOT NEW.trashed OR OLD.current_version IS NOT NEW.current_version
 BEGIN UPDATE thumbnail_revision SET revision=revision+1 WHERE id=1; END;
CREATE TRIGGER thumbnail_revision_location_d AFTER DELETE ON asset_locations
 BEGIN UPDATE thumbnail_revision SET revision=revision+1 WHERE id=1; END;
CREATE TRIGGER thumbnail_revision_library_i AFTER INSERT ON libraries
 BEGIN UPDATE thumbnail_revision SET revision=revision+1 WHERE id=1; END;
CREATE TRIGGER thumbnail_revision_library_u AFTER UPDATE OF data ON libraries
 WHEN OLD.data IS NOT NEW.data
 BEGIN UPDATE thumbnail_revision SET revision=revision+1 WHERE id=1; END;
CREATE TRIGGER thumbnail_revision_library_d AFTER DELETE ON libraries
 BEGIN UPDATE thumbnail_revision SET revision=revision+1 WHERE id=1; END;
-- Selected-image representatives own a distinct preview cache key. Membership
-- changes alter which representative preview the gallery resolves.
CREATE TRIGGER thumbnail_revision_merge_member_i AFTER INSERT ON selected_image_merge_members
 BEGIN UPDATE thumbnail_revision SET revision=revision+1 WHERE id=1; END;
CREATE TRIGGER thumbnail_revision_merge_member_d AFTER DELETE ON selected_image_merge_members
 BEGIN UPDATE thumbnail_revision SET revision=revision+1 WHERE id=1; END;
