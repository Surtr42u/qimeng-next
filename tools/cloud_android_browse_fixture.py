"""Prepare an isolated album, favorites, and history for the Android simulator."""

import json
import os
import sqlite3
import time
from pathlib import Path
from urllib.request import Request, urlopen
from uuid import uuid4

from cloud_android_statistics_fixture import png


def request(method, path, payload=None):
    headers = {
        "Authorization": "Bearer " + os.environ["QIMENG_ADMIN_TOKEN"],
        "Content-Type": "application/json",
        "Idempotency-Key": str(uuid4()),
    }
    body = json.dumps(payload).encode() if payload is not None else None
    with urlopen(Request("http://127.0.0.1:18485/api/v1" + path, data=body, headers=headers, method=method), timeout=15) as response:
        return json.load(response)


def catalog(root, kind, names):
    library = request("POST", "/libraries", {"kind": kind, "name": f"合成相册 {kind}"})
    base = f"/libraries/{library['id']}"
    path = Path(request("GET", f"{base}/location")["rootPath"]).resolve(strict=True)
    if root not in path.parents:
        raise RuntimeError("Browse fixture library escaped its disposable data directory")
    if kind == "cos":
        path /= "Synthetic Directory Author/Synthetic Collection"
        path.mkdir(parents=True)
    for index, name in enumerate(names):
        file = path / name
        file.write_bytes(png(90 if kind == "regular" else 140, index))
        # Older than the events in both of the derived lists. Order the chosen batch cards at the top.
        modified = 1012723200 + (5 - min(index, 5)) * 86400
        os.utime(file, (modified, modified))
    scan = request("POST", f"{base}/scans", {"expectedRevision": library["revision"]})
    for _ in range(90):
        state = request("GET", f"/scans/{scan['id']}")["state"]
        if state == "completed":
            break
        if state not in ("queued", "running"):
            raise RuntimeError(f"Browse fixture scan ended as {state}")
        time.sleep(0.5)
    else:
        raise RuntimeError("Browse fixture scan did not complete")
    assets = request("GET", f"/assets?libraryId={library['id']}&limit=100")["items"]
    if len(assets) != len(names):
        raise RuntimeError("Browse fixture asset count does not match the catalog")
    return {asset["fileName"]: asset for asset in assets}


def view(asset, stamp):
    identifier = str(uuid4())
    request("POST", f"/assets/{asset['id']}/events", {
        "eventId": identifier, "sessionId": identifier, "kind": "view",
        "contentVersion": asset["contentVersion"], "occurredAt": stamp,
    })


def main():
    if os.environ.get("GITHUB_ACTIONS") != "true" or os.environ.get("RUNNER_ENVIRONMENT") != "github-hosted":
        raise SystemExit("Browse fixtures require the isolated cloud service")
    root = (Path(os.environ["RUNNER_TEMP"]) / "qimeng-next-browse").resolve(strict=True)
    names = ["album-first.png", "album-second.png"] + [f"old-photo-{index:02d}.png" for index in range(50)]
    regular = catalog(root, "regular", names)
    cos = catalog(root, "cos", ["album-cos.png"])["album-cos.png"]
    first = regular[names[0]]
    second = regular[names[1]]
    if not cos.get("cosDirectoryAuthorId"):
        raise RuntimeError("COS fixture lacks its directory author")
    request("POST", "/authors", {"name": "合成替换作者"})
    source = request("POST", "/sources", {"name": "合成原作", "aliases": []})
    for position, asset in enumerate([first, second]):
        author = request("POST", "/authors", {"name": f"合成旧作者 {position + 1}"})
        path = f"/assets/{asset['id']}"
        request("PATCH", path, {
            "authorIds": [author["id"]], "expectedRevision": asset["revision"],
            "expectedAuthorsRevision": asset["authorsRevision"], "sourceIds": [source["id"]],
            "acquisitionSources": [f"独立来源 {position + 1}"], "note": f"保留备注 {position + 1}",
        })
        request("PUT", path + "/tags", {"expectedRevision": asset["tagsRevision"], "tags": ["保留标签"]})
    today = request("PUT", f"/assets/{first['id']}/favorite", {"favorite": True})["favoritedAt"]
    historical = "2024-03-04T12:00:00.000000000Z"
    with sqlite3.connect(root / "qimeng.sqlite", timeout=15) as db:
        # These rows stand in for historical known dates and old imported state without an occurrence.
        for asset in list(regular.values()) + [cos]:
            if asset["id"] == first["id"]:
                continue
            timestamp = historical if asset["id"] == second["id"] else today if asset["id"] != cos["id"] else None
            db.execute("UPDATE asset_records SET favorite=1,favorited_at=?,favorite_updated_at=? WHERE id=?", (timestamp, today, asset["id"]))
    view(first, historical)
    view(first, today)
    view(second, historical)
    favorites = request("GET", "/assets?favorite=true&sort=favorited&dateCounts=true&dateCountsBy=favoritedAt&limit=50")
    if not (favorites["totalMatched"] == 53 and len(favorites["items"]) == 50 and favorites["nextCursor"] and
            len(favorites["dateCounts"]) == 2 and favorites["dateCounts"][0]["fileCount"] == 51 and favorites["unknownDateCount"] == 1):
        raise RuntimeError("Browse fixture favorite buckets do not match the expected two-page catalog")
    print("Seeded an isolated two-page favorite list, recent views, and per-media revisions")


if __name__ == "__main__":
    main()
