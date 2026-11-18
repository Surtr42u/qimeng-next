-- Existing tasks retain local previews and their original text-provider grants.
ALTER TABLE delegated_tasks ADD COLUMN allow_image_lookup INTEGER NOT NULL DEFAULT 0 CHECK(allow_image_lookup IN (0,1));
