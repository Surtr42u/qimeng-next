"""云端文档与交付入口检查；只检本仓相对链接和必要入口，不访问外网。"""
from __future__ import annotations

import os
from pathlib import Path
import re
import subprocess
import unicodedata
from urllib.parse import unquote

ROOT = Path(__file__).resolve().parents[1]
REQUIRED = ("README.md", "AGENTS.md", "CONTRIBUTING.md", "docs/requirements.md", "docs/architecture.md",
            "docs/status.md", "docs/verification.md", "docs/migration.md", "docs/provenance.md")

LINK = re.compile(r"!?\[[^\]\n]*\]\(\s*(?:<([^>\n]+)>|([^\s)]+))(?:\s+[\"'][^\"']*[\"'])?\s*\)")


def prose(contents: str) -> str:
    """Exclude fenced examples so sample Markdown is never treated as navigation."""
    output = []
    fence = ""
    for line in contents.splitlines():
        marker = re.match(r"^ {0,3}(`{3,}|~{3,})(.*)$", line)
        if not fence and marker:
            fence = marker[1]
        elif fence:
            if marker and marker[1][0] == fence[0] and len(marker[1]) >= len(fence) and not marker[2].strip():
                fence = ""
        else:
            output.append(line)
    return "\n".join(output)


def anchors(contents: str) -> set[str]:
    result = set()
    for line in prose(contents).splitlines():
        result.update(re.findall(r'<a\s+(?:id|name)=["\']([^"\']+)["\']', line, re.I))
        heading = re.match(r"^ {0,3}#{1,6}\s+(.+?)(?:\s+#+)?\s*$", line)
        if not heading:
            continue
        title = LINK.sub(lambda match: match[0].split("]")[0].lstrip("!["), heading[1])
        slug = "".join(
            char for char in title.lower()
            if char in " -_" or unicodedata.category(char)[0] in "LNM"
        ).replace(" ", "-")
        key = slug
        suffix = 0
        while key in result:
            suffix += 1
            key = f"{slug}-{suffix}"
        result.add(key)
    return result


def link_errors(root: Path, documents: list[str]) -> list[str]:
    errors = []
    fragments: dict[Path, set[str]] = {}
    for name in documents:
        file = root / name
        for match in LINK.finditer(prose(file.read_text(encoding="utf-8"))):
            target = match[1] or match[2]
            if re.match(r"^(https?://|mailto:)", target, re.I):
                continue
            address, _, fragment = target.partition("#")
            address = unquote(address.split("?", 1)[0])
            if not address:
                resolved = file
            elif address.startswith("/"):
                resolved = root / address.lstrip("/")
            else:
                resolved = file.parent / address
            resolved = resolved.resolve()
            if not resolved.is_relative_to(root) or not resolved.exists():
                errors.append(f"{name}: missing/outside link {target}")
                continue
            if resolved.is_dir():
                resolved = resolved / "README.md"
            if fragment and resolved.suffix.lower() == ".md":
                if not resolved.is_file():
                    errors.append(f"{name}: missing Markdown target for {target}")
                    continue
                if resolved not in fragments:
                    fragments[resolved] = anchors(resolved.read_text(encoding="utf-8"))
                if unquote(fragment) not in fragments[resolved]:
                    errors.append(f"{name}: missing section {target}")
    return errors


def main() -> int:
    if os.environ.get("CI") != "true":
        raise SystemExit("Documentation checks run in cloud CI only.")
    errors = []
    for name in REQUIRED:
        if not (ROOT / name).is_file():
            errors.append(f"missing required document: {name}")
    files = list(filter(None, subprocess.check_output(["git", "ls-files", "-z", "*.md"], cwd=ROOT).decode().split("\0")))
    errors.extend(link_errors(ROOT, files))
    for error in errors:
        print(error)
    print(f"Documentation: {len(files)} files, {len(errors)} errors")
    return bool(errors)


if __name__ == "__main__":
    raise SystemExit(main())
