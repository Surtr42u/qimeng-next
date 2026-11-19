"""Seed the isolated Android statistics service through its public API."""

from datetime import datetime, timedelta, timezone
import json
import os
from pathlib import Path
import struct
import time
from urllib.request import Request, urlopen
from uuid import uuid4
import zlib


def call(method, path, data=None):
    headers = {"Authorization": f"Bearer {os.environ['QIMENG_ADMIN_TOKEN']}"}
    body = None
    if data is not None:
        headers.update({"Content-Type": "application/json", "Idempotency-Key": str(uuid4())})
        body = json.dumps(data).encode()
    request = Request(f"http://127.0.0.1:18483/api/v1{path}", body, headers, method=method)
    with urlopen(request, timeout=8) as response:
        return json.load(response)


def png(section, number):
    def chunk(kind, data):
        return struct.pack(">I", len(data)) + kind + data + struct.pack(">I", zlib.crc32(kind + data))

    row = b"\0" + bytes((60 + number * 3, section, 200 - number)) * 8 + bytes((section, 160, 60)) * 8
    return b"\x89PNG\r\n\x1a\n" + b"".join((
        chunk(b"IHDR", struct.pack(">2I5B", 16, 16, 8, 2, 0, 0, 0)),
        chunk(b"IDAT", zlib.compress(row * 16)),
        chunk(b"IEND", b""),
    ))


def catalog(kind, size, hue, data_root):
    library = call("POST", "/libraries", {"name": f"合成统计 {kind}", "kind": kind})
    library_path = f"/libraries/{library['id']}"
    directory = Path(call("GET", f"{library_path}/location")["rootPath"]).resolve(strict=True)
    if data_root not in directory.parents:
        raise RuntimeError("Synthetic library escaped the isolated data root")
    for index in range(size):
        (directory / f"sample_{kind}_{index:02}.png").write_bytes(png(hue, index))
    job = call("POST", f"{library_path}/scans", {"expectedRevision": library["revision"]})
    for _ in range(90):
        state = call("GET", f"/scans/{job['id']}")["state"]
        if state == "completed":
            break
        if state not in ("queued", "running"):
            raise RuntimeError(f"Synthetic catalog scan ended as {state}")
        time.sleep(0.5)
    else:
        raise RuntimeError("Synthetic catalog scan did not complete")
    assets = call("GET", f"/assets?libraryId={library['id']}&limit=100")["items"]
    if len(assets) != size:
        raise RuntimeError(f"Expected {size} assets in the {kind} fixture, got {len(assets)}")
    return sorted(assets, key=lambda item: item["fileName"])


def record(asset, kind, stamp, seconds=0):
    event_id = str(uuid4())
    call("POST", f"/assets/{asset['id']}/events", {
        "kind": kind, "eventId": event_id, "sessionId": event_id, "contentVersion": asset["contentVersion"],
        "occurredAt": stamp.isoformat(), "seconds": seconds,
    })


def main():
    if os.environ.get("GITHUB_ACTIONS") != "true" or os.environ.get("RUNNER_ENVIRONMENT") != "github-hosted":
        raise SystemExit("Statistics fixtures require the isolated cloud service")
    data_root = (Path(os.environ["RUNNER_TEMP"]) / "qimeng-next-statistics").resolve(strict=True)
    regular = catalog("regular", 51, 120, data_root)
    cos = catalog("cos", 2, 70, data_root)
    sample = regular[0]
    sample_path = f"/assets/{sample['id']}"
    call("PATCH", sample_path, {
        "expectedRevision": sample["revision"], "expectedAuthorsRevision": sample["authorsRevision"],
        "newAuthorNames": ["合成作者"],
    })
    call("PUT", f"{sample_path}/tags", {"expectedRevision": sample["tagsRevision"], "tags": ["合成标签"]})
    now = datetime.now(timezone.utc)
    for asset in [*regular, cos[0]]:
        record(asset, "view", now)
    record(sample, "dwell", now, 8)
    for asset, days, seconds in [(sample, 14, 9), (cos[1], 60, 11)]:
        stamp = now - timedelta(days=days)
        record(asset, "view", stamp)
        record(asset, "dwell", stamp, seconds)
    print("Seeded 51 regular assets and 2 COS assets with activity in three time ranges")


if __name__ == "__main__":
    main()
