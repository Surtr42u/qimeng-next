"""Exercise the distributed converter, restore tool and server on synthetic v26 data only."""
from __future__ import annotations

import argparse
from contextlib import closing
import hashlib
import json
import os
from pathlib import Path
import secrets
import socket
import sqlite3
import struct
import subprocess
import tempfile
import time
import urllib.error
import urllib.request
import uuid
import zlib


def ident(n):
    return str(uuid.UUID(int=n, version=4))


LIB, A, B, T, AUTHOR, TAG = (ident(i) for i in range(1, 7))
AT = "2024-01-02T03:04:05.000Z"
NANO = 1704164645 * 1_000_000_000
PERSONAL = "synthetic-sealed-only-content"


def png(red):
    def chunk(kind, data):
        return struct.pack(">I", len(data)) + kind + data + struct.pack(">I", zlib.crc32(kind + data))

    header = struct.pack(">IIBBBBB", 1, 1, 8, 2, 0, 0, 0)
    return b"\x89PNG\r\n\x1a\n" + chunk(b"IHDR", header) + chunk(b"IDAT", zlib.compress(bytes([0, red, 0, 0]))) + chunk(b"IEND", b"")


def digest(path):
    with path.open("rb") as f:
        return hashlib.file_digest(f, "sha256").hexdigest()


def read_json(path):
    return json.loads(path.read_text("utf-8"))


