CREATE TRIGGER asset_tag_requires_active_word BEFORE INSERT ON asset_tags
WHEN NOT EXISTS(SELECT 1 FROM tags WHERE name=NEW.tag AND deleted=0)
BEGIN SELECT RAISE(ABORT,'tag_not_active'); END;
INSERT INTO schema_version VALUES(7);
