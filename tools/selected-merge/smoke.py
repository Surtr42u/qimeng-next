"""Exercise the distributed offline commands and normal server with synthetic originals."""
from __future__ import annotations

import argparse
from contextlib import closing, contextmanager
import hashlib
import json
import os
from pathlib import Path
import secrets
import shutil
import socket
import sqlite3
import subprocess
import sys
import tempfile
import time
import urllib.error
import urllib.request

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "legacy-cold"))
import smoke as cold


def write_json(name, value):
    name.write_text(json.dumps(value, ensure_ascii=False), encoding="utf-8")


def rows(db, sql, args=()):
    with closing(sqlite3.connect(db.as_uri() + "?mode=ro&immutable=1", uri=True)) as con:
        return con.execute(sql, args).fetchall()


def stat_file(p):
    st = p.stat()
    return cold.digest(p), st.st_size, st.st_mtime_ns, st.st_dev, st.st_ino


@contextmanager
def serving(server, data):
    with socket.socket() as sock:
        sock.bind(("127.0.0.1", 0))
        port = sock.getsockname()[1]
    token = secrets.token_urlsafe(40)
    env = {k: v for k, v in os.environ.items() if not k.startswith("QIMENG_")}
    env.update(QIMENG_DATA_DIR=str(data), QIMENG_LISTEN=f"127.0.0.1:{port}", QIMENG_ADMIN_TOKEN=token)

    def request(route, *, post=False, raw=False):
        headers = {"Authorization": "Bearer " + token}
        if post:
            headers.update({"Content-Type": "application/json", "Idempotency-Key": "fixture-" + secrets.token_hex(6)})
        req = urllib.request.Request(f"http://127.0.0.1:{port}" + route, data=b"{}" if post else None, headers=headers)
        with urllib.request.urlopen(req, timeout=12) as response:
            body = response.read()
        return body if raw else json.loads(body)

    with subprocess.Popen([str(server)], cwd=data.parent, env=env, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL) as proc:
        try:
            limit = time.monotonic() + 45
            while True:
                assert proc.poll() is None, "native service exited"
                try:
                    request("/healthz", raw=True)
                    break
                except urllib.error.URLError:
                    if time.monotonic() >= limit:
                        raise RuntimeError("native service readiness timed out")
                    time.sleep(.12)
            yield request
        finally:
            proc.terminate()
            proc.wait(timeout=20)


def native_export(request, destination):
    b = request("/api/v1/backups", post=True)
    base = "/api/v1/backups/" + b["id"]
    request(base + "/export", post=True)
    deadline = time.monotonic() + 60
    while True:
        b = request(base)
        if b.get("exportState") == "completed":
            break
        if b.get("exportState") in {"failed", "interrupted"} or time.monotonic() >= deadline:
            raise RuntimeError("native checkpoint export did not complete")
        time.sleep(.12)
    destination.write_bytes(request(base + "/archive", raw=True))
    assert b.get("scope") == "metadata" and b.get("exportedFiles") == 0, "the owner export must stay records-only"
    assert cold.digest(destination) == b["archiveHash"]
    return b["archiveHash"]


def add_unselected(db, media):
    other = media / "unselected.png"
    other.write_bytes((media / "first.png").read_bytes())
    os.utime(other, ns=(cold.NANO, cold.NANO))
    with closing(sqlite3.connect(db)) as con:
        con.execute("""INSERT INTO assets(asset_id,library_id,rel_path,file_name,media_type,size_bytes,
            mtime,width,height,created_at,updated_at) VALUES(?,?,?,?,'image',?,?,1,1,?,?)""",
                    (cold.ident(710), cold.LIB, other.name, other.name, other.stat().st_size, cold.AT, cold.AT, cold.AT))
        con.execute("""INSERT INTO view_events(asset_id,kind,session_id,started_at,day)
            VALUES(?,'open','fixture-second-session','2024-01-03T03:04:05Z','2024-01-03')""", (cold.B,))
        con.execute("INSERT INTO likes(asset_id,day,created_at) VALUES(?,'2024-01-03','2024-01-03T03:04:05Z')", (cold.B,))
        con.commit()
        con.execute("PRAGMA wal_checkpoint(TRUNCATE)")
    return other


def fresh_restore(binary, archive, parent):
    parent.mkdir()
    stage = parent / "work"
    output = cold.command([binary, "restore", "--archive", archive, "--sha256", cold.digest(archive), "--destination", stage])
    receipt = parent / "restore.json"
    receipt.write_bytes(output.stdout)
    return stage, receipt


