-- A legacy aggregate is a real lifetime fact, not a fabricated set of days.
-- Known last-day votes stay in likes; only the unassignable remainder is here.
CREATE TABLE like_baselines (
 asset_id TEXT PRIMARY KEY REFERENCES assets(id) ON DELETE CASCADE,
 source_id TEXT NOT NULL,
 source_key TEXT NOT NULL,
 source_count INTEGER NOT NULL CHECK(source_count > 0),
 unattributed_count INTEGER NOT NULL CHECK(unattributed_count > 0),
 last_day TEXT NOT NULL,
 observed_day TEXT NOT NULL,
 import_id TEXT NOT NULL
);
