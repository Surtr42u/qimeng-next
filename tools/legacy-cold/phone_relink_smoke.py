"""Run the shipped offline phone command against existing synthetic shared files."""
import argparse
from contextlib import closing, contextmanager
from copy import deepcopy
import json
import os
from pathlib import Path
import secrets
import shutil
import socket
import sqlite3
import subprocess
import tempfile
import time
import urllib.error
import urllib.request

import smoke as cold


def write_json(name, value):
    name.write_text(json.dumps(value, ensure_ascii=False), encoding="utf-8")


def read_rows(db, query, args=()):
    with closing(sqlite3.connect(db.as_uri() + "?mode=ro&immutable=1", uri=True)) as con:
        return con.execute(query, args).fetchall()


@contextmanager
def serving(binary, directory, phone):
    directory.mkdir(exist_ok=True)
    with socket.socket() as sock:
        sock.bind(("127.0.0.1", 0))
        port = sock.getsockname()[1]
    bootstrap = secrets.token_urlsafe(40)
    env = {key: value for key, value in os.environ.items() if not key.startswith("QIMENG_")}
    env.update(QIMENG_DATA_DIR=str(directory), QIMENG_LISTEN=f"127.0.0.1:{port}",
               QIMENG_ADMIN_TOKEN=bootstrap, QIMENG_STORAGE_ROOTS=json.dumps({"phone": str(phone)}))

    def call(route, data=None, method=None, token=None, replay=False, raw=False):
        headers = {"Authorization": "Bearer " + (token or bootstrap)}
        if data is not None:
            headers["Content-Type"] = "application/json"
        if replay:
            headers["Idempotency-Key"] = "relink-fixture-" + secrets.token_hex(8)
        req = urllib.request.Request(f"http://127.0.0.1:{port}" + route, headers=headers, method=method,
                                     data=None if data is None else json.dumps(data).encode())
        with urllib.request.urlopen(req, timeout=12) as response:
            body = response.read()
        return body if raw else json.loads(body)

    with subprocess.Popen([str(binary)], cwd=directory.parent, env=env,
                          stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL) as proc:
        try:
            deadline = time.monotonic() + 45
            while True:
                assert proc.poll() is None, "ordinary service exited"
                try:
                    call("/healthz", raw=True)
                    break
                except urllib.error.URLError:
                    if time.monotonic() >= deadline:
                        raise RuntimeError("ordinary service readiness timeout")
                    time.sleep(.12)
            yield call, bootstrap
        finally:
            proc.terminate()
            proc.wait(timeout=20)


def checkpoint(call, archive):
    item = call("/api/v1/backups", {}, replay=True)
    route = "/api/v1/backups/" + item["id"]
    call(route + "/export", {}, replay=True)
    deadline = time.monotonic() + 60
    while True:
        item = call(route)
        if item["exportState"] == "completed":
            break
        if item["exportState"] in {"failed", "interrupted"} or time.monotonic() >= deadline:
            raise RuntimeError("synthetic native checkpoint unavailable")
        time.sleep(.12)
    archive.write_bytes(call(route + "/archive", raw=True))
    assert cold.digest(archive) == item["archiveHash"]


def restore(backup, archive, destination):
    args = ["--archive", archive, "--sha256", cold.digest(archive)]
    cold.command([backup, "verify", *args])
    return json.loads(cold.command([backup, "restore", *args, "--destination", destination]).stdout)


def census_entry(phone, path):
    st = path.stat()
    return {"phoneRelativePath": path.relative_to(phone).as_posix(), "size": st.st_size,
            "sha256": cold.digest(path), "device": str(st.st_dev), "inode": str(st.st_ino),
            "mtimeUnixNs": st.st_mtime_ns, "ctimeUnixNs": st.st_ctime_ns}


