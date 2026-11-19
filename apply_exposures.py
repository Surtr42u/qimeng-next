"""Apply the reviewed append-only v26 exposure delta to a private Next 0045 copy."""

import argparse
import hashlib
import json
import os
import re
import sqlite3
import stat
import sys
from contextlib import closing
from pathlib import Path


# Public reference: Surtr42u/qimeng-next at 8be0e9ea449c4957fede2e70b09bb53b68ba1f66.
# schema_fixture.py reproduces these digests by applying its migrations to an empty DB.
SCHEMA_DIGEST = "dfcac30da99062e77515e52175c0fd9aded576ac7b23145bcb48db7e4479ee4e"
LEDGER_DIGEST = "61fb5080ead85073e6d0b0e5c169ee7782656defb0b0062d5b2eb14ddc092e8d"
SOURCE_PREFIX = "legacy-cold-v26:"
EXPOSURE_KIND = "cold26/daily_shown"
RECEIPT_KIND = "ops/cold26-daily-shown-append/v1"
SHA_PATTERN = re.compile(r"[0-9a-fA-F]{64}")
SUFFIXES = ("-wal", "-shm", "-journal")


class Refusal(Exception):
    """Only fixed, non-private codes may leave the CLI."""


def require(condition, code):
    if not condition:
        raise Refusal(code)


def file_info(path):
    require(path.is_absolute() and ".." not in path.parts, "absolute_plain_file_required")
    for item in (*reversed(path.parents), path):
        info = item.lstat()
        reparse = getattr(info, "st_file_attributes", 0) & stat.FILE_ATTRIBUTE_REPARSE_POINT
        require(not reparse and not stat.S_ISLNK(info.st_mode), "linked_path")
        if item != path:
            require(stat.S_ISDIR(info.st_mode), "parent_is_not_directory")
    require(stat.S_ISREG(info.st_mode), "ordinary_file_required")
    return info


def no_sidecars(path):
    for suffix in SUFFIXES:
        try:
            path.with_name(path.name + suffix).lstat()
        except FileNotFoundError:
            continue
        raise Refusal("sealed_single_file_required")


def stamp(info):
    return info.st_dev, info.st_ino, info.st_size, info.st_mtime_ns


def hash_file(path, expected_info):
    with path.open("rb") as source:
        require(stamp(os.fstat(source.fileno())) == stamp(expected_info), "file_changed")
        digest = hashlib.sha256()
        while chunk := source.read(1024 * 1024):
            digest.update(chunk)
        require(stamp(os.fstat(source.fileno())) == stamp(expected_info), "file_changed")
    require(stamp(file_info(path)) == stamp(expected_info), "file_changed")
    return digest.hexdigest()


def rows_digest(rows):
    digest = hashlib.sha256()
    for row in rows:
        for value in row:
            field = str(value).encode("utf-8")
            digest.update(len(field).to_bytes(8, "big"))
            digest.update(field)
    return digest.hexdigest()


def empty(db, sql, args, code):
    require(db.execute(sql, args).fetchone() is None, code)


def same_rows(db, left, right, args, code):
    # Both sides have a proven primary key or are grouped by that key.
    empty(db, f"{left} EXCEPT {right} LIMIT 1", args, code)
    empty(db, f"{right} EXCEPT {left} LIMIT 1", args, code)


def integrity(db, schema):
    require(list(db.execute(f"PRAGMA {schema}.quick_check")) == [("ok",)],
            "quick_check_failed_" + schema)
    empty(db, f"PRAGMA {schema}.foreign_key_check", (), "foreign_key_failed_" + schema)


def check_schemas(db):
    require(db.execute("PRAGMA main.user_version").fetchone() == (0,), "native_user_version")
    objects = "SELECT type,name,tbl_name,coalesce(sql,'') FROM main.sqlite_schema ORDER BY type,name"
    require(rows_digest(db.execute(objects)) == SCHEMA_DIGEST, "native_schema")
    ledger = "SELECT version,name,sha256 FROM main.schema_migrations ORDER BY version"
    require(rows_digest(db.execute(ledger)) == LEDGER_DIGEST, "native_migration_ledger")
    for schema in ("baseline", "latest"):
        required = {("assets", "table"), ("schema_migrations", "table"), ("daily_shown", "table")}
        table_types = set(db.execute(
            f"SELECT name,type FROM {schema}.sqlite_schema WHERE name IN ('assets','schema_migrations','daily_shown')"
        ))
        require(table_types == required, "unexpected_v26_tables")
        ledger = list(db.execute(f"SELECT version,dirty FROM {schema}.schema_migrations"))
        require(ledger == [(26, 0)], "clean_v26_required")
        cols = [(name, decl.upper(), nn, pk, hidden) for _, name, decl, nn, _, pk, hidden in
                db.execute(f"PRAGMA {schema}.table_xinfo(daily_shown)")]
        require(cols == [("asset_id", "TEXT", 1, 1, 0), ("day", "TEXT", 1, 2, 0),
                         ("count", "INTEGER", 1, 0, 0)], "unexpected_v26_daily_schema")
        integrity(db, schema)
        empty(db, f"""SELECT 1 FROM {schema}.daily_shown
            WHERE typeof(asset_id)!='text' OR asset_id='' OR typeof(count)!='integer' OR count<0
               OR typeof(day)!='text' OR day NOT GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]'
               OR date(day,'+0 days') IS NOT day LIMIT 1""", (), "invalid_exposure")
    integrity(db, "main")


