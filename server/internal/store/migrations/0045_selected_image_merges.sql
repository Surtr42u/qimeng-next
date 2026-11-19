-- One reviewed group belongs to an exact native archive of a sealed v26 source.
-- Its physical records and versions remain present, including the representative.
CREATE TABLE selected_image_merge_groups (
 representative_id TEXT PRIMARY KEY NOT NULL REFERENCES asset_records(id),
 source_id TEXT NOT NULL, baseline_sha256 TEXT NOT NULL,
 decision TEXT NOT NULL CHECK(json_type(decision)='object'), applied_at TEXT NOT NULL
);
CREATE TABLE selected_image_merge_members (
 physical_asset_id TEXT PRIMARY KEY NOT NULL REFERENCES asset_records(id),
 representative_id TEXT NOT NULL REFERENCES selected_image_merge_groups(representative_id),
 original_asset TEXT NOT NULL CHECK(json_type(original_asset)='object'),
 original_relations TEXT NOT NULL CHECK(json_type(original_relations)='object'),
 UNIQUE(representative_id,physical_asset_id)
);
-- A source alias still resolves through migration_media_aliases to the visible asset.
-- This separate, original binding retains the physical identity for that same key.
CREATE TABLE selected_image_merge_aliases (
 source_id TEXT NOT NULL, source_asset_id TEXT NOT NULL,
 physical_asset_id TEXT NOT NULL REFERENCES selected_image_merge_members(physical_asset_id),
 PRIMARY KEY(source_id,source_asset_id)
) WITHOUT ROWID;
CREATE TABLE selected_image_merge_likes (
 physical_asset_id TEXT NOT NULL REFERENCES selected_image_merge_members(physical_asset_id),
 day TEXT NOT NULL, created_at TEXT NOT NULL, PRIMARY KEY(physical_asset_id,day)
) WITHOUT ROWID;
CREATE TABLE selected_image_merge_sources (
 representative_id TEXT NOT NULL REFERENCES selected_image_merge_groups(representative_id),
 source_key TEXT NOT NULL, url TEXT NOT NULL, occurred_at TEXT, caption TEXT, media_url TEXT,
 PRIMARY KEY(representative_id,source_key)
) WITHOUT ROWID;
-- Sources without an evidenced member association belong only to the group.
CREATE TABLE selected_image_merge_source_members (
 representative_id TEXT NOT NULL, source_key TEXT NOT NULL, physical_asset_id TEXT NOT NULL,
 PRIMARY KEY(representative_id,source_key,physical_asset_id),
 FOREIGN KEY(representative_id,source_key) REFERENCES selected_image_merge_sources(representative_id,source_key),
 FOREIGN KEY(representative_id,physical_asset_id) REFERENCES selected_image_merge_members(representative_id,physical_asset_id)
) WITHOUT ROWID;

-- An archived origin is evidence, independent of subsequent edits to the
-- representative. Each evidence table is append-only.
CREATE TRIGGER selected_merge_group_u BEFORE UPDATE ON selected_image_merge_groups BEGIN SELECT RAISE(ABORT,'immutable selected merge decision'); END;
CREATE TRIGGER selected_merge_group_d BEFORE DELETE ON selected_image_merge_groups BEGIN SELECT RAISE(ABORT,'immutable selected merge decision'); END;
CREATE TRIGGER selected_merge_member_u BEFORE UPDATE ON selected_image_merge_members BEGIN SELECT RAISE(ABORT,'immutable selected merge member'); END;
CREATE TRIGGER selected_merge_member_d BEFORE DELETE ON selected_image_merge_members BEGIN SELECT RAISE(ABORT,'immutable selected merge member'); END;
CREATE TRIGGER selected_merge_alias_u BEFORE UPDATE ON selected_image_merge_aliases BEGIN SELECT RAISE(ABORT,'immutable selected merge alias'); END;
CREATE TRIGGER selected_merge_alias_d BEFORE DELETE ON selected_image_merge_aliases BEGIN SELECT RAISE(ABORT,'immutable selected merge alias'); END;
CREATE TRIGGER selected_merge_like_u BEFORE UPDATE ON selected_image_merge_likes BEGIN SELECT RAISE(ABORT,'immutable selected merge vote'); END;
CREATE TRIGGER selected_merge_like_d BEFORE DELETE ON selected_image_merge_likes BEGIN SELECT RAISE(ABORT,'immutable selected merge vote'); END;
CREATE TRIGGER selected_merge_source_u BEFORE UPDATE ON selected_image_merge_sources BEGIN SELECT RAISE(ABORT,'immutable selected merge source'); END;
CREATE TRIGGER selected_merge_source_d BEFORE DELETE ON selected_image_merge_sources BEGIN SELECT RAISE(ABORT,'immutable selected merge source'); END;
CREATE TRIGGER selected_merge_binding_u BEFORE UPDATE ON selected_image_merge_source_members BEGIN SELECT RAISE(ABORT,'immutable selected merge source binding'); END;
CREATE TRIGGER selected_merge_binding_d BEFORE DELETE ON selected_image_merge_source_members BEGIN SELECT RAISE(ABORT,'immutable selected merge source binding'); END;

-- The projection moves donors into native recoverable storage. From that
-- point ordinary location mutations cannot move or replace their originals.
CREATE TRIGGER selected_merge_donor_location BEFORE UPDATE ON asset_locations
 WHEN OLD.trashed=1 AND EXISTS(SELECT 1 FROM selected_image_merge_members m
 WHERE m.physical_asset_id=OLD.asset_id AND m.physical_asset_id!=m.representative_id)
 AND (NEW.asset_id IS NOT OLD.asset_id OR NEW.library_id IS NOT OLD.library_id OR NEW.trashed IS NOT OLD.trashed
 OR NEW.relative_path IS NOT OLD.relative_path OR NEW.file_name IS NOT OLD.file_name OR NEW.current_version IS NOT OLD.current_version
 OR NEW.storage_layout IS NOT OLD.storage_layout OR NEW.content_mtime IS NOT OLD.content_mtime)
 BEGIN SELECT RAISE(ABORT,'selected merge original is protected'); END;
CREATE TRIGGER selected_merge_donor_location_d BEFORE DELETE ON asset_locations
 WHEN EXISTS(SELECT 1 FROM selected_image_merge_members m
 WHERE m.physical_asset_id=OLD.asset_id AND m.physical_asset_id!=m.representative_id)
 BEGIN SELECT RAISE(ABORT,'selected merge original is protected'); END;
