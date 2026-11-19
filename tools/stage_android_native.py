"""Verify the current cloud native artifact before placing executables in jniLibs."""
import hashlib
import json
import os
from pathlib import Path
import shutil
import sys

import preview_notices

if os.environ.get("CI") != "true":
    raise SystemExit("This artifact staging step belongs to cloud CI")
root = Path(__file__).resolve().parent.parent
artifact = Path(sys.argv[1])
expected = {"libqimeng.so", "libqimeng_runner.so", "libffmpeg_cli.so", "libffprobe_cli.so"}
assets = root / "android/embedded/src/main/assets"
for abi in ("arm64-v8a", "x86_64"):
    source = artifact / abi
    manifest = json.loads((source / "manifest.json").read_text(encoding="utf-8"))
    if manifest["repositoryCommit"] != os.environ["GITHUB_SHA"] or manifest["abi"] != abi:
        raise SystemExit("Native output belongs to a different commit or ABI")
    if {entry["name"] for entry in manifest["files"]} != expected:
        raise SystemExit("Native artifact is incomplete")
    if manifest["previewDecoderNotices"] != preview_notices.verify(source):
        raise SystemExit(f"Decoder notice hashes differ from the {abi} native manifest")
    target = root / "android/embedded/src/main/jniLibs" / abi
    target.mkdir(parents=True, exist_ok=True)
    for entry in manifest["files"]:
        path = source / entry["name"]
        with path.open("rb") as stream:
            digest = hashlib.file_digest(stream, "sha256").hexdigest()
        if path.stat().st_size != entry["bytes"] or digest != entry["sha256"]:
            raise SystemExit("Native artifact integrity check failed")
        shutil.copy2(path, target / entry["name"])
    notices = assets / "third-party" / abi
    notices.mkdir(parents=True, exist_ok=True)
    for name in ("FFmpeg-LICENSE.txt", "dav1d-LICENSE.txt", "go-sqlite3-LICENSE.txt", "manifest.json"):
        shutil.copy2(source / name, notices / name)
preview_notices.copy_to(assets)
