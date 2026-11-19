"""Exercise the distributed ZIP and its own launcher on a disposable native CI runner."""
from __future__ import annotations

import argparse
from contextlib import closing
import hashlib
import json
import os
from pathlib import Path
import platform
import secrets
import signal
import socket
import sqlite3
import subprocess
import tempfile
import time
import urllib.request
import zipfile

import preview_notices

ROOT = Path(__file__).resolve().parents[1]
MIGRATIONS = ROOT / "server/internal/store/migrations"


def digest(path: Path) -> str:
    with path.open("rb") as stream:
        return hashlib.file_digest(stream, "sha256").hexdigest()


def unpack(archive: Path, temporary: Path, target: str) -> Path:
    expected = Path(str(archive) + ".sha256").read_text("ascii").split()[0]
    if digest(archive) != expected:
        raise ValueError("ZIP checksum mismatch")
    with zipfile.ZipFile(archive) as source:
        preview_notices.verify(zipfile.Path(source) / archive.stem)
        source.extractall(temporary)
    app = temporary / archive.stem
    source = json.loads((app / "SOURCE.json").read_text("utf-8"))
    if (source["sourceCommit"], source["target"]) != (os.environ["GITHUB_SHA"], target):
        raise ValueError("Mismatched source or platform")
    verified = {"SHA256SUMS"}
    for line in (app / "SHA256SUMS").read_text("utf-8").splitlines():
        expected, relative = line.split("  ", 1)
        if digest(app / relative) != expected:
            raise ValueError("Packaged file checksum mismatch: " + relative)
        verified.add(relative)
    if verified != {p.relative_to(app).as_posix() for p in app.rglob("*") if p.is_file()}:
        raise ValueError("Package contents differ from SHA256SUMS")
    if os.name != "nt":
        for name in ["qimeng-next", "qimeng-backup", "start.sh", "nas-entry.sh"]:
            (app / name).chmod(0o755)
    return app


def stop(process: subprocess.Popen) -> None:
    if process.poll() is not None:
        return
    try:
        if os.name == "nt":
            process.send_signal(signal.CTRL_BREAK_EVENT)
        else:
            process.terminate()
        process.wait(timeout=15)
    except (subprocess.TimeoutExpired, OSError):
        if os.name == "nt":
            subprocess.run(["taskkill", "/PID", str(process.pid), "/T", "/F"], capture_output=True)
        else:
            process.kill()
        process.wait(timeout=10)


