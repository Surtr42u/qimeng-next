"""Cloud-only regression on independent, entirely synthetic SQLite files."""

import hashlib
import json
import os
import shutil
import sqlite3
import subprocess
import sys
import tempfile
import unittest
from argparse import Namespace
from contextlib import closing
from pathlib import Path
from unittest.mock import patch

import apply_exposures as op
from schema_fixture import create_native_schema


HERE = Path(__file__).resolve().parent
IDS = ("fixture-rep", "fixture-donor", "fixture-third", "fixture-solo", "fixture-fresh")
REP, DONOR, THIRD, SOLO, FRESH = IDS
TRASH = "fixture-archived-trash"
D1, D2, D3 = "2024-01-01", "2024-01-02", "2024-01-03"
OLD = ((REP, D1, 4), (DONOR, D1, 6), (SOLO, D1, 2))
NEW = ((DONOR, D2, 3), (FRESH, D3, 0), (SOLO, D2, 1))


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def edit(path, statement, args=()):
    with closing(sqlite3.connect(path)) as db:
        db.execute(statement, args)
        db.commit()


def read(path, query, args=()):
    with closing(sqlite3.connect(path.as_uri() + "?mode=ro&immutable=1", uri=True)) as db:
        return db.execute(query, args).fetchall()


def logical_state(path):
    tables = read(path, "SELECT name FROM sqlite_schema WHERE type='table' ORDER BY name")
    return {name: sorted(read(path, 'SELECT * FROM "' + name.replace('"', '""') + '"'), key=repr)
            for (name,) in tables}


def seed_v26(path):
    migrations = sorted((HERE / "legacy_reference" / "server" / "migrations").glob("*.up.sql"))
    if [int(p.name.split("_", 1)[0]) for p in migrations] != list(range(1, 27)):
        raise AssertionError("expected all public v26 migrations")
    with closing(sqlite3.connect(path)) as db:
        db.execute("PRAGMA journal_mode=WAL")
        db.execute("PRAGMA foreign_keys=ON")
        for migration in migrations:
            db.executescript(migration.read_text("utf-8"))
        db.execute("CREATE TABLE schema_migrations(version INTEGER NOT NULL PRIMARY KEY,dirty BOOLEAN NOT NULL)")
        db.execute("INSERT INTO schema_migrations VALUES(26,0)")
        db.execute("INSERT INTO libraries(id,name,root_path,created_at) VALUES('fixture-lib','fixture','mock-root',?)", (D1,))
        for asset in IDS:
            file = asset + ".png"
            db.execute("""INSERT INTO assets(asset_id,library_id,rel_path,file_name,media_type,size_bytes,
                mtime,created_at,updated_at) VALUES(?,'fixture-lib',?,?,'image',1,?,?,?)""",
                       (asset, file, file, D1, D1, D1))
        db.executemany("INSERT INTO daily_shown VALUES(?,?,?)", OLD)
        db.commit()


def seed_native(path, source):
    with closing(sqlite3.connect(path)) as db:
        db.execute("PRAGMA journal_mode=WAL")
        create_native_schema(db)
        db.execute("INSERT INTO libraries(id,name,kind,data) VALUES('fixture-lib','fixture','image','{}')")
        db.execute("INSERT INTO authors(id,name,data) VALUES('fixture-author','fixture','{}')")
        for asset in (*IDS, TRASH):
            visible = REP if asset in (DONOR, THIRD) else asset
            db.execute("""INSERT INTO asset_records(id,author_id,revision,created_at,updated_at,data)
                VALUES(?,'fixture-author',1,?,?,'{}')""", (asset, D1, D1))
            db.execute("INSERT INTO asset_content_versions(asset_id,version,size,media_type) VALUES(?,1,1,'image')", (asset,))
            db.execute("""INSERT INTO asset_locations(asset_id,library_id,relative_path,file_name,current_version,trashed,storage_layout)
                VALUES(?,'fixture-lib',?,?,1,?,2)""", (asset, asset + ".png", asset + ".png", int(asset in (DONOR, THIRD, TRASH))))
            db.execute("INSERT INTO migration_media_aliases VALUES(?,?,?)", (source, asset, visible))
            kind = "cold26/trash" if asset == TRASH else "cold26/asset"
            db.execute("INSERT INTO migration_relation_origins VALUES(?,?,?,'{}')", (source, kind, asset))
        db.execute("""INSERT INTO migration_relation_origins VALUES(?,'cold26/activity','v1',
            json_object('source_time_zone','UTC'))""", (source,))
        db.execute("INSERT INTO selected_image_merge_groups VALUES(?,?,?,'{}',?)", (REP, source, "a" * 64, D1))
        for member in (REP, DONOR, THIRD):
            db.execute("INSERT INTO selected_image_merge_members VALUES(?,?,'{}','{}')", (member, REP))
            db.execute("INSERT INTO selected_image_merge_aliases VALUES(?,?,?)", (source, member, member))
        for asset, day, count in OLD:
            db.execute("""INSERT INTO migration_relation_origins VALUES(?1,'cold26/daily_shown',json_array(?2,?3),
                json_object('asset_id',?2,'day',?3,'count',?4))""", (source, asset, day, count))
        db.executemany("INSERT INTO recommendation_shown VALUES(?,?,?)", ((D1, REP, 10), (D1, SOLO, 2)))
        db.commit()


class PrivateCopyTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        temp = tempfile.TemporaryDirectory()
        cls.addClassCleanup(temp.cleanup)
        cls.seed = Path(temp.name)
        base, latest, native = (cls.seed / n for n in ("base.sqlite", "latest.sqlite", "native.sqlite"))
        seed_v26(base)
        shutil.copyfile(base, latest)
        with closing(sqlite3.connect(latest)) as db:
            db.executemany("INSERT INTO daily_shown VALUES(?,?,?)", NEW)
            db.commit()
        seed_native(native, op.SOURCE_PREFIX + digest(base))

    def setUp(self):
        temp = tempfile.TemporaryDirectory(prefix="fixture-exposure-")
        self.addCleanup(temp.cleanup)
        self.root = Path(temp.name)
        self.base, self.latest, self.native = [self.root / n for n in ("base.sqlite", "latest.sqlite", "native.sqlite")]
        for path in (self.base, self.latest, self.native):
            shutil.copyfile(self.seed / path.name, path)
        self.flags = Namespace(baseline_v26=str(self.base), new_v26=str(self.latest), native_copy=str(self.native),
                               baseline_sha256=digest(self.base), new_sha256=digest(self.latest),
                               native_baseline_sha256=digest(self.native))

    def cli(self, flags=None):
        args = []
        for key, value in vars(flags or self.flags).items():
            args.extend(("--" + key.replace("_", "-"), value))
        return subprocess.run([sys.executable, "-B", str(HERE / "apply_exposures.py"), *args],
                              capture_output=True, text=True, timeout=45)

    def refused(self, code):
        before = digest(self.native)
        proc = self.cli()
        self.assertEqual(proc.returncode, 1)
        self.assertEqual(proc.stdout, "")
        self.assertEqual(json.loads(proc.stderr), {"status": "refused", "reason": code})
        self.assertEqual(digest(self.native), before)

    def test_wal_apply_exact_changes_and_byte_identical_replay(self):
        initial = logical_state(self.native)
        proc = self.cli()
        self.assertEqual((proc.returncode, proc.stderr), (0, ""))
        self.assertEqual(json.loads(proc.stdout), {"status": "applied"})
        actual = logical_state(self.native)
        self.assertEqual(set(actual), set(initial))
        for name, rows in initial.items():
            if name not in ("recommendation_shown", "migration_relation_origins"):
                self.assertEqual(actual[name], rows, name)
        self.assertEqual(set(actual["recommendation_shown"]), {
            (D1, REP, 10), (D1, SOLO, 2), (D2, REP, 3), (D2, SOLO, 1), (D3, FRESH, 0)})
        remaining = set(actual["migration_relation_origins"])
        for row in initial["migration_relation_origins"]:
            remaining.remove(row)
        source = op.SOURCE_PREFIX + self.flags.new_sha256
        for asset, day, count in NEW:
            identity = json.dumps([asset, day], separators=(",", ":"))
            original = json.dumps({"asset_id": asset, "day": day, "count": count}, separators=(",", ":"))
            remaining.remove((source, op.EXPOSURE_KIND, identity, original))
        self.assertEqual(len(remaining), 1)
        owner, kind, identity, raw = remaining.pop()
        self.assertEqual((owner, kind), (source, op.RECEIPT_KIND))
        self.assertEqual(identity, hashlib.sha256((kind + "\n" + raw).encode("ascii")).hexdigest())
        self.assertEqual(json.loads(raw), {"baselineSHA256": self.flags.baseline_sha256, "newSHA256": self.flags.new_sha256,
                                         "nativeBaselineSHA256": self.flags.native_baseline_sha256, "addedKeys": 3, "addedDisplays": 4})
        applied = digest(self.native)
        again = self.cli()
        self.assertEqual((again.returncode, again.stderr, json.loads(again.stdout)), (0, "", {"status": "replayed"}))
        self.assertEqual(digest(self.native), applied)
        self.assertEqual((digest(self.base), digest(self.latest)), (self.flags.baseline_sha256, self.flags.new_sha256))
        self.assertEqual(set(self.root.iterdir()), {self.base, self.latest, self.native})

    def test_rollback_between_evidence_and_receipt(self):
        original_connect = sqlite3.connect
        writes = []

        def observed_connection(*args, **kwargs):
            db = original_connect(*args, **kwargs)

            def authorize(action, table, column, schema, trigger):
                if action == sqlite3.SQLITE_INSERT and schema == "main":
                    writes.append(table)
                    if writes.count("migration_relation_origins") == 2:
                        return sqlite3.SQLITE_DENY
                return sqlite3.SQLITE_OK

            db.set_authorizer(authorize)
            return db

        with patch.object(op.sqlite3, "connect", side_effect=observed_connection):
            with self.assertRaises(sqlite3.DatabaseError):
                op.run(self.flags)
        self.assertEqual(writes, ["recommendation_shown", "migration_relation_origins", "migration_relation_origins"])
        self.assertEqual(digest(self.native), self.flags.native_baseline_sha256)
        self.assertEqual(op.run(self.flags), "applied")

    def test_rollback_journal_mode(self):
        edit(self.native, "PRAGMA journal_mode=DELETE")
        self.flags.native_baseline_sha256 = digest(self.native)
        self.assertEqual(op.run(self.flags), "applied")
        self.assertEqual(op.run(self.flags), "replayed")

    def test_edit_and_deletion(self):
        for sql in ("UPDATE daily_shown SET count=1 WHERE asset_id=? AND day=?",
                    "DELETE FROM daily_shown WHERE asset_id=? AND day=?"):
            with self.subTest(sql=sql):
                shutil.copyfile(self.seed / self.latest.name, self.latest)
                edit(self.latest, sql, (REP, D1))
                self.flags.new_sha256 = digest(self.latest)
                self.refused("baseline_exposure_removed_or_changed")

    def test_representative_day_collisions(self):
        for asset, day, reason in ((REP, D2, "new_representative_day_collision"),
                                   (THIRD, D1, "baseline_representative_day_collision")):
            with self.subTest(reason=reason):
                shutil.copyfile(self.seed / self.latest.name, self.latest)
                edit(self.latest, "INSERT INTO daily_shown VALUES(?,?,1)", (asset, day))
                self.flags.new_sha256 = digest(self.latest)
                self.refused(reason)

    def test_baseline_evidence_projection_and_physical_alias(self):
        for sql, args, reason in (
                ("UPDATE migration_relation_origins SET original='{}' WHERE kind=?",
                 (op.EXPOSURE_KIND,), "baseline_exposure_evidence"),
                ("INSERT INTO recommendation_shown VALUES(?,?,1)", (D2, REP), "native_exposures"),
                ("DELETE FROM migration_media_aliases WHERE source_asset_id=?", (FRESH,), "baseline_alias_coverage"),
                ("UPDATE migration_media_aliases SET asset_id=? WHERE source_asset_id=?", (SOLO, DONOR), "alias_physical_mismatch"),
                ("UPDATE schema_migrations SET sha256='incorrect' WHERE version=1", (), "native_migration_ledger"),
                ("PRAGMA user_version=45", (), "native_user_version"),
                ("CREATE TRIGGER extra AFTER INSERT ON recommendation_shown BEGIN DELETE FROM likes; END", (), "native_schema"),
                ("INSERT INTO asset_locations VALUES('absent','missing','x','x',1,0,2,NULL)", (), "foreign_key_failed_main")):
            with self.subTest(reason=reason):
                shutil.copyfile(self.seed / self.native.name, self.native)
                edit(self.native, sql, args)
                self.flags.native_baseline_sha256 = digest(self.native)
                self.refused(reason)

    def test_source_collision_without_receipt(self):
        new_source = op.SOURCE_PREFIX + self.flags.new_sha256
        for sql, args, reason in (
                ("INSERT INTO migration_relation_origins VALUES(?,?,?,'{}')",
                 (new_source, op.EXPOSURE_KIND, "fixture-evidence"), "new_exposure_evidence_without_receipt"),
                ("INSERT INTO migration_media_aliases VALUES(?,?,?)", (new_source, SOLO, SOLO), "new_source_already_used"),
                ("INSERT INTO migration_relation_origins VALUES('fixture-other',?,?,?)",
                 (op.EXPOSURE_KIND, "fixture-evidence", "{}"), "foreign_exposure_provenance")):
            with self.subTest(reason=reason):
                shutil.copyfile(self.seed / self.native.name, self.native)
                edit(self.native, sql, args)
                self.flags.native_baseline_sha256 = digest(self.native)
                self.refused(reason)

    def test_conflicting_receipt_or_missing_replay_fact(self):
        self.assertEqual(op.run(self.flags), "applied")
        saved = self.root / "saved.sqlite"
        shutil.copyfile(self.native, saved)
        for sql, args, reason in (
                ("UPDATE migration_relation_origins SET original='{}' WHERE kind=?", (op.RECEIPT_KIND,), "conflicting_receipt"),
                ("DELETE FROM recommendation_shown WHERE day=?", (D2,), "native_exposures"),
                ("DELETE FROM migration_relation_origins WHERE input_id=? AND kind=?",
                 (op.SOURCE_PREFIX + self.flags.new_sha256, op.EXPOSURE_KIND), "new_exposure_evidence")):
            with self.subTest(reason=reason):
                shutil.copyfile(saved, self.native)
                edit(self.native, sql, args)
                self.refused(reason)
        shutil.copyfile(saved, self.native)
        self.flags.native_baseline_sha256 = "b" * 64
        self.refused("conflicting_receipt")

    def test_refuse_unsealed_aliases_and_fingerprints(self):
        for key, reason in (("baseline_sha256", "baseline_snapshot_digest"), ("new_sha256", "new_snapshot_digest"),
                            ("native_baseline_sha256", "native_baseline_digest")):
            with self.subTest(reason=reason):
                prior = getattr(self.flags, key)
                setattr(self.flags, key, "b" * 64)
                self.refused(reason)
                setattr(self.flags, key, prior)
        for path in (self.base, self.latest, self.native):
            sidecar = path.with_name(path.name + "-journal")
            sidecar.touch()
            self.refused("sealed_single_file_required")
            sidecar.unlink()
        hardlink = self.root / "another-native-name.sqlite"
        os.link(self.native, hardlink)
        self.refused("native_copy_has_hardlinks")
        hardlink.unlink()
        self.flags.native_copy = str(self.base)
        self.refused("three_distinct_files_required")

    def test_invalid_legacy_data(self):
        for sql, args, reason in (
                ("UPDATE schema_migrations SET dirty=1", (), "clean_v26_required"),
                ("INSERT INTO daily_shown VALUES('absent',?,1)", (D3,), "foreign_key_failed_latest"),
                ("INSERT INTO daily_shown VALUES(?,?,-1)", (FRESH, D1), "invalid_exposure"),
                ("INSERT INTO daily_shown VALUES(?,'2024-02-30',1)", (FRESH,), "invalid_exposure")):
            with self.subTest(reason=reason):
                shutil.copyfile(self.seed / self.latest.name, self.latest)
                edit(self.latest, sql, args)
                self.flags.new_sha256 = digest(self.latest)
                self.refused(reason)


if __name__ == "__main__":
    unittest.main()
