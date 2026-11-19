"""Build the public, empty Next schema for CI. No operational inputs are accepted."""

import hashlib
import sqlite3
from pathlib import Path


LEDGER_SQL = (
    "CREATE TABLE IF NOT EXISTS schema_migrations("
    "version INTEGER PRIMARY KEY,name TEXT NOT NULL,sha256 TEXT NOT NULL)"
)
LEDGER_QUERY = "SELECT version,name,sha256 FROM schema_migrations ORDER BY version"
OBJECT_QUERY = (
    "SELECT type,name,tbl_name,coalesce(sql,'') "
    "FROM sqlite_schema ORDER BY type,name"
)
MIGRATIONS = Path(__file__).resolve().parent / "reference" / "server" / "internal" / "store" / "migrations"


def digest_rows(rows):
    digest = hashlib.sha256()
    for row in rows:
        for value in row:
            field = str(value).encode("utf-8")
            digest.update(len(field).to_bytes(8, "big"))
            digest.update(field)
    return digest.hexdigest()


def create_native_schema(db):
    files = sorted(MIGRATIONS.glob("*.sql"))
    if not files or files[-1].name != "0045_selected_image_merges.sql":
        raise RuntimeError("pinned migrations missing")
    # Migration 0014 prepares UPDATEs against the empty assets/events tables.
    def unexpected_timestamp(_value):
        raise AssertionError("empty migration fixture unexpectedly contains a timestamp")

    db.create_function("qimeng_time_key", 1, unexpected_timestamp, deterministic=True)
    db.execute("PRAGMA foreign_keys=ON")
    db.execute(LEDGER_SQL)
    for path in files:
        sql = path.read_bytes()
        version = int(path.name.split("_", 1)[0])
        db.executescript(sql.decode("utf-8"))
        db.execute("INSERT INTO schema_migrations VALUES(?,?,?)",
                   (version, path.name, hashlib.sha256(sql).hexdigest()))
    db.commit()


if __name__ == "__main__":
    db = sqlite3.connect(":memory:")
    create_native_schema(db)
    print("Public schema fingerprint", digest_rows(db.execute(OBJECT_QUERY)))
    print("Public ledger fingerprint", digest_rows(db.execute(LEDGER_QUERY)))
