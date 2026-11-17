"""云端构建自包含工具源码bundle，生成来源清单；不读取运行数据。"""
from __future__ import annotations

import hashlib
import json
import os
from pathlib import Path
import shutil
import zipfile

ROOT = Path(__file__).resolve().parents[1]


def main():
    if os.environ.get("CI") != "true":
        raise SystemExit("Packaging is cloud-only.")
    destination = ROOT / "build/ai-bundle"
    if destination.exists():
        raise SystemExit("Refusing to overwrite an existing bundle.")
    source = ROOT / "ai"
    paths = [source / "README.md", source / "pyproject.toml", source / "requirements.lock"]
    paths.extend(sorted((source / "src").rglob("*.py")))
    manifest = {}
    for path in paths:
        relative = path.relative_to(source)
        output = destination / relative
        output.parent.mkdir(parents=True, exist_ok=True)
        shutil.copyfile(path, output)
        manifest[relative.as_posix()] = hashlib.sha256(path.read_bytes()).hexdigest()
    contract = ROOT / "api/generated/python/operations.json"
    packaged = destination / "src/qimeng_ai/operations.json"
    shutil.copyfile(contract, packaged)
    manifest[packaged.relative_to(destination).as_posix()] = hashlib.sha256(contract.read_bytes()).hexdigest()
    (destination / "manifest.json").write_text(json.dumps({"source": os.environ["GITHUB_SHA"], "files": manifest}, indent=2) + "\n", encoding="utf-8")
    with zipfile.ZipFile(ROOT / "build/qimeng-next-ai.zip", "w", zipfile.ZIP_DEFLATED) as archive:
        for path in sorted(destination.rglob("*")):
            if path.is_file():
                archive.write(path, path.relative_to(destination))


if __name__ == "__main__":
    main()
