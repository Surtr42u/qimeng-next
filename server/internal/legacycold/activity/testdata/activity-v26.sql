-- Activity tables from qimeng-media d0215962, server/migrations (v26).

-- Only declarations used by this fixture are included. All rows are synthetic.

CREATE TABLE libraries (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    root_path   TEXT NOT NULL UNIQUE,
    created_at  TEXT NOT NULL
);

CREATE TABLE assets (
    asset_id    TEXT PRIMARY KEY,
    library_id  TEXT NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
    rel_path    TEXT NOT NULL,
    file_name   TEXT NOT NULL,
    media_type  TEXT NOT NULL CHECK (media_type IN ('image', 'animated_image', 'video')),
    size_bytes  INTEGER NOT NULL,
    mtime       TEXT NOT NULL,
    duration_ms INTEGER,
    width       INTEGER,
    height      INTEGER,
    source      TEXT,
    created_at  TEXT NOT NULL,
    updated_at  TEXT NOT NULL,
    UNIQUE (library_id, rel_path)
);

CREATE TABLE view_events (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    asset_id    TEXT NOT NULL,
    kind        TEXT NOT NULL CHECK (kind IN ('open', 'play', 'dwell')),
    session_id  TEXT NOT NULL,
    started_at  TEXT NOT NULL,
    seconds     INTEGER
);

CREATE TABLE likes (
    asset_id    TEXT NOT NULL REFERENCES assets(asset_id) ON DELETE CASCADE,
    day         TEXT NOT NULL,
    created_at  TEXT NOT NULL,
    PRIMARY KEY (asset_id, day)
);

CREATE TABLE favorites (
    asset_id    TEXT PRIMARY KEY REFERENCES assets(asset_id) ON DELETE CASCADE,
    created_at  TEXT NOT NULL
);

CREATE TABLE timeline_tags (
    id            TEXT PRIMARY KEY,
    asset_id      TEXT NOT NULL REFERENCES assets(asset_id) ON DELETE CASCADE,
    time_millis   INTEGER NOT NULL,
    name          TEXT NOT NULL,
    created_at    TEXT NOT NULL
);

CREATE TABLE daily_shown (
    asset_id  TEXT NOT NULL REFERENCES assets(asset_id) ON DELETE CASCADE,
    day       TEXT NOT NULL,
    count     INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (asset_id, day)
);

CREATE TABLE asset_daily_stats (
    asset_id       TEXT NOT NULL REFERENCES assets(asset_id) ON DELETE CASCADE,
    day            TEXT NOT NULL,
    view_count     INTEGER NOT NULL DEFAULT 0,
    play_count     INTEGER NOT NULL DEFAULT 0,
    browse_seconds INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (asset_id, day)
);
CREATE INDEX idx_asset_daily_stats_day ON asset_daily_stats (day);

ALTER TABLE assets
ADD COLUMN last_position_seconds REAL;
ALTER TABLE assets
ADD COLUMN video_codec TEXT;
ALTER TABLE assets
ADD COLUMN audio_codec TEXT;

ALTER TABLE timeline_tags
ADD COLUMN color TEXT NOT NULL DEFAULT '';

ALTER TABLE view_events ADD COLUMN client_event_id TEXT;
CREATE UNIQUE INDEX idx_view_events_client_event_id ON view_events (client_event_id);

ALTER TABLE view_events ADD COLUMN day TEXT;
CREATE UNIQUE INDEX idx_view_events_session_day
    ON view_events (asset_id, kind, session_id, day)
    WHERE kind IN ('open', 'play');

CREATE TABLE portable_event_identity (
    event_id INTEGER PRIMARY KEY REFERENCES view_events(id),
    stable_id TEXT NOT NULL UNIQUE
);
INSERT INTO portable_event_identity (event_id, stable_id)
SELECT id, 'portable:' || lower(hex(randomblob(16)))
FROM view_events WHERE client_event_id IS NULL;
CREATE TRIGGER portable_event_identity_insert AFTER INSERT ON view_events
WHEN NEW.client_event_id IS NULL
BEGIN
    INSERT INTO portable_event_identity (event_id, stable_id)
    VALUES (NEW.id, 'portable:' || lower(hex(randomblob(16))));
END;
