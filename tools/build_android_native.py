"""Cloud-only, source-built Android executables. Never substitutes another ABI.

The media cache contains only reproducible third-party outputs and their sources.
The application server and supervisor are rebuilt from the current commit every run.
"""
from __future__ import annotations

import hashlib
import json
import os
from pathlib import Path
import shutil
import struct
import subprocess
import sys


ROOT = Path(__file__).resolve().parent.parent
LOCK_PATH = ROOT / "tools/android-native.lock.json"
LOCK = json.loads(LOCK_PATH.read_text(encoding="utf-8"))
ABIS = {
    "arm64-v8a": ("aarch64-linux-android", "aarch64", "arm64", 183),
    "x86_64": ("x86_64-linux-android", "x86_64", "amd64", 62),
}


def run(args: list[str | Path], *, cwd: Path, env: dict | None = None) -> None:
    subprocess.run([str(arg) for arg in args], cwd=cwd, env=env, check=True)


def output(args: list[str | Path], *, cwd: Path) -> str:
    return subprocess.check_output([str(arg) for arg in args], cwd=cwd, text=True).strip()


def sha256(path: Path) -> str:
    with path.open("rb") as stream:
        return hashlib.file_digest(stream, "sha256").hexdigest()


def source(name: str, work: Path, package: Path) -> Path:
    entry = LOCK[name]
    dest = work / name
    dest.mkdir()
    run(["git", "init", "--quiet"], cwd=dest)
    run(["git", "remote", "add", "origin", entry["repository"]], cwd=dest)
    run(["git", "-c", "advice.detachedHead=false", "fetch", "--depth=1", "origin", entry["commit"]], cwd=dest)
    run(["git", "checkout", "--detach", "--quiet", "FETCH_HEAD"], cwd=dest)
    if output(["git", "rev-parse", "HEAD"], cwd=dest) != entry["commit"]:
        raise RuntimeError(f"Source identity mismatch: {name}")
    # Source is distributed with the executable; no patching or prebuilt binaries.
    archive = package / f"{name}-{entry['version']}-source.tar.gz"
    run(["git", "archive", "--format=tar.gz", f"--prefix={name}-{entry['version']}/", f"--output={archive}", "HEAD"], cwd=dest)
    return dest


def verify_elf(path: Path, machine: int) -> dict:
    # Do not invoke target binaries on the build host. Verify ELF64/LE/PIE, ABI,
    # interpreter and every PT_LOAD alignment before packaging for the emulator.
    with path.open("rb") as stream:
        header = stream.read(64)
        if header[:7] != b"\x7fELF\x02\x01\x01":
            raise RuntimeError(f"Expected ELF64 little-endian: {path.name}")
        kind, actual_machine = struct.unpack_from("<HH", header, 16)
        if kind != 3 or actual_machine != machine:
            raise RuntimeError(f"PIE/ABI mismatch: {path.name}")
        phoff = struct.unpack_from("<Q", header, 32)[0]
        phsize, phcount = struct.unpack_from("<HH", header, 54)
        if phsize < 56 or not 1 <= phcount <= 128:
            raise RuntimeError(f"Invalid program header table: {path.name}")
        loads, interpreter = [], None
        for index in range(phcount):
            stream.seek(phoff + index * phsize)
            segment = stream.read(phsize)
            segment_type = struct.unpack_from("<I", segment)[0]
            offset, address, _, size, _, alignment = struct.unpack_from("<QQQQQQ", segment, 8)
            if segment_type == 1:
                if alignment < LOCK["pageAlignment"] or (address - offset) % LOCK["pageAlignment"]:
                    raise RuntimeError(f"Unaligned PT_LOAD: {path.name}")
                loads.append(alignment)
            elif segment_type == 3:
                if size > 256:
                    raise RuntimeError(f"Invalid interpreter: {path.name}")
                stream.seek(offset)
                interpreter = stream.read(size).rstrip(b"\0").decode("ascii")
        if not loads or interpreter != "/system/bin/linker64":
            raise RuntimeError(f"Expected Android executable interpreter: {path.name}")
    return {"name": path.name, "sha256": sha256(path), "bytes": path.stat().st_size,
            "elfMachine": machine, "loadAlignment": min(loads)}


