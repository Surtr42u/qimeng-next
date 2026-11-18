"""Time an independent device write during real upload hashing and video probing."""

from concurrent.futures import ThreadPoolExecutor
import hashlib
import json
import os
from pathlib import Path
import statistics
import subprocess
import sys
import tempfile
import time
import urllib.request

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "scan-comparison"))
from compare import png, proc_number, require, server


API = "/api/v1"
CHUNK = 8 << 20


class Client:
    def __init__(self, origin, token):
        self.origin, self.token = origin, token

    def call(self, method, path, body=None, key="", offset=None):
        headers = {"Authorization": "Bearer " + self.token}
        if key:
            headers["Idempotency-Key"] = key
        if offset is not None:
            headers.update({"Content-Type": "application/octet-stream", "Upload-Offset": str(offset)})
        elif body is not None:
            headers["Content-Type"] = "application/json"
            body = json.dumps(body).encode()
        req = urllib.request.Request(self.origin + API + path, body, headers, method=method)
        with urllib.request.urlopen(req, timeout=90) as response:
            return json.load(response)


def timed(client, method, path, body, key):
    start = time.monotonic()
    result = client.call(method, path, body, key)
    finish = time.monotonic()
    return result, start, finish


def receive(client, library, file, mime, digest):
    upload = client.call("POST", "/uploads", {"libraryId": library, "fileName": file.name,
                         "mode": "ordinary", "mimeType": mime, "size": file.stat().st_size,
                         "expectedHash": digest}, "create-" + file.name)
    path = "/uploads/" + upload["id"]
    offset = 0
    with file.open("rb") as f:
        while chunk := f.read(CHUNK):
            state = client.call("PUT", path + "/content", chunk, offset=offset)
            require(state["offset"] == offset + len(chunk), "chunk offset was not committed")
            if offset == 0:
                again = client.call("PUT", path + "/content", chunk, offset=0)
                require(again["offset"] == state["offset"], "replayed chunk changed offset")
            offset = state["offset"]
    return path


def time_trial(binary, version, index, video, video_hash, temp):
    with tempfile.TemporaryDirectory(dir=temp) as work:
        root = Path(work)
        media, data = root / "storage" / "media", root / "data"
        media.mkdir(parents=True)
        data.mkdir()
        with server(binary, "next", data, media) as (owner, library, pid, _):
            upload_session, edit_session = [owner.call("POST", API + "/sessions",
                {"token": owner.token, "label": label}) for label in ("Video uploader", "Independent editor")]
            require(upload_session["id"] != edit_session["id"], "expected two device sessions")
            uploader = Client(owner.base, upload_session["token"])
            editor = Client(owner.base, edit_session["token"])
            image = root / "separate.png"
            image.write_bytes(png(621))
            image_path = receive(uploader, library["id"], image, "image/png", hashlib.sha256(image.read_bytes()).hexdigest())
            image_asset = uploader.call("POST", image_path + "/complete", {}, "seal-separate-image")["asset"]
            video_path = receive(uploader, library["id"], video, "video/x-msvideo", video_hash) + "/complete"
            tag_path = "/assets/" + image_asset["id"] + "/tags"
            idle, idle_start, idle_end = timed(editor, "PUT", tag_path,
                {"expectedRevision": image_asset["tagsRevision"], "tags": ["idle"]}, "edit-while-idle")
            reaction_path = "/assets/" + image_asset["id"] + "/reaction"
            favorite, favorite_start, favorite_end = timed(editor, "PUT", reaction_path,
                {"favorite": True, "liked": False}, "favorite-while-idle")
            require(favorite["favorite"], "idle favorite was not saved")
            initial_read = proc_number(pid, "io", "rchar")
            key = "complete-large-video"
            with ThreadPoolExecutor(max_workers=3) as pool:
                first = pool.submit(timed, uploader, "POST", video_path, {}, key)

                def edit_when_target_exists():
                    deadline = time.monotonic() + 20
                    while not (media / video.name).is_file():
                        if first.done():
                            first.result()
                            raise RuntimeError("completion ended before the public target was observed")
                        require(time.monotonic() < deadline, "video was not installed within the observation window")
                        time.sleep(0.001)
                    return timed(editor, "PUT", reaction_path, {"favorite": False, "liked": False},
                                 "favorite-while-verifying-installed-file")

                late = pool.submit(edit_when_target_exists)
                # rchar includes page-cache reads. A real full-file digest is
                # already underway when the other device submits its mutation.
                deadline = time.monotonic() + 20
                while proc_number(pid, "io", "rchar") - initial_read < CHUNK:
                    if first.done():
                        first.result()
                        raise RuntimeError("large upload finished before the overlap observation")
                    require(time.monotonic() < deadline, "upload did not read its staged original")
                    time.sleep(0.001)
                replay = pool.submit(timed, uploader, "POST", video_path, {}, key)
                edited, edit_start, edit_end = timed(editor, "PUT", tag_path,
                    {"expectedRevision": idle["tagsRevision"], "tags": ["during-upload"]}, "edit-during-video")
                result, seal_start, seal_end = first.result()
                retried, _, retry_end = replay.result()
                unfavorite, late_start, late_end = late.result()
            require(seal_start < edit_start < seal_end, "second session missed the completion window")
            require(seal_start < late_start < seal_end and not unfavorite["favorite"],
                    "late edit missed the publication window")
            asset = result["asset"]
            require((asset["hash"], asset["size"], asset["width"], asset["height"], asset["durationMs"],
                     asset["probeState"]) == (video_hash, video.stat().st_size, 1280, 720, 4000, "ready"),
                    "completion did not publish the fully hashed and probed video")
            require(result == retried and result == uploader.call("POST", video_path, {}, key),
                    "same-key completions returned different receipts")
            rows = editor.call("GET", "/assets")["items"]
            require(len(rows) == 2 and len({a["id"] for a in rows}) == 2, "completion duplicated or lost an asset")
            saved = editor.call("GET", "/assets/" + image_asset["id"])
            require(saved["tags"] == ["during-upload"] and saved["tagsRevision"] == edited["tagsRevision"] and not saved["favorite"],
                    "independent device edit was lost")
            ms = lambda a, b: round((b - a) * 1000, 3)
            return {"version": version, "trial": index, "idle_tag_ms": ms(idle_start, idle_end),
                    "concurrent_tag_ms": ms(edit_start, edit_end), "complete_ms": ms(seal_start, seal_end),
                    "tag_sent_after_complete_started_ms": ms(seal_start, edit_start),
                    "tag_finished_before_complete": edit_end < seal_end, "replay_finish_ms": ms(seal_start, retry_end),
                    "idle_favorite_ms": ms(favorite_start, favorite_end), "late_favorite_ms": ms(late_start, late_end),
                    "favorite_finished_before_complete": late_end < seal_end,
                    "catalog_assets": len(rows), "probe_state": asset["probeState"]}


