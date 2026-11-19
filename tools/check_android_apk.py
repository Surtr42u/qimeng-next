"""Cloud packaging gate for client removal, native ABIs and embedded notices."""
import json
import os
from pathlib import Path
import sys
import zipfile

import preview_notices

if os.environ.get("CI") != "true" or len(sys.argv) != 3 or sys.argv[1] not in ("client", "embedded"):
    raise SystemExit("Provide the cloud build variant and its APK or AAR")
archive = Path(sys.argv[2])
if archive.suffix not in (".apk", ".aar") or (sys.argv[1] == "client" and archive.suffix != ".apk"):
    raise SystemExit("Expected an APK or embedded AAR")
programs = {"libqimeng.so", "libqimeng_runner.so", "libffmpeg_cli.so", "libffprobe_cli.so"}
with zipfile.ZipFile(archive) as package:
    names = set(package.namelist())
    present = {name for name in names if name.rsplit("/", 1)[-1] in programs}
    if sys.argv[1] == "client":
        if present or any(name.startswith(f"assets/{preview_notices.DIRECTORY}/") for name in names):
            raise SystemExit("Client-only APK unexpectedly includes the embedded runtime or its decoder notices")
    else:
        native = "jni" if archive.suffix == ".aar" else "lib"
        abis = ("arm64-v8a", "x86_64")
        expected = {f"{native}/{abi}/{name}" for abi in abis for name in programs}
        if present != expected:
            raise SystemExit("Embedded package is missing an actual-ABI native executable")
        hashes = preview_notices.verify(zipfile.Path(package) / "assets")
        for abi in abis:
            manifest = json.loads(package.read(f"assets/third-party/{abi}/manifest.json"))
            if manifest["previewDecoderNotices"] != hashes:
                raise SystemExit(f"Packaged decoder notices differ from the {abi} manifest")
print(f"Verified {sys.argv[1]} {archive.name} composition")
