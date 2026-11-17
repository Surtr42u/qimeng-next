CREATE TABLE asset_daily_stats (
 asset_id TEXT NOT NULL, day TEXT NOT NULL,
 views INTEGER NOT NULL DEFAULT 0, plays INTEGER NOT NULL DEFAULT 0,
 seconds INTEGER NOT NULL DEFAULT 0, PRIMARY KEY(asset_id,day)
);
CREATE INDEX daily_stats_day ON asset_daily_stats(day,asset_id);
CREATE TABLE asset_hourly_stats (
 asset_id TEXT NOT NULL,day TEXT NOT NULL,hour INTEGER NOT NULL,
 views INTEGER NOT NULL DEFAULT 0,seconds INTEGER NOT NULL DEFAULT 0,
 PRIMARY KEY(asset_id,day,hour)
);
CREATE TABLE event_sessions (
 asset_id TEXT NOT NULL, session_id TEXT NOT NULL, kind TEXT NOT NULL,
 day TEXT NOT NULL, event_id TEXT NOT NULL,
 PRIMARY KEY(asset_id,session_id,kind,day)
);
CREATE TABLE recommendation_shown (
 day TEXT NOT NULL, asset_id TEXT NOT NULL, count INTEGER NOT NULL,
 PRIMARY KEY(day,asset_id)
);
CREATE TABLE recommendation_snapshots (
 id TEXT PRIMARY KEY, filter TEXT NOT NULL, created_at INTEGER NOT NULL,
 ids TEXT NOT NULL
);
CREATE INDEX recommendation_snapshots_age ON recommendation_snapshots(created_at);
CREATE TRIGGER recommendation_asset_insert AFTER INSERT ON assets BEGIN DELETE FROM recommendation_snapshots; END;
CREATE TRIGGER recommendation_asset_delete AFTER DELETE ON assets BEGIN DELETE FROM recommendation_snapshots; END;
CREATE TRIGGER recommendation_asset_change AFTER UPDATE ON assets WHEN OLD.revision != NEW.revision OR OLD.trashed != NEW.trashed BEGIN DELETE FROM recommendation_snapshots; END;
