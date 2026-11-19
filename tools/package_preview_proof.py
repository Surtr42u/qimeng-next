"""Independent cloud-only source and delivered-byte checks on this proof branch."""
import base64
import hashlib
import io
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tarfile
import tempfile
import urllib.request
import zipfile

ROOT = Path(__file__).resolve().parents[1]
DIR = "third-party/preview-decoders"
OUT = ROOT / "build/preview-proof"
ABI = ("arm64-v8a", "x86_64")
JPEG = "github.com/m8rge/go-scaled-jpeg"
PNG = "github.com/m8rge/go-scaled-png"
IMAGING = "github.com/kovidgoyal/imaging"
PINS = {JPEG: ("v1.2.0", "0725b8baf8893dcf98ec85167077a0a16a9c3aae"),
        PNG: ("v1.0.0", "3888559f36b39ff162168624a4dd9688849cfb7a"),
        IMAGING: ("v1.6.4", "9f2dbdd75748296123dd85ec7ab2509b0037b6a0")}


def sha(data):
    return hashlib.sha256(data).hexdigest()


def download(url):
    with urllib.request.urlopen(url, timeout=40) as stream:
        return stream.read()


def source_module(name):
    version, commit = PINS[name]
    url = f"https://proxy.golang.org/{name}/@v/{version}"
    info = json.loads(download(url + ".info"))
    assert (info["Version"], info["Origin"]["Ref"], info["Origin"]["Hash"]) == (version, "refs/tags/" + version, commit)
    raw = download(url + ".zip")
    with zipfile.ZipFile(io.BytesIO(raw)) as archive:
        entries = {path: archive.read(path) for path in archive.namelist() if not path.endswith("/")}
    lines = "".join(f"{sha(entries[path])}  {path}\n" for path in sorted(entries))
    checksum = "h1:" + base64.b64encode(hashlib.sha256(lines.encode()).digest()).decode()
    prefix = f"{name}@{version}/"
    assert all(path.startswith(prefix) for path in entries)
    if name in (JPEG, PNG):
        assert f"{name} {version} {checksum}" in (ROOT / "server/go.sum").read_text().splitlines()
    return ({path.removeprefix(prefix): data for path, data in entries.items()},
            {"version": version, "commit": commit, "zipSha256": sha(raw), "sum": checksum, "url": url + ".zip"})


def verify_sources():
    jpeg, jpeg_source = source_module(JPEG)
    png, png_source = source_module(PNG)
    img, img_source = source_module(IMAGING)
    assert img_source["version"].encode() in png["go.mod"]
    ijg_url = "https://www.ijg.org/files/jpegsrc.v9f.tar.gz"
    assert ijg_url.encode() in jpeg["idct.go"]
    ijg = download(ijg_url)
    assert sha(ijg) == "04705c110cb2469caa79fb71fba3d7bf834914706e9641a4589485c1f832565b"
    with tarfile.open(fileobj=io.BytesIO(ijg), mode="r:gz") as src:
        ijg_readme = src.extractfile("jpeg-9f/README").read()
    expected = {"LICENSE": jpeg["LICENSE"], "LICENSE.GOLANG": jpeg["LICENSE.GOLANG"],
                "COPYING.IJG": jpeg["COPYING.IJG"], "LICENSE.imaging": img["LICENSE"], "IJG-README.txt": ijg_readme}
    assert all(expected[name] == png[name] for name in ("LICENSE", "LICENSE.GOLANG"))
    directory = ROOT / "deploy" / DIR
    source_notices = (directory / "SOURCE-NOTICES.txt").read_bytes()
    for entry, first, last in ((jpeg["idct.go"], 12, 15), (jpeg["idct_orig.go"], 13, 38),
                               (png["filters.go"], 1, 1), (jpeg["reader.go"], 1, 1),
                               (png["paeth.go"], 1, 1), (jpeg["internal/imageutil/imageutil.go"], 1, 1)):
        assert b"".join(entry.splitlines(keepends=True)[first-1:last]) in source_notices
    expected["SOURCE-NOTICES.txt"] = source_notices
    expected["README.md"] = (directory / "README.md").read_bytes()
    assert b"This software is based in part on the work of the Independent JPEG Group." in expected["README.md"]
    assert set(expected) == {p.name for p in directory.iterdir()}
    for name, data in expected.items():
        assert (directory / name).read_bytes() == data, name
    return {"commit": os.environ["GITHUB_SHA"], "modules": {JPEG: jpeg_source, PNG: png_source, IMAGING: img_source},
            "ijg": {"url": ijg_url, "archiveSha256": sha(ijg), "member": "jpeg-9f/README"},
            "noticeSha256": {name: sha(data) for name, data in expected.items()}}