def check_bindings(db, ids):
    before_assets = "SELECT asset_id FROM baseline.assets"
    same_rows(db, before_assets, "SELECT asset_id FROM latest.assets", ids, "source_asset_set_changed")
    same_rows(db, before_assets, """SELECT identity FROM main.migration_relation_origins
        WHERE input_id=:old AND kind='cold26/asset'""", ids, "baseline_asset_origins")
    zones = db.execute("""SELECT json_extract(original,'$.source_time_zone') FROM main.migration_relation_origins
        WHERE input_id=:old AND kind='cold26/activity' AND identity='v1'""", ids).fetchall()
    require(len(zones) == 1 and isinstance(zones[0][0], str) and zones[0][0] not in ("", "Local"),
            "cold_activity_receipt")
    groups = db.execute("SELECT DISTINCT source_id,baseline_sha256 FROM main.selected_image_merge_groups").fetchall()
    require(len(groups) == 1 and groups[0][0] == ids["old"] and
            isinstance(groups[0][1], str) and re.fullmatch("[0-9a-f]{64}", groups[0][1]), "merge_baseline")

    # Cold conversion uses the old ID as physical ID, including separately recovered trash.
    # 0045 changes only the visible alias and freezes its previous physical binding.
    db.execute("""CREATE TEMP TABLE binding AS
        SELECT a.source_asset_id AS old_asset, a.asset_id AS visible, p.asset_id AS physical,
               m.representative_id AS member_rep, frozen.physical_asset_id AS frozen_physical,
               p.trashed AS physical_trash, v.trashed AS visible_trash
        FROM main.migration_media_aliases a
        LEFT JOIN main.asset_locations p ON p.asset_id=a.source_asset_id
        LEFT JOIN main.asset_locations v ON v.asset_id=a.asset_id
        LEFT JOIN main.selected_image_merge_members m ON m.physical_asset_id=a.source_asset_id
        LEFT JOIN main.selected_image_merge_aliases frozen USING(source_id,source_asset_id)
        WHERE a.source_id=:old""", ids)
    db.execute("CREATE UNIQUE INDEX temp.bound_old_asset ON binding(old_asset)")
    empty(db, """SELECT 1 FROM temp.binding
        WHERE physical IS NOT old_asset OR visible_trash IS NULL OR
          (member_rep IS NULL AND (visible IS NOT old_asset OR frozen_physical IS NOT NULL)) OR
          (member_rep IS NOT NULL AND (frozen_physical IS NOT old_asset OR member_rep IS NOT visible
                 OR visible_trash!=0 OR physical_trash!=(physical!=visible))) LIMIT 1""", ids, "alias_physical_mismatch")
    empty(db, """SELECT physical_asset_id FROM main.selected_image_merge_members
        EXCEPT SELECT old_asset FROM temp.binding WHERE member_rep IS NOT NULL LIMIT 1""", ids, "missing_member_alias")
    empty(db, """SELECT source_asset_id FROM main.selected_image_merge_aliases WHERE source_id=:old
        EXCEPT SELECT old_asset FROM temp.binding LIMIT 1""", ids, "orphan_frozen_alias")
    same_rows(db, "SELECT old_asset FROM temp.binding", """SELECT identity FROM main.migration_relation_origins
        WHERE input_id=:old AND kind IN ('cold26/asset','cold26/trash')""", ids, "baseline_alias_coverage")
    empty(db, """SELECT 1 FROM main.asset_records r LEFT JOIN main.asset_locations l ON l.asset_id=r.id
        WHERE l.asset_id IS NULL LIMIT 1""", ids, "missing_physical_location")
    empty(db, """SELECT 1 FROM baseline.assets a JOIN temp.binding b ON b.old_asset=a.asset_id
        WHERE b.visible_trash!=0 LIMIT 1""", ids, "source_asset_not_visible")


def source_evidence(schema):
    return f"""SELECT json_array(asset_id,day),json_object('asset_id',asset_id,'day',day,'count',count)
                 FROM {schema}.daily_shown"""


