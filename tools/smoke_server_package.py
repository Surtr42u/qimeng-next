"""Exercise the distributed ZIP and its own launcher on a disposable native CI runner."""
from __future__ import annotations

import argparse
import hashlib
import json
import os
from pathlib import Path
import platform
import secrets
import signal
import socket
import subprocess
import tempfile
import time
import urllib.request
import zipfile

ROOT = Path(__file__).resolve().parents[1]


def digest(path: Path) -> str:
    with path.open("rb") as stream:
        return hashlib.file_digest(stream, "sha256").hexdigest()


def unpack(archive: Path, temporary: Path, target: str) -> Path:
    expected = Path(str(archive) + ".sha256").read_text("ascii").split()[0]
    if digest(archive) != expected:
        raise ValueError("ZIP checksum mismatch")
    with zipfile.ZipFile(archive) as source:
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


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--target", required=True)
    parser.add_argument("--packages", type=Path, required=True)
    parser.add_argument("--compose", action="store_true")
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
        with socket.socket() as probe:
            probe.bind(("127.0.0.1", 0))
            port = probe.getsockname()[1]
        if port in (8420, 8421):
            raise RuntimeError("Refusing a default service port in CI.")
        mode = "compose" if args.compose else "native"
        env = {**os.environ, "QIMENG_PACKAGE_STATE": str(work / "synthetic-state"),
               "QIMENG_PACKAGE_PASSWORD_FILE": str(secret), "QIMENG_PACKAGE_PORT": str(port),
               "QIMENG_PACKAGE_TEST_MODE": mode, "QIMENG_PACKAGE_TARGET": args.target,
               "QIMENG_PACKAGE_APP_DIR": str(app), "QIMENG_PACKAGE_TEST_DIR": str(work / "restore-check"),
               "QIMENG_PACKAGE_EVIDENCE_DIR": str(packages)}
        process = None
        try:
            if args.compose:
                subprocess.run(["bash", str(app / "start.sh"), "nas"], env=env, check=True, timeout=180)
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
            subprocess.run(["node", "tests/packaged-home.mjs"], cwd=ROOT / "web", env=env, check=True, timeout=180)
        finally:
            if args.compose:
                subprocess.run(["bash", str(app / "start.sh"), "nas-stop"], env=env, check=True, timeout=60)
            elif process is not None:
                stop(process)


if __name__ == "__main__":
    main()
