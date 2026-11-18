-- Only trusted enum codes are stored. No request content, paths, credentials,
-- raw stack traces or device identities are accepted as diagnostics.
CREATE TABLE client_diagnostics(seq INTEGER PRIMARY KEY AUTOINCREMENT,id TEXT NOT NULL UNIQUE,data TEXT NOT NULL);