def fixture(migrations, base):
    source = base / "sealed"
    data = source / "old-data"
    media = source / "media"
    data.mkdir(parents=True)
    media.mkdir()
    db = data / "v26.sqlite"
    files = [media / "first.png", media / "second.png"]
    for file in files:
        file.write_bytes(png(60))
        os.utime(file, ns=(NANO, NANO))
    (media / "separate-notes.txt").write_text("synthetic auxiliary file", encoding="utf-8")
    trash = data / "trash"
    discarded = trash / f"{NANO:019d}" / T / "folder" / "recycled.png"
    discarded.parent.mkdir(parents=True)
    discarded.write_bytes(png(224))
    os.utime(discarded, ns=(NANO, NANO))
    meta = discarded.with_name(discarded.name + ".meta.json")
    meta.write_text(json.dumps({"asset_id": T, "library_id": LIB, "original_path": "folder/recycled.png",
                                "media_type": "image", "deleted_at": AT}), encoding="utf-8")
    schema = sorted(migrations.glob("*.up.sql"))
    assert [int(p.name.split("_", 1)[0]) for p in schema] == list(range(1, 27))
    with closing(sqlite3.connect(db)) as con:
        con.execute("PRAGMA journal_mode=WAL")
        con.execute("PRAGMA foreign_keys=ON")
        for sql in schema:
            con.executescript(sql.read_text("utf-8"))
        con.execute("CREATE TABLE schema_migrations(version INTEGER NOT NULL PRIMARY KEY,dirty BOOLEAN NOT NULL)")
        con.execute("INSERT INTO schema_migrations VALUES(26,0)")
        con.execute("INSERT INTO libraries(id,name,root_path,created_at) VALUES(?,?,?,?)",
                    (LIB, "Fixture library", PERSONAL, AT))
        for ident_, file in zip((A, B), files):
            con.execute("""INSERT INTO assets(asset_id,library_id,rel_path,file_name,media_type,size_bytes,
                        mtime,width,height,created_at,updated_at) VALUES(?,?,?,?,'image',?,?,1,1,?,?)""",
                        (ident_, LIB, file.name, file.name, file.stat().st_size, AT, AT, AT))
        con.execute("INSERT INTO authors(id,display_name,type,created_at,followed,origin) VALUES(?,?,'regular',?,1,'client')",
                    (AUTHOR, "Fixture Artist", AT))
        con.execute("INSERT INTO asset_authors(asset_id,author_id,created_at,origin) VALUES(?,?,?,'client')", (A, AUTHOR, AT))
        con.execute("INSERT INTO tags(id,name,created_at) VALUES(?,?,?)", (TAG, "fixture-tag", AT))
        con.execute("INSERT INTO asset_tags(asset_id,tag_id,created_at,origin) VALUES(?,?,?,'client')", (A, TAG, AT))
        con.execute("INSERT INTO favorites(asset_id,created_at) VALUES(?,?)", (A, AT))
        con.execute("INSERT INTO likes(asset_id,day,created_at) VALUES(?,'2024-01-02',?)", (A, AT))
        con.execute("""INSERT INTO view_events(asset_id,kind,session_id,started_at,day)
                       VALUES(?,'open','synthetic-view-session',?,'2024-01-02')""", (A, AT))
        con.execute("INSERT INTO users(id,name,password_hash,role,token_hash,created_at) VALUES('old','test',?,'admin',?,?)",
                    (PERSONAL, PERSONAL, AT))
        con.execute("INSERT INTO ai_audit(client_id,method,path,action,status_code,request_id,created_at) VALUES('old','GET',?,?,200,?,?)",
                    (PERSONAL, PERSONAL, PERSONAL, AT))
        con.execute("INSERT INTO upload_receipts VALUES('upload',?,?,?,?)", (PERSONAL, A, files[0].stat().st_size, NANO // 10**6))
        con.commit()
        con.execute("PRAGMA wal_checkpoint(TRUNCATE)")
    assert not any(Path(str(db) + suffix).exists() for suffix in ("-wal", "-shm", "-journal"))
    roots = source / "roots.json"
    roots.write_text(json.dumps({LIB: str(media)}), encoding="utf-8")
    return source, db, media, trash, discarded, meta, roots


def command(args, success=True):
    proc = subprocess.run(list(map(str, args)), capture_output=True, timeout=90)
    if (proc.returncode == 0) != success:
        # Only synthetic diagnostics and the fixed converter/restore error
        # codes can reach the CI log. Do not dump a full fixture or command.
        raise RuntimeError(f"{Path(args[0]).name} exited {proc.returncode}: {proc.stderr.decode(errors='replace')[-400:]}")
    return proc


def source_state(source):
    return {p.relative_to(source).as_posix(): (digest(p), p.stat().st_mtime_ns, p.stat().st_size)
            for p in source.rglob("*") if p.is_file()}


def serve_restored(binary, data, original):
    with socket.socket() as sock:
        sock.bind(("127.0.0.1", 0))
        port = sock.getsockname()[1]
    token = secrets.token_urlsafe(40)
    env = {k: v for k, v in os.environ.items() if not k.startswith("QIMENG_")}
    env.update(QIMENG_DATA_DIR=str(data), QIMENG_LISTEN=f"127.0.0.1:{port}", QIMENG_ADMIN_TOKEN=token)

    def request(path, payload=None, extra_headers=None):
        req = urllib.request.Request(f"http://127.0.0.1:{port}" + path, data=payload,
                                     headers={"Authorization": "Bearer " + token, **(extra_headers or {})})
        with urllib.request.urlopen(req, timeout=10) as res:
            return res.read()

    with subprocess.Popen([str(binary)], cwd=data.parent, env=env, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL) as p:
        try:
            deadline = time.monotonic() + 40
            while True:
                assert p.poll() is None, "restored service failed to start"
                try:
                    request("/healthz")
                    break
                except urllib.error.URLError:
                    if time.monotonic() >= deadline:
                        raise RuntimeError("restored service did not become ready")
                    time.sleep(.15)
            asset = json.loads(request("/api/v1/assets/" + A))
            assert asset["id"] == A and asset["libraryId"] == LIB and asset["contentVersion"] == 1
            assert asset["favorite"] and asset["likeCount"] == 1 and asset["tags"] == ["fixture-tag"], "catalog/activity projection"
            assert asset["width"] == asset["height"] == 1
            author = json.loads(request("/api/v1/authors/" + asset["authorId"]))
            assert author["name"] == "Fixture Artist"
            assert json.loads(request("/api/v1/history"))["items"], "native history read"
            assert request(asset["contentUrl"]) == original.read_bytes()
            second = json.loads(request("/api/v1/assets/" + B))
            assert second["id"] != asset["id"] and second["hash"] == asset["hash"]
            try:
                request("/api/v1/migration/v2/imports?sourceId=fixture-old-source", b"{}",
                        {"Content-Type": "application/octet-stream", "Idempotency-Key": "fixture-retired-import"})
            except urllib.error.HTTPError as e:
                assert e.code == 410, e.code
            else:
                raise AssertionError("retired import unexpectedly accepted")
        finally:
            p.terminate()
            p.wait(timeout=20)


def run(args, base):
    ext = ".exe" if os.name == "nt" else ""
    convert = args.bin.resolve() / ("qimeng-legacy-convert" + ext)
    backup = args.bin.resolve() / ("qimeng-backup" + ext)
    server = args.server.resolve() / ("qimeng-next" + ext)
    source, db, media, trash, discarded, meta, roots = fixture(args.migrations, base)
    initial = source_state(source)
    cli = [convert, "--source-db", db, "--roots", roots, "--trash-root", trash, "--source-timezone", "Asia/Shanghai"]
    out = base / "converted"
    attempted = command([*cli, "--output", out])
    receipt = read_json(out / "receipt.json")
    assert json.loads(attempted.stdout) == receipt and receipt["assetCount"] == 3
    coverage = read_json(out / "coverage.json")
    assert coverage["liveAssets"] == 2 and coverage["trashedAssets"] == 1 and coverage["unarchivedNonMediaFiles"] == 1
    assert coverage["sourceId"] == "legacy-cold-v26:" + digest(db)
    assert not (out / ".incomplete").exists()
    archive = out / "library.qnext"
    expected = (out / "library.qnext.sha256").read_text("ascii").strip()
    assert expected == digest(archive) == receipt["archiveSHA256"]
    # The synthetic archive is intentionally small. A unique old-only canary
    # appears in the source credential, absolute binding and opaque audit rows.
    assert PERSONAL.encode() not in archive.read_bytes()
    verified = json.loads(command([backup, "verify", "--archive", archive, "--sha256", expected]).stdout)
    assert verified["state"] == "verified" and verified["assetCount"] == 3
    dst = base / "restored"
    restored = json.loads(command([backup, "restore", "--archive", archive, "--sha256", expected, "--destination", dst]).stdout)
    assert restored["instanceId"] != receipt["sourceInstanceId"] == restored["sourceInstanceId"]
    expected_locations = [(media / "first.png", dst / "libraries" / LIB / "first.png"),
                          (media / "second.png", dst / "libraries" / LIB / "second.png"),
                          (discarded, dst / "libraries" / LIB / ".qimeng-internal" / "trash" / T / discarded.name)]
    for old, new in expected_locations:
        assert (digest(new), new.stat().st_mtime_ns) == (digest(old), old.stat().st_mtime_ns)
    with closing(sqlite3.connect((dst / "qimeng.sqlite").as_uri() + "?mode=ro", uri=True)) as con:
        assert con.execute("SELECT count(*) FROM migration_media_aliases").fetchone() == (3,)
        assert con.execute("SELECT count(*) FROM migration_inputs").fetchone() == (0,)
        assert con.execute("SELECT count(*) FROM credentials").fetchone() == (0,)
        assert con.execute("SELECT count(*) FROM migration_relation_origins WHERE kind='cold26/history/ai_audit'").fetchone() == (1,)
        assert con.execute("SELECT count(*) FROM asset_content_versions WHERE version=1").fetchone() == (3,)
    serve_restored(server, dst, media / "first.png")
    command([backup, "restore", "--archive", archive, "--sha256", expected, "--destination", dst], False)
    command([backup, "verify", "--archive", archive, "--sha256", "0" * 64], False)
    command([*cli, "--output", out], False)
    inside = media / "forbidden-output"
    command([*cli, "--output", inside], False)
    assert not inside.exists() and source_state(source) == initial

    def refused(name, why, *flags):
        target = base / name
        result = command([*cli, "--output", target, *flags], False)
        assert not (target / "library.qnext").exists()
        report = read_json(target / "coverage.json")
        actual = report["problem"]["code"]
        assert actual == why, f"{name} expected {why}, received {actual}"
        assert str(media).encode() not in result.stderr and b"first.png" not in result.stderr
        return report

    refused("no-space", "output_reserve_exhausted", "--reserve-bytes", str(2**62))
    sidecar = Path(str(db) + "-wal")
    sidecar.write_bytes(b"synthetic")
    refused("live-sidecar", "sealed_database_with_no_sidecars_required")
    sidecar.unlink()
    # A missing file, a one-byte size discrepancy, and an orphaned trash payload
    # are separate errors. Each failure has its own never-reused destination.
    original = media / "first.png"
    saved = original.read_bytes()
    original.write_bytes(saved + b"x")
    os.utime(original, ns=(NANO, NANO))
    row = refused("wrong-size", "original_differs_from_catalog_size_or_mtime")["problem"]
    assert row["table"] == "assets" and row["rowId"] > 0
    original.unlink()
    refused("missing-original", "source_file_missing")
    original.write_bytes(saved)
    os.utime(original, ns=(NANO, NANO))
    extra = media / "unindexed.png"
    extra.write_bytes(png(1))
    refused("unindexed", "unindexed_source_media")
    extra.unlink()
    old_meta = meta.read_bytes()
    meta.unlink()
    refused("missing-meta", "trash_metadata_missing")
    meta.write_bytes(old_meta)
    alternate = base / "second-catalog"
    alternate.mkdir()
    db2 = alternate / "extra.sqlite"
    with closing(sqlite3.connect(db.as_uri() + "?mode=ro", uri=True)) as src, closing(sqlite3.connect(db2)) as con:
        src.backup(con)
        con.executescript("CREATE TABLE unclassified_work(k INTEGER); INSERT INTO unclassified_work VALUES(7)")
        con.commit()
    unsupported = refused("unknown-table", "unknown_nonempty_table", "--source-db", db2)
    assert any(t["table"] == "unclassified_work" and t["rows"] == 1 for t in unsupported["tables"])
    return {"sourceCommit": os.environ["GITHUB_SHA"], "legacyCommit": os.environ["REFERENCE_SHA"],
            "runner": os.environ["RUNNER_OS"], "oldSchemaVersion": 26, "originalsReadBack": 3,
            "sameContentDistinctAssets": 2, "sourceDigestAndFileTimesUnchanged": True,
            "restoredIndependentInstance": True, "restoredServerContentAndHistory": True,
            "sourceCredentialsExcluded": True, "retiredHTTPStatus": 410,
            "refusals": ["existing-destination", "incorrect-archive-digest", "output-inside-source", "insufficient-space",
                         "unsealed-database", "incorrect-length", "missing-original", "unindexed-media", "orphaned-trash", "unknown-table"]}


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    for arg in ("migrations", "bin", "server", "receipt"):
        parser.add_argument("--" + arg, type=Path, required=True)
    options = parser.parse_args()
    with tempfile.TemporaryDirectory(prefix="legacy-cold-synthetic-") as temporary:
        evidence = run(options, Path(temporary))
    options.receipt.write_text(json.dumps(evidence, indent=2) + "\n", encoding="utf-8")