def run(options, base):
    backup = options.bin.resolve() / ("qimeng-backup.exe" if os.name == "nt" else "qimeng-backup")
    convert = backup.with_name(backup.name.replace("backup", "legacy-convert"))
    server = options.server.resolve() / ("qimeng-next.exe" if os.name == "nt" else "qimeng-next")
    _, old_db, media, trash, discarded, _, roots = cold.fixture(options.migrations, base)
    cold.command([convert, "--source-db", old_db, "--roots", roots, "--trash-root", trash,
                  "--source-timezone", "Asia/Shanghai", "--output", base / "converted"])
    phone = base / "synthetic-phone"
    phone.mkdir()
    initial = base / "native-source"
    restore(backup, base / "converted" / "library.qnext", initial)
    archive = base / "native.qnext"
    with serving(server, initial, phone) as (call, bootstrap):
        source_login = call("/api/v1/sessions", {"label": "synthetic-source-only-session", "token": bootstrap})
        call("/api/v1/settings", {"expectedRevision": 1, "scan": {"workers": 1, "thumbEdge": 640},
                                  "upload": {"autoAccept": True}}, method="PUT", replay=True)
        checkpoint(call, archive)
    fresh = base / "verified-restore"
    restored = restore(backup, archive, fresh)
    receipt_file = base / "restored.json"
    write_json(receipt_file, restored)
    source_db = fresh / "qimeng.sqlite"
    target_db = base / "target-cold.sqlite"
    with serving(server, base / "target-instance", phone) as (call, bootstrap):
        original_id = call("/api/v1/instance")["id"]
        phone_login = call("/api/v1/sessions", {"label": "synthetic-target-session", "token": bootstrap})
        with closing(sqlite3.connect((base / "target-instance" / "qimeng.sqlite").as_uri() + "?mode=ro", uri=True)) as src, closing(sqlite3.connect(target_db)) as dst:
            src.backup(dst)
    with closing(sqlite3.connect(target_db)) as con:
        con.execute("INSERT INTO settings VALUES(?,?)", ("acquisition_vocabulary", '{"revision":2,"sources":["synthetic-target"]}'))
        con.commit()
    root = "Pictures/Fixture"
    records = [(cold.A, "first.png", "moved/first.png", media / "first.png"),
               (cold.B, "second.png", "first.png", media / "second.png"),
               (cold.T, "folder/recycled.png", f".qimeng-internal/trash/{cold.T}/recycled.png", discarded)]
    plan = {"format": "qimeng-phone-relink-v1", "libraries": [{"libraryId": cold.LIB, "rootRelativePath": root}], "bindings": []}
    inventory = {"format": "qimeng-phone-files-v1", "storageId": "phone", "files": []}
    for asset, source, physical, origin in records:
        path = phone / root / physical
        path.parent.mkdir(parents=True, exist_ok=True)
        shutil.copyfile(origin, path)
        plan["bindings"].append({"assetId": asset, "libraryId": cold.LIB, "sourceRelativePath": source, "phoneRelativePath": root + "/" + physical})
        inventory["files"].append(census_entry(phone, path))
    pfile, ifile = base / "plan.json", base / "inventory.json"
    write_json(pfile, plan)
    write_json(ifile, inventory)
    before = (cold.digest(source_db), cold.digest(target_db))
    dest = base / "prepared"

    def invoke(target=target_db, source=source_db, destination=dest, success=True, budget=0):
        return cold.command([backup, "phone-relink", "--archive", archive, "--sha256", cold.digest(archive),
                             "--restored-db", source, "--restored-sha256", cold.digest(source), "--restore-receipt", receipt_file,
                             "--target-db", target, "--target-sha256", cold.digest(target),
                             "--plan", pfile, "--plan-sha256", cold.digest(pfile), "--inventory", ifile,
                             "--inventory-sha256", cold.digest(ifile), "--destination", destination,
                             "--trash-budget-bytes", str(budget)], success)

    receipt = json.loads(invoke().stdout)
    assert receipt == cold.read_json(dest / "phone-relink.json")
    assert (receipt["liveOriginals"], receipt["trashOriginals"], receipt["libraries"]) == (2, 1, 1)
    assert receipt["targetInstanceId"] == original_id != receipt["restoredInstanceId"]
    assert original_id != receipt["sourceInstanceId"]
    assert sorted(p.name for p in dest.iterdir()) == ["phone-relink.json", "qimeng.sqlite"]
    out = dest / "qimeng.sqlite"
    assert receipt["outputCatalogSHA256"] == cold.digest(out)
    for table in ("asset_content_versions", "events", "likes", "migration_media_aliases", "migration_relation_origins"):
        sql = f"SELECT * FROM {table} ORDER BY 1,2"
        assert read_rows(source_db, sql) == read_rows(out, sql), table
    assert read_rows(out, "SELECT * FROM credentials ORDER BY id") == read_rows(target_db, "SELECT * FROM credentials ORDER BY id")
    assert source_login["id"].encode() not in out.read_bytes()
    assert read_rows(out, "SELECT value FROM settings WHERE key='acquisition_vocabulary'") == read_rows(target_db, "SELECT value FROM settings WHERE key='acquisition_vocabulary'")
    assert read_rows(out, "SELECT value FROM settings WHERE key='operational_settings'") == read_rows(source_db, "SELECT value FROM settings WHERE key='operational_settings'")
    assert read_rows(out, "SELECT relative_path FROM asset_locations WHERE asset_id=?", (cold.T,)) == [("folder/recycled.png",)]
    previous = cold.digest(out)
    invoke(success=False)
    assert cold.digest(out) == previous
    refused = []

    def refusal(name, reason, **kwargs):
        destination = base / ("refusal-" + name)
        proc = invoke(destination=destination, success=False, **kwargs)
        assert reason.encode() in proc.stderr, f"{name} returned {proc.stderr.decode(errors='replace')[-120:]}"
        assert not destination.exists() and not list(base.glob(".qimeng-phone-relink-*")), name
        refused.append(name)

    for name, amend, why in (
        ("traversal", lambda p, i: i["files"][0].update(phoneRelativePath="../escape.png"), "invalid_phone_file_evidence"),
        ("cross-library", lambda p, i: p["bindings"][0].update(libraryId="other-library"), "source_asset_binding_mismatch"),
        ("outside-root", lambda p, i: p["bindings"][0].update(phoneRelativePath="Other/first.png"), "phone_path_outside_selected_library"),
        ("duplicate-path", lambda p, i: i["files"][1].update(phoneRelativePath=i["files"][0]["phoneRelativePath"]), "duplicate_phone_path_or_file_identity"),
        ("duplicate-inode", lambda p, i: i["files"][1].update(device=i["files"][0]["device"], inode=i["files"][0]["inode"]), "duplicate_phone_path_or_file_identity"),
        ("source-relative-path", lambda p, i: p["bindings"][0].update(sourceRelativePath="different.png"), "source_asset_binding_mismatch"),
        ("wrong-size", lambda p, i: i["files"][0].update(size=1), "missing_or_changed_phone_original"),
        ("wrong-sha", lambda p, i: i["files"][0].update(sha256="0"*64), "missing_or_changed_phone_original"),
        ("missing-trash", lambda p, i: i["files"].pop(), "incomplete_plan_or_phone_inventory"),
    ):
        bad_plan, bad_inventory = deepcopy(plan), deepcopy(inventory)
        amend(bad_plan, bad_inventory)
        write_json(pfile, bad_plan)
        write_json(ifile, bad_inventory)
        refusal(name, why)
    bad_plan, bad_inventory = deepcopy(plan), deepcopy(inventory)
    wrong_trash = root + "/unrelated/recycled.png"
    bad_plan["bindings"][-1]["phoneRelativePath"] = wrong_trash
    bad_inventory["files"][-1]["phoneRelativePath"] = wrong_trash
    write_json(pfile, bad_plan)
    write_json(ifile, bad_inventory)
    refusal("wrong-trash-route", "missing_native_phone_trash_location")
    write_json(pfile, plan)
    write_json(ifile, inventory)
    for name, statement, why in (
        ("activity", "INSERT INTO recommendation_shown VALUES('2024-01-02','old-target',1)", "target_has_business_or_nondefault_state"),
        ("queue", "INSERT INTO batches VALUES('old-target','2024','{}')", "target_has_business_or_nondefault_state"),
        ("author-edit", "UPDATE authors SET data=json_set(data,'$.biography','edit')", "target_default_author_changed"),
        ("conflicting-settings", "INSERT INTO settings VALUES('operational_settings','{}')", "target_setting_conflicts_with_source"),
        ("unknown-schema", "CREATE TABLE non_native_state(id INTEGER)", "incompatible_or_damaged_catalog"),
    ):
        alt = base / (name + ".sqlite")
        shutil.copyfile(target_db, alt)
        with closing(sqlite3.connect(alt)) as con:
            con.execute(statement)
            con.commit()
        refusal(name, why, target=alt)
    changed = base / "different-restore"
    changed.mkdir()
    other = changed / "qimeng.sqlite"
    shutil.copyfile(source_db, other)
    with closing(sqlite3.connect(other)) as con:
        con.execute("DELETE FROM events")
        con.commit()
    refusal("mismatched-restored-business", "catalog_differs_from_verified_restore", source=other)
    wal = Path(str(target_db) + "-wal")
    wal.write_bytes(b"fixture")
    refusal("unsealed-target", "cold_database_has_sidecars")
    wal.unlink()
    assert before == (cold.digest(source_db), cold.digest(target_db))
    assert receipt == cold.read_json(dest / "phone-relink.json")

    with serving(server, dest, phone) as (call, _):
        session = call("/api/v1/sessions/current", token=phone_login["token"])
        assert session["id"] == phone_login["id"]
        assert call("/api/v1/instance")["id"] == original_id
        try:
            call("/api/v1/sessions/current", token=source_login["token"])
        except urllib.error.HTTPError as exc:
            assert exc.code == 401
        else:
            raise AssertionError("source session survived")
        for asset, _, path, original in records[:2]:
            summary = call("/api/v1/assets/" + asset, token=phone_login["token"])
            assert summary["relativePath"] == path and summary["contentVersion"] == 1
            assert call(summary["contentUrl"], token=phone_login["token"], raw=True) == original.read_bytes()
        assert call("/api/v1/history", token=phone_login["token"])["items"]
        checkpoint(call, base / "reexported.qnext")
    final = base / "further-restore"
    restored_again = restore(backup, base / "reexported.qnext", final)
    assert restored_again["assetCount"] == 3
    assert (final / "libraries" / cold.LIB / records[-1][2]).read_bytes() == discarded.read_bytes()
    assert [census_entry(phone, phone / f["phoneRelativePath"]) for f in inventory["files"]] == inventory["files"]
    trash_file = inventory["files"][-1]
    trash_path = phone / trash_file["phoneRelativePath"]
    trash_path.unlink()
    def selected_trash(index):
        selected, observed = deepcopy(plan), deepcopy(inventory)
        item = observed["files"].pop(index)
        selected["restoreTrashAssetIds"] = [selected["bindings"][index]["assetId"]]
        observed["absentTrashPaths"] = [item["phoneRelativePath"]]
        write_json(pfile, selected)
        write_json(ifile, observed)
    selected_trash(0)
    refusal("live-copy-forbidden", "unproven_or_over_budget_trash_copy", budget=1<<20)
    selected_trash(-1)
    refusal("trash-copy-without-budget", "explicit_trash_ids_and_byte_budget_required")
    refusal("trash-copy-over-budget", "unproven_or_over_budget_trash_copy", budget=trash_file["size"]-1)
    native_trash = fresh / "libraries" / cold.LIB / records[-1][2]
    saved = native_trash.with_name("held-fixture-bytes")
    native_trash.rename(saved)
    refusal("missing-restored-trash-payload", "restored_trash_original_unavailable", budget=trash_file["size"])
    saved.rename(native_trash)
    with_payload = base / "with-one-trash-file"
    result = json.loads(invoke(destination=with_payload, budget=trash_file["size"]).stdout)
    staged, = result["stagedTrash"]
    assert staged["assetId"] == cold.T and staged["sha256"] == cold.digest(discarded)
    payload_root = with_payload / "phone-files"
    payloads = [p for p in payload_root.rglob("*") if p.is_file()]
    assert payloads == [payload_root / trash_file["phoneRelativePath"]]
    assert not trash_path.exists()
    with (payloads[0]).open("rb") as src, trash_path.open("xb") as dst:
        shutil.copyfileobj(src, dst)
    assert cold.digest(trash_path) == staged["sha256"]
    return {"sourceCommit": os.environ["GITHUB_SHA"], "runner": os.environ["RUNNER_OS"], "schema": 45,
            "liveOriginalsRead": 2, "nativeTrashReadBack": 1, "distinctSameContentFiles": 2,
            "targetInstanceAndLoginVerified": True, "sourceCredentialExcluded": True, "eventAndOriginRowsPreserved": True,
            "ordinaryServiceContentReadBack": True, "nativeArchiveRoundTrip": True,
            "physicalCensusUnchanged": True, "explicitBoundedTrashCopies": 1, "refusals": ["existing-destination", *refused]}


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    for option in ("migrations", "bin", "server", "receipt"):
        parser.add_argument("--" + option, type=Path, required=True)
    opts = parser.parse_args()
    with tempfile.TemporaryDirectory(prefix="phone-relink-synthetic-") as temporary:
        result = run(opts, Path(temporary))
    write_json(opts.receipt, result)