def synthetic_omissions(hashes):
    with tempfile.TemporaryDirectory() as temp:
        for extension, binaries in (("aar", "jni"), ("apk", "lib")):
            for fault in ("complete", "missing", "altered", "wrong-manifest"):
                path = Path(temp) / f"{fault}.{extension}"
                with zipfile.ZipFile(path, "w") as archive:
                    for abi in ABI:
                        for program in ("libqimeng.so", "libqimeng_runner.so", "libffmpeg_cli.so", "libffprobe_cli.so"):
                            archive.writestr(f"{binaries}/{abi}/{program}", b"synthetic")
                        recorded = {f"{DIR}/{name}": digest for name, digest in hashes.items()}
                        if fault == "wrong-manifest" and abi == "x86_64":
                            recorded[f"{DIR}/COPYING.IJG"] = "0" * 64
                        archive.writestr(f"assets/third-party/{abi}/manifest.json", json.dumps({"previewDecoderNotices": recorded}))
                    for name in hashes:
                        if name == "LICENSE.GOLANG" and fault == "missing":
                            continue
                        data = (ROOT / "deploy" / DIR / name).read_bytes()
                        if name == "COPYING.IJG" and fault == "altered":
                            data += b"changed"
                        archive.writestr(f"assets/{DIR}/{name}", data)
                check = subprocess.run([sys.executable, ROOT / "tools/check_android_apk.py", "embedded", path], capture_output=True)
                assert (check.returncode == 0) == (fault == "complete"), (extension, fault, check.stderr)
                print(extension, fault, check.returncode)


def module_info(file, required):
    output = subprocess.check_output(["go", "version", "-m", file], text=True)
    deps = {}
    for line in output.splitlines():
        fields = line.split()
        if len(fields) == 4 and fields[0] == "dep" and fields[1] in (JPEG, PNG):
            name, version, checksum = fields[1:]
            assert f"{name} {version} {checksum}" in (ROOT / "server/go.sum").read_text().splitlines()
            assert version == PINS[name][0]
            deps[name] = [version, checksum]
    if required:
        assert set(deps) == {JPEG, PNG}, output
    return deps


def paths(root, hashes, prefix):
    result = {}
    for name, wanted in hashes.items():
        member = f"{prefix}{DIR}/{name}"
        actual = sha((root / member).read_bytes())
        assert actual == wanted, member
        result[member] = actual
    return result


def one(root, pattern):
    found = list(root.rglob(pattern))
    assert len(found) == 1, (root, pattern, found)
    return found[0]