def media_tools(abi: str, work: Path, compiler: Path, toolchain: Path, cpu: str, flags: str) -> Path:
    cache = ROOT / "build/android-media-cache" / abi
    stamp = cache / "media.json"
    key = hashlib.sha256(LOCK_PATH.read_bytes() + Path(__file__).read_bytes()).hexdigest()
    if stamp.exists():
        record = json.loads(stamp.read_text(encoding="utf-8"))
        if record["buildKey"] != key:
            raise RuntimeError("Unexpected media cache key; use a clean CI cache")
        for item in record["files"]:
            if sha256(cache / item["name"]) != item["sha256"]:
                raise RuntimeError("Cached media output hash mismatch")
        return cache
    cache.mkdir(parents=True, exist_ok=True)
    dav1d = source("dav1d", work, cache)
    ffmpeg = source("ffmpeg", work, cache)
    prefix = work / "media-prefix"
    cross = work / "android.ini"
    cross.write_text(f"""[binaries]
c = '{compiler}'
ar = '{toolchain / 'llvm-ar'}'
strip = '{toolchain / 'llvm-strip'}'
pkg-config = 'pkg-config'
[host_machine]
system = 'android'
cpu_family = '{cpu}'
cpu = '{cpu}'
endian = 'little'
[built-in options]
c_args = ['-fPIC', '-ffunction-sections', '-fdata-sections']
c_link_args = ['-Wl,-z,max-page-size=16384']
[properties]
needs_exe_wrapper = true
""", encoding="utf-8")
    run(["meson", "setup", work / "dav1d-build", dav1d, "--cross-file", cross,
         "--prefix", prefix, "--libdir=lib", "--default-library=static", "--buildtype=release",
         "-Denable_tools=false", "-Denable_tests=false", "-Denable_examples=false"], cwd=work)
    run(["meson", "compile", "-C", work / "dav1d-build", "-j", str(os.cpu_count() or 2)], cwd=work)
    run(["meson", "install", "-C", work / "dav1d-build"], cwd=work)
    env = os.environ | {"PKG_CONFIG_LIBDIR": str(prefix / "lib/pkgconfig"), "PKG_CONFIG_PATH": ""}
    run([ffmpeg / "configure", "--target-os=android", "--enable-cross-compile", f"--arch={cpu}",
         f"--cc={compiler}", f"--ar={toolchain / 'llvm-ar'}", f"--nm={toolchain / 'llvm-nm'}",
         f"--ranlib={toolchain / 'llvm-ranlib'}", f"--strip={toolchain / 'llvm-strip'}",
         "--enable-static", "--disable-shared", "--enable-pic", "--enable-small",
         "--disable-autodetect", "--disable-network", "--disable-gpl", "--disable-nonfree",
         "--disable-version3", "--disable-doc", "--disable-ffplay", "--disable-avdevice",
         "--disable-encoders", "--enable-encoder=mjpeg", "--disable-muxers", "--enable-muxer=image2pipe",
         "--disable-protocols", "--enable-protocol=file,pipe", "--disable-hwaccels",
         "--enable-libdav1d", "--pkg-config=pkg-config", "--pkg-config-flags=--static",
         "--extra-cflags=-fPIE -ffunction-sections -fdata-sections",
         f"--extra-ldflags=-pie {flags} -Wl,--gc-sections"], cwd=ffmpeg, env=env)
    run(["make", "-j", str(os.cpu_count() or 2), "ffmpeg", "ffprobe"], cwd=ffmpeg, env=env)
    for program in ("ffmpeg", "ffprobe"):
        target = cache / f"lib{program}_cli.so"
        shutil.copy2(ffmpeg / program, target)
        run([toolchain / "llvm-strip", "--strip-debug", target], cwd=work)
    shutil.copy2(ffmpeg / "COPYING.LGPLv2.1", cache / "FFmpeg-LICENSE.txt")
    shutil.copy2(dav1d / "COPYING", cache / "dav1d-LICENSE.txt")
    # Record the exact configure choices for relinking/rebuilding the separate CLI.
    shutil.copy2(ffmpeg / "ffbuild/config.mak", cache / "ffmpeg-config.mak")
    stamp.write_text(json.dumps({"buildKey": key, "files": [
        {"name": p.name, "sha256": sha256(p)} for p in sorted(cache.iterdir()) if p.is_file()
    ]}, indent=2) + "\n", encoding="utf-8")
    return cache


def main() -> None:
    if os.environ.get("CI") != "true" or len(sys.argv) != 2 or sys.argv[1] not in ABIS:
        raise SystemExit("Native compilation requires the cloud runner and an explicit supported ABI")
    abi = sys.argv[1]
    triple, cpu, goarch, machine = ABIS[abi]
    sdk = Path(os.environ["ANDROID_SDK_ROOT"])
    ndk = sdk / "ndk" / LOCK["ndk"]
    toolchain = ndk / "toolchains/llvm/prebuilt/linux-x86_64/bin"
    compiler = toolchain / f"{triple}{LOCK['androidApi']}-clang"
    if not compiler.is_file():
        raise SystemExit("Pinned NDK compiler is missing")
    work = Path(os.environ["RUNNER_TEMP"]) / f"qimeng-native-{abi}"
    work.mkdir()  # Fail on reused build directories; never recursively erase a path.
    dest = ROOT / "build/android-native" / abi
    dest.mkdir(parents=True, exist_ok=True)
    flags = "-Wl,-z,max-page-size=16384 -Wl,-z,common-page-size=16384"
    media = media_tools(abi, work, compiler, toolchain, cpu, flags)
    for name in ("libffmpeg_cli.so", "libffprobe_cli.so"):
        shutil.copy2(media / name, dest / name)
    run([compiler, "-std=c11", "-D_GNU_SOURCE", "-O2", "-fPIE", "-pie", "-Wall", "-Wextra", "-Werror",
         *flags.split(), ROOT / "android/embedded/src/main/cpp/runner.c", "-o", dest / "libqimeng_runner.so"], cwd=work)
    env = os.environ | {"GOOS": "android", "GOARCH": goarch, "CGO_ENABLED": "1", "CC": str(compiler),
                        "CGO_CFLAGS": "-fPIE", "CGO_LDFLAGS": flags}
    run(["go", "build", "-trimpath", "-buildmode=pie",
         "-ldflags", f"-s -w -linkmode=external -extldflags '{flags}'", "-o", dest / "libqimeng.so", "./cmd/qimeng"], cwd=ROOT / "server", env=env)
    records = [verify_elf(path, machine) for path in sorted(dest.glob("*.so"))]
    for name in ("FFmpeg-LICENSE.txt", "dav1d-LICENSE.txt", "ffmpeg-config.mak"):
        shutil.copy2(media / name, dest / name)
    for archive in media.glob("*-source.tar.gz"):
        shutil.copy2(archive, dest / archive.name)
    manifest = {"repositoryCommit": os.environ["GITHUB_SHA"], "abi": abi, "sourceLock": LOCK,
                "go": output(["go", "version"], cwd=ROOT), "files": records}
    (dest / "manifest.json").write_text(json.dumps(manifest, indent=2) + "\n", encoding="utf-8")


if __name__ == "__main__":
    main()
