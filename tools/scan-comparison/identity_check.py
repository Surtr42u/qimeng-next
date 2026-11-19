"""Drive the two production scanners through changes where metadata and bytes disagree."""

import hashlib
import json
import os
from pathlib import Path
import sqlite3
import sys
import tempfile

from compare import EPOCH, png, require, server, wait_for


def identities(version, data, library):
    if version == "installed":
        database, columns = "scan.sqlite", "rel_path,asset_id"
    else:
        database, columns = "qimeng.sqlite", "relative_path,id"
    connection = sqlite3.connect((data / database).as_uri() + "?mode=ro", uri=True)
    try:
        return dict(connection.execute(f"SELECT {columns} FROM assets WHERE library_id=?", (library,)))
    finally:
        connection.close()


def scan(api, version, data, library, phase, expected_paths):
    route = "/api/v1/libraries/" + library["id"]
    if version == "installed":
        api.call("POST", route + "/scan")
    else:
        job = api.call("POST", route + "/scans", {"expectedRevision": library["revision"]}, key="identity-" + phase)

    def committed():
        if version == "installed":
            item, = api.call("GET", "/api/v1/libraries")
            require(item["scanState"] != "error", "installed scan failed")
            if item["scanState"] != "idle":
                return None
        else:
            item = api.call("GET", "/api/v1/scans/" + job["id"])
            if item["state"] in ("queued", "running"):
                return None
            require(item["state"] in ("completed", "partial"), "Next scan failed")
        rows = identities(version, data, library["id"])
        return (rows, item) if expected_paths <= rows.keys() else None

    return wait_for(committed, f"{version} did not publish the expected synthetic paths")


def exercise(binary, version):
    with tempfile.TemporaryDirectory() as workspace:
        root = Path(workspace)
        data, media = root / "data", root / "storage/media"
        data.mkdir()
        media.mkdir(parents=True)
        with server(binary, version, data, media) as (api, library, _, _):
            first, other, moved = (png(seed) for seed in (41, 42, 43))
            require(len(first) == len(other) and first != other, "fixture must distinguish bytes from size")
            original = media / "original.png"
            original.write_bytes(first)
            os.utime(original, (EPOCH, EPOCH))
            initial_move = media / "before-move.png"
            initial_move.write_bytes(moved)
            os.utime(initial_move, (EPOCH + 10, EPOCH + 10))
            before, _ = scan(api, version, data, library, "initial", {original.name, initial_move.name})
            if version == "next":
                # A separate, untimed server checks human metadata through its normal API.
                route = "/api/v1/assets/" + before[initial_move.name]
                asset = api.call("GET", route)
                tagged = api.call("PUT", route + "/tags", {"expectedRevision": asset["tagsRevision"],
                                  "tags": ["synthetic manual tag"]}, key="identity-human-tags")
                api.call("PUT", route + "/favorite", {"favorite": True}, key="identity-human-favorite")

            original.unlink()
            replacement = media / "other-content.png"
            replacement.write_bytes(other)
            os.utime(replacement, (EPOCH, EPOCH))
            destination = media / "after-move.png"
            initial_move.rename(destination)
            os.utime(destination, (EPOCH + 20, EPOCH + 20))
            after, outcome = scan(api, version, data, library, "changed", {replacement.name, destination.name})
            result = {
                "differentContentKeptSeparate": after[replacement.name] != before[original.name],
                "sameContentWithChangedMtimeKeptId": after[destination.name] == before[initial_move.name],
                "missingOriginalStillInCatalog": after.get(original.name) == before[original.name],
            }
            if version == "next":
                current = api.call("GET", route)
                result["movedHumanTagAndFavoritePreserved"] = (
                    current["favorite"] and current["tags"] == ["synthetic manual tag"]
                    and current["tagsRevision"] == tagged["tagsRevision"]
                    and current["contentVersion"] == asset["contentVersion"])
                require(all(result.values()), "Next lost the distinction between content and timestamps")
                require(outcome["missing"] == 1 and outcome["state"] == "partial", "expected one reported missing original")
            require(hashlib.sha256(destination.read_bytes()).digest() == hashlib.sha256(moved).digest(),
                    "moved fixture bytes changed")
            return result


if __name__ == "__main__":
    require(os.environ.get("GITHUB_ACTIONS") == "true", "use the isolated cloud workflow")
    installed, next_binary, evidence = (Path(value).resolve() for value in sys.argv[1:])
    report = {"installedCommit": os.environ["REFERENCE_SHA"], "nextCommit": os.environ["GITHUB_SHA"],
              "installed": exercise(installed, "installed"), "next": exercise(next_binary, "next")}
    (evidence / "identity-cases.json").write_text(json.dumps(report, indent=2) + "\n", encoding="utf-8")
    print(json.dumps(report))