def open_archive(file, hashes, native):
    with file.open("rb") as stream:
        archive_hash = hashlib.file_digest(stream, "sha256").hexdigest()
    with zipfile.ZipFile(file) as archive:
        zroot = zipfile.Path(archive)
        if native:
            entries = paths(zroot, hashes, file.stem + "/")
            assert json.loads((zroot / file.stem / "SOURCE.json").read_bytes())["sourceCommit"] == built_commit()
            manifest = (zroot / file.stem / "SHA256SUMS").read_text().splitlines()
            assert all(f"{digest}  {name.removeprefix(file.stem + '/')}" in manifest for name, digest in entries.items())
            executable = "qimeng-next.exe" if "windows-" in file.name else "qimeng-next"
            binaries = [file.stem + "/" + executable]
        else:
            entries = paths(zroot, hashes, "assets/")
            native_root = "jni" if file.suffix == ".aar" else "lib"
            binaries = [f"{native_root}/{abi}/libqimeng.so" for abi in ABI]
            for abi in ABI:
                manifest = json.loads((zroot / f"assets/third-party/{abi}/manifest.json").read_bytes())
                assert (manifest["repositoryCommit"], manifest["abi"]) == (built_commit(), abi)
                assert manifest["previewDecoderNotices"] == {name.removeprefix("assets/"): digest for name, digest in entries.items()}
        modules = {}
        with tempfile.TemporaryDirectory() as temporary:
            binary = Path(temporary) / "inspect-go-program"
            for member in binaries:
                with archive.open(member) as src, binary.open("wb") as dst:
                    shutil.copyfileobj(src, dst)
                modules[member] = module_info(binary, True)
        return {"name": file.name, "archiveSha256": archive_hash, "noticeMembers": entries, "goBuildInfo": modules}


def built_commit():
    return os.environ.get("NOTICE_BUILD_COMMIT", os.environ["GITHUB_SHA"])


def delivered():
    sources = json.loads((OUT / "sources.json").read_text())
    assert sources["commit"] == built_commit()
    hashes = sources["noticeSha256"]
    root = ROOT / "build/delivered"
    artifacts = {}
    for target in ("windows-amd64", "linux-amd64", "linux-arm64"):
        name = "qimeng-next-package-" + target
        artifacts[name] = open_archive(one(root / name, "*.zip"), hashes, True)
    for extension in ("aar", "apk"):
        artifact = root / "android-embedded-check"
        artifacts[f"{artifact.name}/{extension}"] = open_archive(one(artifact, "*." + extension), hashes, False)
    for abi in ABI:
        name = "android-native-" + abi
        directory = one(root / name, "manifest.json").parent
        records = paths(directory, hashes, "")
        manifest = json.loads((directory / "manifest.json").read_text())
        assert (manifest["repositoryCommit"], manifest["abi"]) == (built_commit(), abi)
        assert manifest["previewDecoderNotices"] == records
        artifacts[name] = {"noticeMembers": records, "goBuildInfo": module_info(directory / "libqimeng.so", True)}
    for kind in ("qimeng-legacy-cold-", "qimeng-selected-image-merge-"):
        for target in ("windows-amd64", "linux-amd64"):
            name = kind + target
            product = root / name
            records = paths(product, hashes, "")
            manifest = set((product / "SHA256SUMS").read_text().splitlines())
            for entry, digest in records.items():
                # GNU sha256sum records text mode with a space and binary mode with an asterisk.
                assert {f"{digest}  {entry}", f"{digest} *{entry}"} & manifest, (name, entry)
            binary = one(product, "qimeng-backup.exe" if target.startswith("windows") else "qimeng-backup")
            artifacts[name] = {"noticeMembers": records, "restoreGoBuildInfo": module_info(binary, False)}
    linux = root / "linux-runtime"
    artifacts["linux-runtime"] = {"noticeMembers": paths(linux, hashes, ""), "goBuildInfo": module_info(linux / "qimeng-next", True)}
    return {"buildCommit": sources["commit"], "verificationCommit": os.environ["GITHUB_SHA"], "artifacts": artifacts}


if __name__ == "__main__":
    if os.environ.get("GITHUB_ACTIONS") != "true":
        raise SystemExit("Cloud proof only")
    mode = sys.argv[1]
    if mode == "sources":
        report = verify_sources()
        synthetic_omissions(report["noticeSha256"])
    elif mode == "archives":
        report = delivered()
    else:
        raise SystemExit("Expected sources or archives")
    OUT.mkdir(parents=True, exist_ok=True)
    body = json.dumps(report, indent=2, sort_keys=True)
    (OUT / (mode + ".json")).write_text(body + "\n", encoding="utf-8")
    print(body)
