CREATE TABLE author_profile_blocks (
 author_id TEXT NOT NULL REFERENCES authors(id) ON DELETE CASCADE,
 block TEXT NOT NULL CHECK(block IN ('x','basic','accounts')),
 revision INTEGER NOT NULL CHECK(revision > 0),
 updated_at TEXT NOT NULL,
 data TEXT NOT NULL,
 PRIMARY KEY(author_id,block)
);
CREATE TABLE author_profile_candidates (
 id TEXT PRIMARY KEY,
 author_id TEXT NOT NULL,
 block TEXT NOT NULL,
 state TEXT NOT NULL,
 created_at TEXT NOT NULL,
 archive_id TEXT NOT NULL DEFAULT '',
 data TEXT NOT NULL
);
CREATE INDEX author_profile_candidates_page ON author_profile_candidates(author_id,archive_id,created_at DESC,id DESC);
CREATE TABLE author_profile_avatars (
 id TEXT PRIMARY KEY,
 created_at TEXT NOT NULL,
 image BLOB NOT NULL
);
CREATE TABLE author_profile_archives (
 id TEXT PRIMARY KEY,
 author_id TEXT NOT NULL,
 target_author_id TEXT NOT NULL,
 reason TEXT NOT NULL,
 created_at TEXT NOT NULL,
 data TEXT NOT NULL
);
CREATE INDEX author_profile_archives_source ON author_profile_archives(author_id,created_at DESC,id DESC);
CREATE INDEX author_profile_archives_target ON author_profile_archives(target_author_id,created_at DESC,id DESC);
CREATE TABLE author_profile_archive_avatars (
 archive_id TEXT NOT NULL REFERENCES author_profile_archives(id),
 avatar_id TEXT NOT NULL REFERENCES author_profile_avatars(id),
 PRIMARY KEY(archive_id,avatar_id)
);
INSERT INTO schema_version VALUES(11);
