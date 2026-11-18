"""Compare the published migration ledger with the files at the candidate commit."""
from __future__ import annotations

import os
from pathlib import Path
import re
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[1]
DIRECTORY = "server/internal/store/migrations"
MIGRATION = re.compile(r"([0-9]{4,})_[A-Za-z0-9_]+\.sql\Z")


def snapshot(revision: str) -> dict[str, str]:
    listing = subprocess.check_output(
        ["git", "ls-tree", "-r", "-z", revision, "--", DIRECTORY], cwd=ROOT
    )
    result = {}
    for item in filter(None, listing.split(b"\0")):
        entry, name = item.split(b"\t", 1)
        result[name.decode().removeprefix(DIRECTORY + "/")] = entry.decode().split()[2]
    return result


def changes(before: dict[str, str], after: dict[str, str]) -> list[str]:
    errors = [f"published migration changed or disappeared: {name}"
              for name, blob in before.items() if after.get(name) != blob]
    previous_max = max((int(m[1]) for name in before if (m := MIGRATION.fullmatch(name))), default=0)
    versions = {}
    for name in sorted(after):
        match = MIGRATION.fullmatch(name)
        if not match:
            errors.append(f"invalid migration name: {name}")
            continue
        version = int(match[1])
        if version in versions:
            errors.append(f"duplicate version {version}: {versions[version]} and {name}")
        versions[version] = name
        if name not in before and version <= previous_max:
            errors.append(f"{name}: choose a version after published {previous_max}")
    return errors


def main() -> int:
    if os.environ.get("CI") != "true":
        raise SystemExit("Migration checks run in cloud CI only.")
    before = sys.argv[1]
    baseline = snapshot(before) if before else {}
    errors = changes(baseline, snapshot("HEAD"))
    for error in errors:
        print(error)
    return bool(errors)


if __name__ == "__main__":
    raise SystemExit(main())