def check_occupied_upgrade(app: Path, state: Path, port: int) -> dict:
    data = state / "data"
    data.mkdir(parents=True)
    (state / ".qimeng-next-package").write_text("qimeng-next-package-v1\n", "ascii")
    catalog = data / "qimeng.sqlite"
    # The first Next build (e2bd1cf) shipped these four migrations.
    historical = ["0001_initial.sql", "0002_analytics.sql", "0003_management.sql", "0005_author_archives.sql"]
    ledger = []
    instance = secrets.token_hex(16)
    author = {"id": secrets.token_hex(16), "kind": "regular", "name": "Synthetic prior author", "revision": 3,
              "createdAt": "2026-01-01T00:00:00Z", "updatedAt": "2026-01-02T00:00:00Z"}
    with closing(sqlite3.connect(catalog)) as db:
        db.execute("PRAGMA journal_mode=WAL")
        db.execute("PRAGMA foreign_keys=ON")
        db.execute("CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY,name TEXT NOT NULL,sha256 TEXT NOT NULL)")
        for name in historical:
            path = MIGRATIONS / name
            db.executescript(path.read_text("utf-8"))
            entry = (int(name.split("_", 1)[0]), name, digest(path))
            db.execute("INSERT INTO schema_migrations VALUES(?,?,?)", entry)
            ledger.append(entry)
        db.execute("INSERT INTO settings VALUES('instance_id',?)", (instance,))
        db.execute("INSERT INTO authors(id,name,data) VALUES(?,?,?)", (author["id"], author["name"], json.dumps(author)))
        db.commit()
    original_hash = digest(catalog)

    receipt = state / "attempt.json"
    binary = app / ("qimeng-next.exe" if os.name == "nt" else "qimeng-next")
    env = {key: value for key, value in os.environ.items() if not key.startswith("QIMENG_")}
    env.update(QIMENG_DATA_DIR=str(data), QIMENG_LISTEN=f"127.0.0.1:{port}", QIMENG_ADMIN_TOKEN=secrets.token_urlsafe(40),
               QIMENG_STARTUP_RECEIPT=str(receipt), QIMENG_LAUNCH_ID=secrets.token_hex(16))
    attempt = subprocess.run([str(binary)], env=env, capture_output=True, timeout=45)
    if attempt.returncode != 1 or b"cannot bind TCP listener" not in attempt.stderr:
        raise RuntimeError("The packaged service did not report the occupied listener.")
    if receipt.exists() or (data / "authority.sqlite").exists() or digest(catalog) != original_hash:
        raise RuntimeError("The failed start published a receipt or touched the historical database.")
    with closing(sqlite3.connect(catalog.as_uri() + "?mode=ro", uri=True)) as db:
        rows = db.execute("SELECT version,name,sha256 FROM schema_migrations ORDER BY version").fetchall()
        record = db.execute("SELECT data FROM authors WHERE id=?", (author["id"],)).fetchone()
        if rows != ledger or record is None or json.loads(record[0]) != author:
            raise RuntimeError("Cannot reopen the historical ledger and author after the occupied-port failure.")
    return {"instanceId": instance, "author": author, "schemaVersionBefore": ledger[-1][0]}


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--target", required=True)
    parser.add_argument("--packages", type=Path, required=True)
    parser.add_argument("--compose", action="store_true")
    parser.add_argument("--media-tools", action="store_true")
    args = parser.parse_args()
    if os.environ.get("GITHUB_ACTIONS") != "true":
        raise SystemExit("The package smoke test is cloud-only.")
    actual = platform.machine().lower()
    expected = "arm64" if args.target.endswith("arm64") else "amd64"
    aliases = {"aarch64": "arm64", "x86_64": "amd64"}
    if aliases.get(actual, actual) != expected:
        raise SystemExit("A native runner is required for this smoke test.")
    packages = args.packages.resolve()
    archives = list(packages.glob("qimeng-next-" + args.target + "-*.zip"))
    if len(archives) != 1:
        raise RuntimeError("Expected exactly one matching server ZIP.")
    with tempfile.TemporaryDirectory() as temporary:
        work = Path(temporary)
        app = unpack(archives[0], work / "extracted package", args.target)
        secret = work / "fixture-secret"
        secret.write_text(secrets.token_urlsafe(40), encoding="ascii")
        if os.name != "nt":
            secret.chmod(0o600)
        state = work / "synthetic-state"
        prior = None
        with socket.socket() as probe:
            probe.bind(("127.0.0.1", 0))
            port = probe.getsockname()[1]
            if port in (8420, 8421):
                raise RuntimeError("Refusing a default service port in CI.")
            if not args.compose:
                probe.listen()
                prior = check_occupied_upgrade(app, state, port)
        if args.media_tools and not args.compose:
            raise ValueError("Media-tools smoke uses the packaged NAS image.")
        mode = "compose-media" if args.media_tools else "compose" if args.compose else "native"
        env = {**os.environ, "QIMENG_PACKAGE_STATE": str(state),
               "QIMENG_PACKAGE_PASSWORD_FILE": str(secret), "QIMENG_PACKAGE_PORT": str(port),
               "QIMENG_PACKAGE_TEST_MODE": mode, "QIMENG_PACKAGE_TARGET": args.target,
               "QIMENG_PACKAGE_APP_DIR": str(app), "QIMENG_PACKAGE_TEST_DIR": str(work / "restore-check"),
               "QIMENG_PACKAGE_EVIDENCE_DIR": str(packages)}
        process = None
        try:
            if args.compose:
                subprocess.run(["bash", str(app / "start.sh"), "nas-media" if args.media_tools else "nas"], env=env, check=True, timeout=480)
                if args.media_tools:
                    video = subprocess.check_output(["docker", "run", "--rm", "qimeng-next-local-media:alpine3.24.4-ffmpeg8.1.3-r0",
                        "ffmpeg", "-v", "error", "-nostdin", "-f", "lavfi", "-i", "testsrc2=size=160x120:rate=10", "-t", "3",
                        "-c:v", "mpeg4", "-threads", "1", "-movflags", "frag_keyframe+empty_moov", "-f", "mp4", "pipe:1"], timeout=60)
                    fixture = work / "synthetic-preview.mp4"
                    fixture.write_bytes(video)
                    env["QIMENG_PACKAGE_VIDEO"] = str(fixture)
            else:
                command = ["powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "RemoteSigned", "-File", str(app / "start.ps1"), "-NoBrowser"] if os.name == "nt" else ["bash", str(app / "start.sh")]
                process = subprocess.Popen(command, env=env, creationflags=subprocess.CREATE_NEW_PROCESS_GROUP if os.name == "nt" else 0)
            origin = f"http://127.0.0.1:{port}"
            for _ in range(180):
                if process is not None and process.poll() is not None:
                    raise RuntimeError("The package launcher exited before readiness.")
                try:
                    with urllib.request.urlopen(origin + "/healthz", timeout=2) as ready:
                        if ready.status == 200:
                            break
                except OSError:
                    pass
                time.sleep(0.5)
            else:
                raise TimeoutError("The packaged service did not become ready.")
            if prior is not None:
                with closing(sqlite3.connect((state / "data/qimeng.sqlite").as_uri() + "?mode=ro", uri=True)) as db:
                    version = db.execute("SELECT MAX(version) FROM schema_migrations").fetchone()[0]
                if version != max(int(path.name.split("_", 1)[0]) for path in MIGRATIONS.glob("*.sql")):
                    raise RuntimeError("The successful retry did not apply the current schema.")
                prior["schemaVersionAfter"] = version
                env["QIMENG_PACKAGE_PRIOR_RECORDS"] = json.dumps(prior)
            subprocess.run(["node", "tests/packaged-home.mjs"], cwd=ROOT / "web", env=env, check=True, timeout=180)
        finally:
            if args.compose:
                subprocess.run(["bash", str(app / "start.sh"), "nas-stop"], env=env, check=True, timeout=60)
            elif process is not None:
                stop(process)


if __name__ == "__main__":
    main()
