"""Cloud packaging gate: client removal and native ABI completeness."""
import os
import sys
import zipfile

if os.environ.get("CI") != "true" or len(sys.argv) != 3 or sys.argv[1] not in ("client", "embedded"):
    raise SystemExit("Provide the cloud build variant and its APK")
programs = {"libqimeng.so", "libqimeng_runner.so", "libffmpeg_cli.so", "libffprobe_cli.so"}
with zipfile.ZipFile(sys.argv[2]) as apk:
    names = set(apk.namelist())
    present = {name for name in names if name.rsplit("/", 1)[-1] in programs}
    if sys.argv[1] == "client":
        if present:
            raise SystemExit("Client-only APK unexpectedly includes local runtime executables")
    else:
        expected = {f"lib/{abi}/{name}" for abi in ("arm64-v8a", "x86_64") for name in programs}
        if present != expected:
            raise SystemExit("Embedded APK is missing an actual-ABI native executable")
print(f"Verified {sys.argv[1]} APK composition")