def main():
    binary, evidence, *before = [Path(s).resolve() for s in sys.argv[1:]]
    evidence.mkdir(parents=True, exist_ok=True)
    versions = [("next", binary)]
    if before:
        versions.insert(0, ("before", before[0]))
    with tempfile.TemporaryDirectory() as work:
        temp = Path(work)
        video = temp / "large-synthetic.avi"
        subprocess.run(["ffmpeg", "-v", "error", "-nostdin", "-f", "lavfi", "-i", "testsrc2=size=1280x720:rate=24",
                        "-t", "4", "-c:v", "rawvideo", "-threads", "1", "-pix_fmt", "yuv420p", str(video)], check=True)
        require(video.stat().st_size > 120 << 20, "large media fixture is undersized")
        with video.open("rb") as f:
            digest = hashlib.file_digest(f, "sha256").hexdigest()
        runs = []
        for i in range(3):
            for version, exe in (versions if i % 2 == 0 else reversed(versions)):
                row = time_trial(exe, version, i + 1, video, digest, temp)
                print(json.dumps(row), flush=True)
                runs.append(row)
        details = {"head": os.environ["GITHUB_SHA"], "before": os.environ.get("BEFORE_NEXT") or None,
                   "bytes": video.stat().st_size, "sha256": digest,
                   "ffprobe": subprocess.check_output(["ffprobe", "-version"], text=True).splitlines()[0], "runs": runs}
        (evidence / "results.json").write_text(json.dumps(details, indent=2) + "\n")
        lines = ["## 大视频上传完成与独立标签修改", "", f"合成 AVI {details['bytes']:,} 字节，1280×720、4 秒。"
                 "两台设备会话分别完成上传和修改另一项资产的标签。采样服务进程读数增长 8 MiB 后发出标签请求，同时重放同一完成键。"
                 "目标文件出现后再取消另一项资产的收藏。每次核对视频摘要、真实 ffprobe 尺寸时长、标签修订、收藏、入库数量及回执。", "",
                 "| 版本 | 空闲标签 ms | 哈希时标签 ms | 空闲收藏 ms | 目标出现后收藏 ms | 视频完成 ms | 标签/收藏先完成次数 |",
                 "|---|---:|---:|---:|---:|---:|---:|"]
        for version, _ in versions:
            group = [r for r in runs if r["version"] == version]
            vals = [statistics.median(r[k] for r in group) for k in
                    ("idle_tag_ms", "concurrent_tag_ms", "idle_favorite_ms", "late_favorite_ms", "complete_ms")]
            lines.append(f"| {version} | " + " | ".join(f"{v:.2f}" for v in vals)
                         + f" | {sum(r['tag_finished_before_complete'] for r in group)}/3，"
                         + f"{sum(r['favorite_finished_before_complete'] for r in group)}/3 |")
        report = "\n".join(lines) + "\n"
        (evidence / "report.md").write_text(report)
        if summary := os.environ.get("GITHUB_STEP_SUMMARY"):
            with open(summary, "a") as f:
                f.write(report)


if __name__ == "__main__":
    main()
