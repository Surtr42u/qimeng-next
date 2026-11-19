"""Assemble a portable same-commit server, recovery utility and Web distribution in CI."""
from __future__ import annotations

import argparse
import gzip
import hashlib
from html.parser import HTMLParser
import json
import os
from pathlib import Path
import re
import shutil
import stat
import subprocess
import zipfile

ROOT = Path(__file__).resolve().parents[1]


def digest(path: Path) -> str:
    with path.open("rb") as source:
        return hashlib.file_digest(source, "sha256").hexdigest()


class WebEntry(HTMLParser):
    def __init__(self) -> None:
        super().__init__()
        self.styles_and_scripts: set[str] = set()

    def handle_starttag(self, tag, attrs):
        fields = dict(attrs)
        if tag == "script" and fields.get("type") == "module":
            url, extension = fields.get("src"), "js"
        elif tag == "link" and "stylesheet" in (fields.get("rel") or "").split():
            url, extension = fields.get("href"), "css"
        else:
            return
        if url and re.fullmatch(r"/assets/index-[A-Za-z0-9_-]{8}\." + extension, url):
            self.styles_and_scripts.add(url[1:])


def precompress_web_entry(web: Path) -> None:
    entry = WebEntry()
    entry.feed((web / "index.html").read_text("utf-8"))
    for relative in sorted(entry.styles_and_scripts):
        source = web / relative
        with source.open("rb") as original, (web / (relative + ".gz")).open("xb") as target:
            with gzip.GzipFile(filename="", mode="wb", fileobj=target, compresslevel=9, mtime=0) as encoded:
                shutil.copyfileobj(original, encoded)


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--target", choices=["windows-amd64", "linux-amd64", "linux-arm64"], required=True)
    parser.add_argument("--web", type=Path, required=True)
    parser.add_argument("--bin", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    sha = os.environ.get("GITHUB_SHA", "")
    if os.environ.get("GITHUB_ACTIONS") != "true" or not re.fullmatch(r"[0-9a-f]{40}", sha):
        raise SystemExit("Packages are assembled only in the same-commit cloud workflow.")
    head = subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=ROOT, text=True).strip()
    if head != sha:
        raise SystemExit("The checked-out source differs from the workflow commit.")
    windows = args.target.startswith("windows")
    suffix = ".exe" if windows else ""
    name = f"qimeng-next-{args.target}-{sha[:12]}"
    staging = args.output / name
    staging.mkdir(parents=True, exist_ok=False)
    for binary in ("qimeng-next", "qimeng-backup"):
        shutil.copyfile(args.bin / (binary + suffix), staging / (binary + suffix))
    if not (args.web / "index.html").is_file() or not list((args.web / "assets").glob("*.js")):
        raise SystemExit("Missing the Web application built by the web job.")
    shutil.copytree(args.web, staging / "web")
    precompress_web_entry(staging / "web")
    for launcher in (("start.cmd", "start.ps1") if windows else ("start.sh", "compose.yaml", "nas-entry.sh", "Dockerfile.media")):
        text = (ROOT / "deploy" / launcher).read_text(encoding="utf-8")
        # Windows PowerShell 5.1 needs the BOM to decode Chinese UI text.
        (staging / launcher).write_bytes((text.replace("\r\n", "\n").replace("\n", "\r\n") if windows else text.replace("\r\n", "\n")).encode("utf-8-sig" if launcher == "start.ps1" else "utf-8"))
    shutil.copyfile(ROOT / "docs/quickstart.md", staging / "快速开始.md")
    shutil.copyfile(ROOT / "api/sdk.lock", staging / "sdk.lock")
    metadata = {"sourceCommit": sha, "target": args.target, "server": "qimeng-next" + suffix,
                "restoreUtility": "qimeng-backup" + suffix, "webEntry": "web/index.html",
                "openapiSha256": digest(ROOT / "api/openapi.yaml"), "sdkLockSha256": digest(staging / "sdk.lock")}
    (staging / "SOURCE.json").write_text(json.dumps(metadata, indent=2) + "\n", encoding="utf-8")
    files = sorted(path for path in staging.rglob("*") if path.is_file())
    sums = "".join(f"{digest(path)}  {path.relative_to(staging).as_posix()}\n" for path in files)
    (staging / "SHA256SUMS").write_text(sums, encoding="utf-8", newline="\n")
    archive = args.output / (name + ".zip")
    executable = {"qimeng-next", "qimeng-backup", "start.sh", "nas-entry.sh"}
    with zipfile.ZipFile(archive, "x", zipfile.ZIP_DEFLATED, compresslevel=6) as output:
        for path in sorted([*files, staging / "SHA256SUMS"]):
            relative = path.relative_to(staging).as_posix()
            info = zipfile.ZipInfo(name + "/" + relative)
            info.create_system = 3
            mode = 0o755 if relative in executable else 0o644
            info.external_attr = (stat.S_IFREG | mode) << 16
            info.compress_type = zipfile.ZIP_DEFLATED
            with path.open("rb") as source, output.open(info, "w") as target:
                shutil.copyfileobj(source, target, 1024 * 1024)
    (args.output / (archive.name + ".sha256")).write_text(f"{digest(archive)}  {archive.name}\n", encoding="ascii", newline="\n")


if __name__ == "__main__":
    main()