def project(db, ids):
    facts = "SELECT asset_id,day,count FROM "
    empty(db, facts + "baseline.daily_shown EXCEPT " + facts + "latest.daily_shown LIMIT 1",
          ids, "baseline_exposure_removed_or_changed")
    same_rows(db, source_evidence("baseline"), """SELECT identity,original FROM main.migration_relation_origins
        WHERE input_id=:old AND kind=:exposure""", ids, "baseline_exposure_evidence")
    empty(db, """SELECT 1 FROM main.migration_relation_origins
        WHERE kind=:exposure AND input_id NOT IN (:old,:new) LIMIT 1""", ids, "foreign_exposure_provenance")

    db.execute("""CREATE TEMP TABLE old_projection AS
        SELECT d.day,b.visible AS asset_id,sum(d.count) AS count FROM baseline.daily_shown d
        JOIN temp.binding b ON b.old_asset=d.asset_id GROUP BY d.day,b.visible""")
    db.execute("CREATE UNIQUE INDEX temp.old_projected_key ON old_projection(day,asset_id)")
    # Keep the SQL JSON bytes identical to legacycold/activity.impressions.
    db.execute("""CREATE TEMP TABLE additions AS
        SELECT d.day,b.visible AS asset_id,d.count,json_array(d.asset_id,d.day) AS identity,
            json_object('asset_id',d.asset_id,'day',d.day,'count',d.count) AS original
        FROM latest.daily_shown d JOIN temp.binding b ON b.old_asset=d.asset_id
        WHERE NOT EXISTS(SELECT 1 FROM baseline.daily_shown o WHERE o.asset_id=d.asset_id AND o.day=d.day)""")
    n = db.execute("SELECT count(*) FROM temp.additions").fetchone()[0]
    require(n > 0, "no_new_exposure_keys")
    empty(db, """SELECT 1 FROM temp.additions GROUP BY day,asset_id HAVING count(*)>1 LIMIT 1""",
          ids, "new_representative_day_collision")
    empty(db, "SELECT 1 FROM temp.additions JOIN temp.old_projection USING(day,asset_id) LIMIT 1",
          ids, "baseline_representative_day_collision")
    return n


def receipt_record(db, ids, count):
    # Use Python's unbounded integer sum for the private receipt.
    total = sum(row[0] for row in db.execute("SELECT count FROM temp.additions"))
    body = json.dumps({"baselineSHA256": ids["old"][len(SOURCE_PREFIX):],
                       "newSHA256": ids["new"][len(SOURCE_PREFIX):],
                       "nativeBaselineSHA256": ids["native"], "addedKeys": count,
                       "addedDisplays": total}, separators=(",", ":"), sort_keys=True)
    identity = hashlib.sha256((RECEIPT_KIND + "\n" + body).encode("ascii")).hexdigest()
    return ids["new"], RECEIPT_KIND, identity, body


def check_namespace(db, ids):
    for table in ("migration_inputs", "migration_media_aliases", "migration_author_aliases",
                  "migration_event_origins", "author_txt_fragments", "selected_image_merge_aliases"):
        empty(db, f"SELECT 1 FROM main.{table} WHERE source_id=:new LIMIT 1", ids, "new_source_already_used")
    empty(db, """SELECT 1 FROM main.migration_relation_origins WHERE input_id=:new
        AND kind NOT IN (:exposure,:receipt) LIMIT 1""", ids, "new_source_already_used")


def check_projection(db, ids, applied):
    # Wrapping the UNION as a subquery keeps both EXCEPT checks over the complete set.
    expected = "SELECT day,asset_id,count FROM temp.old_projection"
    if applied:
        expected = "SELECT * FROM (" + expected + " UNION ALL SELECT day,asset_id,count FROM temp.additions)"
    same_rows(db, expected, "SELECT day,asset_id,count FROM main.recommendation_shown", ids, "native_exposures")
    origins = """SELECT identity,original FROM main.migration_relation_origins WHERE input_id=:new AND kind=:exposure"""
    if applied:
        same_rows(db, "SELECT identity,original FROM temp.additions", origins, ids, "new_exposure_evidence")
    else:
        empty(db, origins + " LIMIT 1", ids, "new_exposure_evidence_without_receipt")