def run(args, base):
    suffix = ".exe" if os.name == "nt" else ""
    merge = args.bin.resolve() / ("qimeng-merge-selected" + suffix)
    backup = args.bin.resolve() / ("qimeng-backup" + suffix)
    converter = args.fixture_bin.resolve() / ("qimeng-legacy-convert" + suffix)
    server = args.fixture_bin.resolve() / ("qimeng-next" + suffix)
    source, db, media, trash, recycled, _, roots = cold.fixture(args.migrations, base)
    other = add_unselected(db, media)
    before = cold.source_state(source)
    converted = base / "converted"
    cold.command([converter, "--source-db", db, "--roots", roots, "--trash-root", trash,
                  "--source-timezone", "Asia/Shanghai", "--output", converted])
    original = converted / "library.qnext"
    baseline = cold.digest(original)
    source_id = cold.read_json(converted / "receipt.json")["sourceId"]
    assert json.loads(cold.command([backup, "verify", "--archive", original, "--sha256", baseline]).stdout)["assetCount"] == 4
    content = cold.digest(media / "first.png")
    size = (media / "first.png").stat().st_size
    plan = base / "owner.json"
    decision = {"baselineSHA256": baseline, "sourceId": source_id, "groups": [{
        "representativeId": cold.A, "libraryId": cold.LIB, "relativePath": "second.png", "sha256": content,
        "members": [{"assetId": cold.A, "expectedRelativePath": "first.png", "expectedVersion": 1, "expectedSize": size, "sourceKeys": ["s1"]},
                    {"assetId": cold.B, "expectedRelativePath": "second.png", "expectedVersion": 1, "expectedSize": size, "sourceKeys": ["s2"]}],
        "sources": [{"key": "s1", "url": "https://example.org/posts/1", "at": cold.AT},
                    {"key": "s2", "url": "https://example.org/posts/2", "at": cold.AT, "caption": "synthetic other source"}]}]}
    write_json(plan, decision)

    accepted_catalogs = {}

    def restore_for_review(archive, parent):
        stage, receipt = fresh_restore(backup, archive, parent)
        accepted_catalogs[parent] = cold.digest(stage/"qimeng.sqlite")
        return stage, receipt

    def command(mode, parent, archive=original, encoding=plan, success=True, **changes):
        flags = {"archive": archive, "sha256": cold.digest(archive), "plan": encoding, "plan-sha256": cold.digest(encoding),
                 "restore-receipt": parent/"restore.json", "stage": parent/"work", "preimage": parent/"preimage", "destination": parent/"data"}
        if mode == "apply":
            flags["catalog-sha256"] = accepted_catalogs[parent]
        flags.update(changes)
        result = cold.command([merge, mode, *(val for k, v in flags.items() if v is not None for val in ("--"+k, v))], success)
        if success:
            return json.loads(result.stdout)
        assert str(parent).encode() not in result.stderr and b"second.png" not in result.stderr

    # The baseline is independently read by the ordinary service in a distinct,
    # tiny synthetic clone. The stage to be claimed has never been served.
    control = base / "baseline-control"
    ctrl, _ = restore_for_review(original, control)
    with serving(server, ctrl) as get:
        for asset_id in [cold.A, cold.B, cold.ident(710)]:
            asset = get("/api/v1/assets/" + asset_id)
            assert asset["id"] == asset_id and asset["hash"] == content
            assert hashlib.sha256(get(asset["contentUrl"], raw=True)).hexdigest() == content
        command("apply", control, success=False)
        assert not (control/"preimage").exists()

    changed = base/"changed-history"
    altered, _ = restore_for_review(original, changed)
    assert command("check", changed)["restoredCatalogSHA256"] == accepted_catalogs[changed]
    old_tree = cold.source_state(altered)
    with closing(sqlite3.connect(altered/"qimeng.sqlite")) as catalog:
        catalog.execute("UPDATE events SET data=json_set(data,'$.sessionId','fixture-changed-history')")
        catalog.commit()
        catalog.execute("PRAGMA wal_checkpoint(TRUNCATE)")
    new_tree = cold.source_state(altered)
    assert old_tree.keys() == new_tree.keys()
    assert {name for name in old_tree if old_tree[name] != new_tree[name]} == {"qimeng.sqlite"}
    command("apply", changed, success=False)
    assert new_tree == cold.source_state(altered) and not (changed/"preimage").exists() and not (changed/"data").exists()

    parent = base / "operation"
    stage, receipt = restore_for_review(original, parent)
    state = cold.source_state(stage)
    lib = stage / "libraries" / cold.LIB
    originals = {p.name: stat_file(p) for p in lib.glob("*.png")}
    saved_receipt = receipt.read_bytes()
    changed_receipt = cold.read_json(receipt)
    changed_receipt["instanceId"] = "fixture-different-restore"
    write_json(receipt, changed_receipt)
    command("apply", parent, success=False)
    receipt.write_bytes(saved_receipt)
    command("apply", parent, success=False, sha256="0"*64)
    command("apply", parent, success=False, **{"plan-sha256": "f"*64})
    command("apply", parent, success=False, **{"catalog-sha256": "f"*64})
    command("apply", parent, success=False, **{"catalog-sha256": None})
    command("apply", parent, success=False, **{"reserve-bytes": str(2**62)})
    wrong = json.loads(json.dumps(decision))
    wrong["groups"][0]["sha256"] = "a"*64
    bad = base / "another-plan.json"
    write_json(bad, wrong)
    command("apply", parent, encoding=bad, success=False)
    wrong["groups"][0]["sha256"] = content
    wrong["groups"][0]["relativePath"] = "unselected.png"
    write_json(bad, wrong)
    command("apply", parent, encoding=bad, success=False)
    wrong["groups"][0]["relativePath"] = "../outside.png"
    write_json(bad, wrong)
    command("apply", parent, encoding=bad, success=False)

    sentinel = parent / "data"
    sentinel.mkdir()
    (sentinel/"keep").write_bytes(b"unrelated")
    command("apply", parent, success=False)
    assert (sentinel/"keep").read_bytes() == b"unrelated"
    (sentinel/"keep").unlink()
    sentinel.rmdir()
    unexpected = lib / "extra.png"
    unexpected.write_bytes(b"unlisted")
    command("apply", parent, success=False)
    assert unexpected.read_bytes() == b"unlisted"
    unexpected.unlink()
    f = lib/"first.png"
    link = parent/"unrelated-hardlink"
    os.link(f, link)
    command("apply", parent, success=False)
    link.unlink()
    previous = f.read_bytes()
    f.write_bytes(previous[:-1] + bytes([previous[-1] ^ 1]))
    os.utime(f, ns=(cold.NANO, cold.NANO))
    command("apply", parent, success=False)
    f.write_bytes(previous)
    os.utime(f, ns=(cold.NANO-1_000_000_000, cold.NANO-1_000_000_000))
    command("apply", parent, success=False)
    os.utime(f, ns=(cold.NANO, cold.NANO))
    assert not (parent/"preimage").exists() and cold.source_state(stage) == state
    checked = command("check", parent)
    assert checked["state"] == "checked" and checked["members"] == 2 and checked["donors"] == 1
    assert checked["restoredCatalogSHA256"] == cold.digest(stage/"qimeng.sqlite")
    assert cold.source_state(stage) == state and not (parent/"preimage").exists()
    successful = command("apply", parent)
    assert not successful["alreadyApplied"] and successful["verifiedAssets"] == 4
    assert successful == command("apply", parent) and not stage.exists()
    command("apply", parent, success=False, **{"catalog-sha256": None})
    command("apply", parent, success=False, **{"catalog-sha256": "f"*64})

    pre = parent/"preimage"
    final = parent/"data"
    assert rows(pre/"qimeng.sqlite", "SELECT id FROM assets WHERE trashed=0 ORDER BY id") == sorted([(cold.A,), (cold.B,), (cold.ident(710),)])
    selections = cold.read_json(pre/"selected-files.json")
    assert len(cold.read_json(pre/"baseline-files.json")["files"]) == 4
    for m in selections:
        old = originals[Path(m["from"]).name]
        assert stat_file(pre/m["preimage"])[:3] == old[:3]
        assert stat_file(final/m["to"]) == old
    assert stat_file(final/"libraries"/cold.LIB/other.name) == originals[other.name]
    assert cold.digest(final/"libraries"/cold.LIB/".qimeng-internal"/"trash"/cold.T/recycled.name) == cold.digest(recycled)
    assert cold.digest(pre/"qimeng.sqlite") == cold.read_json(pre/"catalog-fingerprint.json")["sha256"]
    assert rows(final/"qimeng.sqlite", "SELECT count(*) FROM selected_image_merge_sources") == [(2,)]
    assert rows(final/"qimeng.sqlite", "SELECT asset_id,count(*) FROM events GROUP BY asset_id") == [(cold.A, 2)]
    assert rows(final/"qimeng.sqlite", "SELECT asset_id FROM migration_media_aliases WHERE source_id=? AND source_asset_id=?", (source_id, cold.B)) == [(cold.A,)]

    # Recreate the two durable interrupted-handoff layouts using only these
    # synthetic files. A new CLI process must preserve a competing destination.
    (pre/"published.json").rename(final/"RESTORE_INCOMPLETE")
    final.rename(stage)
    final.mkdir()
    flag = final/"keep"
    flag.write_bytes(b"other owner")
    command("apply", parent, success=False)
    assert flag.read_bytes() == b"other owner" and (stage/"RESTORE_INCOMPLETE").exists()
    flag.unlink()
    final.rmdir()
    assert command("apply", parent) == successful
    (pre/"published.json").rename(final/"RESTORE_INCOMPLETE")
    assert command("apply", parent) == successful

    # Receipts from the earlier optional-flag release sealed the actual catalog
    # fingerprint even when their request omitted expectedCatalogSHA256.
    for note in (pre/"ready.json", final/"SELECTED_IMAGE_MERGE.json", pre/"published.json"):
        record = cold.read_json(note)
        claim = record.get("claim", record)
        del claim["request"]["expectedCatalogSHA256"]
        write_json(note, record)
    assert command("apply", parent) == successful
    command("apply", parent, success=False, **{"catalog-sha256": "f"*64})

    merged = base/"merged.qnext"
    with serving(server, final) as get:
        asset = get("/api/v1/assets/"+cold.A)
        assert asset["fileName"] == "second.png" and asset["favorite"] and asset["likeCount"] == 2
        assert get(asset["contentUrl"], raw=True) == previous
        visible = get("/api/v1/assets?libraryId="+cold.LIB)["items"]
        assert {v["id"] for v in visible} == {cold.A, cold.ident(710)}
        history = get("/api/v1/history")["items"]
        assert len(history) == 1 and history[0]["assetId"] == cold.A and history[0]["fileName"] == "second.png"
        new_digest = native_export(get, merged)
    assert new_digest != baseline
    restored = base/"replay"
    replay_stage, _ = restore_for_review(merged, restored)
    # 记录型备份只带数据库记录，不含原件：真实恢复是“恢复记录 + 操作者已有的媒体根”，
    # 合并工具按目录里的哈希/大小/时间核对原件。这里把合并后的媒体目录原样放回。
    shutil.copytree(final/"libraries", replay_stage/"libraries", dirs_exist_ok=True, copy_function=shutil.copy2)
    command("apply", restored, merged, success=False, **{"catalog-sha256": accepted_catalogs[parent]})
    replay = command("apply", restored, merged)
    assert replay["alreadyApplied"] and replay["baselineSHA256"] == baseline
    assert command("apply", restored, merged) == replay
    with serving(server, restored/"data") as get:
        asset = get("/api/v1/assets/"+cold.A)
        assert asset["likeCount"] == 2 and get(asset["contentUrl"], raw=True) == previous
    assert cold.source_state(source) == before and cold.digest(original) == baseline
    return {"sourceCommit": os.environ["GITHUB_SHA"], "runner": os.environ["RUNNER_OS"],
            "separateBaselineAssets": 4, "explicitMembers": 2, "donors": 1, "sourcePosts": 2,
            "unselectedEqualFileIdentityRetained": True, "sealedPreimagesReadBack": True,
            "normalAPIContentAndHistory": True, "nativeBackupRestoreReplay": True,
            "resumedBothHandoffWindows": True,
            "refusals": ["started-service", "different-restore-ID", "wrong-archive-digest", "wrong-plan-digest",
                         "missing-catalog-digest", "changed-history-only", "different-replay-catalog",
                         "storage-reserve", "wrong-group-digest", "occupied-file", "invalid-relative-path",
                         "existing-final", "unlisted-file", "hard-link", "changed-bytes", "changed-mtime"]}


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    for name in ["migrations", "bin", "fixture-bin", "receipt"]:
        parser.add_argument("--"+name, type=Path, required=True)
    arguments = parser.parse_args()
    with tempfile.TemporaryDirectory(prefix="qimeng-selection-synthetic-") as tmp:
        result = run(arguments, Path(tmp))
    write_json(arguments.receipt, result)
