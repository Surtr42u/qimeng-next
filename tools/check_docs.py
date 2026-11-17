"""云端文档与交付入口检查；只检本仓相对链接和必要入口，不访问外网。"""
from __future__ import annotations

import os
from pathlib import Path
import re
import subprocess
from urllib.parse import unquote

ROOT = Path(__file__).resolve().parents[1]
REQUIRED = ("README.md", "AGENTS.md", "docs/requirements.md", "docs/architecture.md",
            "docs/status.md", "docs/verification.md", "docs/migration.md", "docs/provenance.md")


def main() -> int:
    if os.environ.get("CI") != "true":
        raise SystemExit("Documentation checks run in cloud CI only.")
    errors = []
    for name in REQUIRED:
        if not (ROOT / name).is_file():
            errors.append(f"missing required document: {name}")
    files = subprocess.check_output(["git", "ls-files", "-z", "*.md"], cwd=ROOT).decode().split("\0")
    for name in filter(None, files):
        file = ROOT / name
        contents = re.sub(r"```.*?```", "", file.read_text(encoding="utf-8"), flags=re.S)
        for target in re.findall(r"!?\[[^\]]*\]\(([^\s)]+)(?:\s+[^)]*)?\)", contents):
            if re.match(r"[a-z]+:", target, flags=re.I) or target.startswith("#"):
                continue
            target = unquote(target.split("#", 1)[0].split("?", 1)[0]).strip("<>")
            if not target:
                continue
            resolved = (ROOT / target.lstrip("/")) if target.startswith("/") else file.parent / target
            if not resolved.resolve().is_relative_to(ROOT.resolve()) or not resolved.exists():
                errors.append(f"{name}: missing/outside link {target}")
    for error in errors:
        print(error)
    print(f"Documentation: {len(list(filter(None, files)))} files, {len(errors)} errors")
    return bool(errors)


if __name__ == "__main__":
    raise SystemExit(main())