def process(db, ids, native_digest):
    check_schemas(db)
    check_bindings(db, ids)
    check_namespace(db, ids)
    count = project(db, ids)
    receipt = receipt_record(db, ids, count)
    receipt_sql = "SELECT input_id,kind,identity,original FROM main.migration_relation_origins WHERE kind=:receipt"
    previous = db.execute(receipt_sql, ids).fetchall()
    if previous:
        require(previous == [receipt], "conflicting_receipt")
        check_projection(db, ids, True)
        return "replayed"
    require(native_digest == ids["native"], "native_baseline_digest")
    check_projection(db, ids, False)

    changes = db.total_changes
    db.execute("""INSERT INTO main.recommendation_shown(day,asset_id,count)
        SELECT day,asset_id,count FROM temp.additions""")
    db.execute("""INSERT INTO main.migration_relation_origins(input_id,kind,identity,original)
        SELECT :new,:exposure,identity,original FROM temp.additions""", ids)
    db.execute("INSERT INTO main.migration_relation_origins(input_id,kind,identity,original) VALUES(?,?,?,?)", receipt)
    require(db.total_changes - changes == 2 * count + 1, "unexpected_write_scope")
    check_projection(db, ids, True)
    require(db.execute(receipt_sql, ids).fetchall() == [receipt], "receipt_readback")
    integrity(db, "main")
    return "applied"


def run(options):
    paths = tuple(Path(path) for path in (options.baseline_v26, options.new_v26, options.native_copy))
    infos = [file_info(path) for path in paths]
    require(all(not os.path.samestat(infos[i], infos[j]) for i, j in ((0, 1), (1, 2), (0, 2))),
            "three_distinct_files_required")
    require(infos[2].st_nlink == 1, "native_copy_has_hardlinks")
    for path in paths:
        no_sidecars(path)
    digests = (options.baseline_sha256, options.new_sha256, options.native_baseline_sha256)
    require(all(SHA_PATTERN.fullmatch(value) for value in digests), "invalid_sha256")
    old, new, native = (value.lower() for value in digests)
    require(old != new, "identical_source_snapshots")
    require(hash_file(paths[0], infos[0]) == old, "baseline_snapshot_digest")
    require(hash_file(paths[1], infos[1]) == new, "new_snapshot_digest")
    ids = {"old": SOURCE_PREFIX + old, "new": SOURCE_PREFIX + new, "native": native,
           "exposure": EXPOSURE_KIND, "receipt": RECEIPT_KIND}

    with closing(sqlite3.connect(paths[2].as_uri() + "?mode=rw", uri=True, timeout=0, isolation_level=None)) as db:
        db.execute("PRAGMA trusted_schema=OFF")
        db.execute("PRAGMA temp_store=MEMORY")
        db.execute("PRAGMA foreign_keys=ON")
        db.execute("PRAGMA synchronous=FULL")
        db.execute("PRAGMA locking_mode=EXCLUSIVE")
        for schema, path in zip(("baseline", "latest"), paths[:2]):
            db.execute(f"ATTACH DATABASE ? AS {schema}", (path.as_uri() + "?mode=ro&immutable=1",))
        db.execute("BEGIN EXCLUSIVE")
        try:
            # A committed WAL from an outside writer would not be covered by the main-file digest.
            wal = paths[2].with_name(paths[2].name + "-wal")
            if wal.exists():
                require(wal.stat().st_size <= 32, "nonempty_native_wal")
            actual = hash_file(paths[2], infos[2])
            result = process(db, ids, actual)
            for path, info in zip(paths[:2], infos[:2]):
                no_sidecars(path)
                require(stamp(file_info(path)) == stamp(info), "sealed_input_changed")
            require(os.path.samestat(file_info(paths[2]), infos[2]), "native_copy_replaced")
            db.execute("COMMIT" if result == "applied" else "ROLLBACK")
        except BaseException:
            if db.in_transaction:
                db.rollback()
            raise
    return result


class PrivateParser(argparse.ArgumentParser):
    def error(self, message):
        self.exit(2, '{"status":"refused","reason":"invalid_arguments"}\n')


def main():
    parser = PrivateParser(description=__doc__, allow_abbrev=False)
    for name in ("baseline-v26", "new-v26", "native-copy", "baseline-sha256",
                 "new-sha256", "native-baseline-sha256"):
        parser.add_argument("--" + name, required=True)
    try:
        result = run(parser.parse_args())
    except Refusal as exc:
        print(json.dumps({"status": "refused", "reason": str(exc)}), file=sys.stderr)
        return 1
    except (OSError, sqlite3.Error, ValueError, OverflowError):
        print('{"status":"refused","reason":"database_or_filesystem_error"}', file=sys.stderr)
        return 1
    except KeyboardInterrupt:
        print('{"status":"interrupted","reason":"inspect_or_retry_private_copy"}', file=sys.stderr)
        return 130
    except Exception:
        print('{"status":"refused","reason":"unexpected_runtime_failure"}', file=sys.stderr)
        return 1
    print(json.dumps({"status": result}))
    return 0


if __name__ == "__main__":
    sys.exit(main())
