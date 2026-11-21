-- The default browse order is file date descending with NULL last. These are
-- full expression indexes on asset_records; trashed lives on asset_locations
-- and remains an ordinary filter, not a partial-index predicate. A plain
-- (file_date,id) index cannot serve the order because SQLite sorts NULL first
-- in ascending order, and an ORDER BY "file_date IS NULL" prefix forces a
-- temporary B-tree. Folding NULL into a direction-specific sentinel keeps the
-- observable order (NULL last, id as the tie-break) and lets both the first
-- page and keyset continuations scan one index for the ORDER BY. Ascending uses
-- the largest integer so NULL still sorts last.
CREATE INDEX assets_file_date_null_last_desc ON asset_records(COALESCE(file_date,-9223372036854775808) DESC,id DESC);
CREATE INDEX assets_file_date_null_last_asc ON asset_records(COALESCE(file_date,9223372036854775807),id);
